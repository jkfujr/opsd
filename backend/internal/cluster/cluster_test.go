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
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
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

func TestOperationPagesFollowCreatedTimeAcrossNodes(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for _, entry := range []struct {
		node   *Node
		suffix string
		offset time.Duration
	}{
		{a, "old", time.Second}, {a, "new", 3 * time.Second}, {a, "middle", 2 * time.Second},
		{b, "same-b", 2 * time.Second}, {b, "fraction", 2*time.Second + time.Nanosecond}, {b, "same-a", 2 * time.Second},
	} {
		entry.node.Now = func() time.Time { return base.Add(entry.offset) }
		if _, err := entry.node.Submit(t.Context(), entry.node.ID, Request{ID: entry.node.ID + "_" + entry.suffix, Kind: "name", Payload: payload(entry.suffix)}); err != nil {
			t.Fatal(err)
		}
	}
	local, err := a.localOperations(t.Context(), ListQuery{Limit: 2})
	if err != nil || len(local.Items) != 2 || local.Items[0].ID != a.ID+"_new" || local.Items[1].ID != a.ID+"_middle" || local.Next == nil {
		t.Fatalf("本机时间排序无效：%+v %v", local, err)
	}
	q := ListQuery{Limit: 2}
	ids := []string{}
	for range 4 {
		page, err := a.ListOperations(t.Context(), "", q)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range page.Items {
			ids = append(ids, op.ID)
		}
		if len(page.NextPositions) == 0 {
			break
		}
		q.Positions = page.NextPositions
	}
	middle := []string{a.ID + "_middle", b.ID + "_same-a", b.ID + "_same-b"}
	slices.Sort(middle)
	slices.Reverse(middle)
	want := append([]string{a.ID + "_new", b.ID + "_fraction"}, middle...)
	want = append(want, a.ID+"_old")
	if !slices.Equal(ids, want) {
		t.Fatalf("跨节点翻页遗漏或顺序错误：%v，预期 %v", ids, want)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	position := OperationPosition{CreatedAt: base.Add(2 * time.Second), ID: b.ID + "_same-a"}
	partial, err := a.ListOperations(t.Context(), "", ListQuery{Limit: 1, Positions: map[string]OperationPosition{b.ID: position}})
	if err != nil || len(partial.Items) != 1 || !slices.Contains(partial.Unavailable, b.ID) || partial.NextPositions[b.ID] != position {
		t.Fatalf("离线节点的位置被推进：%+v %v", partial, err)
	}
	_, err = a.ListOperations(t.Context(), "", ListQuery{Limit: 1, Positions: map[string]OperationPosition{a.ID: {CreatedAt: base, ID: a.ID + "_before"}}})
	var unavailable *Fault
	if !errors.As(err, &unavailable) || unavailable.Code != "dependency_unavailable" {
		t.Fatal("无可用操作仍返回完整集合", err)
	}
}

func TestAPIV2ReceiptsSurviveNodeReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.db")
	n, err := Open(t.Context(), path, Options{Password: "test-password"})
	if err != nil {
		t.Fatal(err)
	}
	actor := "account:" + n.ID + ":1"
	command := TokenCommand{Write: &requeststate.Write{Actor: actor, Key: "create-once", Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Input: apiauth.Input{Name: "原令牌", Enabled: true, Grants: []apiauth.Grant{{Module: "tasks", Level: "read", Scope: "all"}}}}
	var created createdToken
	if err = n.Call(t.Context(), n.ID, "api_token.create", command, &created); err != nil {
		t.Fatal(err)
	}
	op, err := n.Submit(t.Context(), n.ID, Request{Actor: actor, Kind: "name", Key: "queued-once", Payload: payload("等待执行")})
	if err != nil {
		t.Fatal(err)
	}
	if err = n.Close(); err != nil {
		t.Fatal(err)
	}
	n, err = Open(t.Context(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	for _, key := range []string{"create-once", "queued-once"} {
		receipt, err := n.RequestReceipt(t.Context(), n.ID, requeststate.Write{Actor: actor, Key: key})
		if err != nil || receipt.Actor != actor {
			t.Fatal("重启后原身份回执不可读", receipt, err)
		}
		if key == "create-once" && (!receipt.Secret || receipt.ResourceID != created.Token.ID) {
			t.Fatal("秘密回执改变", receipt)
		}
		if key == "queued-once" && (receipt.OperationID != op.ID || receipt.Operation == nil || receipt.Operation.State != protocol.Accepted) {
			t.Fatal("未结束操作改变", receipt)
		}
	}
	err = n.Call(t.Context(), n.ID, "api_token.create", command, new(createdToken))
	var secret *Fault
	if !errors.As(err, &secret) || secret.Code != "secret_unavailable" {
		t.Fatal("重启后重新生成了秘密", err)
	}
	var count int
	if err = n.DB.QueryRow("SELECT count(*) FROM api_tokens").Scan(&count); err != nil || count != 1 {
		t.Fatal("重启后产生重复令牌", count, err)
	}
}
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
	if resident > 8 || total > 32 {
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
