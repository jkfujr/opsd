package cluster

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type containerExecutor struct {
	lostExecutor
	engine string
	reads  atomic.Int32
}

func (e *containerExecutor) Call(ctx context.Context, p protocol.Payload) (protocol.Payload, error) {
	if _, ok := p.(*protocol.DockerRead); ok {
		e.reads.Add(1)
		return &protocol.DockerReadResult{State: protocol.DockerState{EngineID: e.engine, Version: "29.4.2", State: "ready", Containers: []protocol.ContainerSummary{{ContainerRef: protocol.ContainerRef{EngineID: e.engine, ContainerID: strings.Repeat("a", 64)}, Name: "同名容器", State: "running", Ports: []protocol.ContainerPort{}}}, CheckedAt: time.Now().UTC()}}, nil
	}
	return e.lostExecutor.Call(ctx, p)
}
func TestContainerPeerReadsAndLostWriteResponse(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	x := &containerExecutor{engine: "Docker-A"}
	y := &containerExecutor{engine: "Docker-B"}
	a.Executor = x
	b.Executor = y
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var state protocol.DockerState
			if err := a.Call(t.Context(), b.ID, "containers.read", nil, &state); err != nil || state.EngineID != "Docker-B" || len(state.Containers) != 1 {
				t.Error("跨节点容器读取错误", state, err)
			}
		}()
	}
	wg.Wait()
	if y.reads.Load() != 1 || x.reads.Load() != 0 {
		t.Fatal("同目标读取未合并或读取了入口主机", x.reads.Load(), y.reads.Load())
	}
	request := Request{Key: "container-restart", Kind: "container", Payload: payload(protocol.ContainerAction{ContainerRef: protocol.ContainerRef{EngineID: "Docker-B", ContainerID: strings.Repeat("a", 64)}, Action: "restart"})}
	op, err := a.Submit(t.Context(), b.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := a.Submit(t.Context(), b.ID, request)
	if err != nil || duplicate.ID != op.ID {
		t.Fatal("重复请求生成新操作", duplicate, err)
	}
	b.advance()
	b.advance()
	result, err := a.GetOperation(t.Context(), op.ID)
	if err != nil || result.State != protocol.Succeeded || y.submits != 1 || x.submits != 0 {
		t.Fatal("容器操作投递或未知应答处理错误", result, err, x.submits, y.submits)
	}
	var state protocol.DockerState
	if err = a.Call(t.Context(), b.ID, "containers.read", nil, &state); err != nil || y.reads.Load() != 2 {
		t.Fatal("操作后列表缓存未失效", state, err, y.reads.Load())
	}
	request.Payload = payload(protocol.ContainerAction{ContainerRef: protocol.ContainerRef{EngineID: "Docker-B", ContainerID: strings.Repeat("a", 64)}, Action: "stop"})
	if _, err = a.Submit(t.Context(), b.ID, request); err == nil {
		t.Fatal("同一幂等键接受了其他容器动作")
	}
	if err = a.Revoke(t.Context(), b.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.Call(t.Context(), b.ID, "containers.read", nil, &state); err == nil {
		t.Fatal("已撤销节点仍可读取容器")
	}
}
func TestContainerRecoverUsesExecutorRevision(t *testing.T) {
	n, _ := openTestNode(t)
	x := &revisionContainerExecutor{}
	n.Executor = x
	op, err := n.Submit(t.Context(), n.ID, Request{Key: "recover", Kind: "container", Payload: payload(protocol.ContainerAction{ContainerRef: protocol.ContainerRef{EngineID: "Docker-A", ContainerID: strings.Repeat("a", 64)}, Action: "restart"})})
	if err != nil {
		t.Fatal(err)
	}
	n.advance()
	op, err = n.GetOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = n.Decide(t.Context(), "operation.recover", OperationDecision{ID: op.ID, ExpectedRevision: op.Revision}); err != nil {
		t.Fatal("核对状态使用了入口修订号", err)
	}
}

type revisionContainerExecutor struct{ lostExecutor }

func (e *revisionContainerExecutor) Call(_ context.Context, p protocol.Payload) (protocol.Payload, error) {
	switch p := p.(type) {
	case *protocol.OperationSubmit:
		e.record = &protocol.OperationRecord{ID: p.OperationID, State: protocol.AttentionRequired, Revision: 3, Steps: []protocol.OperationStep{}}
		return &protocol.OperationUpdated{Record: *e.record}, nil
	case *protocol.OperationRecover:
		if p.ExpectedRevision != e.record.Revision {
			return nil, &Fault{Code: "conflict", Message: "阶段已变化"}
		}
		return &protocol.OperationUpdated{Record: *e.record}, nil
	}
	return nil, nil
}
func TestContainerProtocolActionIsTyped(t *testing.T) {
	a := protocol.OperationAction{Kind: "container", Container: &protocol.ContainerAction{ContainerRef: protocol.ContainerRef{EngineID: "Docker-A", ContainerID: strings.Repeat("a", 64)}, Action: "restart"}}
	for _, value := range []string{"rm", "sh", "exec"} {
		a.Container.Action = value
		raw, _ := json.Marshal(a)
		var b protocol.OperationAction
		_ = json.Unmarshal(raw, &b)
		if _, err := protocol.Encode(protocol.Message{RequestID: "typed", Payload: &protocol.OperationSubmit{OperationID: "op", ContractVersion: protocol.Version, Action: b}}); err == nil {
			t.Fatal("接受了非类型化容器动作", value)
		}
	}
}
