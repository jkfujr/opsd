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
		d := &database{ID: m.ID, URL: m.Storage, Region: m.Region, Auth: auth, Client: dbClient()}
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
	nextSample := time.Time{}
	nextManage := time.Time{}
	for {
		ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
		c, e := n.Config(ctx)
		if e == nil {
			if !n.Now().Before(nextSample) {
				_ = n.sample(ctx)
				nextSample = n.Now().Add(time.Duration(c.IntervalMinutes) * time.Minute)
			}
			if c.Storage.Enabled && !n.Now().Before(nextManage) {
				e = n.manageDatabase(ctx)
				nextManage = n.Now().Add(30 * time.Second)
			}
			if e == nil {
				var d *database
				d, e = n.selectDatabase(ctx)
				if e == nil {
					e = n.upload(ctx, d)
				}
			}
		}
		n.storageMu.Lock()
		if e != nil {
			n.storageError = "数据尚未同步"
		} else {
			n.storageError = ""
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
func (n *Node) archivedMetrics(ctx context.Context, owner string, q HistoryQuery) ([]protocol.Metrics, error) {
	d, e := n.selectDatabase(ctx)
	if e != nil {
		return nil, e
	}
	items := []protocol.Metrics{}
	from := q.From.UTC().Truncate(24 * time.Hour)
	for day := from; !day.After(q.To) && day.Before(from.AddDate(0, 0, 8)); day = day.AddDate(0, 0, 1) {
		name := "opsd_metrics_" + day.Format("20060102")
		if !activeMetricDatabase(name, n.Now()) {
			continue
		}
		prefix := owner + "~metrics~"
		start, _ := json.Marshal(prefix)
		end, _ := json.Marshal(prefix + "\ufff0")
		cursor := ""
		for {
			values := url.Values{"include_docs": {"true"}, "limit": {"200"}, "startkey": {string(start)}, "endkey": {string(end)}}
			if cursor != "" {
				b, _ := json.Marshal(cursor)
				values.Set("startkey", string(b))
				values.Set("skip", "1")
			}
			var response struct {
				Rows []struct {
					ID  string `json:"id"`
					Doc Record `json:"doc"`
				} `json:"rows"`
			}
			status, err := d.request(ctx, "GET", "/"+name+"/_all_docs?"+values.Encode(), nil, &response)
			if status == 404 {
				break
			}
			if err != nil {
				return nil, err
			}
			for _, row := range response.Rows {
				r := row.Doc
				if e = n.Verify(ctx, r); e != nil {
					return nil, e
				}
				var m protocol.Metrics
				if e = json.Unmarshal(r.Data, &m); e != nil {
					return nil, e
				}
				if !m.CollectedAt.Before(q.From) && !m.CollectedAt.After(q.To) {
					items = append(items, m)
				}
			}
			if len(response.Rows) < 200 {
				break
			}
			cursor = response.Rows[len(response.Rows)-1].ID
		}
	}
	return items, nil
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
	if q.Cursor > 0 {
		upper = q.Cursor - 1
	}
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
			if q.Key!=""&&op.Key!=q.Key{continue}
 if q.Pending && terminalState(op.State) {
				continue
			}
			if q.State != "" && string(op.State) != q.State {
				continue
			}
			op.ResultPendingSync = true
			out.Items = append(out.Items, op)
			if len(out.Items) == q.Limit {
				out.Next = op.Sequence
				return out, nil
			}
		}
		if len(response.Rows) < 100 {
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
