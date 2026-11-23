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

// TestExecutorRestartKeepsRunningOperation 覆盖 A2 的恢复语义验收：
// 一个正在运行的 Shell 任务，在执行器进程重启之后仍能跑完；重启不会拉起
// 恢复单元去停掉操作单元。
func TestExecutorRestartKeepsRunningOperation(t *testing.T) {
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
	f := installation.NodeFile{
		DataDir: data, ExecutorDir: execDir,
		Socket:        filepath.Join(dir, "executor.sock"),
		ControlSocket: filepath.Join(data, "control.sock"),
		Binary:        binary,
		OperationPolicy: json.RawMessage(`{"arbitrary_code":true}`),
	}
	config := filepath.Join(dir, "node.json")
	if err := localstate.Write(config, f, 0644); err != nil {
		t.Fatal(err)
	}

	// 受理一条会跑一段时间的任务。
	marker := filepath.Join(dir, "done")
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(execDir, "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	store := operations.Store{DB: db}
	action := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{
		TaskID: "restart-job", Name: "重启验收", Script: "sleep 3; touch " + marker, Directory: dir,
	}}
	id := fmt.Sprintf("restart%d", time.Now().UnixNano())
	if _, _, err := store.Receive(ctx, id, protocol.ContractVersion, action, ""); err != nil {
		t.Fatalf("受理失败：%v", err)
	}
	db.Close()

	// 单元：本次唯一的实例单元，指向测试配置。
	units := installation.Units(f, config)
	instanceName := "opsd-operation@" + id
	instancePath := "/run/systemd/system/" + instanceName + ".service"
	body := strings.ReplaceAll(units["opsd-operation@.service"], "%i", id)
	file, err := os.OpenFile(instancePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("无法独占创建实例单元：%v", err)
	}
	if _, err := file.WriteString(body); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	t.Cleanup(func() {
		runCommand("systemctl", "stop", instanceName+".service")
		os.Remove(instancePath)
		runCommand("systemctl", "daemon-reload")
	})
	if output, err := runCommand("systemctl", "daemon-reload"); err != nil {
		t.Fatalf("daemon-reload 失败：%v %s", err, firstLine(output))
	}

	// 启动执行器（进程级，不装单元）并跑任务。
	executor := launch(t, binary, "executor", "--config", config)
	waitLocal(t, executor, func() bool {
		_, err := os.Stat(f.Socket)
		return err == nil
	})
	if output, err := runCommand("systemctl", "start", instanceName+".service"); err != nil {
		t.Logf("单元启动返回：%v %s", err, firstLine(output))
	}

	// 任务已经在跑时重启执行器：任务必须跑完，不被中断。
	time.Sleep(300 * time.Millisecond)
	executor.stop()
	restarted := launch(t, binary, "executor", "--config", config)
	waitLocal(t, restarted, func() bool {
		_, err := os.Stat(f.Socket)
		return err == nil
	})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return // 任务跑完了
		}
		time.Sleep(200 * time.Millisecond)
	}
	db, err = storage.Open(ctx, filepath.Join(execDir, "executor.db"), storage.Executor)
	if err != nil {
		t.Fatal(err)
	}
	local, err := operations.Store{DB: db}.Get(ctx, id)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("执行器重启后任务未完成，状态 %v", local.Record.State)
}

// TestInstallUnitsForBootCheck 把当前构建的单元写进 /etc/systemd/system 并启用，
// 供一次真实的启动顺序验收使用（wsl --terminate 后观察 journal）。
// 由 OPSD_BOOT_CHECK=1 显式触发，避免在普通测试里改动宿主。
func TestInstallUnitsForBootCheck(t *testing.T) {
	if os.Getenv("OPSD_BOOT_CHECK") != "1" || os.Geteuid() != 0 {
		t.Skip("设置 OPSD_BOOT_CHECK=1 且以 root 运行")
	}
	binary := os.Getenv("OPSD_TEST_BINARY")
	if binary == "" {
		t.Skip("未提供本地 Linux 节点程序")
	}
	// 必须用持久目录：重启会清空 /tmp，配置与二进制都得留在原处，
	// 否则开机验收会退化成"文件不存在"的假失败。
	dir := "/var/lib/opsd-bootcheck"
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "node")
	execDir := filepath.Join(dir, "exec")
	for _, path := range []string{data, execDir} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
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
	for name, body := range installation.Units(f, config) {
		if err := os.WriteFile(filepath.Join("/etc/systemd/system", name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runCommand("systemctl", "daemon-reload"); err != nil {
		t.Fatalf("daemon-reload 失败：%v %s", err, firstLine(output))
	}
	// 只启用不启动：启动动作交给一次真实的发行版启动来完成。
	if output, err := runCommand("systemctl", "enable", "opsd-recover.service", "opsd-executor.service"); err != nil {
		t.Fatalf("enable 失败：%v %s", err, firstLine(output))
	}
	// 写一行标记，供重启后确认配置路径没变。
	if err := os.WriteFile("/tmp/opsd-boot-check.json", []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("单元已安装并启用，配置：%s", config)
}
