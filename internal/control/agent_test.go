package control

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opsd-labs/opsd/internal/agent"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/storage"
)

func TestActualAgentHeartbeatReconnectAndRevocation(t *testing.T) {
	f := setup(t)
	session := f.login(f.client, "/secret-test", "admin", "test-password")
	token := f.token(session.CsrfToken)
	var active atomic.Value
	active.Store(f.service.Handler())
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { active.Load().(http.Handler).ServeHTTP(w, r) }))
	defer bridge.Close()
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "agent.db"), storage.Agent)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := agent.Initialize(t.Context(), db, bridge.URL, identity.Encode(f.service.key.Public), token.Token); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ready := make(chan struct{})
	result := make(chan error, 1)
	go func() { result <- agent.Run(ctx, db, func() { close(ready) }) }()
	select {
	case <-ready:
	case err := <-result:
		t.Fatalf("Agent 未就绪：%v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Agent 登记超时")
	}
	state, err := agent.Load(t.Context(), db)
	if err != nil || state.NodeID == "" || state.EnrollmentToken != "" {
		t.Fatalf("Agent 未持久保存身份或清除接入码：%+v %v", state, err)
	}
	f.clock.Add(int64(time.Minute))
	// 使用真实的 15 秒心跳周期，覆盖运行入口而非直接调用登记处理器。
	waitUntil := func(timeout time.Duration, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatal("未在期限内观察到连接状态")
			}
			select {
			case err := <-result:
				t.Fatalf("Agent 提前退出：%v", err)
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	waitUntil(20*time.Second, func() bool {
		var last int64
		if err := f.db.QueryRow("SELECT last_seen_at FROM nodes WHERE id=?", state.NodeID).Scan(&last); err != nil {
			t.Fatal(err)
		}
		return last == time.Unix(0, f.clock.Load()).Unix()
	})
	old := f.service
	next, err := New(t.Context(), f.db, old.options)
	if err != nil {
		t.Fatal(err)
	}
	active.Store(next.Handler())
	f.service = next
	old.Close()
	waitUntil(5*time.Second, func() bool {
		nodes, err := next.nodes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return len(nodes) == 1 && nodes[0].Connected
	})
	// 重启后原会话仍可撤销原节点；Agent 收到拒绝后停止普通网络重试。
	request, _ := http.NewRequestWithContext(ctx, "POST", bridge.URL+"/api/v1/nodes/"+state.NodeID+"/revoke", nil)
	request.Header.Set("Origin", bridge.URL)
	request.Header.Set("X-CSRF-Token", session.CsrfToken)
	response, err := f.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("撤销节点失败：%d", response.StatusCode)
	}
	select {
	case err := <-result:
		var permanent *agent.PermanentError
		if !errors.As(err, &permanent) {
			t.Fatalf("撤销身份未停止自动重试：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Agent 撤销后未停止")
	}
}
