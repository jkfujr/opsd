package localstate

import (
	"golang.org/x/sys/windows"
	"os"
)

func lockFile(file *os.File) (func(), error) {
	overlapped := &windows.Overlapped{}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped); err != nil {
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, ErrBusy
		}
		return nil, err
	}
	return func() { windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped) }, nil
}

func SyncDirectory(path string) error { return nil }
