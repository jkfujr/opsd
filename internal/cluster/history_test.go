package cluster

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestHistoryBatchPaginationAndSignatureIsolation(t *testing.T) {
	n, _ := openTestNode(t)
	b, _ := openTestNode(t)
	c, _ := openTestNode(t)
	joinTest(t, b, n)
	joinTest(t, c, n)
	from := time.Now().UTC().Truncate(24 * time.Hour)
	docs := []Record{}
	for i, owner := range []*Node{b, c} {
		tx, err := owner.DB.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for k := range []int{300, 201}[i] {
			at := from.Add(time.Duration(k) * time.Minute)
			if err = owner.appendTx(t.Context(), tx, "metrics", fmt.Sprint(at.Unix()), protocol.Metrics{CollectedAt: at, CPUCores: 1, Errors: []string{}}, at); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		rows, err := owner.DB.Query("SELECT body FROM records WHERE kind='metrics'")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var body string
			var r Record
			if err = rows.Scan(&body); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal([]byte(body), &r); err != nil {
				t.Fatal(err)
			}
			docs = append(docs, r)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	var calls atomic.Int32
	var tamper atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_up" {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/_all_docs/queries") {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		var input struct {
			Queries []struct {
				Start string `json:"startkey"`
				End   string `json:"endkey"`
				Limit int    `json:"limit"`
				Skip  int    `json:"skip"`
			} `json:"queries"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || len(input.Queries) == 0 || len(input.Queries) > 2 {
			w.WriteHeader(400)
			return
		}
		results := []any{}
		for _, q := range input.Queries {
			rows := []any{}
			skip := q.Skip
			for _, doc := range docs {
				if doc.ID < q.Start || doc.ID > q.End {
					continue
				}
				if skip > 0 {
					skip--
					continue
				}
				if tamper.Load() && doc.Owner == b.ID {
					doc.Data = []byte(`{"tampered":true}`)
				}
				rows = append(rows, map[string]any{"id": doc.ID, "doc": doc})
				if len(rows) == q.Limit {
					break
				}
			}
			results = append(results, map[string]any{"rows": rows})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer server.Close()
	n.storage = &database{ID: n.ID, URL: server.URL, Client: server.Client()}
	q := HistoryQuery{From: from, To: from.Add(6 * time.Hour)}
	counts := map[string]int{}
	failed := n.ReadHistory(t.Context(), []string{b.ID, c.ID}, q, func(id string, _ protocol.Metrics) { counts[id]++ })
	if len(failed) != 0 || counts[b.ID] != 300 || counts[c.ID] != 201 || calls.Load() != 2 {
		t.Fatal("批量或分页丢失", failed, counts, calls.Load())
	}
	tamper.Store(true)
	counts = map[string]int{}
	failed = n.ReadHistory(t.Context(), []string{b.ID, c.ID}, q, func(id string, _ protocol.Metrics) { counts[id]++ })
	if len(failed) != 1 || failed[0] != b.ID || counts[b.ID] != 0 || counts[c.ID] != 201 {
		t.Fatal("篡改来源未隔离", failed, counts)
	}
}
