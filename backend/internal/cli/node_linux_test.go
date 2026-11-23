package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/executor"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

type child struct {
	cmd     *exec.Cmd
	done    chan error
	log     bytes.Buffer
	stopped bool
}

func launch(t *testing.T, args ...string) *child {
	t.Helper()
	p := &child{cmd: exec.Command(args[0], args[1:]...), done: make(chan error, 1)}
	p.cmd.Env = append(os.Environ(), "GOMEMLIMIT=64MiB")
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	p.cmd.Stdout = &p.log
	p.cmd.Stderr = &p.log
	if e := p.cmd.Start(); e != nil {
		t.Fatal(e)
	}
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() { p.stop() })
	return p
}
func (p *child) stop() {
	if p.stopped {
		return
	}
	p.stopped = true
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
}
func waitLocal(t *testing.T, p *child, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		select {
		case e := <-p.done:
			p.stopped = true
			t.Fatalf("节点进程退出：%v %s", e, p.log.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
	p.stop()
	t.Fatalf("节点未就绪：%s", p.log.String())
}
func freeAddress(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := l.Addr().String()
	l.Close()
	return address
}
func TestRealConsoleLifecycleAndNodeRestart(t *testing.T) {
	binary := os.Getenv("OPSD_TEST_BINARY")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("未提供本地 Linux 节点程序")
	}
	dir := t.TempDir()
	if e := os.Chmod(filepath.Dir(dir), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(dir, 0755); e != nil {
		t.Fatal(e)
	}
	data := filepath.Join(dir, "node")
	execDir := filepath.Join(dir, "executor")
	if e := os.MkdirAll(execDir, 0700); e != nil {
		t.Fatal(e)
	}
	peer, web := freeAddress(t), freeAddress(t)
	n, e := cluster.Open(t.Context(), filepath.Join(data, "node.db"), cluster.Options{Config: cluster.Config{Listen: peer, Address: "http://" + peer, ConsoleListen: web}, Username: "admin", Password: "test-password", Entrance: "/signin"})
	if e != nil {
		t.Fatal(e)
	}
	public := append([]byte(nil), n.Key.Public...)
	if e = n.Close(); e != nil {
		t.Fatal(e)
	}
	files, e := os.ReadDir(data)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if e = os.Chown(filepath.Join(data, f.Name()), 65534, 65534); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.Chown(data, 65534, 65534); e != nil {
		t.Fatal(e)
	}
	// A1：集成测试以 nobody(65534) 启动节点，必须显式写入允许的调用方 UID，
	// 不能依赖按账号名推导（nobody 与生产账号名不同）。
	uid := 65534
	f := installation.NodeFile{DataDir: data, ExecutorDir: execDir, Socket: filepath.Join(dir, "executor.sock"), ControlSocket: filepath.Join(data, "control.sock"), Binary: binary, GroupID: 65534, UserID: &uid}
	config := filepath.Join(dir, "node.json")
	if e = localstate.Write(config, f, 0644); e != nil {
		t.Fatal(e)
	}
	root := launch(t, binary, "executor", "--config", config)
	waitLocal(t, root, func() bool {
		c, e := net.Dial("unix", f.Socket)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	node := launch(t, "runuser", "-u", "nobody", "--", binary, "node", "--config", config)
	waitLocal(t, node, func() bool {
		c, e := net.Dial("unix", f.ControlSocket)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	if c, e := net.DialTimeout("tcp", web, time.Second); e == nil {
		c.Close()
		t.Fatal("默认启动了控制台")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if output, e := exec.CommandContext(ctx, binary, "console", "--config", config, "--enabled=true").CombinedOutput(); e != nil {
		t.Fatalf("开启控制台失败：%v %s", e, output)
	}
	response, e := http.Get("http://" + web + "/signin")
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(body, []byte("OPSD")) {
		t.Fatal("控制台没有提供界面", response.StatusCode)
	}
	if output, e := exec.CommandContext(ctx, binary, "console", "--config", config, "--enabled=false").CombinedOutput(); e != nil {
		t.Fatalf("关闭控制台失败：%v %s", e, output)
	}
	if c, e := net.DialTimeout("tcp", web, time.Second); e == nil {
		c.Close()
		t.Fatal("控制台端口没有关闭")
	}
	if c, e := net.DialTimeout("tcp", peer, time.Second); e != nil {
		t.Fatal("关闭控制台同时停止了节点通信", e)
	} else {
		c.Close()
	}
	node.stop()
	node = launch(t, "runuser", "-u", "nobody", "--", binary, "node", "--config", config)
	waitLocal(t, node, func() bool {
		c, e := net.Dial("unix", f.ControlSocket)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	db, e := storage.ReadOnly(t.Context(), filepath.Join(data, "node.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var after []byte
	var raw string
	if e = db.QueryRow("SELECT public_key,config FROM node_identity WHERE id=1").Scan(&after, &raw); e != nil {
		t.Fatal(e)
	}
	var saved cluster.Config
	if e = json.Unmarshal([]byte(raw), &saved); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(public, after) || saved.Console {
		t.Fatal("重启后身份或控制台状态未保留")
	}
	var samples int
	if e = db.QueryRow("SELECT count(*) FROM monitoring_samples").Scan(&samples); e != nil || samples == 0 {
		t.Fatal("控制台关闭时未采集本机历史", samples, e)
	}
}

// TestExecutorDerivesAllowedCallerFromServiceAccount 覆盖 A1 的升级路径与真实拒绝路径：
// 配置没有 user_id（真实旧配置形态），执行器固定解析 opsd；
// opsd 能完成合法只读 RPC，同组其他 UID 与 root 都被拒绝，且拒绝之后服务仍可用。
//
// 目录权限：测试根目录、helper 与执行器目录都由 root 拥有，只给其他账号遍历与执行权限。
// 若把这些路径交给 opsd，root 再执行其中的程序就等于允许提权。
func TestExecutorDerivesAllowedCallerFromServiceAccount(t *testing.T) {
	binary := os.Getenv("OPSD_TEST_BINARY")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("未提供本地 Linux 节点程序")
	}
	created := false
	if _, err := user.Lookup("opsd"); err != nil {
		if output, err := runCommand("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "opsd"); err != nil {
			t.Skipf("无法创建 opsd 账号：%v %s", err, output)
		}
		created = true
	}
	if created {
		defer runCommand("userdel", "opsd")
	}
	account, err := user.Lookup("opsd")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid == 0 {
		t.Fatalf("opsd 账号 UID 不可用：%v %v", uid, err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	// root 拥有、其他账号可遍历；helper 与目录都不交给 opsd 写。
	if e := os.Chmod(filepath.Dir(dir), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(dir, 0755); e != nil {
		t.Fatal(e)
	}
	data := filepath.Join(dir, "node")
	execDir := filepath.Join(dir, "exec")
	for _, path := range []string{data, execDir} {
		if e := os.MkdirAll(path, 0755); e != nil {
			t.Fatal(e)
		}
	}
	// 不写 UserID，模拟旧配置；目录保持 root:root。
	f := installation.NodeFile{DataDir: data, ExecutorDir: execDir, Socket: filepath.Join(dir, "executor.sock"), ControlSocket: filepath.Join(data, "control.sock"), Binary: binary, GroupID: gid}
	config := filepath.Join(dir, "node.json")
	if e := localstate.Write(config, f, 0644); e != nil {
		t.Fatal(e)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe.test")
	if e := copyExecutable(self, probe); e != nil {
		t.Fatal(e)
	}
	probeEnv := []string{"OPSD_EXECUTOR_PROBE_SOCKET=" + f.Socket}

	root := launch(t, binary, "executor", "--config", config)
	waitLocal(t, root, func() bool {
		_, e := os.Stat(f.Socket)
		return e == nil
	})

	// 同组但不同 UID 的账号：拥有 Socket 的组权限，能连上，但必须被 A1 拒绝。
	intruder := "opsd-probe"
	intruderReady := false
	if output, err := runCommand("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "-g", "opsd", intruder); err == nil {
		intruderReady = true
		defer runCommand("userdel", intruder)
	} else {
		t.Logf("无法创建同组账号，跳过同组拒绝检查：%v %s", err, firstLine(output))
	}

	call := func(as string) (string, error) {
		return runCommandEnv(probeEnv, "runuser", "-u", as, "--", probe, "-test.run=TestHelperExecutorReadRPC", "-test.v")
	}
	mustSucceed := func(as, label string) {
		var last error
		for attempt := 0; attempt < 50; attempt++ {
			output, e := call(as)
			if e == nil && strings.Contains(output, "PASS") && !strings.Contains(output, "SKIP") {
				t.Logf("%s：只读 RPC 成功", label)
				return
			}
			if e == nil {
				last = fmt.Errorf("helper 未真正执行：%s", firstLine(output))
				break
			}
			last = fmt.Errorf("%v %s", e, firstLine(output))
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("%s：无法调用执行器：%v", label, last)
	}
	mustReject := func(as, label string) {
		output, e := call(as)
		if e == nil {
			t.Fatalf("%s 不应被放行，却完成了 RPC：%s", label, firstLine(output))
		}
		if !closedByPeer(output) {
			t.Fatalf("%s 的失败不是连接关闭：%s", label, firstLine(output))
		}
		t.Logf("%s 被连接关闭", label)
	}

	// 1) 允许账号能完成真实 RPC（缺 user_id 的旧配置）。
	mustSucceed("opsd", "opsd")
	// 2) 同组不同 UID 被拒。
	if intruderReady {
		mustReject(intruder, "同组其他 UID")
		// 3) 拒绝之后，允许账号必须仍能成功——证明拒绝没有打停服务。
		mustSucceed("opsd", "拒绝之后的 opsd")
	}
	// 4) root 也被拒。
	mustReject("root", "root")
}

// TestHelperExecutorReadRPC 是子进程入口：以指定账号连接执行器 socket，
// 发一条合法只读 RPC 并断言成功。
func TestHelperExecutorReadRPC(t *testing.T) {
	socket := os.Getenv("OPSD_EXECUTOR_PROBE_SOCKET")
	if socket == "" {
		t.Skip("helper")
	}
	client := executor.NewClient(socket)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Call(ctx, &protocol.ResourceMetricsRead{}); err != nil {
		t.Fatalf("只读 RPC 失败：%v", err)
	}
}

// copyExecutable 把测试二进制复制到 root 拥有、其他账号可执行的路径。
func copyExecutable(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0755)
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}

// closedByPeer 判断失败是否来自对端关闭连接（A1 的拒绝方式），
// 而不是子进程启动失败或缺依赖。
func closedByPeer(text string) bool {
	for _, marker := range []string{"EOF", "connection reset", "broken pipe", "connection closed"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func runCommand(name string, args ...string) (string, error) {
	return runCommandEnv(nil, name, args...)
}

// runCommandEnv 在继承父进程环境的基础上追加 env，用于把 socket 路径传给子进程。
func runCommandEnv(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}
