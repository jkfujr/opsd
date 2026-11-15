//go:build !linux && !windows

package localstate

import (
	"fmt"
	"os"
)

func lockFile(file *os.File, shared bool) (func(), error) {
	return nil, fmt.Errorf("当前平台不支持本机部署互斥")
}

func SyncDirectory(path string) error { return nil }
