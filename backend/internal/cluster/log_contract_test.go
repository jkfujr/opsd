package cluster

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

type failedLogExecutor struct{ lostExecutor }

func (e *failedLogExecutor) Call(_ context.Context, value protocol.Payload) (protocol.Payload, error) {
	if _, ok := value.(*protocol.ExecutionLogsRead); ok {
		return nil, fmt.Errorf("日志文件读取失败")
	}
	return nil, fmt.Errorf("读取入口无效")
}

func TestLogCapabilityEmptyAndFailureDoNotChangeBusinessResult(t *testing.T) {
	n, _ := openTestNode(t)
	op, err := n.Submit(t.Context(), n.ID, Request{Key: "no-logs", Kind: "name", Payload: payload("新名称")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = n.dispatch(t.Context(), n.ID, "execution.logs", payload(protocol.ExecutionLogsRead{OperationID: op.ID, Limit: 65536}))
	var faultValue *Fault
	if !errors.As(err, &faultValue) || faultValue.Code != "logs_unsupported" {
		t.Fatal("不支持日志被表示为业务失败", err)
	}
	action := protocol.ShellAction{TaskID: "log-task", Name: "日志任务", Directory: "/", Script: "true"}
	op, err = n.Submit(t.Context(), n.ID, Request{Key: "with-logs", Kind: "task_run", Payload: payload(action)})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := n.dispatch(t.Context(), n.ID, "execution.logs", payload(protocol.ExecutionLogsRead{OperationID: op.ID, Limit: 65536}))
	if err != nil || len(empty.(protocol.ExecutionLogs).Data) != 0 {
		t.Fatal("尚未产生日志应返回空结果", empty, err)
	}
	op.State = protocol.Succeeded
	op.Revision++
	if err = n.saveOperation(t.Context(), &op, "finished"); err != nil {
		t.Fatal(err)
	}
	n.Executor = &failedLogExecutor{}
	_, err = n.dispatch(t.Context(), n.ID, "execution.logs", payload(protocol.ExecutionLogsRead{OperationID: op.ID, Limit: 65536}))
	if !errors.As(err, &faultValue) || faultValue.Code != "logs_read_failed" {
		t.Fatal("日志故障丢失读取语义", err)
	}
	read, err := n.GetOperation(t.Context(), op.ID)
	if err != nil || read.State != protocol.Succeeded {
		t.Fatal("日志故障改变了业务结果", read, err)
	}
}
