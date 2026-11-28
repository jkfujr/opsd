package deploymentdir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

// Open 逐级持有目录句柄，仅允许指定的最终目录由数据库用户持有。
func Open(path string, mode WalkMode, owners ...uint32) (*Directory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, fmt.Errorf("请使用独立的数据库绝对目录")
	}
	root, err := os.Open("/")
	if err != nil {
		return nil, err
	}
	if err = checkOwner(root, "/", true); err != nil {
		root.Close()
		return nil, err
	}
	dir := &Directory{File: root, path: "/"}
	parts := strings.Split(path[1:], "/")
	var perm os.FileMode
	if mode == Create {
		perm = 0700
	}
	for i, name := range parts {
		var allowed []uint32
		if i == len(parts)-1 {
			allowed = owners
		}
		next, err := OpenDirectory(dir, name, perm, allowed...)
		if mode == ExistingParent && errors.Is(err, os.ErrNotExist) {
			return dir, nil
		}
		dir.Close()
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

func OpenDirectory(dir *Directory, name string, perm os.FileMode, owners ...uint32) (*Directory, error) {
	if !entryName(name) {
		return nil, fmt.Errorf("数据库目录名称无效")
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, err := unix.Openat(int(dir.Fd()), name, flags, 0)
	if errors.Is(err, unix.ENOENT) && perm != 0 {
		err = unix.Mkdirat(int(dir.Fd()), name, uint32(perm))
		if err == nil || errors.Is(err, unix.EEXIST) {
			fd, err = unix.Openat(int(dir.Fd()), name, flags, 0)
		}
	}
	path := filepath.Join(dir.path, name)
	if err != nil {
		return nil, fmt.Errorf("数据库目录包含链接、非目录对象或不可打开：%s：%w", path, err)
	}
	file, err := checkedFile(fd, path, true, owners)
	if err != nil {
		return nil, err
	}
	return &Directory{File: file, path: path}, nil
}

func OpenFile(dir *Directory, name string, flags int, owners ...uint32) (*os.File, error) {
	if !entryName(name) {
		return nil, fmt.Errorf("数据库文件名称无效")
	}
	path := filepath.Join(dir.path, name)
	fd, err := unix.Openat(int(dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: path, Err: err}
	}
	return checkedFile(fd, path, false, owners)
}

func checkedFile(fd int, path string, directory bool, owners []uint32) (*os.File, error) {
	// 打开的描述符负责 I/O，原路径仅用于报告校验错误。
	file := os.NewFile(uintptr(fd), "opsd-database")
	if err := checkOwner(file, path, directory, owners...); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func checkOwner(file *os.File, path string, directory bool, owners ...uint32) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	if st.Uid != 0 && !slices.Contains(owners, st.Uid) || st.Mode&022 != 0 {
		return fmt.Errorf("数据库路径的属主或写权限不安全：%s", path)
	}
	if !directory && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1) {
		return fmt.Errorf("数据库文件不是独立的普通文件：%s", path)
	}
	return nil
}

func entryName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name
}

func Replace(dir *Directory, from, to string) error {
	if !entryName(from) || !entryName(to) {
		return fmt.Errorf("数据库文件名称无效")
	}
	if err := unix.Renameat(int(dir.Fd()), from, int(dir.Fd()), to); err != nil {
		return err
	}
	return dir.Sync()
}

func Remove(dir *Directory, name string) error {
	if !entryName(name) {
		return fmt.Errorf("数据库文件名称无效")
	}
	return unix.Unlinkat(int(dir.Fd()), name, 0)
}

func FreeBytes(dir *Directory) (uint64, error) {
	var st unix.Statfs_t
	err := unix.Fstatfs(int(dir.Fd()), &st)
	return st.Bavail * uint64(st.Bsize), err
}
