package localstate

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockFile(file *os.File) (func(), error) {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if err == unix.EWOULDBLOCK || err == unix.EAGAIN {
			return nil, ErrBusy
		}
		return nil, err
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
