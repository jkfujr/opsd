package cluster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type couchFixture struct {
	mu           sync.Mutex
	docs         map[string]Record
	firstPartial bool
	writes       map[string]int
}

func (f *couchFixture) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/_up" {
		w.Write([]byte(`{"status":"ok"}`))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/_bulk_docs") {
		var body struct {
			Docs []Record `json:"docs"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		results := []map[string]any{}
		for i, doc := range body.Docs {
			if f.firstPartial && i > 0 {
				results = append(results, map[string]any{"id": doc.ID, "error": "forbidden"})
				continue
			}
			if _, ok := f.docs[doc.ID]; ok {
				results = append(results, map[string]any{"id": doc.ID, "error": "conflict"})
				continue
			}
			f.docs[doc.ID] = doc
			f.writes[doc.ID]++
			results = append(results, map[string]any{"id": doc.ID, "ok": true})
		}
		f.firstPartial = false
		json.NewEncoder(w).Encode(results)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(parts) == 2 {
		if doc, ok := f.docs[parts[1]]; ok {
			json.NewEncoder(w).Encode(doc)
			return
		}
	}
	w.WriteHeader(404)
}
func TestBulkPartialFailureRetriesOnlyUnconfirmed(t *testing.T) {
	n, _ := openTestNode(t)
	f := &couchFixture{docs: map[string]Record{}, writes: map[string]int{}, firstPartial: true}
	s := httptest.NewServer(http.HandlerFunc(f.handler))
	defer s.Close()
	d := &database{ID: n.ID, URL: s.URL, Client: s.Client()}
	if e := n.upload(t.Context(), d); e == nil {
		t.Fatal("批量部分失败不能算作成功")
	}
	var pending int
	if e := n.DB.QueryRow("SELECT count(*) FROM records WHERE uploaded=0").Scan(&pending); e != nil || pending < 1 {
		t.Fatal(pending, e)
	}
	if e := n.upload(t.Context(), d); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, count := range f.writes {
		if count != 1 {
			t.Fatalf("成功记录重复写入：%s %d", id, count)
		}
	}
	if e := n.DB.QueryRow("SELECT count(*) FROM records WHERE uploaded=0").Scan(&pending); e != nil || pending != 0 {
		t.Fatal(pending, e)
	}
}
func TestImmutableConflictCannotOverwriteRecord(t *testing.T) {
	n, _ := openTestNode(t)
	f := &couchFixture{docs: map[string]Record{}, writes: map[string]int{}}
	s := httptest.NewServer(http.HandlerFunc(f.handler))
	defer s.Close()
	var body string
	if e := n.DB.QueryRow("SELECT body FROM records ORDER BY sequence LIMIT 1").Scan(&body); e != nil {
		t.Fatal(e)
	}
	var doc Record
	_ = json.Unmarshal([]byte(body), &doc)
	bad := doc
	bad.Data = json.RawMessage(`{"tampered":true}`)
	f.docs[doc.ID] = bad
	d := &database{ID: n.ID, URL: s.URL, Client: s.Client()}
	if e := n.upload(t.Context(), d); e == nil {
		t.Fatal("不匹配的同 ID 记录被认可")
	}
	var done bool
	if e := n.DB.QueryRow("SELECT uploaded FROM records WHERE id=?", doc.ID).Scan(&done); e != nil || done {
		t.Fatal(done, e)
	}
}
func TestMetricUTCBucketsAndSevenDayExpiry(t *testing.T) {
	at := time.Date(2026, 10, 30, 1, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	if !activeMetricDatabase("opsd_metrics_20261023", at) || activeMetricDatabase("opsd_metrics_20261022", at) || activeMetricDatabase("opsd_metrics_20261030", at) {
		t.Fatal("UTC 七天范围错误")
	}
	n, _ := openTestNode(t)
	n.Now = func() time.Time { return at }
	tx, e := n.DB.BeginTx(t.Context(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = n.appendTx(t.Context(), tx, "metrics", "sample", map[string]int{"value": 1}, at); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	var db string
	if e = n.DB.QueryRow("SELECT database_name FROM records WHERE kind='metrics'").Scan(&db); e != nil || db != "opsd_metrics_20261029" {
		t.Fatal(db, e)
	}
}
func TestDatabaseSwitchRejectsOlderObservedVersion(t *testing.T) {
	n, _ := openTestNode(t)
	var body string
	if e := n.DB.QueryRow("SELECT body FROM records WHERE kind='account' ORDER BY sequence DESC LIMIT 1").Scan(&body); e != nil {
		t.Fatal(e)
	}
	var doc Record
	_ = json.Unmarshal([]byte(body), &doc)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_up" {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"rows": []any{map[string]any{"doc": doc}}})
	}))
	defer server.Close()
	n.storage = &database{ID: n.ID, URL: server.URL, Client: server.Client()}
	key := doc.Owner + "~account~" + doc.Object
	if _, e := n.DB.Exec("INSERT INTO observed_versions(id,sequence) VALUES(?,?)", key, doc.Sequence+1); e != nil {
		t.Fatal(e)
	}
	if _, e := n.latest(t.Context(), n.ID, "account", n.ID); e == nil {
		t.Fatal("切换后接受了较旧版本")
	}
}
