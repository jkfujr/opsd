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

// TestAttemptedDatabaseStepKeepsManualPath 覆盖 A2 的收尾语义：
// 已尝试、结果待核对的部署步骤在策略关闭后被拒时，必须保留待核对状态与执行进度，
// 不能重复下发 Apply，人工解决仍然可受理。
func TestAttemptedDatabaseStepKeepsManualPath(t *testing.T) {
	e, _, _ := operationFixture(t)
	runtime := databaseRuntimeFixture()
	runtime.stages = []string{"deploy_docker", "deploy_create"}
	e.Databases = runtime
	local := accept(t, e, "policy-db", databaseAction())

	// 模拟"已尝试、结果未知"：记录进度并置 Attempted。
	step := 0
	local.IntentStep = &step
	local.Record.State = protocol.AttentionRequired
	local.ContainerState = nil
	if err := e.Store.Save(t.Context(), &local); err != nil {
		t.Fatal(err)
	}

	denied := true
	e.SetDenyAction(func(protocol.OperationAction) error {
		if denied {
			return errPolicyClosedForTest
		}
		return nil
	})
	before := runtime.writes["deploy_docker"]

	// 人工请求"接受现状"后重跑：应被策略拒绝，但保留待核对而不是结案。
	if _, err := e.Store.Request(t.Context(), local.Record.ID, "resolve", local.Record.Revision); err != nil {
		t.Fatal(err)
	}
	_ = e.Run(t.Context(), local.Record.ID)

	after, err := e.Store.Get(t.Context(), local.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Record.State == protocol.Failed {
		t.Fatal("已尝试的步骤不应被结案为失败，否则人工解决入口丢失")
	}
	if runtime.writes["deploy_docker"] != before {
		t.Fatal("被拒绝时不应再次下发 Apply")
	}
}
