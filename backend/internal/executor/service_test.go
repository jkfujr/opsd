package executor

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

func TestSubmitBusyDoesNotPersistOperation(t *testing.T) {
	db, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := operations.Store{DB: db}
	a := protocol.OperationAction{Kind: "component", Component: &protocol.ComponentAction{Component: "firewall", Action: "install", Backend: "ufw"}}
	request := &protocol.OperationSubmit{OperationID: "busy", ContractVersion: protocol.ContractVersion, Action: a}
	for _, action := range []struct {
		name  string
		value protocol.OperationAction
	}{
		{"operation_lock", a},
		{"task_lock", protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{TaskID: "locked-task", Name: "锁测试", Script: "true", Directory: "/tmp"}}},
	} {
		for _, test := range []struct {
			name, code string
			err        error
		}{
			{"busy", "not_accepted_busy", localstate.ErrBusy},
			{"wrapped_busy", "not_accepted_busy", fmt.Errorf("读取操作锁：%w", localstate.ErrBusy)},
			{"lock_error", "dependency_unavailable", errors.New("锁文件不可读")},
		} {
			t.Run(action.name+"/"+test.name, func(t *testing.T) {
				starts := 0
				taskID := ""
				s := &Services{Engine: &operations.Engine{Store: store, Clock: func() (string, int64, error) { return "boot", 0, nil }}, Start: func(context.Context, string) error {
					starts++
					return nil
				}, OperationLock: func() (func(), error) { return nil, test.err }, TaskLock: func(id string) (func(), error) {
					taskID = id
					return nil, test.err
				}}
				request := &protocol.OperationSubmit{OperationID: action.name + "-" + test.name, ContractVersion: protocol.ContractVersion, Action: action.value}
				if _, err := s.Handle(t.Context(), request); firewall.Code(err) != test.code || (test.code == "dependency_unavailable" && err != test.err) {
					t.Fatal("锁错误分类或原错误发生变化", err)
				}
				if _, err := store.Get(t.Context(), request.OperationID); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("锁失败的操作被保存", err)
				}
				body, err := protocol.Encode(protocol.Message{RequestID: "lock-request", Payload: request})
				if err != nil {
					t.Fatal(err)
				}
				reply := httptest.NewRecorder()
				(&Server{Services: s}).Handler().ServeHTTP(reply, httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body)))
				if reply.Code != http.StatusOK {
					t.Fatal("锁错误 RPC 应答失败", reply.Code, reply.Body.String())
				}
				message, err := protocol.Decode(reply.Body.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				failure, ok := message.Payload.(*protocol.RequestFailed)
				if !ok || failure.Error.Code != test.code || failure.Error.Message != test.err.Error() {
					t.Fatal("锁错误 RPC 分类或消息发生变化", message.Payload)
				}
				if _, err := store.Get(t.Context(), request.OperationID); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("锁失败的 RPC 操作被保存", err)
				}
				if starts != 0 || (action.value.Shell != nil && taskID != action.value.Shell.TaskID) {
					t.Fatal("锁失败后启动操作或任务锁标识不符", starts, taskID)
				}
			})
		}
	}
	s := &Services{Engine: &operations.Engine{Store: store, Clock: func() (string, int64, error) { return "boot", 0, nil }}, Start: func(context.Context, string) error {
		t.Fatal("繁忙操作被启动")
		return nil
	}, OperationLock: func() (func(), error) { return func() {}, nil }}
	if _, _, err = store.Receive(t.Context(), "other", protocol.ContractVersion, a, "boot"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Handle(t.Context(), request); firewall.Code(err) != "not_accepted_busy" {
		t.Fatal("已有写操作未返回未受理", err)
	}
	if _, err = store.Get(t.Context(), request.OperationID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("繁忙操作被保存", err)
	}
}

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
