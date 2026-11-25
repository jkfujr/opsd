package telemetry

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
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

func TestArchiveKeepsHostAndDockerWhenDatabaseTimesOut(t *testing.T) {
	cluster := "cluster"
	s := testService(t, &cluster)
	s.runtime.Config.Enabled = true
	started := make(chan struct{})
	finished := make(chan error, 1)
	s.Source = Source{
		Host: func(context.Context, string) (protocol.MonitoringResult, error) {
			return protocol.MonitoringResult{Metrics: protocol.Metrics{CollectedAt: time.Now().UTC(), CPUCores: 4}}, nil
		},
		Docker: func(context.Context) (protocol.ResourceMetrics, error) {
			return protocol.ResourceMetrics{Docker: &protocol.DockerState{State: "ready", CheckedAt: time.Now().UTC()}, Metrics: []protocol.ResourceMetric{{Metric: "opsd_docker_up", Value: 1}}}, nil
		},
		Databases: func(ctx context.Context) (protocol.ResourceMetrics, error) {
			close(started)
			<-ctx.Done()
			return protocol.ResourceMetrics{}, ctx.Err()
		},
	}
	buffer, err := s.Buffer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	go func() { finished <- s.Sample(t.Context(), buffer) }()
	<-started
	for s.CurrentHost() == nil || s.CurrentDocker() == nil {
		select {
		case err = <-finished:
			t.Fatal("数据库超时前未独立发布宿主及容器缓存", err)
		case <-time.After(time.Millisecond):
		}
	}
	err = <-finished
	if err == nil || !strings.Contains(err.Error(), "database: context deadline exceeded") {
		t.Fatal("未报告数据库采集超时", err)
	}
	batches, err := buffer.Pending(t.Context(), s.NodeID, s.Now())
	if err != nil || len(batches) != 1 {
		t.Fatal(batches, err)
	}
	for _, name := range []string{"opsd_node_cpu_cores", "opsd_docker_up"} {
		if !slices.ContainsFunc(batches[0].Samples, func(m metricsstore.Sample) bool { return m.Metric == name }) {
			t.Fatal("归档丢失正常指标", name, batches[0])
		}
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
