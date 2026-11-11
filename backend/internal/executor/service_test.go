package executor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

func TestFirewallInspectionWaitsForBusyLockAndKeepsOtherErrors(t *testing.T) {
	attempts := 0
	released := false
	s := &Services{OperationLock: func() (func(), error) {
		attempts++
		if attempts < 3 {
			return nil, localstate.ErrBusy
		}
		return func() { released = true }, nil
	}}
	unlock, err := s.inspectionLock(t.Context())
	if err != nil || attempts != 3 || released {
		t.Fatal("读取未等待本机写锁释放", attempts, err)
	}
	unlock()
	if !released {
		t.Fatal("读取锁未释放")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s.OperationLock = func() (func(), error) { return nil, localstate.ErrBusy }
	if _, err = s.inspectionLock(ctx); firewall.Code(err) != "node_busy" {
		t.Fatal("等待结束未返回繁忙状态", err)
	}
	want := errors.New("锁文件读取失败")
	s.OperationLock = func() (func(), error) { return nil, want }
	if _, err = s.inspectionLock(t.Context()); !errors.Is(err, want) {
		t.Fatal("实际锁错误被改成繁忙状态", err)
	}
}

func TestManualRequestPersistsBeforeReplacingExitingWorker(t *testing.T) {
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "agent.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := operations.Store{DB: db}
	a := protocol.OperationAction{Kind: "component", Component: &protocol.ComponentAction{Component: "firewall", Action: "install", Backend: "ufw"}}
	l, _, err := store.Receive(t.Context(), "manual", protocol.ContractVersion, a, "boot")
	if err != nil {
		t.Fatal(err)
	}
	l.Record.State = protocol.AttentionRequired
	if err = store.Save(t.Context(), &l); err != nil {
		t.Fatal(err)
	}
	restarts := 0
	s := &Services{Engine: &operations.Engine{Store: store}, Start: func(context.Context, string) error {
		t.Fatal("人工处理不能仅 start 一个即将退出的旧进程")
		return nil
	}, Restart: func(ctx context.Context, id string) error {
		restarts++
		saved, e := store.Get(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if saved.RequestRevision != l.Record.Revision || (saved.Request != "recover" && saved.Request != "resolve") {
			t.Fatal("替换执行进程前必须先持久登记请求")
		}
		return nil
	}}
	if _, err = s.Handle(t.Context(), &protocol.OperationRecover{OperationID: "manual", ExpectedRevision: l.Record.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Handle(t.Context(), &protocol.OperationResolve{OperationID: "manual", ExpectedRevision: l.Record.Revision, AcceptCurrent: true}); err != nil {
		t.Fatal(err)
	}
	if restarts != 2 {
		t.Fatalf("人工处理未替换执行进程：%d", restarts)
	}
	if _, err = s.Handle(t.Context(), &protocol.OperationRecover{OperationID: "manual", ExpectedRevision: l.Record.Revision - 1}); firewall.Code(err) != "conflict" || restarts != 2 {
		t.Fatal("旧修订不得启动执行进程")
	}
}
