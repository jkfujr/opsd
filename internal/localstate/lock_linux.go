package localstate

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

func lockFile(file *os.File) (func(), error) {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("已有安装、恢复或部署变更正在执行")
	}
	return func() { unix.Flock(int(file.Fd()), unix.LOCK_UN) }, nil
}

func SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
