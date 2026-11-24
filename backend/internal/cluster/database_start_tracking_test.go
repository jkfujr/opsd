package cluster

import (
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
)

// 从已受理且已提交的步骤开始，每次只推进一轮真实队列。
func databaseStartTestFlow(t *testing.T, firewall bool) (*Node, *deploymentExecutor, Operation, protocol.OperationRecord) {
	t.Helper()
	n, x := deploymentNode(t)
	action, err := seal([]byte("{}"), []string{n.age.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(id, kind string) Operation {
		t.Helper()
		op := Operation{OperationRecord: protocol.OperationRecord{ID: id, State: protocol.Applying, Revision: 1},
			NodeID: n.ID, Subject: kind, Actor: n.ID}
		_, err := n.DB.ExecContext(t.Context(), `INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,phase,body)
			VALUES(?,?,?,?,'',X'','submitted',?)`, id, n.ID, kind, string(payload(action)), string(payload(op)))
		if err != nil {
			t.Fatal(err)
		}
		return op
	}
	child := protocol.OperationRecord{ID: NewOperationID(n.ID), State: protocol.Prepared, Revision: 1,
		Error: &protocol.Error{Code: "operation_start_failed", Message: "等待启动"}}
	x.records[child.ID] = child
	step := databaseWorkflowStep{Ref: DatabaseRef{NodeID: n.ID}, ID: child.ID, State: "applying"}
	if firewall {
		step.Firewall = &FirewallWrite{}
		insert(child.ID, "firewall_config")
	}
	op := insert(NewOperationID(n.ID), "database")
	if err := n.persistDatabaseWorkflow(t.Context(), &op, databaseWorkflow{Built: true, Steps: []databaseWorkflowStep{step}}); err != nil {
		t.Fatal(err)
	}
	return n, x, op, child
}

func pollDatabaseStart(t *testing.T, n *Node, id string) Operation {
	t.Helper()
	n.advanceFiltered("id=?", id)
	op, err := n.operation(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestDatabaseStartDecisions(t *testing.T) {
	n, x, op, child := databaseStartTestFlow(t, false)
	op = pollDatabaseStart(t, n, op.ID)
	if op.State != protocol.AttentionRequired || !sameOperationError(op.Error, child.Error) {
		t.Fatal("父操作未报告启动停滞", op)
	}
	d := OperationDecision{ID: op.ID, ExpectedRevision: op.Revision, AcceptCurrent: true}
	write := requeststate.Write{Actor: n.ID, Key: "resolve", Digest: strings.Repeat("a", 64)}
	for range 2 {
		if _, err := n.CallOperationDecision(t.Context(), op.ID, "operation.resolve", d, &write); faultCode(err) != "conflict" {
			t.Fatalf("接受现状及其同键重试均应返回 conflict，实际 %v", err)
		}
	}
	d.AcceptCurrent = false
	write.Key, write.Digest = "recover", strings.Repeat("b", 64)
	recovered, err := n.CallOperationDecision(t.Context(), op.ID, "operation.recover", d, &write)
	if err != nil || recovered.ID != op.ID {
		t.Fatal("重新处理未返回原操作", recovered, err)
	}
	flow, err := n.loadDatabaseWorkflow(t.Context(), op.ID)
	if err != nil || flow.Request != "" || len(flow.Steps) != 1 || flow.Steps[0].ID != child.ID || flow.Steps[0].State != "unknown" {
		t.Fatal("重新处理改变了原步骤或登记了重发请求", flow, err)
	}
	child.State, child.Error, child.Revision = protocol.Succeeded, nil, child.Revision+1
	x.records[child.ID] = child
	pollDatabaseStart(t, n, op.ID)
	if got := pollDatabaseStart(t, n, op.ID); got.State != protocol.Succeeded || x.submits != 0 {
		t.Fatal("原步骤成功后父操作未完成或重新提交了步骤", got, x.submits)
	}
}

func TestDatabaseStartReadResumesAfterDisconnect(t *testing.T) {
	n, x, op, child := databaseStartTestFlow(t, false)
	op = pollDatabaseStart(t, n, op.ID)
	n.Executor = nil
	got := pollDatabaseStart(t, n, op.ID)
	if got.Revision != op.Revision || got.State != protocol.AttentionRequired || !sameOperationError(got.Error, child.Error) {
		t.Fatal("临时读错覆盖了启动停滞", got)
	}
	child.State, child.Error, child.Revision = protocol.Succeeded, nil, child.Revision+1
	x.records[child.ID] = child
	n.Executor = x
	pollDatabaseStart(t, n, op.ID)
	if got = pollDatabaseStart(t, n, op.ID); got.State != protocol.Succeeded || x.submits != 0 {
		t.Fatal("连接恢复后未沿原步骤完成", got, x.submits)
	}
}

func TestDeploymentFirewallStartProgress(t *testing.T) {
	n, x, op, child := databaseStartTestFlow(t, true)
	first := pollDatabaseStart(t, n, op.ID)
	if first.State != protocol.AttentionRequired || !sameOperationError(first.Error, child.Error) {
		t.Fatal("部署未报告防火墙步骤的启动停滞", first)
	}
	if got := pollDatabaseStart(t, n, op.ID); got.Revision != first.Revision {
		t.Fatal("同一启动错误重复保存", got)
	}
	child.Error = &protocol.Error{Code: "operation_start_failed", Message: "启动服务返回具体原因"}
	x.records[child.ID] = child
	changed := pollDatabaseStart(t, n, op.ID)
	if changed.Revision != first.Revision+1 || !sameOperationError(changed.Error, child.Error) {
		t.Fatal("同一子修订上的新错误未保存", changed)
	}
	child.State, child.Error, child.Revision = protocol.Applying, nil, child.Revision+1
	x.records[child.ID] = child
	active := pollDatabaseStart(t, n, op.ID)
	if active.State != protocol.Applying || active.Error != nil {
		t.Fatal("防火墙步骤推进后未清错", active)
	}
	if got := pollDatabaseStart(t, n, op.ID); got.Revision != active.Revision {
		t.Fatal("同一执行进度重复保存", got)
	}
	child.State, child.Revision = protocol.Succeeded, child.Revision+1
	x.records[child.ID] = child
	pollDatabaseStart(t, n, op.ID)
	if got := pollDatabaseStart(t, n, op.ID); got.State != protocol.Succeeded || x.submits != 0 {
		t.Fatal("防火墙步骤恢复后部署未完成", got, x.submits)
	}
}
