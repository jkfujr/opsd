package cluster

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type blockedReadExecutor struct {
	metricExecutor
	started atomic.Int32
	release chan struct{}
}

func (x *blockedReadExecutor) Call(ctx context.Context, p protocol.Payload) (protocol.Payload, error) {
	switch p.(type) {
	case *protocol.FirewallRead:
		x.started.Add(1)
		select {
		case <-x.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &protocol.FirewallReadResult{State: protocol.FirewallState{Backend: "none"}}, nil
	case *protocol.MonitoringRead:
		x.started.Add(1)
		select {
		case <-x.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return x.metricExecutor.Call(ctx, p)
}

func TestBidirectionalSaturatedReadsKeepOperationQueriesAvailable(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	x := &blockedReadExecutor{release: make(chan struct{})}
	y := &blockedReadExecutor{release: make(chan struct{})}
	a.Executor = x
	b.Executor = y
	op, err := a.Submit(t.Context(), a.ID, Request{Kind: "name", Key: "control-read", Payload: payload("新名称")})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		for _, direction := range []struct{ from, to *Node }{{a, b}, {b, a}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var out protocol.FirewallReadResult
				if e := direction.from.Call(t.Context(), direction.to.ID, "firewall.read", protocol.FirewallRead{}, &out); e != nil {
					t.Error(e)
				}
			}()
		}
	}
	released := false
	defer func() {
		if !released {
			close(x.release)
			close(y.release)
		}
		wg.Wait()
	}()
	deadline := time.Now().Add(3 * time.Second)
	for x.started.Load() < 6 || y.started.Load() < 6 {
		if time.Now().After(deadline) {
			t.Fatal("双向业务未进入满载", x.started.Load(), y.started.Load())
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	var got Operation
	err = b.Call(ctx, a.ID, "operation.get", op.ID, &got)
	cancel()
	if err != nil || got.ID != op.ID {
		t.Fatal("业务读取阻塞了原操作查询", got.ID, err)
	}
	close(x.release)
	close(y.release)
	released = true
	wg.Wait()
}

func TestSharedCurrentReadsReturnQueuedAndReuseActiveRefresh(t *testing.T) {
	n, _ := openTestNode(t)
	x := &blockedReadExecutor{release: make(chan struct{})}
	n.Executor = x
	for range 12 {
		status := n.CurrentRead(t.Context(), n.ID, "metrics", nil, new(any), 20*time.Millisecond)
		if status.State != "queued" && status.State != "reading" {
			t.Fatal("冷读取被标成离线", status)
		}
	}
	deadline := time.Now().Add(time.Second)
	for x.started.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("读取未开始")
		}
		time.Sleep(time.Millisecond)
	}
	if x.started.Load() != 1 {
		t.Fatal("多浏览器重复执行采集", x.started.Load())
	}
	close(x.release)
	for {
		var value protocol.MonitoringResult
		status := n.CurrentRead(t.Context(), n.ID, "metrics", nil, &value, 20*time.Millisecond)
		if status.State == "ready" {
			if status.DataAt == nil || value.Metrics.CollectedAt.IsZero() {
				t.Fatal("缺少实际数据时间")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("共享读取未完成", status)
		}
		time.Sleep(time.Millisecond)
	}
	n.mu.Lock()
	entry := n.current[n.ID+"/metrics/null"]
	entry.used = n.Now().Add(-time.Minute)
	n.mu.Unlock()
	for {
		n.mu.Lock()
		running := entry.running
		n.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("页面离开后仍然持续刷新")
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	for i := range 100 {
		status := n.CurrentRead(t.Context(), fmt.Sprintf("cold-%d", i), "config.read", nil, new(Config), time.Second)
		if status.State != "queued" {
			t.Fatal("排队目标被标成离线", status)
		}
	}
	if time.Since(start) > time.Second {
		t.Fatal("冷读取等待了全部远端")
	}
}

func TestDirectoryUnchangedSkipsEventsAndConnectionsAreReused(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	d, err := a.Directory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	before := a.eventSequence.Load()
	if err = a.merge(t.Context(), d, false); err != nil {
		t.Fatal(err)
	}
	if a.eventSequence.Load() != before || !emptyDirectory(directoryDelta(d, directorySummary(d))) {
		t.Fatal("相同目录触发了维护或事件")
	}
	var value Config
	if err = a.Call(t.Context(), b.ID, "config.read", nil, &value); err != nil {
		t.Fatal(err)
	}
	p, err := a.connection(t.Context(), b.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Call(t.Context(), b.ID, "config.read", nil, &value); err != nil {
		t.Fatal(err)
	}
	again, err := a.connection(t.Context(), b.ID, true)
	if err != nil || p != again || !again.resident {
		t.Fatal("连接未复用或提升", err)
	}
	a.mu.Lock()
	p.resident = false
	p.last = a.Now().Add(-2 * time.Minute)
	a.mu.Unlock()
	// 明确无常驻需求的连接由同一维护方法回收。
	a.pruneConnections(map[string]Member{})
	a.mu.Lock()
	remaining := a.connections[b.ID]
	a.mu.Unlock()
	if remaining != nil {
		t.Fatal("空闲连接未释放")
	}
}
