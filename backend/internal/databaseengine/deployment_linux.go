//go:build linux

package databaseengine

import "golang.org/x/sys/unix"

func deploymentFreeBytes(path string) (uint64, error) {
	var s unix.Statfs_t
	err := unix.Statfs(path, &s)
	return s.Bavail * uint64(s.Bsize), err
}
