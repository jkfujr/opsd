//go:build linux

package executor

import (
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestCallerGuardRejectsOtherUIDAndKeepsServing 覆盖 A1 的两条硬要求：
// 同组其他 UID 被拒，且拒绝单条连接之后合法连接仍能建立。
//
// 用真实 Unix socket 与真实 SO_PEERCRED；当前进程 UID 作为"合法"身份，
// 把 Allowed 设成别的值即可模拟"另一个 UID 来连"。
func TestCallerGuardRejectsOtherUIDAndKeepsServing(t *testing.T) {
	probe := unixProbe(t)
	_, err := peerCredential(probe)
	probe.Close()
	if err != nil {
		t.Skip("当前平台无法读取对端凭据：", err)
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "executor.sock")
	base, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()

	uid := uint32(os.Getuid())
	var rejected atomic.Int32
	guard := &CallerGuard{
		Listener: base,
		Allowed:  uid,
		Logf: func(string, ...any) { rejected.Add(1) },
	}
	defer guard.Close()

	accepted := make(chan net.Conn, 2)
	go func() {
		for {
			conn, err := guard.Accept()
			if err != nil {
				return
			}
			accepted <- conn
		}
	}()

	// Allowed 已是本进程 UID：第一条连接应当被接受。
	first, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	select {
	case conn := <-accepted:
		conn.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("合法连接未被接受")
	}

	// 把 Allowed 改成另一个 UID，模拟"同组其他 UID 来连"：应被拒绝且不进入 accepted。
	guard.Allowed = uid + 1
	second, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	select {
	case conn := <-accepted:
		conn.Close()
		t.Fatal("非允许 UID 的连接被放行")
	case <-time.After(500 * time.Millisecond):
	}
	if rejected.Load() == 0 {
		t.Fatal("拒绝时没有记录日志")
	}
	// 拒绝发生在关闭连接后；对端应观察到连接被关闭。
	if _, err = second.Write([]byte("x")); err == nil {
		buffer := make([]byte, 1)
		second.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, readErr := second.Read(buffer); readErr == nil {
			t.Fatal("被拒连接仍可收发")
		}
	}
	second.Close()

	// 关键：拒绝之后服务仍然存活——恢复允许值，再连一次必须成功。
	guard.Allowed = uid
	third, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	select {
	case conn := <-accepted:
		conn.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("拒绝一条连接之后服务不再接受合法连接")
	}
}

// unixProbe 返回一个可读取凭据的本地连接，用于探测平台支持。
func unixProbe(t *testing.T) net.Conn {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "probe.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- nil
			return
		}
		done <- conn
	}()
	client, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-done
	if server == nil {
		t.Fatal("探测连接失败")
	}
	// 不能在这里关闭：调用方要用这条连接读取凭据。
	return server
}

// TestResolveCallerUID 覆盖配置解析的三种情况：显式值、显式 0、缺字段。
func TestResolveCallerUID(t *testing.T) {
	value := 1234
	got, err := ResolveCallerUID(&value, "opsd")
	if err != nil || got != 1234 {
		t.Fatalf("显式 UID 解析失败：%v %v", got, err)
	}
	zero := 0
	if _, err = ResolveCallerUID(&zero, "opsd"); err == nil {
		t.Fatal("显式 0 应被判为非法配置")
	}
	// 缺字段时按账号名推导；用一个几乎不可能存在的账号名确保走失败分支。
	if _, err = ResolveCallerUID(nil, "opsd-nonexistent-account"); err == nil {
		t.Fatal("推导失败时应拒绝启动，而不是放行")
	}
}