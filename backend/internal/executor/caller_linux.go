//go:build linux

package executor

import (
	"fmt"
	"net"
	"os/user"
	"strconv"

	"golang.org/x/sys/unix"
)

// peerCredential 读取内核在连接建立时记录的调用方凭据。
// SO_PEERCRED 由内核填写，调用方无法伪造。
func peerCredential(conn net.Conn) (peerID, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return peerID{}, fmt.Errorf("非 Unix 连接")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return peerID{}, err
	}
	var credential *unix.Ucred
	var readErr error
	if err = raw.Control(func(fd uintptr) {
		credential, readErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return peerID{}, err
	}
	if readErr != nil {
		return peerID{}, readErr
	}
	if credential == nil {
		return peerID{}, fmt.Errorf("未取得对端凭据")
	}
	return peerID{uid: credential.Uid, gid: credential.Gid, pid: uint32(credential.Pid)}, nil
}

// lookupServiceUID 按服务账号名解析 UID，用于旧配置缺字段时的推导。
func lookupServiceUID(name string) (uint32, error) {
	account, err := user.Lookup(name)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(value), nil
}