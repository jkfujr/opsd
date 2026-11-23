package operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

// newPolicyEngine 构造带临时数据库与拒绝钩子的引擎。
func newPolicyEngine(t *testing.T, deny func(protocol.OperationAction) error) (*Engine, *Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	engine := &Engine{Store: Store{DB: db}, Clock: func() (string, int64, error) { return "boot", 1, nil }}
	if deny != nil {
		engine.SetDenyAction(deny)
	}
	return engine, &engine.Store
}

// TestRunRefusesUnstartedRestrictedAction 覆盖 A2 的执行期判定：
// 尚未开始的受限操作被拒绝，原因写入记录，且脚本没有执行。
func TestRunRefusesUnstartedRestrictedAction(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "executed")
	engine, store := newPolicyEngine(t, func(action protocol.OperationAction) error {
		if protocol.ActionRequiresArbitraryCode(action) {
			return errPolicyClosedForTest
		}
		return nil
	})
	engine.LogDirectory = t.TempDir()

	action := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{
		TaskID: "job-1", Name: "任务", Script: "touch " + marker, Directory: "/tmp/opsd-policy-test",
	}}
	ctx := context.Background()
	if _, _, err := store.Receive(ctx, "oprefuse", protocol.ContractVersion, action, ""); err != nil {
		t.Fatal(err)
	}

	// e.end 结案成功时返回 nil，因此用记录判断是否被拒，而不是看返回值。
	if err := engine.Run(ctx, "oprefuse"); err != nil {
		t.Fatalf("被拒操作应已结案：%v", err)
	}
	record, err := store.Get(ctx, "oprefuse")
	if err != nil {
		t.Fatal(err)
	}
	if record.Record.State != protocol.Failed {
		t.Fatalf("被拒操作应标记失败，实际 %v", record.Record.State)
	}
	// 失败原因记录在记录的 Error 里。
	if record.Record.Error == nil || !strings.Contains(record.Record.Error.Message, "策略") {
		t.Fatalf("拒绝原因未持久化：%+v", record.Record.Error)
	}
	if exists(marker) {
		t.Fatal("被拒绝的操作不应执行脚本")
	}
}

// TestRunAllowsCancelOfRestrictedAction 覆盖收尾优先：受限操作仍可被取消。
func TestRunAllowsCancelOfRestrictedAction(t *testing.T) {
	engine, store := newPolicyEngine(t, func(action protocol.OperationAction) error {
		if protocol.ActionRequiresArbitraryCode(action) {
			return errPolicyClosedForTest
		}
		return nil
	})
	engine.LogDirectory = t.TempDir()
	ctx := context.Background()
	action := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{
		TaskID: "job-2", Name: "任务", Script: "true", Directory: "/tmp/opsd-policy-test",
	}}
	received, _, err := store.Receive(ctx, "opcancel", protocol.ContractVersion, action, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Request(ctx, "opcancel", "cancel", received.Record.Revision); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(ctx, "opcancel"); err != nil {
		t.Fatalf("取消不应被策略拒绝：%v", err)
	}
	record, err := store.Get(ctx, "opcancel")
	if err != nil {
		t.Fatal(err)
	}
	if record.Record.State != protocol.Cancelled {
		t.Fatalf("取消后状态应为 cancelled，实际 %v", record.Record.State)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var errPolicyClosedForTest = fmt.Errorf("本机策略已关闭可执行代码能力")

// TestAttemptedDatabaseStepKeepsManualPath 覆盖 A2 的收尾语义。
//
// 必须是真实的 R2 部署载荷，并且 DatabaseState.Attempted 已为真，才会走到
// "读回之后、再次 Apply 之前"的策略判定；用 proxy_upsert 且 DatabaseState
// 为空会提前按 resolve 结案，那样即使删掉策略判定用例也会通过。
func TestAttemptedDatabaseStepKeepsManualPath(t *testing.T) {
	e, _, _ := operationFixture(t)
	runtime := databaseRuntimeFixture()
	runtime.stages = []string{"deploy_docker", "deploy_create"}
	e.Databases = runtime

	action := protocol.OperationAction{
		Kind: "database",
		Database: &protocol.DatabaseAction{
			Kind: "deploy_database",
			Deployment: &protocol.DatabaseDeployment{
				ID: "db-test", Kind: "galera", Image: "mariadb:11.4", Directory: "/var/lib/db-test",
				Address: "10.0.0.5", SQLPort: 3306, ReplicationPort: 4567, ISTPort: 4568, SSTPort: 4444,
				ClusterName: "db-test", ClusterID: "cluster-test", Bootstrap: true,
				Seeds: []string{"10.0.0.5:4567"}, SSTMethod: "mariabackup",
				SSTUser: "sstuser", SSTPassword: "sst-secret",
				Connection: protocol.DatabaseConnection{Kind: "galera", Address: "127.0.0.1:3306", Username: "opsd_admin", Password: "admin-secret"},
			},
		},
	}
	if !protocol.ActionRequiresArbitraryCode(action) {
		t.Fatal("该载荷应被判为可执行代码能力")
	}
	local := accept(t, e, "policy-db", action)

	// 构造"已尝试、结果待核对"：记录指纹并置 Attempted，使 recover 能通过核对。
	observed, err := runtime.Observe(t.Context(), *action.Database, "deploy_create")
	if err != nil {
		t.Fatal(err)
	}
	step := 0
	local.IntentStep = &step
	local.Record.State = protocol.AttentionRequired
	local.DatabaseState = &databaseState{
		Stages:    runtime.stages,
		Index:     1,
		Attempted: true,
		Before:    observed.Fingerprint,
		Source:    observed.Source,
	}
	if err := e.Store.Save(t.Context(), &local); err != nil {
		t.Fatal(err)
	}

	// 关闭任意代码能力。
	e.SetDenyAction(func(action protocol.OperationAction) error {
		if protocol.ActionRequiresArbitraryCode(action) {
			return errPolicyClosedForTest
		}
		return nil
	})
	before := runtime.writes["deploy_create"]

	// 人工选择"继续处理"：应被策略拒绝，但不能结案。
	if _, err := e.Store.Request(t.Context(), local.Record.ID, "recover", local.Record.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(t.Context(), local.Record.ID); err != nil {
		t.Fatalf("拒绝时应正常返回而不是报错：%v", err)
	}

	after, err := e.Store.Get(t.Context(), local.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Record.State != protocol.AttentionRequired {
		t.Fatalf("已尝试步骤应保留待核对，实际 %v", after.Record.State)
	}
	if after.Record.Error == nil || !strings.Contains(after.Record.Error.Message, "策略") {
		t.Fatalf("策略拒绝原因应记录：%+v", after.Record.Error)
	}
	if runtime.writes["deploy_create"] != before {
		t.Fatal("被拒绝时不应再次下发 Apply")
	}

	// 人工解决仍必须可用。
	if _, err := e.Store.Request(t.Context(), local.Record.ID, "resolve", after.Record.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(t.Context(), local.Record.ID); err != nil {
		t.Fatalf("人工解决应被受理：%v", err)
	}
	final, err := e.Store.Get(t.Context(), local.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Record.State != protocol.ResolvedManually {
		t.Fatalf("人工解决后应结束原操作，实际 %v", final.Record.State)
	}
}
