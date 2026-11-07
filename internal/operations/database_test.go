package operations

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

type databaseRuntime struct {
	goals      map[string]bool
	writes     map[string]int
	source     string
	lost       bool
	notApplied bool
	conflict   bool
	interrupt  func()
}

func (d *databaseRuntime) Prepare(context.Context, protocol.DatabaseAction) ([]string, error) {
	return []string{"memory", "runtime", "disk"}, nil
}
func (d *databaseRuntime) Observe(_ context.Context, _ protocol.DatabaseAction, stage string) (databaseengine.Observation, error) {
	source := d.source
	if d.conflict && stage == "runtime" {
		source = "external"
	}
	return databaseengine.Observation{Source: source, Fingerprint: fmt.Sprintf("%s/%v", source, d.goals), Satisfied: d.goals[stage], CheckedAt: time.Now().UTC()}, nil
}
func (d *databaseRuntime) Apply(_ context.Context, _ protocol.DatabaseAction, stage string) error {
	d.writes[stage]++
	if !d.notApplied {
		d.goals[stage] = true
		if stage == "memory" {
			d.source = "after"
		}
	}
	if d.interrupt != nil {
		d.interrupt()
	}
	if d.lost || d.notApplied {
		return errors.New("数据库应答丢失")
	}
	return nil
}
func databaseAction() protocol.OperationAction {
	return protocol.OperationAction{Kind: "database", Database: &protocol.DatabaseAction{Connection: protocol.DatabaseConnection{Kind: "proxysql", Address: "127.0.0.1:6032", Username: "opsd_admin"}, Kind: "proxy_upsert", User: "business", Host: "%", PasswordHash: databaseengine.NativePassword("test-password")}}
}
func databaseRuntimeFixture() *databaseRuntime {
	return &databaseRuntime{goals: map[string]bool{}, writes: map[string]int{}, source: "before"}
}

func TestDatabaseInterruptedWriteContinuesFromObservedStage(t *testing.T) {
	e, _, _ := operationFixture(t)
	runtime := databaseRuntimeFixture()
	e.Databases = runtime
	accept(t, e, "database-interrupted", databaseAction())
	ctx, cancel := context.WithCancel(t.Context())
	runtime.interrupt = cancel
	if err := e.Run(ctx, "database-interrupted"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	local, err := e.Store.Get(t.Context(), "database-interrupted")
	if err != nil || local.DatabaseState == nil || !local.DatabaseState.Attempted || runtime.writes["memory"] != 1 {
		t.Fatal("未保存数据库发送意图", err)
	}
	var path string
	if err = e.Store.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	e.Store.DB.Close()
	db, err := storage.Open(t.Context(), path, storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	runtime.interrupt = nil
	restarted := &Engine{Store: Store{DB: db}, Clock: e.Clock, Databases: runtime}
	if err = restarted.Run(t.Context(), "database-interrupted"); err != nil {
		t.Fatal(err)
	}
	local, err = restarted.Store.Get(t.Context(), "database-interrupted")
	if err != nil || local.Record.State != protocol.Succeeded || runtime.writes["memory"] != 1 || runtime.writes["runtime"] != 1 || runtime.writes["disk"] != 1 {
		t.Fatal("中断后的阶段被重复执行或未完成", runtime.writes, err)
	}
}
func TestDatabaseUnknownResultWaitsForExplicitRecover(t *testing.T) {
	e, _, _ := operationFixture(t)
	runtime := databaseRuntimeFixture()
	runtime.notApplied = true
	e.Databases = runtime
	accept(t, e, "database-unknown", databaseAction())
	if err := e.Run(t.Context(), "database-unknown"); err != nil {
		t.Fatal(err)
	}
	local, _ := e.Store.Get(t.Context(), "database-unknown")
	if local.Record.State != protocol.AttentionRequired {
		t.Fatal(local.Record.State)
	}
	for range 3 {
		if err := e.Run(t.Context(), local.Record.ID); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.writes["memory"] != 1 {
		t.Fatal("自动重发了未知数据库步骤")
	}
	runtime.notApplied = false
	if _, err := e.Store.Request(t.Context(), local.Record.ID, "recover", local.Record.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(t.Context(), local.Record.ID); err != nil {
		t.Fatal(err)
	}
	local, _ = e.Store.Get(t.Context(), local.Record.ID)
	if local.Record.State != protocol.Succeeded || runtime.writes["memory"] != 2 || runtime.writes["runtime"] != 1 || runtime.writes["disk"] != 1 {
		t.Fatal("原操作未继续剩余步骤", runtime.writes)
	}
}
func TestDatabaseLostReplyAndProxyExternalChange(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprint(external), func(t *testing.T) {
			e, _, _ := operationFixture(t)
			runtime := databaseRuntimeFixture()
			runtime.lost = true
			runtime.conflict = external
			e.Databases = runtime
			accept(t, e, "database-reply", databaseAction())
			if err := e.Run(t.Context(), "database-reply"); err != nil {
				t.Fatal(err)
			}
			local, _ := e.Store.Get(t.Context(), "database-reply")
			if !external {
				if local.Record.State != protocol.Succeeded || runtime.writes["memory"] != 1 || runtime.writes["runtime"] != 1 || runtime.writes["disk"] != 1 {
					t.Fatal("应答丢失后未按实际状态推进", runtime.writes)
				}
				return
			}
			if local.Record.State != protocol.AttentionRequired || runtime.writes["runtime"] != 0 {
				t.Fatal("加载了外部待应用配置")
			}
			runtime.conflict = false
			if _, err := e.Store.Request(t.Context(), local.Record.ID, "recover", local.Record.Revision); err != nil {
				t.Fatal(err)
			}
			if err := e.Run(t.Context(), local.Record.ID); err != nil {
				t.Fatal(err)
			}
			local, _ = e.Store.Get(t.Context(), local.Record.ID)
			if local.Record.State != protocol.Succeeded || runtime.writes["memory"] != 1 {
				t.Fatal("重做了已完成代理阶段")
			}
		})
	}
}
func TestDatabaseActionRejectsRemoteAndUntypedSQL(t *testing.T) {
	for _, change := range []func(*protocol.DatabaseAction){func(a *protocol.DatabaseAction) { a.Connection.Address = "192.0.2.1:3306" }, func(a *protocol.DatabaseAction) { a.Kind = "sql" }, func(a *protocol.DatabaseAction) { a.User = "root" }, func(a *protocol.DatabaseAction) { a.PasswordHash = "plain" }} {
		action := databaseAction()
		change(action.Database)
		if _, err := protocol.Encode(protocol.Message{RequestID: "validation", Payload: &protocol.OperationSubmit{OperationID: filepath.Base("typed-database"), ContractVersion: protocol.Version, Action: action}}); err == nil {
			t.Fatal("接受了范围外的数据库动作")
		}
	}
}
