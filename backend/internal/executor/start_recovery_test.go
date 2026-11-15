package executor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

func TestAcceptedOperationRetriesOriginalUnitAfterStartFailure(t *testing.T) {
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewServices(db)
	s.Engine.Clock = func() (string, int64, error) { return "boot", 0, nil }
	var ids []string
	s.Start = func(_ context.Context, id string) error {
		ids = append(ids, id)
		if len(ids) == 1 {
			return errors.New("systemd 暂时不可用")
		}
		return nil
	}
	request := &protocol.OperationSubmit{OperationID: "original", ContractVersion: protocol.ContractVersion, Action: protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{TaskID: "job", Name: "任务", Script: "true", Directory: "/tmp"}}}
	if _, err = s.Handle(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	reply, err := s.Handle(t.Context(), &protocol.OperationQuery{OperationID: request.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	query := reply.(*protocol.OperationQueryResult)
	if !query.Found || query.Record.Error == nil || query.Record.Error.Code != "operation_start_failed" {
		t.Fatal("启动错误未持久保存", query)
	}
	if err = s.Recover(t.Context()); err != nil || len(ids) != 2 || ids[0] != ids[1] {
		t.Fatal("启动恢复改变原操作", ids, err)
	}
}
