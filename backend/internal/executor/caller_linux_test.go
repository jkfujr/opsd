//go:build linux

package executor

import (
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// newTestGuard 用注入的凭据读取函数构造 guard，避免在监听期间改写 Allowed
// （那会与 Accept goroutine 形成数据竞态）。
func newTestGuard(t *testing.T, allowed uint32, credentials func(net.Conn) (peerID, error), rejected *atomic.Int32) (*CallerGuard, string) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "executor.sock")
	base, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	guard := &CallerGuard{
		Listener:     base,
		Allowed:      allowed,
		credential:   credentials,
		Logf:         func(string, ...any) { rejected.Add(1) },
	}
	t.Cleanup(func() { guard.Close() })
	return guard, socket
}

// TestCallerGuardRejectsOtherUIDAndKeepsServing 覆盖 A1 的硬要求：
// 非允许 UID 被拒、且拒绝之后合法连接仍能建立。
//
// 凭据通过注入给出，因此不依赖特权，也不在运行时改写 Allowed。
func TestCallerGuardRejectsOtherUIDAndKeepsServing(t *testing.T) {
	const allowed = uint32(4242)
	var rejected atomic.Int32
	var calls atomic.Int32
	credentials := func(net.Conn) (peerID, error) {
		// 第一次返回别的 UID，之后返回允许的 UID。
		if calls.Add(1) == 1 {
			return peerID{uid: allowed + 1, gid: allowed, pid: 99}, nil
		}
		return peerID{uid: allowed, pid: 100}, nil
	}
	guard, socket := newTestGuard(t, allowed, credentials, &rejected)

	accepted := make(chan net.Conn, 4)
	serveErr := make(chan error, 1)
	go func() {
		for {
			conn, err := guard.Accept()
			if err != nil {
				serveErr <- err
				return
			}
			accepted <- conn
		}
	}()

	// 第一条：凭据不是允许 UID，应被关闭且不进入 accepted。
	first, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case conn := <-accepted:
		conn.Close()
		t.Fatal("非允许 UID 的连接被放行")
	case <-time.After(500 * time.Millisecond):
	}
	first.Close()
	if rejected.Load() == 0 {
		t.Fatal("拒绝时没有记录日志")
	}

	// 第二条：允许 UID，必须成功——证明拒绝没有把 Accept 循环打停。
	second, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	select {
	case conn := <-accepted:
		conn.Close()
	case err := <-serveErr:
		t.Fatalf("拒绝一条连接之后 Accept 退出：%v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("拒绝一条连接之后不再接受合法连接")
	}
}

// TestCallerGuardUsesRealCredentials 用真实 SO_PEERCRED 验证准入：
// 只有配置的 UID 能连，其他 UID（含 root）被拒。
func TestCallerGuardUsesRealCredentials(t *testing.T) {
	probe := unixProbe(t)
	_, err := peerCredential(probe)
	probe.Close()
	if err != nil {
		t.Skip("当前平台无法读取对端凭据：", err)
	}
	self := uint32(os.Getuid())
	var rejected atomic.Int32

	// 允许值设成"别的 UID"：本进程连接必须被拒。
	guard, socket := newTestGuard(t, self+1, peerCredential, &rejected)
	accepted := make(chan net.Conn, 2)
	go func() {
		if conn, err := guard.Accept(); err == nil {
			accepted <- conn
		}
	}()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case c := <-accepted:
		c.Close()
		t.Fatal("UID 不匹配时仍被放行")
	case <-time.After(2 * time.Second):
	}
	if rejected.Load() == 0 {
		t.Fatal("真实凭据拒绝时没有记录日志")
	}
}

// TestResolveCallerUID 覆盖三种来源：显式值、显式 0/越界、缺字段。
func TestResolveCallerUID(t *testing.T) {
	value := 1234
	if got, err := ResolveCallerUID(&value, "opsd"); err != nil || got != 1234 {
		t.Fatalf("显式 UID 解析失败：%v %v", got, err)
	}
	for _, invalid := range []int{0, -1, int(int64(^uint32(0)) + 1)} {
		bad := invalid
		if _, err := ResolveCallerUID(&bad, "opsd"); err == nil {
			t.Fatalf("非法 UID %d 未被拒绝（可能被截断成 0 而放行 root）", invalid)
		}
	}
	// 缺字段：用真实存在的非 root 账号验证成功路径（root 下 user.Current() 会是 0，
	// 不能拿它当成功样本）。
	if _, err := user.Lookup("nobody"); err != nil {
		t.Skip("缺少 nobody 账号，无法验证推导成功路径")
	}
	if got, err := ResolveCallerUID(nil, "nobody"); err != nil || got == 0 {
		t.Fatalf("按账号名推导失败：%v %v", got, err)
	}
	if _, err := ResolveCallerUID(nil, "root"); err == nil {
		t.Fatal("解析到 UID 0 时应拒绝，不能把 root 当调用方")
	}
	if _, err := ResolveCallerUID(nil, "opsd-nonexistent-account"); err == nil {
		t.Fatal("推导失败时应拒绝启动，而不是放行")
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