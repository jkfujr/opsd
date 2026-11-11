package cluster

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

// ReadHistory 一次选库、按日批量分页，并复用请求内的来源公钥；调用方可直接归并时间桶。
func (n *Node) ReadHistory(ctx context.Context, targets []string, q HistoryQuery, visit func(string, protocol.Metrics)) []string {
	failed := map[string]bool{}
	remote := []string{}
	for _, id := range targets {
		if id == n.ID {
			items, err := n.localHistory(ctx, q)
			if err != nil {
				failed[id] = true
				continue
			}
			for _, m := range items {
				visit(id, m)
			}
		} else {
			remote = append(remote, id)
		}
	}
	if len(remote) == 0 {
		if failed[n.ID] {
			return []string{n.ID}
		}
		return []string{}
	}
	d, err := n.selectDatabase(ctx)
	dir, dirErr := n.Directory(ctx)
	keys := map[string][]byte{}
	revoked := map[string]bool{}
	for _, r := range dir.Revocations {
		revoked[r.ID] = true
	}
	for _, m := range dir.Members {
		if !revoked[m.ID] {
			if key, e := identity.Decode(m.Signing); e == nil {
				keys[m.ID] = key
			}
		}
	}
	if err != nil || dirErr != nil {
		for _, id := range remote {
			failed[id] = true
		}
		return historyFailures(failed)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	from := q.From.UTC().Truncate(24 * time.Hour)
	for day := from; !day.After(q.To) && day.Before(from.AddDate(0, 0, 8)); day = day.AddDate(0, 0, 1) {
		name := "opsd_metrics_" + day.Format("20060102")
		if !activeMetricDatabase(name, n.Now()) {
			continue
		}
		for start := 0; start < len(remote); start += 8 {
			owners := append([]string(nil), remote[start:min(start+8, len(remote))]...)
			wg.Add(1)
			go func(name string, owners []string) {
				defer wg.Done()
				select {
				case slots <- struct{}{}:
				case <-ctx.Done():
					mu.Lock()
					for _, id := range owners {
						failed[id] = true
					}
					mu.Unlock()
					return
				}
				defer func() { <-slots }()
				cursors := map[string]string{}
				for len(owners) > 0 {
					queries := make([]map[string]any, len(owners))
					for i, id := range owners {
						prefix := id + "~metrics~"
						query := map[string]any{"include_docs": true, "limit": 200, "startkey": prefix, "endkey": prefix + "\ufff0"}
						if cursor := cursors[id]; cursor != "" {
							query["startkey"] = cursor
							query["skip"] = 1
						}
						queries[i] = query
					}
					var response struct {
						Results []struct {
							Rows []struct {
								ID  string `json:"id"`
								Doc Record `json:"doc"`
							} `json:"rows"`
							Error string `json:"error"`
						} `json:"results"`
					}
					status, e := d.request(ctx, "POST", "/"+name+"/_all_docs/queries", map[string]any{"queries": queries}, &response)
					if status == 404 {
						return
					}
					if e != nil || len(response.Results) != len(owners) {
						mu.Lock()
						for _, id := range owners {
							failed[id] = true
						}
						mu.Unlock()
						return
					}
					next := []string{}
					for i, result := range response.Results {
						id := owners[i]
						valid := result.Error == "" && len(keys[id]) == ed25519.PublicKeySize
						items := []protocol.Metrics{}
						for _, row := range result.Rows {
							r := row.Doc
							if !valid || r.Owner != id || r.Kind != "metrics" || r.Cluster != n.cluster || r.Sequence == 0 || row.ID != r.ID || r.ID != fmt.Sprintf("%s~%s~%s~%020d", r.Owner, r.Kind, r.Object, r.Sequence) || !ed25519.Verify(keys[id], recordBytes(r), r.Signature) {
								valid = false
								break
							}
							var m protocol.Metrics
							if json.Unmarshal(r.Data, &m) != nil {
								valid = false
								break
							}
							if !m.CollectedAt.Before(q.From) && !m.CollectedAt.After(q.To) {
								items = append(items, m)
							}
						}
						if !n.Active(ctx, id) {
							valid = false
						}
						mu.Lock()
						if !valid {
							failed[id] = true
						} else {
							for _, m := range items {
								visit(id, m)
							}
						}
						mu.Unlock()
						if valid && len(result.Rows) == 200 {
							cursor := result.Rows[len(result.Rows)-1].ID
							if cursor <= cursors[id] {
								mu.Lock()
								failed[id] = true
								mu.Unlock()
							} else {
								cursors[id] = cursor
								next = append(next, id)
							}
						}
					}
					owners = next
				}
			}(name, owners)
		}
	}
	wg.Wait()
	return historyFailures(failed)
}

func historyFailures(failed map[string]bool) []string {
	out := []string{}
	for id := range failed {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
