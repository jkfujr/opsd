package cluster

import (
	"context"
	"errors"
	"testing"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

type admissionExecutor struct {
	lostExecutor
	err        error
	ids        []string
	executions int
	reconciles int
}

func (x *admissionExecutor) Call(ctx context.Context, p protocol.Payload) (protocol.Payload, error) {
	switch p := p.(type) {
	case *protocol.OperationSubmit:
		x.ids = append(x.ids, p.OperationID)
		if len(x.ids) == 1 {
			return nil, x.err
		}
		x.executions++
		x.record = &protocol.OperationRecord{ID: p.OperationID, State: protocol.Succeeded, Revision: 2}
		return &protocol.OperationUpdated{Record: *x.record}, nil
	case *protocol.OperationReconcile:
		x.reconciles++
		return &protocol.OperationReconcileResult{Record: protocol.OperationRecord{ID: p.OperationID, State: protocol.Rejected, Revision: 1, Error: &protocol.Error{Code: "not_accepted"}}}, nil
	}
	return x.lostExecutor.Call(ctx, p)
}

func TestQueueAdmissionBusyFaultAndUnknownReply(t *testing.T) {
	for _, test := range []struct {
		name, phase, code string
		err               error
	}{
		{"busy", "queued", "", &firewall.Fault{Code: "not_accepted_busy", Message: "繁忙"}},
		{"invalid", "finished", "invalid_request", &firewall.Fault{Code: "invalid_request", Message: "参数无效"}},
		{"dependency", "finished", "dependency_unavailable", &firewall.Fault{Code: "dependency_unavailable", Message: "锁文件不可读"}},
		{"transport", "dispatching", "not_accepted", errors.New("传输失败")},
	} {
		for _, reopen := range []bool{false, true} {
			t.Run(test.name+map[bool]string{true: "/重启", false: "/原进程"}[reopen], func(t *testing.T) {
				n, _ := openTestNode(t)
				x := &admissionExecutor{err: test.err}
				n.Executor = x
				op, err := n.Submit(t.Context(), n.ID, Request{ID: NewOperationID(n.ID), Kind: "component", Payload: payload(protocol.ComponentAction{Component: "firewall", Action: "install", Backend: "ufw"})})
				if err != nil {
					t.Fatal(err)
				}
				n.advance()
				var phase, path string
				var terminal bool
				if err = n.DB.QueryRow("SELECT phase,terminal FROM queue WHERE id=?", op.ID).Scan(&phase, &terminal); err != nil || phase != test.phase || terminal != (test.phase == "finished") {
					t.Fatal("受理结果未保存正确阶段", phase, terminal, err)
				}
				if reopen {
					if err = n.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
						t.Fatal(err)
					}
					second, err := Open(t.Context(), path, Options{Executor: x})
					if err != nil {
						t.Fatal(err)
					}
					defer second.Close()
					n = second
				}
				n.advance()
				n.advance()
				got, err := n.GetOperation(t.Context(), op.ID)
				if err != nil {
					t.Fatal(err)
				}
				if test.name == "busy" {
					if got.State != protocol.Succeeded || len(x.ids) != 2 || x.ids[0] != x.ids[1] || x.executions != 1 || x.reconciles != 0 {
						t.Fatal("繁忙后未以原 ID 执行一次", got, x)
					}
				} else if got.State != protocol.Rejected || got.Error == nil || got.Error.Code != test.code || len(x.ids) != 1 || x.executions != 0 || x.reconciles != map[string]int{"invalid": 0, "transport": 1}[test.name] {
					t.Fatal("明确拒绝或未知结果处理错误", got, x)
				}
				if test.phase == "finished" && got.Error.Message != test.err.Error() {
					t.Fatal("明确拒绝未保留错误消息", got.Error)
				}
				if err = n.DB.QueryRow("SELECT phase,terminal FROM queue WHERE id=?", op.ID).Scan(&phase, &terminal); err != nil || !terminal || (test.phase == "finished" && phase != "finished") {
					t.Fatal("操作结束后队列仍可调度", phase, terminal, err)
				}
			})
		}
	}
}
