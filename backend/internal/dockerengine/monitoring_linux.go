//go:build linux

package dockerengine

import (
	"context"
	"golang.org/x/sys/unix"
	"io/fs"
	"path/filepath"
)

func monitoringDisk(ctx context.Context, directory string) (int64, int64, error) {
	var size int64
	err := filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, e := d.Info()
			if e != nil {
				return e
			}
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	var stat unix.Statfs_t
	err = unix.Statfs(directory, &stat)
	return size, int64(stat.Bavail) * int64(stat.Bsize), err
}
