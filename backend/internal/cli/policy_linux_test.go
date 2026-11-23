//go:build linux

package cli

import (
	"context"
	"fmt"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

// TestQueuedRestrictedOperationRefusedByPolicy 覆盖 A2 的真机验收：
// 一条已经受理、尚未开跑的 Shell 任务，在策略关闭后由真实 systemd 单元启动，
// 必须被拒绝、原因可读回，且脚本没有产生任何副作用。
//
// 需要 root、systemd 与 OPSD_TEST_BINARY；缺任一条即跳过。
func TestQueuedRestrictedOperationRefusedByPolicy(t *testing.T) {
	binary := os.Getenv("OPSD_TEST_BINARY")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("未提供本地 Linux 节点程序或非 root")
	}
	if output, err := runCommand("systemctl", "is-system-running"); err != nil && !strings.Contains(output, "degraded") {
		t.Skipf("systemd 不可用：%v %s", err, firstLine(output))
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "node")
	execDir := filepath.Join(dir, "exec")
	for _, path := range []string{data, execDir} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// 策略显式关闭任意代码能力。
	f := installation.NodeFile{
		DataDir: data, ExecutorDir: execDir,
		Socket:        filepath.Join(dir, "executor.sock"),
		ControlSocket: filepath.Join(data, "control.sock"),
		Binary:        binary,
		OperationPolicy: json.RawMessage(`{"arbitrary_code":false}`),
	}
	config := filepath.Join(dir, "node.json")
	if err := localstate.Write(config, f, 0644); err != nil {
		t.Fatal(err)
	}

	// 用正常持久化路径构造"已受理、未开跑"的 Shell 操作。
	marker := filepath.Join(dir, "executed")
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(execDir, "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	store := operations.Store{DB: db}
	action := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{
		TaskID: "policy-job", Name: "策略验收", Script: "touch " + marker, Directory: dir,
	}}
	// 每次运行用唯一 ID：单元文件与操作记录都不会与上一次或别人的残留冲突。
	id := fmt.Sprintf("policy%d", time.Now().UnixNano())
	if _, _, err := store.Receive(ctx, id, protocol.ContractVersion, action, ""); err != nil {
		t.Fatalf("构造已受理操作失败：%v", err)
	}
	db.Close()

	// 单元名是契约的一部分（operation 入口按 cgroup 名核对 opsd-operation@<id>.service），
	// 但内容不能沿用正式模板——模板里的 --config 指向现有安装，会把测试操作
	// 塞进真实实例。systemd 会先找具体实例文件、再找模板，因此这里在
	// /run/systemd/system 独占创建"本次 ID 的实例单元"，指向测试配置。
	units := installation.Units(f, config)
	instanceName := "opsd-operation@" + id
	instancePath := "/run/systemd/system/" + instanceName + ".service"
	if _, err := os.Stat(instancePath); err == nil {
		t.Fatalf("临时实例单元已存在：%s", instancePath)
	}
	if err := os.MkdirAll("/run/systemd/system", 0755); err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(units["opsd-operation@.service"], "%i", id)
	// 独占创建：文件已存在就失败，避免覆盖别人的实例单元。
	file, err := os.OpenFile(instancePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("无法独占创建实例单元：%v", err)
	}
	if _, err := file.WriteString(body); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runCommand("systemctl", "stop", instanceName+".service")
		os.Remove(instancePath)
		runCommand("systemctl", "daemon-reload")
	})
	if output, err := runCommand("systemctl", "daemon-reload"); err != nil {
		t.Fatalf("daemon-reload 失败：%v %s", err, firstLine(output))
	}
	// 启动前核对：systemd 解析到的确实是本实例单元，且 ExecStart 指向测试配置。
	fragment, err := runCommand("systemctl", "show", "-p", "FragmentPath", "--value", "opsd-operation@"+id+".service")
	if err != nil {
		t.Fatalf("无法查询单元来源：%v %s", err, firstLine(fragment))
	}
	if strings.TrimSpace(fragment) != instancePath {
		t.Fatalf("systemd 加载的不是本次实例单元：%q，期望 %q", strings.TrimSpace(fragment), instancePath)
	}
	execStart, err := runCommand("systemctl", "show", "-p", "ExecStart", "--value", "opsd-operation@"+id+".service")
	if err != nil || !strings.Contains(execStart, config) {
		t.Fatalf("单元未指向测试配置：%v %s", err, firstLine(execStart))
	}
	if output, err := runCommand("systemctl", "start", "opsd-operation@"+id+".service"); err != nil {
		t.Logf("单元启动返回：%v %s", err, firstLine(output))
	}

	// 轮询记录：必须在有限时间内结案为失败，并写明策略原因。
	deadline := time.Now().Add(30 * time.Second)
	var record protocol.OperationRecord
	for time.Now().Before(deadline) {
		db, err := storage.Open(ctx, filepath.Join(execDir, "executor.db"), storage.Executor)
		if err != nil {
			t.Fatal(err)
		}
		local, err := operations.Store{DB: db}.Get(ctx, id)
		db.Close()
		if err != nil {
			t.Fatal(err)
		}
		record = local.Record
		if record.State == protocol.Failed {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if record.State != protocol.Failed {
		t.Fatalf("已受理任务应被策略拒绝并结案，实际状态 %v", record.State)
	}
	if record.Error == nil || !strings.Contains(record.Error.Message, "策略") {
		t.Fatalf("拒绝原因未持久化：%+v", record.Error)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("被拒绝的任务不应执行脚本")
	}
}

// TestExecutorUnitNoLongerWantsRecovery 覆盖恢复语义：执行器单元不再拉起恢复单元，
// 但保留启动顺序；恢复单元自身仍随开机启动。
func TestExecutorUnitNoLongerWantsRecovery(t *testing.T) {
	units := installation.Units(installation.NodeFile{}, "/etc/opsd/instance.json")
	executor := units["opsd-executor.service"]
	if strings.Contains(executor, "Wants=opsd-recover.service") {
		t.Fatal("执行器单元不应再 Wants 恢复单元：日常重启会打断运行中的任务")
	}
	if !strings.Contains(executor, "After=network.target opsd-recover.service") {
		t.Fatal("执行器仍需在恢复之后启动，保留 After 顺序")
	}
	if !strings.Contains(units["opsd-recover.service"], "WantedBy=multi-user.target") {
		t.Fatal("恢复单元应保留开机启动")
	}
}

// installedInstance 读取已安装实例的配置（默认 /etc/opsd/instance.json）。
func installedInstance(t *testing.T) installation.NodeFile {
	t.Helper()
	config := os.Getenv("OPSD_INSTALLED_CONFIG")
	if config == "" {
		config = "/etc/opsd/instance.json"
	}
	f, err := installation.NodePaths(config)
	if err != nil {
		t.Skipf("没有可用安装实例：%v", err)
	}
	return f
}

// waitForState 轮询记录状态直到满足条件或超时。
func waitForState(t *testing.T, f installation.NodeFile, id string, want func(protocol.OperationRecord) bool, timeout time.Duration) protocol.OperationRecord {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	var record protocol.OperationRecord
	for time.Now().Before(deadline) {
		db, err := storage.Open(ctx, filepath.Join(f.ExecutorDir, "executor.db"), storage.Executor)
		if err != nil {
			t.Fatal(err)
		}
		local, err := operations.Store{DB: db}.Get(ctx, id)
		db.Close()
		if err != nil {
			t.Fatal(err)
		}
		record = local.Record
		if want(record) {
			return record
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("等待状态超时，最后状态 %v", record.State)
	return record
}

// acceptInstalledOperation 在已安装实例里受理一条 Shell 任务。
func acceptInstalledOperation(t *testing.T, f installation.NodeFile, script string) string {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(f.ExecutorDir, "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id := fmt.Sprintf("restart%d", time.Now().UnixNano())
	action := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{
		TaskID: "restart-job", Name: "重启验收", Script: script, Directory: "/tmp",
	}}
	if _, _, err := (operations.Store{DB: db}).Receive(ctx, id, protocol.ContractVersion, action, ""); err != nil {
		t.Fatalf("受理失败：%v", err)
	}
	return id
}

// TestInstalledExecutorRestartKeepsRunningOperation 在**已安装实例**上验收：
// 任务异步启动并确认处于运行中之后，重启真正的 opsd-executor.service，
// 任务仍应跑完，且期间没有拉起恢复单元。随后补一次重复安装，任务同样跑完。
func TestInstalledExecutorRestartKeepsRunningOperation(t *testing.T) {
	if os.Getenv("OPSD_INSTALLED_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("设置 OPSD_INSTALLED_TEST=1 且以 root 运行")
	}
	if output, err := runCommand("systemctl", "is-system-running"); err != nil && !strings.Contains(output, "degraded") {
		t.Skipf("systemd 不可用：%v %s", err, firstLine(output))
	}
	f := installedInstance(t)
	marker := "/tmp/opsd-restart-marker"
	os.Remove(marker)
	// 只处理本次创建记录：不清理、不结案实例里别人的操作。
	if pending, err := pendingOperations(t, f); err == nil && len(pending) > 0 {
		t.Skipf("实例存在未结束操作（%v），请在干净实例上运行", pending)
	}

	// 接受后由执行器自己的调度拉起操作单元（MaintainOperations）。
	// 不要再手动 systemctl start：那会并行启动第二个进程，
	// 它看到 IntentStep 已置位就会转成待人工核对。
	started := marker + ".started"
	os.Remove(started)
	id := acceptInstalledOperation(t, f, "touch "+started+"; sleep 6; touch "+marker)
	// 先确认它真的在运行，而不是已经结束——否则重启验收毫无意义。
	waitForState(t, f, id, func(r protocol.OperationRecord) bool {
		return r.State == protocol.Applying
	}, 30*time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("任务在重启之前就已完成，验收无效")
	}
	recoverBefore, _ := runCommand("systemctl", "show", "-p", "ActiveEnterTimestamp", "--value", "opsd-recover.service")

	// 重启真正的执行器单元。
	if output, err := runCommand("systemctl", "restart", "opsd-executor.service"); err != nil {
		t.Fatalf("重启执行器失败：%v %s", err, firstLine(output))
	}
	waitForState(t, f, id, func(r protocol.OperationRecord) bool {
		return r.State == protocol.Succeeded
	}, 60*time.Second)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("重启后任务未完成")
	}
	recoverAfter, _ := runCommand("systemctl", "show", "-p", "ActiveEnterTimestamp", "--value", "opsd-recover.service")
	if strings.TrimSpace(recoverBefore) != strings.TrimSpace(recoverAfter) {
		t.Fatal("重启执行器不应唤起恢复单元")
	}

	// 重复安装：正在运行的任务同样不能被中止。
	os.Remove(marker)
	os.Remove(started)
	second := acceptInstalledOperation(t, f, "touch "+started+"; sleep 6; touch "+marker)
	waitForState(t, f, second, func(r protocol.OperationRecord) bool {
		return r.State == protocol.Applying
	}, 30*time.Second)
	if output, err := runCommand(installedBinary(t, f), "install", "-n", "重复安装保留账号", "--address", "http://127.0.0.1:1"); err != nil {
		t.Fatalf("重复安装失败：%v %s", err, firstLine(output))
	}
	waitForState(t, f, second, func(r protocol.OperationRecord) bool {
		return r.State == protocol.Succeeded
	}, 60*time.Second)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("重复安装后任务未完成")
	}
}

func installedBinary(t *testing.T, f installation.NodeFile) string {
	t.Helper()
	if f.Binary != "" {
		if _, err := os.Stat(f.Binary); err == nil {
			return f.Binary
		}
	}
	return "/opt/opsd/bin/opsd"
}


// pendingOperations 只读地列出未结束操作，用于判断实例是否干净。
func pendingOperations(t *testing.T, f installation.NodeFile) ([]string, error) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(f.ExecutorDir, "executor.db"), storage.Executor)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return operations.Store{DB: db}.Pending(context.Background())
}
