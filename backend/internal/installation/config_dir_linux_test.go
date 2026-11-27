package installation

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNormalizeNodeConfigDir(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("需要 root 验证目录属组与 ACL")
	}
	const gid = 65534
	dir := t.TempDir()
	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		t.Helper()
		require(normalizeNodeConfigDir(dir, gid))
		var info unix.Stat_t
		require(unix.Stat(dir, &info))
		if info.Uid != 0 || info.Gid != gid || info.Mode&07777 != 0750 {
			t.Fatalf("目录权限不符：%d:%d %04o", info.Uid, info.Gid, info.Mode&07777)
		}
	}
	check()

	// 模拟 root:root 目录通过扩展 ACL 给服务组保留读和遍历权限。
	acl := binary.LittleEndian.AppendUint32(nil, 2)
	for _, item := range []struct {
		Tag, Perm uint16
		ID        uint32
	}{{1, 7, ^uint32(0)}, {4, 0, ^uint32(0)}, {8, 5, gid}, {16, 5, ^uint32(0)}, {32, 0, ^uint32(0)}} {
		acl = binary.LittleEndian.AppendUint16(acl, item.Tag)
		acl = binary.LittleEndian.AppendUint16(acl, item.Perm)
		acl = binary.LittleEndian.AppendUint32(acl, item.ID)
	}
	require(os.Chown(dir, 0, 0))
	for _, name := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		require(unix.Setxattr(dir, name, acl, 0))
	}
	check()
	check()
	for _, name := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		if _, err := unix.Getxattr(dir, name, nil); !errors.Is(err, unix.ENODATA) {
			t.Fatalf("目录 ACL 仍存在或无法读取：%s %v", name, err)
		}
	}
	link := filepath.Join(t.TempDir(), "config")
	require(os.Symlink(dir, link))
	if normalizeNodeConfigDir(link, gid) == nil {
		t.Fatal("接受了配置目录符号链接")
	}
	require(os.Chmod(dir, 0770))
	if normalizeNodeConfigDir(dir, gid) == nil {
		t.Fatal("接受了组可写的配置目录")
	}
}
