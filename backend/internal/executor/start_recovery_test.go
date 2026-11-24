package executor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

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

func TestPreparedOperationReportsStallUntilProgress(t *testing.T) {
	ctx := t.Context()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewServices(db)
	s.Start = func(context.Context, string) error { return nil }
	action := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{TaskID: "job", Name: "任务", Script: "true", Directory: "/tmp"}}
	local, _, err := s.Engine.Store.Receive(ctx, "stalled", protocol.ContractVersion, action, "boot")
	if err != nil {
		t.Fatal(err)
	}
	var previous map[preparedRevision]time.Time
	base := time.Now()
	tick := func(elapsed time.Duration, wantError bool) {
		t.Helper()
		previous, err = s.recoverOperations(ctx, previous, func() time.Time { return base.Add(elapsed) })
		if err != nil {
			t.Fatal(err)
		}
		reply, err := s.Handle(ctx, &protocol.OperationQuery{OperationID: local.Record.ID})
		if err != nil {
			t.Fatal(err)
		}
		record := reply.(*protocol.OperationQueryResult).Record
		if record == nil || (record.Error != nil) != wantError {
			t.Fatalf("经过 %v，预期启动错误=%t，记录=%+v", elapsed, wantError, record)
		}
		if wantError && (record.Error.Code != "operation_start_failed" || record.Error.Message == "") {
			t.Fatal(record.Error)
		}
	}
	tick(0, false)
	tick(operationStartTimeout-time.Nanosecond, false)
	tick(operationStartTimeout, true)
	tick(operationStartTimeout+5*time.Second, true)

	stale := local.Record
	if err := s.Engine.Store.Save(ctx, &local); err != nil {
		t.Fatal(err)
	}
	if err := s.startOperation(ctx, stale, true); err != nil {
		t.Fatal(err)
	}
	tick(2*operationStartTimeout, false)
	local.Record.State = protocol.Applying
	if err := s.Engine.Store.Save(ctx, &local); err != nil {
		t.Fatal(err)
	}
	tick(3*operationStartTimeout, false)
	tick(4*operationStartTimeout, false)
}
