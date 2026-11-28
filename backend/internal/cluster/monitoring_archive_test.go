package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/objectstore"
	"github.com/jkfujr/opsd/internal/protocol"
)

func archiveTestConfig(t *testing.T, n, monitor *Node) metricsstore.Config {
	t.Helper()
	c := metricsstore.DefaultConfig(n.cluster)
	c.Enabled, c.NodeID, c.StorageTargetID, c.State, c.Revision = true, monitor.ID, "storage", "ready", 1
	if err := n.cacheArchiveConfig(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if n.Executor == nil {
		n.Executor = &archiveCollector{}
	}
	n.telemetry.Storage.LocalTarget = func(context.Context, string) (objectstore.Target, error) { return objectstore.Target{}, nil }
	n.telemetry.Storage.Credentials = func(context.Context, objectstore.Target) (protocol.S3Credentials, error) {
		return protocol.S3Credentials{}, nil
	}
	n.telemetry.Refresh(t.Context())
	return c
}

func TestMonitoringAuthenticatedStreamReceiptsAndRejections(t *testing.T) {
	t.Run("WebSocket", func(t *testing.T) { testMonitoringAuthenticatedStream(t, false) })
	t.Run("TCP", func(t *testing.T) { testMonitoringAuthenticatedStream(t, true) })
}

func testMonitoringAuthenticatedStream(t *testing.T, tcp bool) {
	t.Helper()
	monitor := openPeerTestNode(t, tcp)
	source := openPeerTestNode(t, tcp)
	joinTest(t, source, monitor)
	archiveTestConfig(t, monitor, monitor)
	archiveTestConfig(t, source, monitor)
	listener, err := net.Listen("tcp", "127.0.0.1:19291")
	if err != nil {
		t.Fatal(err)
	}
	var writes atomic.Int32
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writes.Add(1); w.WriteHeader(204) }))
	receiver.Listener.Close()
	receiver.Listener = listener
	receiver.Start()
	defer receiver.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	channel, err := source.openArchiveChannel(ctx, monitor.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	now := time.Now().UTC().Truncate(time.Second)
	buffer, err := source.metricsBuffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buffer.Append(ctx, metricsstore.Batch{At: now.UnixMilli(), Samples: []metricsstore.Sample{{Metric: "opsd_node_up", At: now.UnixMilli(), Value: 1}}}, source.ID, now, metricsstore.BufferBytes)
	if err != nil {
		t.Fatal(err)
	}
	var ack []archiveAck
	if err = archiveCall(ctx, channel, "upload", archiveUpload{Batches: []metricsstore.Batch{b}}, &ack); err != nil || len(ack) != 1 || ack[0].Digest != metricsstore.Digest(b) || ack[0].Error != "" {
		t.Fatal("首次上传失败", ack, err)
	}
	ack = nil
	if err = archiveCall(ctx, channel, "receipts", []string{b.ID}, &ack); err != nil || len(ack) != 1 || ack[0].Digest != metricsstore.Digest(b) {
		t.Fatal("应答丢失后的回执查询失败", ack, err)
	}
	if err = archiveCall(ctx, channel, "upload", archiveUpload{Batches: []metricsstore.Batch{b}}, &ack); err != nil || writes.Load() != 1 {
		t.Fatal("原批次被重复写入", writes.Load(), err)
	}
	forged := b
	forged.ID, forged.Sequence = identity.Random(), b.Sequence+1
	forged.Samples = []metricsstore.Sample{{Metric: "opsd_container_running", ResourceID: strings.Repeat("f", 64), At: forged.At, Value: 1}}
	if err = archiveCall(ctx, channel, "upload", archiveUpload{Batches: []metricsstore.Batch{forged}}, &ack); err != nil || len(ack) != 1 || ack[0].Error == "" || writes.Load() != 1 {
		t.Fatal("伪装资源进入接收端", ack, err)
	}
	if err = archiveCall(ctx, channel, "receipts", []string{forged.ID}, &ack); err != nil || len(ack) != 1 || ack[0].Error == "" {
		t.Fatal("永久拒绝的回执未持久保存", ack, err)
	}
	conflict := b
	conflict.ID = identity.Random()
	if err = archiveCall(ctx, channel, "upload", archiveUpload{Batches: []metricsstore.Batch{conflict}}, nil); err == nil || writes.Load() != 1 {
		t.Fatal("序号复用覆盖了原批次", err)
	}
	if err = archiveCall(ctx, channel, "directory", nil, nil); err == nil {
		t.Fatal("监控数据通道执行了控制请求")
	}
	monitor.Executor.(*archiveCollector).full.Store(true)
	monitor.telemetry.Refresh(t.Context())
	if err = archiveCall(ctx, channel, "upload", archiveUpload{Batches: []metricsstore.Batch{b}}, nil); err == nil {
		t.Fatal("磁盘压力期间继续接收")
	}
	if err = archiveCall(ctx, channel, "receipts", []string{b.ID}, &ack); err != nil || len(ack) != 1 {
		t.Fatal("暂停接收阻断了已有回执查询", ack, err)
	}
	var pong struct{}
	if err = source.Call(ctx, monitor.ID, "ping", nil, &pong); err != nil {
		t.Fatal("上传故障阻断了控制通道", err)
	}
}

type archiveCollector struct {
	reads atomic.Int32
	fail  atomic.Bool
	full  atomic.Bool
}

func (c *archiveCollector) Call(_ context.Context, p protocol.Payload) (protocol.Payload, error) {
	switch p.(type) {
	case *protocol.MonitoringStateRead:
		budget := p.(*protocol.MonitoringStateRead).Deployment.DiskBudgetBytes
		used := int64(0)
		if c.full.Load() {
			used = budget
		}
		return &protocol.MonitoringState{DiskUsedBytes: used, DiskAvailableBytes: budget}, nil
	case *protocol.MonitoringRead:
		c.reads.Add(1)
		if c.fail.Load() {
			return nil, errors.New("采集超时")
		}
		cpu := float64(c.reads.Load())
		return &protocol.MonitoringResult{Metrics: protocol.Metrics{CollectedAt: time.Now().UTC(), CPUCores: 4, CPUUsage: &cpu}}, nil
	case *protocol.ResourceMetricsRead:
		if c.fail.Load() {
			return nil, errors.New("资源采集超时")
		}
		return &protocol.ResourceMetrics{At: time.Now().UTC(), Metrics: []protocol.ResourceMetric{{Metric: "opsd_container_running", ResourceID: strings.Repeat("a", 64), Value: 1}}, Errors: []string{}}, nil
	}
	return nil, errors.New("未使用此执行入口")
}
func (*archiveCollector) Storage(context.Context, StorageConfig) (Credentials, error) {
	return Credentials{}, nil
}

func TestMonitoringSamplingKeepsOriginalTimeAndSharesCurrentReading(t *testing.T) {
	n, _ := openTestNode(t)
	collector := &archiveCollector{}
	n.Executor = collector
	archiveTestConfig(t, n, n)
	b, err := n.metricsBuffer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = n.telemetry.Sample(t.Context(), b); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	got, err := n.metrics(t.Context(), "live")
	if err != nil || collector.reads.Load() != 2 || got.Metrics.CPUUsage == nil || *got.Metrics.CPUUsage != 2 {
		t.Fatal("页面未复用本轮采样", got, err, collector.reads.Load())
	}
	collector.fail.Store(true)
	time.Sleep(2 * time.Millisecond)
	if err = n.telemetry.Sample(t.Context(), b); err == nil || !strings.Contains(err.Error(), "资源采集超时") {
		t.Fatal("采集故障未进入节点状态", err)
	}
	pending, err := b.Pending(t.Context(), n.ID, n.Now())
	if err != nil || len(pending) != 3 || pending[0].At >= pending[1].At || len(pending[2].Errors) == 0 || slices.ContainsFunc(pending[2].Samples, func(s metricsstore.Sample) bool { return s.Metric == "opsd_container_running" }) {
		t.Fatal("失败采集重写了旧值或丢失缺口", pending, err)
	}
	data, _ := json.Marshal(pending)
	if strings.Contains(string(data), "secret_key") {
		t.Fatal("普通节点缓冲包含 S3 凭据")
	}
}

func TestMonitoringParentAdapterKeepsQueueMetadata(t *testing.T) {
	n, _ := openTestNode(t)
	c := metricsstore.DefaultConfig(n.cluster)
	body, _ := json.Marshal(c)
	op, err := n.Submit(t.Context(), n.ID, Request{Key: "monitoring-parent-adapter", Kind: "monitoring_config", Payload: body})
	if err != nil {
		t.Fatal(err)
	}
	before := op
	if err = n.advanceArchive(t.Context(), &op, body); err != nil {
		t.Fatal(err)
	}
	stored, err := n.GetOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != before.ID || stored.Key != before.Key || stored.NodeID != before.NodeID || stored.Sequence != before.Sequence || stored.Subject != before.Subject || stored.Actor != before.Actor || !stored.CreatedAt.Equal(before.CreatedAt) {
		t.Fatal("监控保存改变了队列归属或请求标识", before, stored)
	}
	if stored.State != protocol.AttentionRequired || stored.Revision <= before.Revision || stored.Error == nil || stored.Error.RequestID != before.ID {
		t.Fatal("共享库停止后的父操作状态未保存", stored)
	}
}
