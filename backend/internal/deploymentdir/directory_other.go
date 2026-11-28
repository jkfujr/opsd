//go:build !linux

package deploymentdir

import (
	"errors"
	"os"
)

var errPlatform = errors.New("数据库部署需要 Linux 节点")

func Open(string, WalkMode, ...uint32) (*Directory, error) {
	return nil, errPlatform
}

func OpenDirectory(*Directory, string, os.FileMode, ...uint32) (*Directory, error) {
	return nil, errPlatform
}

func OpenFile(*Directory, string, int, ...uint32) (*os.File, error) {
	return nil, errPlatform
}

func Replace(*Directory, string, string) error { return errPlatform }
func Remove(*Directory, string) error          { return errPlatform }
func FreeBytes(*Directory) (uint64, error)     { return 0, errPlatform }
