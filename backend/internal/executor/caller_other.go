//go:build !linux

package executor

import (
	"fmt"
	"net"
)

// peerCredential 在非 Linux 平台不可用：没有 SO_PEERCRED。
// 部署目标只有 Linux，这里一律返回错误，让调用方拒绝连接而不是放行。
func peerCredential(net.Conn) (peerID, error) {
	return peerID{}, fmt.Errorf("当前平台不支持 SO_PEERCRED")
}

// lookupServiceUID 在非 Linux 平台不支持按服务账号名推导。
func lookupServiceUID(string) (uint32, error) {
	return 0, fmt.Errorf("当前平台不支持按服务账号名解析 UID")
}