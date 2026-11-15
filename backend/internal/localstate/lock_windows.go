package localstate

import (
	"golang.org/x/sys/windows"
	"os"
)

func lockFile(file *os.File, shared bool) (func(), error) {
	overlapped := &windows.Overlapped{}
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if !shared {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, overlapped); err != nil {
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, ErrBusy
		}
		return nil, err
	}
	return func() { windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped) }, nil
}

func SyncDirectory(path string) error { return nil }
