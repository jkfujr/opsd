package executor

import (
	"fmt"
	"net"
)

// CallerGuard 只放行来自指定 UID 的本地连接。
//
// 结构上包在 Accept 之外：每条连接单独判定，拒绝一条不影响后续连接。
// 拒绝时关闭连接并继续 Accept——绝不能把拒绝当成 Accept 错误返回，
// 那会让 http.Server.Serve 终止整个服务。
type CallerGuard struct {
	Listener net.Listener
	// Allowed 是唯一允许的本机 UID；0 表示配置非法，由 ResolveCallerUID 保证不出现。
	Allowed uint32
	// Logf 用于记录拒绝原因；为空时丢弃。
	Logf func(format string, args ...any)
	// credential 默认读取真实 SO_PEERCRED；测试可注入以覆盖拒绝路径，
	// 避免在监听期间改写 Allowed 造成数据竞态。
	credential func(net.Conn) (peerID, error)
}

// Accept 读取对端凭据，只把允许的连接交给上层。
func (g *CallerGuard) Accept() (net.Conn, error) {
	for {
		conn, err := g.Listener.Accept()
		if err != nil {
			return nil, err
		}
		credential, err := g.readCredential(conn)
		if err != nil {
			g.logf("执行器拒绝连接：无法读取对端凭据：%v", err)
			conn.Close()
			continue
		}
		if credential.uid != g.Allowed {
			// uid/gid/pid 只用于日志；准入只看 uid。
			g.logf("执行器拒绝连接：uid=%d gid=%d pid=%d，期望 uid=%d", credential.uid, credential.gid, credential.pid, g.Allowed)
			conn.Close()
			continue
		}
		return conn, nil
	}
}

// Close 关闭底层监听。
func (g *CallerGuard) Close() error { return g.Listener.Close() }

// Addr 返回底层监听地址。
func (g *CallerGuard) Addr() net.Addr { return g.Listener.Addr() }

func (g *CallerGuard) readCredential(conn net.Conn) (peerID, error) {
	if g.credential != nil {
		return g.credential(conn)
	}
	return peerCredential(conn)
}

func (g *CallerGuard) logf(format string, args ...any) {
	if g.Logf != nil {
		g.Logf(format, args...)
	}
}

type peerID struct {
	uid, gid, pid uint32
}

// ResolveCallerUID 把配置里的服务账号 UID 解析成可用的准入值。
//
// configured 为 nil 表示旧配置缺字段，按服务账号名推导；显式 0 视为非法配置。
func ResolveCallerUID(configured *int, serviceAccount string) (uint32, error) {
	if configured != nil {
		// 先按 int64 判定范围再转换：直接转 uint32 会截断，2^32 会变成 0 而放行 root。
		if int64(*configured) <= 0 || int64(*configured) > int64(^uint32(0)) {
			return 0, fmt.Errorf("配置的服务账号 UID 无效：%d", *configured)
		}
		return uint32(*configured), nil
	}
	uid, err := lookupServiceUID(serviceAccount)
	if err != nil {
		// 推导失败必须拒绝启动，不能降级为"不校验"。
		return 0, fmt.Errorf("无法解析服务账号 %s 的 UID：%w", serviceAccount, err)
	}
	// 账号库把 0 给 root；任何来源的 0 都不得成为准入值。
	if uid == 0 {
		return 0, fmt.Errorf("服务账号 %s 解析到 UID 0，拒绝以 root 作为调用方", serviceAccount)
	}
	return uid, nil
}