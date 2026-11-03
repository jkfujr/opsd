package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
)

func openTestNode(t *testing.T) (*Node, *httptest.Server) {
	t.Helper()
	n, e := Open(t.Context(), filepath.Join(t.TempDir(), "节点.db"), Options{Password: "test-password"})
	if e != nil {
		t.Fatal(e)
	}
	s := httptest.NewServer(n.PeerHandler())
	c, e := n.Config(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	c.Address = s.URL
	b, _ := json.Marshal(c)
	tx, e := n.DB.BeginTx(t.Context(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec("UPDATE node_identity SET config=? WHERE id=1", string(b)); e != nil {
		t.Fatal(e)
	}
	if e = n.publishMemberTx(t.Context(), tx, c); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { n.Close(); s.Close() })
	return n, s
}
func TestNodeWithHundredMembersUnderMemoryLimit(t *testing.T) {
	old := debug.SetMemoryLimit(64 << 20)
	defer debug.SetMemoryLimit(old)
	n, _ := openTestNode(t)
	d, _ := n.Directory(t.Context())
	for range 99 {
		k, e := identity.New()
		if e != nil {
			t.Fatal(e)
		}
		public, _, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		m := Member{ID: nodeID(k.Public), Cluster: n.cluster, Name: "低配节点", Address: "http://127.0.0.1:1", Public: identity.Encode(k.Public), Signing: identity.Encode(public), Recipient: n.age.Recipient().String(), Version: 1, Issuer: n.ID}
		m.Signature = ed25519.Sign(n.signing, memberBytes(m))
		d.Admissions = append(d.Admissions, m)
		d.Members = append(d.Members, m)
	}
	if e := n.merge(t.Context(), d, false); e != nil {
		t.Fatal(e)
	}
	for i := range 100 {
		op, e := n.Submit(t.Context(), n.ID, Request{Key: fmt.Sprint(i), Kind: "name", Payload: payload(fmt.Sprint(i))})
		if e != nil {
			t.Fatal(e)
		}
		n.advance()
		result, e := n.GetOperation(t.Context(), op.ID)
		if e != nil || result.State != protocol.Succeeded {
			t.Fatal(result, e)
		}
	}
	d, e := n.Directory(t.Context())
	if e != nil || len(d.Members) != 100 {
		t.Fatal(len(d.Members), e)
	}
	var foreign int
	if e = n.DB.QueryRow("SELECT count(*) FROM records WHERE substr(id,1,32)<>?", n.ID).Scan(&foreign); e != nil || foreign != 0 {
		t.Fatal("普通节点保存了其他成员的完整记录", foreign, e)
	}
}
func joinTest(t *testing.T, n, seed *Node) {
	t.Helper()
	token, _, _, e := seed.Token(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	m, e := seed.Self(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if e = n.Join(ctx, m.Address, m.Public, token); e != nil {
		t.Fatal(e)
	}
}
func payload(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func TestPeersContinueWithoutInitialNode(t *testing.T) {
	a, server := openTestNode(t)
	b, _ := openTestNode(t)
	c, _ := openTestNode(t)
	joinTest(t, b, a)
	joinTest(t, c, a)
	b.exchange()
	c.exchange()
	if e := a.Close(); e != nil {
		t.Fatal(e)
	}
	server.Close()
	op, e := b.Submit(t.Context(), c.ID, Request{ID: NewOperationID(c.ID), Kind: "name", Payload: payload("东京")})
	if e != nil {
		t.Fatal(e)
	}
	c.advance()
	result, e := b.GetOperation(t.Context(), op.ID)
	if e != nil || result.State != protocol.Succeeded {
		t.Fatalf("初始节点离线后互控失败：%+v %v", result, e)
	}
	conf, e := c.Config(t.Context())
	if e != nil || conf.Name != "东京" {
		t.Fatal(conf, e)
	}
	d, e := b.Directory(t.Context())
	if e != nil || len(d.Members) != 3 {
		t.Fatal(d, e)
	}
}
func TestEnrollmentOnceAndRevocationDoesNotRevive(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	c, _ := openTestNode(t)
	token, _, _, e := a.Token(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	m, _ := a.Self(t.Context())
	if e = b.Join(t.Context(), m.Address, m.Public, token); e != nil {
		t.Fatal(e)
	}
	if e = c.Join(t.Context(), m.Address, m.Public, token); e == nil {
		t.Fatal("接入码被重复消费")
	}
	old, _ := a.Directory(t.Context())
	if e = a.Revoke(t.Context(), b.ID); e != nil {
		t.Fatal(e)
	}
	if e = a.merge(t.Context(), old, false); e != nil {
		t.Fatal(e)
	}
	if !a.Revoked(t.Context(), b.ID) {
		t.Fatal("旧目录恢复了撤销成员")
	}
	if e = b.Call(t.Context(), a.ID, "config.read", nil, new(Config)); e == nil {
		t.Fatal("已撤销成员仍能控制")
	}
}
func TestQueueOrderIdempotencyAndCancel(t *testing.T) {
	n, _ := openTestNode(t)
	const count = 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	ops := []Operation{}
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			op, e := n.Submit(t.Context(), n.ID, Request{Key: fmt.Sprint(i), Kind: "name", Payload: payload(fmt.Sprint(i))})
			if e != nil {
				t.Error(e)
				return
			}
			mu.Lock()
			ops = append(ops, op)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(ops) != count {
		t.Fatal(len(ops))
	}
	var last Operation
	for _, op := range ops {
		if op.Sequence > last.Sequence {
			last = op
		}
	}
	for range count {
		n.advance()
	}
	var expected string
	_ = n.DB.QueryRow("SELECT request_key FROM queue WHERE id=?", last.ID).Scan(&expected)
	c, e := n.Config(t.Context())
	if e != nil || c.Name != expected {
		t.Fatal(c, e)
	}
	stored, e := n.operation(t.Context(), last.ID)
	if e != nil || stored.State != protocol.Succeeded {
		t.Fatal(stored, e)
	}
	before := c.Revision
	for _, op := range ops {
		var old string
		_ = n.DB.QueryRow("SELECT request_key FROM queue WHERE id=?", op.ID).Scan(&old)
		again, e := n.Submit(t.Context(), n.ID, Request{Key: old, Kind: "name", Payload: payload(old)})
		if e != nil || again.ID != op.ID {
			t.Fatal(again, e)
		}
	}
	n.advance()
	c, _ = n.Config(t.Context())
	if c.Revision != before {
		t.Fatal("重复提交再次修改了节点")
	}
	queued, e := n.Submit(t.Context(), n.ID, Request{ID: NewOperationID(n.ID), Kind: "name", Payload: payload("取消")})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = n.cancelQueued(t.Context(), queued.ID); e != nil {
		t.Fatal(e)
	}
	n.advance()
	c, _ = n.Config(t.Context())
	if c.Name == "取消" {
		t.Fatal("取消的请求仍被执行")
	}
}

type lostExecutor struct {
	mu      sync.Mutex
	record  *protocol.OperationRecord
	submits int
}

func (e *lostExecutor) Call(_ context.Context, p protocol.Payload) (protocol.Payload, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch p := p.(type) {
	case *protocol.OperationSubmit:
		e.submits++
		e.record = &protocol.OperationRecord{ID: p.OperationID, State: protocol.Succeeded, Revision: 2, Decision: "keep", Steps: []protocol.OperationStep{}}
		return nil, errors.New("应答丢失")
	case *protocol.OperationQuery:
		return &protocol.OperationQueryResult{OperationID: p.OperationID, Found: e.record != nil, Record: e.record}, nil
	}
	return nil, errors.New("未知操作")
}
func (*lostExecutor) Storage(context.Context, StorageConfig) (Credentials, error) {
	return Credentials{}, nil
}
func TestLostReplyAndRestartDoNotResubmit(t *testing.T) {
	n, _ := openTestNode(t)
	x := &lostExecutor{}
	n.Executor = x
	op, e := n.Submit(t.Context(), n.ID, Request{ID: NewOperationID(n.ID), Kind: "component", Payload: payload(protocol.ComponentAction{Component: "firewall", Action: "install", Backend: "ufw"})})
	if e != nil {
		t.Fatal(e)
	}
	n.advance()
	var path string
	if e = n.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); e != nil {
		t.Fatal(e)
	}
	second, e := Open(t.Context(), path, Options{Executor: x})
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	second.advance()
	result, e := second.GetOperation(t.Context(), op.ID)
	if e != nil || result.State != protocol.Succeeded || x.submits != 1 {
		t.Fatalf("未知结果被重发：%+v %v 次数=%d", result, e, x.submits)
	}
}
func TestAccountAuthorizationAndNewRecipient(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	if e := a.Rewrap(t.Context()); e != nil {
		t.Fatal(e)
	}
	p, e := b.Password(t.Context(), a.ID)
	if e != nil || p.Password != "test-password" {
		t.Fatal(p.Username, e)
	}
	if e = a.Revoke(t.Context(), b.ID); e != nil {
		t.Fatal(e)
	}
	op, e := a.Submit(t.Context(), a.ID, Request{ID: NewOperationID(a.ID), Kind: "account", Payload: payload(AccountChange{Password: "new-password"})})
	if e != nil {
		t.Fatal(e)
	}
	a.advance()
	result, e := a.GetOperation(t.Context(), op.ID)
	if e != nil || result.State != protocol.Succeeded {
		t.Fatal(result, e)
	}
	account, e := a.Account(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = unseal(account.Secret, b.age); e == nil {
		t.Fatal("已撤销成员取得新密钥")
	}
}
func TestLiveReadSharedAndHistoryIndependent(t *testing.T) {
	n, _ := openTestNode(t)
	x := &metricExecutor{}
	n.Executor = x
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := n.metrics(t.Context(), "live")
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if x.live.Load() != 1 {
		t.Fatal(x.live.Load())
	}
	if e := n.sample(t.Context()); e != nil {
		t.Fatal(e)
	}
	if x.history.Load() != 1 || x.live.Load() != 1 {
		t.Fatal("历史与实时采集相互影响")
	}
}

type metricExecutor struct{ live, history atomic.Int32 }

func (e *metricExecutor) Call(_ context.Context, p protocol.Payload) (protocol.Payload, error) {
	switch p := p.(type) {
	case *protocol.MonitoringRead:
		if p.Stream == "live" {
			e.live.Add(1)
		} else {
			e.history.Add(1)
		}
		return &protocol.MonitoringResult{Metrics: protocol.Metrics{CollectedAt: time.Now().UTC(), Errors: []string{}}, Firewall: "enabled"}, nil
	}
	return nil, errors.New("未知操作")
}
func (*metricExecutor) Storage(context.Context, StorageConfig) (Credentials, error) {
	return Credentials{}, nil
}
func TestBoundedConnectionsWithTwentyPeers(t *testing.T) {
	if testing.Short() {
		t.Skip("多节点验收")
	}
	nodes := make([]*Node, 20)
	for i := range nodes {
		nodes[i], _ = openTestNode(t)
		if i > 0 {
			joinTest(t, nodes[i], nodes[0])
		}
	}
	for _, n := range nodes {
		n.exchange()
	}
	first := nodes[0]
	dir, _ := first.Directory(t.Context())
	for _, m := range dir.Members {
		if m.ID == first.ID {
			continue
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		if _, err := first.connection(ctx, m.ID, false); err != nil {
			cancel()
			t.Fatalf("连接 %s 失败：%v", m.ID, err)
		}
		e := first.Call(ctx, m.ID, "config.read", nil, new(Config))
		cancel()
		if e != nil {
			t.Fatal(e)
		}
	}
	first.mu.Lock()
	resident, total := 0, len(first.connections)
	for _, p := range first.connections {
		if p.resident {
			resident++
		}
	}
	first.mu.Unlock()
	if resident > 8 || total > 8 {
		t.Fatalf("连接未受限：resident=%d total=%d", resident, total)
	}
}
func TestQueuedRevokedOriginRejected(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	op, e := b.Submit(t.Context(), a.ID, Request{ID: NewOperationID(a.ID), Kind: "name", Payload: payload("不应执行")})
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Revoke(t.Context(), b.ID); e != nil {
		t.Fatal(e)
	}
	a.advance()
	op, e = a.GetOperation(t.Context(), op.ID)
	if e != nil || !operations.Terminal(op.State) || op.State != protocol.Rejected {
		t.Fatal(op, e)
	}
}
