package storageroot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/jkfujr/opsd/internal/localstate"
	"golang.org/x/sys/unix"
)

type inventory struct {
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
	SHA   string `json:"sha256,omitempty"`
}

func inspectTree(ctx context.Context, dir string, hashFiles bool) (inventory, error) {
	out := inventory{}
	hash := sha256.New()
	links := map[uint64]int64{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if !hashFiles && errors.Is(err, os.ErrNotExist) && path != dir {
			return nil
		}
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		var st unix.Stat_t
		if err = unix.Lstat(path, &st); err != nil {
			if !hashFiles && errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG {
			return fault("conflict", "数据目录包含链接或特殊文件：%s", path)
		}
		out.Files++
		out.Bytes += st.Size
		if !hashFiles {
			return nil
		}
		if st.Nlink > 1 {
			if v, ok := links[st.Ino]; ok {
				links[st.Ino] = v - 1
			} else {
				links[st.Ino] = int64(st.Nlink) - 1
			}
		}
		data, err := os.Open(path)
		if err != nil {
			return err
		}
		sum := sha256.New()
		_, err = io.Copy(sum, data)
		closeErr := data.Close()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
		rel, _ := filepath.Rel(dir, path)
		fmt.Fprintf(hash, "%q %d %d %d %o %x\n", rel, st.Size, st.Uid, st.Gid, st.Mode&07777, sum.Sum(nil))
		return nil
	})
	if err != nil {
		return out, err
	}
	for _, remaining := range links {
		if remaining != 0 {
			return out, fault("conflict", "数据目录存在跨目录的硬链接：%s", dir)
		}
	}
	if hashFiles {
		out.SHA = fmt.Sprintf("%x", hash.Sum(nil))
	}
	return out, nil
}

func inodeOf(path string) (uint64, error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return 0, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return 0, fault("conflict", "%s 须为真实目录", path)
	}
	return st.Ino, nil
}

func checkCapacity(path string, bytes int64) error {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return err
	}
	if uint64(bytes) > st.Bavail*uint64(st.Frsize) {
		return fault("insufficient_storage", "存储根 %s 剩余空间不足，需要 %d 字节", path, bytes)
	}
	return nil
}

func (r *record) staging(suffix string) string {
	direction := "forward"
	if r.Reverse {
		direction = "reverse"
	}
	return filepath.Join(filepath.Dir(r.To.Directory), ".opsd-"+r.ID+"-"+direction+"."+suffix)
}

func (m *Manager) copy(ctx context.Context, r *record) error {
	if err := m.validateBinding(ctx, r.To, true); err != nil {
		return err
	}
	if err := checkWriters(r.From.Directory, r.To.Directory); err != nil {
		return err
	}
	if err := m.capacity(ctx, r); err != nil {
		return err
	}
	stage := r.staging("staging")
	if err := os.MkdirAll(filepath.Dir(stage), 0700); err != nil {
		return err
	}
	if err := os.Mkdir(stage, 0700); err != nil {
		return err
	}
	var err error
	r.StageInode, err = inodeOf(stage)
	if err != nil {
		return err
	}
	if err = m.saveRecord(r); err != nil {
		return err
	}
	_, err = output(ctx, "rsync", "-aHAXx", "--numeric-ids", "--", r.From.Directory+"/", stage+"/")
	if err != nil {
		return err
	}
	if p := r.Definition.Pool; p != nil {
		size, err := p.capacityBytes(stage)
		if err != nil {
			return err
		}
		if _, err = output(ctx, "fallocate", "--length", fmt.Sprint(size), "--", filepath.Join(stage, "data.img")); err != nil {
			return err
		}
	}
	_, err = output(ctx, "sync", "-f", stage)
	return err
}

func (m *Manager) verifyCopy(ctx context.Context, r *record) error {
	stage := r.staging("staging")
	for _, b := range []binding{r.From, r.To} {
		if err := m.validateBinding(ctx, b, b == r.To); err != nil {
			return err
		}
	}
	if err := checkWriters(r.From.Directory, r.To.Directory, stage); err != nil {
		return err
	}
	if _, err := m.audit(ctx, r, r.From, false); err != nil {
		return err
	}
	diff, err := output(ctx, "rsync", "-aHAXxnic", "--delete", "--numeric-ids", "--", r.From.Directory+"/", stage+"/")
	if err != nil {
		return err
	}
	if diff != "" {
		return fmt.Errorf("复制内容、属主、权限、ACL 或扩展属性不同：%s", diff)
	}
	source, err := inspectTree(ctx, r.From.Directory, true)
	if err != nil {
		return err
	}
	r.Copied, err = inspectTree(ctx, stage, true)
	if err == nil && r.Copied != source {
		return fault("conflict", "复制前后的文件数、字节数或摘要不同")
	}
	if err == nil && r.Definition.Pool != nil {
		err = r.Definition.Pool.image(ctx, stage)
	}
	return err
}

func (m *Manager) publishCopy(r *record) error {
	stage, err := inodeOf(r.staging("staging"))
	if err != nil || stage != r.StageInode {
		return fault("conflict", "暂存目录身份发生变化：%s", r.staging("staging"))
	}
	current, err := inodeOf(r.To.Directory)
	if err == nil {
		if !r.Reverse || current != r.OriginalInode {
			return fault("conflict", "目标目录已被使用：%s", r.To.Directory)
		}
		if _, err = os.Lstat(r.staging("retained")); !errors.Is(err, os.ErrNotExist) {
			return fault("conflict", "旧副本保留目录已被使用：%s", r.staging("retained"))
		}
		if err = os.Rename(r.To.Directory, r.staging("retained")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Rename(r.staging("staging"), r.To.Directory); err != nil {
		return err
	}
	return localstate.SyncDirectory(filepath.Dir(r.To.Directory))
}

func (m *Manager) unpublishBeforeOpen(ctx context.Context, r *record) error {
	if r.StageInode == 0 || m.validateBinding(ctx, r.To, true) != nil {
		return nil
	}
	dst := r.To.Directory
	current, err := inodeOf(dst)
	if err == nil && current == r.StageInode {
		if err := checkWriters(dst); err != nil {
			return err
		}
		if _, err := os.Lstat(r.staging("staging")); !errors.Is(err, os.ErrNotExist) {
			return fault("conflict", "暂存路径已被占用：%s", r.staging("staging"))
		}
		if err := os.Rename(dst, r.staging("staging")); err != nil {
			return err
		}
		current, err = 0, os.ErrNotExist
	}
	if r.Reverse && current == 0 && errors.Is(err, os.ErrNotExist) {
		old := r.staging("retained")
		if ino, read := inodeOf(old); read == nil && ino == r.OriginalInode {
			if err := os.Rename(old, dst); err != nil {
				return err
			}
		}
	}
	return localstate.SyncDirectory(filepath.Dir(dst))
}
