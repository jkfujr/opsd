//go:build !linux

package deploymentdir

import (
	"errors"
	"os"
)

var errPlatform = errors.New("数据库部署需要 Linux 节点")

func Open(string, WalkMode, ...uint32) (*os.File, error) {
	return nil, errPlatform
}

func OpenDirectory(*os.File, string, os.FileMode, ...uint32) (*os.File, error) {
	return nil, errPlatform
}

func OpenFile(*os.File, string, int, ...uint32) (*os.File, error) {
	return nil, errPlatform
}

func Replace(*os.File, string, string) error { return errPlatform }
func Remove(*os.File, string) error          { return errPlatform }
func FreeBytes(*os.File) (uint64, error)     { return 0, errPlatform }
