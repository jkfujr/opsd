package cluster

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestDatabaseMaintenanceOnlyWritesDifferences(t *testing.T) {
	n, _ := openTestNode(t)
	var mu sync.Mutex
	documents := map[string]json.RawMessage{}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "PUT" {
			writes++
			var value json.RawMessage
			if strings.HasSuffix(r.URL.Path, "/opsd_state") {
				value = json.RawMessage(`{}`)
			} else if json.NewDecoder(r.Body).Decode(&value) != nil {
				http.Error(w, "invalid", 400)
				return
			}
			documents[r.URL.Path] = value
			w.Write([]byte(`{"ok":true}`))
			return
		}
		if value, ok := documents[r.URL.Path]; ok {
			w.Write(value)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/_security") {
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	d := &database{URL: server.URL, Client: server.Client()}
	if err := n.ensureDatabase(t.Context(), d, "opsd_state"); err != nil {
		t.Fatal(err)
	}
	if writes != 2 {
		t.Fatal("首次维护缺少建库或权限配置", writes)
	}
	if err := n.ensureDatabase(t.Context(), d, "opsd_state"); err != nil {
		t.Fatal(err)
	}
	if writes != 2 {
		t.Fatal("无差异仍重复写入", writes)
	}
}

func TestCouchUnavailableKeepsLocalQueueAndResumesUpload(t *testing.T) {
	n, _ := openTestNode(t)
	var available atomic.Bool
	records := &couchFixture{docs: map[string]Record{}, writes: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() {
			http.Error(w, "unavailable", 503)
			return
		}
		records.handler(w, r)
	}))
	defer server.Close()
	d := &database{URL: server.URL, Client: server.Client()}
	if err := n.upload(t.Context(), d); err == nil {
		t.Fatal("数据库故障被当成上传成功")
	}
	op, err := n.Submit(t.Context(), n.ID, Request{Key: "local-without-couch", Kind: "name", Payload: payload("断库期间的本机修改")})
	if err != nil {
		t.Fatal(err)
	}
	n.advance()
	completed, err := n.GetOperation(t.Context(), op.ID)
	if err != nil || completed.State != protocol.Succeeded {
		t.Fatal("数据库故障阻塞本机队列", completed, err)
	}
	available.Store(true)
	if err = n.upload(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err = n.DB.QueryRow("SELECT count(*) FROM records WHERE uploaded=0").Scan(&pending); err != nil || pending != 0 {
		t.Fatal("恢复后积压未补齐", pending, err)
	}
	for id, count := range records.writes {
		if count != 1 {
			t.Fatal("成功记录被重复写入", id, count)
		}
	}
}

type changingTrustExecutor struct {
	lostExecutor
	checks int
}

func (e *changingTrustExecutor) Call(_ context.Context, payload protocol.Payload) (protocol.Payload, error) {
	e.checks++
	at := time.Now().UTC().Add(time.Duration(e.checks) * time.Second)
	target := payload.(*protocol.PeerTrustTarget)
	return &protocol.PeerTrustUpdated{TargetRevision: target.Revision, ReceivedRevision: target.Revision, AppliedRevision: target.Revision, Results: []protocol.ScopeResult{}, CheckedAt: &at}, nil
}

func TestUnchangedTrustMaintenanceDoesNotInvalidateFrontend(t *testing.T) {
	n, _ := openTestNode(t)
	manualAddress(t, n, "8.8.8.8")
	if _, err := n.ChangeTrust(t.Context(), TrustPolicyPatch{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	x := &changingTrustExecutor{}
	n.Executor = x
	n.syncTrust(t.Context())
	before := n.eventSequence.Load()
	n.syncTrust(t.Context())
	if x.checks != 2 || n.eventSequence.Load() != before {
		t.Fatal("相同互信状态重复发送变更通知", x.checks, n.eventSequence.Load(), before)
	}
}

func TestCouchHTTPClientReusesConnectionsAfterError(t *testing.T) {
	n, _ := openTestNode(t)
	var opened atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "missing", 404) }))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	first, second := n.databaseClient(), n.databaseClient()
	if first != second {
		t.Fatal("每次读取都新建连接池")
	}
	d := &database{URL: server.URL, Client: first}
	for range 3 {
		if status, err := d.request(t.Context(), "GET", "/missing", nil, nil); status != 404 || err == nil {
			t.Fatal(status, err)
		}
	}
	if opened.Load() != 1 {
		t.Fatal("错误响应导致连接无法复用", opened.Load())
	}
}
