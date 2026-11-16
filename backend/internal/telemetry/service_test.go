package telemetry

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
	"github.com/jkfujr/opsd/internal/storage"
)

func testService(t *testing.T, cluster *string) *Service {
	t.Helper()
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "node.sqlite"), storage.Node)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(t.TempDir(), Dependencies{DB: db, NodeID: "node", ClusterID: func() string { return *cluster }, Now: time.Now,
		Shared: Shared{Store: func(context.Context) (*sharedsql.Store, error) { return nil, errors.New("共享库停止") }},
		Errors: Errors{New: func(code, message string) error { return fmt.Errorf("%s: %s", code, message) }},
	})
	t.Cleanup(s.Close)
	return s
}

func TestConfigUsesCurrentIdentityAndKeepsConfirmedCache(t *testing.T) {
	cluster := "before-join"
	s := testService(t, &cluster)
	cluster = "after-join"
	c, err := s.Config(t.Context())
	if err != nil || c.Prefix != metricsstore.DefaultConfig(cluster).Prefix {
		t.Fatal(c, err)
	}
	c.Revision, c.State = 3, "ready"
	if err = s.CacheConfig(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	old := c
	old.Revision, old.Prefix = 2, "old/"
	if err = s.CacheConfig(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	got, err := s.Config(t.Context())
	if err != nil || got != c {
		t.Fatal("共享库停止后丢失确认配置", got, err)
	}
}

type testParent struct {
	record protocol.OperationRecord
	save   func(context.Context, string) error
}

func (p *testParent) Record() *protocol.OperationRecord            { return &p.record }
func (p *testParent) Actor() string                                { return "account:original" }
func (p *testParent) Save(ctx context.Context, phase string) error { return p.save(ctx, phase) }

func TestWorkflowPersistsBeforeParentAndRecoveryKeepsChild(t *testing.T) {
	cluster := "cluster"
	s := testService(t, &cluster)
	p := &testParent{record: protocol.OperationRecord{ID: "original-parent", Revision: 7, State: protocol.AttentionRequired}}
	flow := archiveWorkflow{Stage: 2, Child: "original-child", ChildRevision: 4}
	saves := 0
	p.save = func(ctx context.Context, phase string) error {
		got, err := s.loadWorkflow(ctx, p.record.ID)
		if err != nil || got.Child != flow.Child || got.Stage != flow.Stage || got.ChildRevision != flow.ChildRevision {
			t.Fatal("父操作保存前未持久保存原流程", got, err)
		}
		if phase != "submitted" || p.record.Revision != uint64(8+saves) {
			t.Fatal(phase, p.record)
		}
		saves++
		return nil
	}
	if err := s.persistWorkflow(t.Context(), p, flow); err != nil {
		t.Fatal(err)
	}
	if err := s.Decide(t.Context(), "operation.recover", 8, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.loadWorkflow(t.Context(), p.record.ID)
	if err != nil || got.Request != "recover" || p.record.State != protocol.Applying || saves != 2 {
		t.Fatal(got, p.record, saves, err)
	}
	if err = s.Decide(t.Context(), "operation.recover", 8, p); err == nil || saves != 2 {
		t.Fatal("旧版本恢复改变了原操作", err, saves)
	}
}

type waitingStream struct {
	entered chan struct{}
	closed  atomic.Bool
}

func (s *waitingStream) Send(context.Context, protocol.Message) error { return nil }
func (s *waitingStream) Receive(ctx context.Context) (protocol.Message, error) {
	close(s.entered)
	<-ctx.Done()
	return protocol.Message{}, ctx.Err()
}
func (s *waitingStream) Close() { s.closed.Store(true) }

func TestCloseDrainsStreamsAndKeepsBorrowedDatabase(t *testing.T) {
	cluster := "cluster"
	s := testService(t, &cluster)
	buffer, err := s.Buffer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stream := &waitingStream{entered: make(chan struct{})}
	go s.Serve(t.Context(), "source", stream)
	select {
	case <-stream.entered:
	case <-time.After(time.Second):
		t.Fatal("接收流未启动")
	}
	s.Start(t.Context())
	s.Start(t.Context())
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("关闭未结束流和循环")
	}
	if !stream.closed.Load() || len(s.streams) != 0 {
		t.Fatal("关闭后仍有接收流")
	}
	if err = buffer.DB.PingContext(t.Context()); err == nil {
		t.Fatal("缓冲数据库未关闭")
	}
	if err = s.DB.PingContext(t.Context()); err != nil {
		t.Fatal("关闭了借用的节点数据库", err)
	}
	s.Start(t.Context())
	if _, err = s.Buffer(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatal("关闭后重新打开缓冲", err)
	}
}
