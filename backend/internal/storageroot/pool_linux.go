package storageroot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"golang.org/x/sys/unix"
)

type poolDeployment struct {
	ID        string                             `json:"id"`
	Directory string                             `json:"directory"`
	UUID      string                             `json:"image_uuid"`
	Templates []dockerengine.StoragePoolTemplate `json:"data_containers"`
}

func (m *Manager) pool(id string) (definition, error) {
	if !operations.ValidID(id) {
		return definition{}, fault("invalid_request", "池标识无效")
	}
	path := filepath.Join(m.ExecutorDir, "pools", id, "storage-data.json")
	if err := trustedDir(filepath.Dir(path)); err != nil {
		return definition{}, err
	}
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil || st.Uid != 0 || st.Gid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG ||
		st.Mode&0077 != 0 {
		return definition{}, fault("conflict", "池 %s 缺少 root 私有数据部署记录", id)
	}
	var p poolDeployment
	if err := localstate.Read(path, &p); err != nil {
		return definition{}, err
	}
	if p.ID != id || !filepath.IsAbs(p.Directory) || filepath.Clean(p.Directory) != p.Directory || p.Directory == "/" ||
		p.UUID == "" || len(p.Templates) == 0 {
		return definition{}, fault("conflict", "池 %s 的数据部署记录无效", id)
	}
	return definition{Pool: &p}, nil
}

func (p poolDeployment) specs(directory string) ([]dockerengine.StorageContainer, error) {
	out := []dockerengine.StorageContainer{}
	names := map[string]bool{}
	for _, t := range p.Templates {
		spec, err := t.StorageSpec(p.ID, directory)
		if err != nil {
			return nil, err
		}
		if names[spec.Name] {
			return nil, fault("conflict", "池数据容器名称重复：%s", spec.Name)
		}
		names[spec.Name] = true
		out = append(out, spec)
	}
	return out, nil
}

func (p poolDeployment) image(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 2 || entries[0].Name() != "data" || !entries[0].IsDir() || entries[1].Name() != "data.img" {
		return fault("conflict", "池数据目录只允许数据镜像及其挂载目录：%s", dir)
	}
	path := filepath.Join(dir, "data.img")
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 ||
		st.Gid != 0 || st.Mode&0077 != 0 || st.Nlink != 1 || st.Size <= 0 || st.Blocks*512 < st.Size {
		return fault("conflict", "池数据镜像须为预分配的 root 私有普通文件：%s", path)
	}
	actual, err := output(ctx, "blkid", "-p", "-s", "UUID", "-o", "value", "--", path)
	if err != nil || !strings.EqualFold(actual, p.UUID) {
		return fault("conflict", "池镜像 UUID 不匹配：%s %v", path, err)
	}
	return nil
}

func (p poolDeployment) activeLoop(ctx context.Context, dir string) (string, error) {
	point := filepath.Join(dir, "data")
	at, err := covering(point)
	if err != nil || at.Point != point {
		return "", err
	}
	at, uuid, err := rootIdentity(ctx, point)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(at.Device, "/dev/loop") || !strings.EqualFold(uuid, p.UUID) {
		return "", fault("conflict", "池数据挂载的设备或 UUID 不同：%s", point)
	}
	actual, err := output(ctx, "losetup", "--noheadings", "--output", "BACK-FILE", at.Device)
	if err != nil || actual != filepath.Join(dir, "data.img") {
		return "", fault("conflict", "池数据 loop 后端不同：%s %v", point, err)
	}
	queue := filepath.Join("/sys/class/block", filepath.Base(at.Device), "queue", "discard_max_bytes")
	if body, err := os.ReadFile(queue); err != nil || strings.TrimSpace(string(body)) != "0" {
		return "", fault("conflict", "池数据设备仍可回收底层镜像块：%s", at.Device)
	}
	return at.Device, nil
}

func (p poolDeployment) mount(ctx context.Context, dir string) error {
	if err := p.image(ctx, dir); err != nil {
		return err
	}
	if dev, err := p.activeLoop(ctx, dir); err != nil || dev != "" {
		return err
	}
	image, point := filepath.Join(dir, "data.img"), filepath.Join(dir, "data")
	if err := realDir(point, false); err != nil {
		return err
	}
	entries, err := os.ReadDir(point)
	if err != nil || len(entries) != 0 {
		return fault("conflict", "池镜像挂载目录需要为空：%s", point)
	}
	if attached, err := output(ctx, "losetup", "--associated", image); err != nil || attached != "" {
		return fault("conflict", "池镜像已经关联其他 loop：%s %v", image, err)
	}
	dev, err := output(ctx, "losetup", "--find", "--show", "--nooverlap", image)
	if err != nil {
		return err
	}
	mounted := false
	defer func() {
		if !mounted {
			_, _ = output(context.WithoutCancel(ctx), "losetup", "--detach", dev)
		}
	}()
	queue := filepath.Join("/sys/class/block", filepath.Base(dev), "queue", "discard_max_bytes")
	if err = os.WriteFile(queue, []byte("0\n"), 0600); err != nil {
		return err
	}
	if body, err := os.ReadFile(queue); err != nil || strings.TrimSpace(string(body)) != "0" {
		return fault("conflict", "loop 设备仍可回收数据镜像块：%s", dev)
	}
	if _, err = output(ctx, "mount", "-t", "ext4", "-o", "nodiscard", dev, point); err != nil {
		return err
	}
	mounted = true
	_, err = p.activeLoop(ctx, dir)
	return err
}

func (p poolDeployment) unmount(ctx context.Context, dir string) error {
	dev, err := p.activeLoop(ctx, dir)
	if err != nil {
		return err
	}
	if dev == "" {
		// 中断可能发生在 umount 与 loop detach 之间。
		image := filepath.Join(dir, "data.img")
		if _, err := os.Stat(image); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		attached, err := output(ctx, "losetup", "--noheadings", "--output", "NAME", "--associated", image)
		if err != nil || attached == "" {
			return err
		}
		return fault("conflict", "池镜像仍关联未核验的 loop：%s", attached)
	}
	point := filepath.Join(dir, "data")
	if _, err = output(ctx, "umount", point); err != nil {
		return err
	}
	_, err = output(ctx, "losetup", "--detach", dev)
	return err
}

func (p poolDeployment) sync(ctx context.Context, dir string) error {
	if dev, err := p.activeLoop(ctx, dir); err != nil || dev == "" {
		return fault("conflict", "池数据镜像挂载未验证：%s %v", dir, err)
	}
	_, err := output(ctx, "sync", "-f", filepath.Join(dir, "data"))
	return err
}

func (p poolDeployment) capacityBytes(dir string) (int64, error) {
	var st unix.Stat_t
	if err := unix.Lstat(filepath.Join(dir, "data.img"), &st); err != nil {
		return 0, err
	}
	if st.Size <= 0 {
		return 0, fmt.Errorf("池数据镜像为空")
	}
	return st.Size, nil
}
