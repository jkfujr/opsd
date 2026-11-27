package installation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkfujr/opsd/internal/command"
	"golang.org/x/sys/unix"
)

func prepareNodeConfigDir(configPath string, create bool) error {
	path, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != filepath.Dir(command.DefaultInstance) {
		if create {
			return os.MkdirAll(dir, 0755)
		}
		return nil
	}
	gid, err := nodeGroupID()
	if err != nil {
		return err
	}
	if create {
		if err := os.Mkdir(dir, 0750); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if err := normalizeNodeConfigDir(dir, gid); err != nil {
		return fmt.Errorf("设置实例配置目录 %s 的权限：%w", dir, err)
	}
	return nil
}

func normalizeNodeConfigDir(dir string, gid int) error {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return err
	}
	if info.Uid != 0 || info.Mode&0022 != 0 {
		return fmt.Errorf("目录须为 root 所有且禁止组和其他用户写入")
	}
	if err := unix.Fchown(fd, 0, gid); err != nil {
		return err
	}
	if err := unix.Fchmod(fd, 0750); err != nil {
		return err
	}
	for _, name := range []string{"system.posix_acl_default", "system.posix_acl_access"} {
		if err := unix.Fremovexattr(fd, name); err != nil && !errors.Is(err, unix.ENODATA) && !errors.Is(err, unix.EOPNOTSUPP) {
			return err
		}
	}
	return nil
}
