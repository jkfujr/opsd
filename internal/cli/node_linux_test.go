package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/localstate"
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
	f := installation.NodeFile{DataDir: data, ExecutorDir: execDir, Socket: filepath.Join(dir, "executor.sock"), ControlSocket: filepath.Join(data, "control.sock"), Binary: binary, GroupID: 65534}
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
