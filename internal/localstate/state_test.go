//go:build linux || windows

package localstate

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLockReportsBusyAndCanBeReacquired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if unlock != nil {
			unlock()
		}
	})
	other, err := Lock(path)
	if other != nil {
		other()
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatal("重复取得本机锁未返回繁忙状态", err)
	}
	unlock()
	unlock = nil
	again, err := Lock(path)
	if err != nil {
		t.Fatal("释放后无法重新取得本机锁", err)
	}
	again()
}
