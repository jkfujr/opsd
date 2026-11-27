package storageroot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
	"golang.org/x/sys/unix"
)

func (m *Manager) poolRoots(ctx context.Context, rootID string) (registry, protocol.StorageRoot, protocol.StorageRoot, error) {
	r, err := m.paths(ctx)
	if err != nil {
		return r, protocol.StorageRoot{}, protocol.StorageRoot{}, err
	}
	b, err := r.root(rootID)
	if err == nil {
		err = m.checkRoot(ctx, b)
	}
	if err != nil {
		return r, protocol.StorageRoot{}, b, err
	}
	a, err := covering(m.ExecutorDir)
	if err != nil {
		return r, protocol.StorageRoot{}, b, err
	}
	relative, err := filepath.Rel(a.Point, m.ExecutorDir)
	if err != nil {
		return r, protocol.StorageRoot{}, b, err
	}
	control, err := captureRoot(ctx, "", a.Point, relative)
	return r, control, b, err
}

func diskSpace(root protocol.StorageRoot) (protocol.PoolDisk, error) {
	statPath := root.Base
	for {
		var stat unix.Statfs_t
		err := unix.Statfs(statPath, &stat)
		if err == nil {
			blockSize := int64(stat.Frsize)
			size, avail := int64(stat.Blocks)*blockSize, int64(stat.Bavail)*blockSize
			return protocol.PoolDisk{UUID: root.UUID, MountPoint: root.MountPoint, CapacityBytes: size,
				AvailableBytes: avail, ReserveBytes: max(2*protocol.PoolGiB, (size+9)/10)}, nil
		}
		if !errors.Is(err, os.ErrNotExist) || statPath == root.MountPoint {
			return protocol.PoolDisk{}, err
		}
		statPath = filepath.Dir(statPath)
	}
}

func (m *Manager) poolDisks(ctx context.Context, r registry, a, b protocol.StorageRoot) (protocol.PoolDisk, protocol.PoolDisk, error) {
	control, err := diskSpace(a)
	if err != nil {
		return control, protocol.PoolDisk{}, err
	}
	data, err := diskSpace(b)
	if err != nil {
		return control, data, err
	}
	for _, bind := range r.Bindings {
		if bind.Archive == nil || !bind.Archive.Enabled || bind.Identity.UUID != a.UUID && bind.Identity.UUID != b.UUID {
			continue
		}
		body, err := output(ctx, "du", "-s", "-B1", "--one-file-system", "--", bind.Directory)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return control, data, fault("dependency_unavailable", "无法核对归档已用容量：%s", bind.Directory)
		}
		used, _, _ := strings.Cut(body, "\t")
		occupied, err := strconv.ParseInt(used, 10, 64)
		if err != nil {
			return control, data, fault("dependency_unavailable", "归档已用容量格式无效：%s", bind.Directory)
		}
		budget := max(0, bind.Archive.DiskBudgetBytes-occupied)
		if bind.Identity.UUID == a.UUID {
			control.ArchiveBytes += budget
		}
		if bind.Identity.UUID == b.UUID {
			data.ArchiveBytes += budget
		}
	}
	return control, data, nil
}

func portGroupFree(ctx context.Context, ip string, ports []int, reserved map[int]bool) bool {
	for _, port := range ports {
		if reserved[port] {
			return false
		}
		l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", storagepool.Address(ip, port))
		if err != nil {
			return false
		}
		_ = l.Close()
	}
	return true
}

func (m *Manager) reservedPoolPorts(ip string) (map[int]bool, error) {
	out := map[int]bool{}
	entries, err := os.ReadDir(filepath.Join(m.ExecutorDir, "pools"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		d, err := m.readPoolNode(entry.Name())
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if d.Member.PrivateIP != ip {
			continue
		}
		data, control := storagepool.PortGroups(d.Member.Ports)
		if d.Member.Control() {
			data = append(data, control...)
		}
		for _, port := range data {
			out[port] = true
		}
	}
	return out, nil
}

func poolLocalIP(ip string) error {
	local, err := net.InterfaceAddrs()
	if err != nil {
		return err
	}
	for _, addr := range local {
		if n, _, ok := strings.Cut(addr.String(), "/"); ok && n == ip {
			return nil
		}
	}
	return fault("invalid_request", "私网地址 %s 未配置在目标主机", ip)
}

func (m *Manager) inspectPool(ctx context.Context, n protocol.PoolMemberInput, offset int) (protocol.PoolHostResult, error) {
	var out protocol.PoolHostResult
	if err := poolLocalIP(n.PrivateIP); err != nil {
		return out, err
	}
	r, a, b, err := m.poolRoots(ctx, n.StorageRootID)
	if err != nil {
		return out, err
	}
	out.ControlDisk, out.DataDisk, err = m.poolDisks(ctx, r, a, b)
	if err != nil {
		return out, err
	}
	id, err := os.ReadFile("/etc/machine-id")
	if err != nil || len(strings.TrimSpace(string(id))) != 32 {
		return out, fault("dependency_unavailable", "无法读取宿主机身份")
	}
	out.HostID = fmt.Sprintf("host-%x", sha256.Sum256([]byte(strings.TrimSpace(string(id)))))
	reserved, err := m.reservedPoolPorts(n.PrivateIP)
	if err != nil {
		return out, err
	}
	data, control := storagepool.PortGroups(storagepool.Ports(offset))
	out.PortsFree = portGroupFree(ctx, n.PrivateIP, data, reserved)
	out.ControlPortsFree = portGroupFree(ctx, n.PrivateIP, control, reserved)
	if m.Docker != nil {
		out.ECImage, _ = m.Docker.StoragePoolECImage(ctx)
	}
	return out, nil
}

func lockPoolDisks(a, b string) (func(), error) {
	ids := []string{a, b}
	slices.Sort(ids)
	var locks []func()
	release := func() {
		for i := len(locks) - 1; i >= 0; i-- {
			locks[i]()
		}
	}
	for _, id := range slices.Compact(ids) {
		unlock, err := localstate.Lock(filepath.Join("/run/opsd-storage-pools", id+".lock"))
		if err != nil {
			release()
			return nil, err
		}
		locks = append(locks, unlock)
	}
	return release, nil
}

func poolImageUUID(pool, node, role string) string {
	sum := sha256.Sum256([]byte(pool + "\x00" + node + "\x00" + role))
	sum[6] = sum[6]&0x0f | 0x40
	sum[8] = sum[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func createPoolImage(ctx context.Context, dir, name, uuid string, size int64) error {
	if err := trustedDir(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	image := filepath.Join(dir, name+".img")
	if _, err := os.Lstat(image); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return verifyPoolImage(ctx, image, uuid, size)
	}
	stage := filepath.Join(dir, "."+name+".allocating")
	// 临时文件仅来自已持久化的同一建池意图。正式镜像从不格式化。
	open, err := unix.Open(stage, unix.O_WRONLY|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(open), stage)
	var st unix.Stat_t
	err = unix.Fstat(open, &st)
	if err == nil && (st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || st.Mode&(unix.S_IFMT|0777) != unix.S_IFREG|0600 || st.Size != 0 && st.Size != size) {
		err = fault("conflict", "镜像暂存文件身份不同：%s", stage)
	}
	if err == nil {
		err = unix.Fallocate(open, 0, 0, size)
	}
	if err = errors.Join(err, f.Close()); err != nil {
		if errors.Is(err, unix.ENOSPC) {
			return fault("insufficient_storage", "镜像预分配空间不足：%s", image)
		}
		return err
	}
	if verifyPoolImage(ctx, stage, uuid, size) != nil {
		if _, err = output(ctx, "mkfs.ext4", "-F", "-m", "0", "-U", uuid, "-E", "nodiscard,lazy_itable_init=0,lazy_journal_init=0", "--", stage); err != nil {
			return err
		}
	}
	if _, err = output(ctx, "fallocate", "-l", strconv.FormatInt(size, 10), "--", stage); err != nil {
		return err
	}
	if err = verifyPoolImage(ctx, stage, uuid, size); err != nil {
		return err
	}
	if _, err = output(ctx, "sync", "-f", stage); err != nil {
		return err
	}
	if err = unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, image, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return localstate.SyncDirectory(dir)
}
