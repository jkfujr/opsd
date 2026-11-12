package cluster

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type database struct {
	ID, URL, Region string
	Auth            Credentials
	Client          *http.Client
	Latency         time.Duration
}

func storageURL(c StorageConfig) string {
	return "http://" + net.JoinHostPort(c.Address, fmt.Sprint(c.Port))
}
func validateStorage(c StorageConfig) error {
	if !c.Enabled {
		return nil
	}
	ip := net.ParseIP(c.Address)
	if ip == nil || ip.IsUnspecified() || c.Port < 1 || c.Port > 65535 {
		return fault("invalid_request", "数据库须绑定明确的组网 IP 和有效端口")
	}
	return nil
}
func dbClient() *http.Client {
	return &http.Client{Transport: &http.Transport{MaxIdleConns: 4, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 4, IdleConnTimeout: time.Minute, ResponseHeaderTimeout: 5 * time.Second}, Timeout: 15 * time.Second}
}

func (n *Node) databaseClient() *http.Client {
	n.storageMu.Lock()
	defer n.storageMu.Unlock()
	if n.databaseHTTP == nil {
		n.databaseHTTP = dbClient()
	}
	return n.databaseHTTP
}
func (d *database) request(ctx context.Context, method, path string, body, out any) (int, error) {
	var r io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return 0, e
		}
		r = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(d.URL, "/")+path, r)
	if e != nil {
		return 0, e
	}
	req.SetBasicAuth(d.Auth.Username, d.Auth.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, e := d.Client.Do(req)
	if e != nil {
		return 0, e
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		return response.StatusCode, fmt.Errorf("数据库请求失败（%d）", response.StatusCode)
	}
	if out != nil {
		e = json.NewDecoder(io.LimitReader(response.Body, 8*1024*1024)).Decode(out)
	} else {
		_, e = io.Copy(io.Discard, io.LimitReader(response.Body, 1024*1024))
	}
	return response.StatusCode, e
}
func (n *Node) savedCredentials(ctx context.Context, id string) (Credentials, error) {
	var b string
	var secret Secret
	var c Credentials
	e := n.DB.QueryRowContext(ctx, "SELECT secret FROM storage_credentials WHERE id=?", id).Scan(&b)
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal([]byte(b), &secret); e != nil {
		return c, e
	}
	plain, e := unseal(secret, n.age)
	if e == nil {
		e = json.Unmarshal(plain, &c)
	}
	return c, e
}
func (n *Node) saveCredentials(ctx context.Context, id string, c Credentials) error {
	plain, _ := json.Marshal(c)
	s, e := seal(plain, []string{n.age.Recipient().String()})
	if e != nil {
		return e
	}
	b, _ := json.Marshal(s)
	_, e = n.DB.ExecContext(ctx, "INSERT INTO storage_credentials(id,secret) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET secret=excluded.secret", id, string(b))
	return e
}
func (n *Node) selectDatabase(ctx context.Context) (*database, error) {
	c, e := n.Config(ctx)
	if e != nil {
		return nil, e
	}
	n.storageMu.Lock()
	current := n.storage
	n.storageMu.Unlock()
	if current != nil && n.Active(ctx, current.ID) && (c.PreferredStorage == "" || c.PreferredStorage == current.ID) {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		_, e = current.request(probe, "GET", "/_up", nil, nil)
		cancel()
		if e == nil {
			return current, nil
		}
	}
	dir, e := n.Directory(ctx)
	if e != nil {
		return nil, e
	}
	candidates := []*database{}
	for _, m := range dir.Members {
		if m.Storage == "" || !n.Active(ctx, m.ID) {
			continue
		}
		auth, err := n.savedCredentials(ctx, "access:"+m.ID)
		if err != nil {
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = n.Call(readCtx, m.ID, "storage.credentials", nil, &auth)
			cancel()
			if err != nil {
				continue
			}
			if err = n.saveCredentials(ctx, "access:"+m.ID, auth); err != nil {
				continue
			}
		}
		d := &database{ID: m.ID, URL: m.Storage, Region: m.Region, Auth: auth, Client: n.databaseClient()}
		start := time.Now()
		probe, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		_, err = d.request(probe, "GET", "/_up", nil, nil)
		cancel()
		if err != nil {
			continue
		}
		d.Latency = time.Since(start)
		candidates = append(candidates, d)
	}
	if len(candidates) == 0 {
		return nil, fault("dependency_unavailable", "数据库不可用")
	}
	rank := func(d *database) int {
		if c.PreferredStorage != "" && d.ID == c.PreferredStorage {
			return 0
		}
		if d.Region == c.Region {
			return 1
		}
		return 2
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := rank(candidates[i]), rank(candidates[j])
		if a != b {
			return a < b
		}
		return candidates[i].Latency < candidates[j].Latency
	})
	n.storageMu.Lock()
	n.storage = candidates[0]
	n.storageMu.Unlock()
	return candidates[0], nil
}
func (n *Node) storageLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
		d, e := n.selectDatabase(ctx)
		if e == nil {
			e = n.upload(ctx, d)
		}
		if e == nil {
			e = n.compactFirewallRecords(ctx)
		}
		n.storageMu.Lock()
		n.storageError = ""
		if e != nil {
			n.storageError = "数据尚未同步"
		}
		n.storageMu.Unlock()
		cancel()
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *Node) sampleLoop() {
	for {
		ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		c, e := n.Config(ctx)
		interval := 5 * time.Minute
		if e == nil {
			interval = time.Duration(c.IntervalMinutes) * time.Minute
			_ = n.sample(ctx)
		}
		cancel()
		timer := time.NewTimer(interval)
		select {
		case <-n.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (n *Node) maintenanceLoop() {
	var last string
	var next time.Time
	delay := 5 * time.Second
	for {
		ctx, cancel := context.WithTimeout(n.ctx, 25*time.Second)
		c, e := n.Config(ctx)
		if e == nil && c.Storage.Enabled {
			var key string
			key, e = n.maintenanceKey(ctx, c)
			if e == nil && (key != last || !n.Now().Before(next)) {
				if key != last {
					delay = 5 * time.Second
				}
				last = key
				e = n.manageDatabase(ctx)
				if e == nil {
					next, delay = n.Now().Add(5*time.Minute), 5*time.Second
				} else {
					next = n.Now().Add(delay)
					delay = min(delay*2, 5*time.Minute)
				}
			} else if e == nil {
				// 保留上次维护错误，等待本轮重试期限。
				cancel()
				if !n.waitMaintenance(next) {
					return
				}
				continue
			}
		} else if e == nil {
			last, next = "", time.Time{}
		}
		n.storageMu.Lock()
		n.maintenanceError = ""
		if e != nil {
			n.maintenanceError = "数据库维护未完成"
		}
		n.storageMu.Unlock()
		cancel()
		if !n.waitMaintenance(next) {
			return
		}
	}
}

func (n *Node) waitMaintenance(next time.Time) bool {
	wait := 30 * time.Second
	if remaining := next.Sub(n.Now()); remaining > 0 {
		wait = min(wait, remaining)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-n.ctx.Done():
		return false
	case <-n.maintenanceWake:
		return true
	case <-timer.C:
		return true
	}
}

func (n *Node) maintenanceKey(ctx context.Context, c Config) (string, error) {
	d, err := n.Directory(ctx)
	if err != nil {
		return "", err
	}
	type member struct{ ID, Storage string }
	members := []member{}
	for _, m := range d.Members {
		members = append(members, member{m.ID, m.Storage})
	}
	b, err := json.Marshal(struct {
		Storage     StorageConfig
		Members     []member
		Revocations []Revocation
		Day         string
	}{c.Storage, members, d.Revocations, n.Now().UTC().Format("20060102")})
	return string(b), err
}
func (n *Node) upload(ctx context.Context, d *database) error {
	rows, e := n.DB.QueryContext(ctx, "SELECT body,database_name FROM records WHERE uploaded=0 ORDER BY sequence LIMIT 100")
	if e != nil {
		return e
	}
	groups := map[string][]Record{}
	for rows.Next() {
		var body, db string
		var r Record
		if e = rows.Scan(&body, &db); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(body), &r); e != nil {
			break
		}
		groups[db] = append(groups[db], r)
	}
	err := rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if err != nil {
		return err
	}
	for name, docs := range groups {
		if strings.HasPrefix(name, "opsd_metrics_") && !activeMetricDatabase(name, n.Now()) {
			continue
		}
		var results []struct {
			ID    string `json:"id"`
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if _, e = d.request(ctx, "POST", "/"+name+"/_bulk_docs", map[string]any{"docs": docs}, &results); e != nil {
			return e
		}
		byID := map[string]Record{}
		for _, r := range docs {
			byID[r.ID] = r
		}
		success := []string{}
		for _, r := range results {
			original, ok := byID[r.ID]
			if !ok {
				continue
			}
			if r.Error == "conflict" {
				var existing Record
				if _, err := d.request(ctx, "GET", "/"+name+"/"+url.PathEscape(r.ID), nil, &existing); err == nil && bytes.Equal(recordBytes(existing), recordBytes(original)) && bytes.Equal(existing.Signature, original.Signature) {
					r.OK = true
				}
			}
			if r.OK && r.Error == "" || r.OK && r.Error == "conflict" {
				success = append(success, r.ID)
			}
		}
		tx, e := n.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		for _, id := range success {
			if _, e = tx.ExecContext(ctx, "UPDATE records SET uploaded=1 WHERE id=?", id); e != nil {
				break
			}
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
		if e != nil {
			return e
		}
		if len(success) != len(docs) {
			return fault("dependency_unavailable", "部分数据尚未同步")
		}
	}
	return nil
}
func activeMetricDatabase(name string, now time.Time) bool {
	if !strings.HasPrefix(name, "opsd_metrics_") {
		return false
	}
	at, e := time.Parse("20060102", strings.TrimPrefix(name, "opsd_metrics_"))
	today := now.UTC().Truncate(24 * time.Hour)
	return e == nil && !at.Before(today.AddDate(0, 0, -6)) && !at.After(today)
}
func (n *Node) latest(ctx context.Context, owner, kind, object string) (Record, error) {
	d, e := n.selectDatabase(ctx)
	if e != nil {
		return Record{}, e
	}
	name := "opsd_state"
	if kind == "operation" {
		name = "opsd_operations"
	}
	prefix := owner + "~" + kind + "~" + object + "~"
	start, _ := json.Marshal(prefix + "\ufff0")
	end, _ := json.Marshal(prefix)
	q := url.Values{"include_docs": {"true"}, "descending": {"true"}, "limit": {"1"}, "startkey": {string(start)}, "endkey": {string(end)}}
	var response struct {
		Rows []struct {
			Doc Record `json:"doc"`
		} `json:"rows"`
	}
	if _, e = d.request(ctx, "GET", "/"+name+"/_all_docs?"+q.Encode(), nil, &response); e != nil {
		return Record{}, e
	}
	if len(response.Rows) == 0 {
		return Record{}, fault("dependency_unavailable", "数据尚未同步")
	}
	r := response.Rows[0].Doc
	if e = n.Verify(ctx, r); e != nil {
		return Record{}, e
	}
	key := owner + "~" + kind + "~" + object
	var seen uint64
	e = n.DB.QueryRowContext(ctx, "SELECT sequence FROM observed_versions WHERE id=?", key).Scan(&seen)
	if e != nil && !sqlMissing(e) {
		return Record{}, e
	}
	if seen > r.Sequence {
		return Record{}, fault("dependency_unavailable", "数据尚未同步")
	}
	_, e = n.DB.ExecContext(ctx, "INSERT INTO observed_versions(id,sequence) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET sequence=MAX(sequence,excluded.sequence)", key, r.Sequence)
	return r, e
}
func (n *Node) archivedOperations(ctx context.Context, owner string, q ListQuery) (OperationList, error) {
	d, e := n.selectDatabase(ctx)
	out := OperationList{Items: []Operation{}}
	if e != nil {
		return out, e
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 50
	}
	upper := uint64(9007199254740991)
	for {
		start, _ := json.Marshal([]any{owner, upper})
		end, _ := json.Marshal([]any{owner})
		values := url.Values{"group": {"true"}, "descending": {"true"}, "startkey": {string(start)}, "endkey": {string(end)}, "limit": {"100"}}
		var response struct {
			Rows []struct {
				Value Record `json:"value"`
			} `json:"rows"`
		}
		if _, e = d.request(ctx, "GET", "/opsd_operations/_design/opsd/_view/latest?"+values.Encode(), nil, &response); e != nil {
			return out, e
		}
		for _, row := range response.Rows {
			r := row.Value
			if e = n.Verify(ctx, r); e != nil {
				return out, e
			}
			var op Operation
			if e = json.Unmarshal(r.Data, &op); e != nil {
				return out, e
			}
			if r.Owner != owner || op.NodeID != owner || op.ID != r.Object {
				return out, fault("conflict", "操作来源不匹配")
			}
			if e = n.observe(ctx, "operation:"+op.ID, op.Revision); e != nil {
				return out, e
			}
			if q.Key != "" && (op.Key != q.Key || (q.Actor != "" && op.Actor != q.Actor)) {
				continue
			}
			if q.ResourceID != "" && op.ResourceID != q.ResourceID {
				continue
			}
			if q.Before != nil && !q.Before.includes(op) {
				continue
			}
			if q.Pending && terminalState(op.State) {
				continue
			}
			if q.State != "" && string(op.State) != q.State {
				continue
			}
			op.ResultPendingSync = true
			out.Items = append(out.Items, op)
			sort.Slice(out.Items, func(i, j int) bool { return operationBefore(out.Items[i], out.Items[j]) })
			if len(out.Items) > q.Limit+1 {
				out.Items = out.Items[:q.Limit+1]
			}
		}
		if len(response.Rows) < 100 {
			if len(out.Items) > q.Limit {
				out.Items = out.Items[:q.Limit]
				last := out.Items[len(out.Items)-1]
				out.Next = &OperationPosition{CreatedAt: last.CreatedAt, ID: last.ID}
			}
			return out, nil
		}
		last := response.Rows[len(response.Rows)-1].Value.QueueSequence
		if last == 0 || last >= upper {
			return out, fault("conflict", "历史分页无效")
		}
		upper = last - 1
	}
}
func (n *Node) observe(ctx context.Context, key string, version uint64) error {
	var seen uint64
	e := n.DB.QueryRowContext(ctx, "SELECT sequence FROM observed_versions WHERE id=?", key).Scan(&seen)
	if e != nil && !sqlMissing(e) {
		return e
	}
	if seen > version {
		return fault("dependency_unavailable", "数据尚未同步")
	}
	_, e = n.DB.ExecContext(ctx, "INSERT INTO observed_versions(id,sequence) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET sequence=MAX(sequence,excluded.sequence)", key, version)
	return e
}
func terminalState(s protocol.OperationState) bool {
	switch s {
	case protocol.Succeeded, protocol.Rejected, protocol.Failed, protocol.Reverted, protocol.ResolvedManually:
		return true
	}
	return false
}
func basicAuth(c Credentials) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Password))
}
