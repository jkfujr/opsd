package storageroot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/command"
	"github.com/jkfujr/opsd/internal/protocol"
	"golang.org/x/sys/unix"
)

type mount struct {
	Point, Device, Number, FSRoot, Type string
	ReadOnly                            bool
}

func containsPath(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func overlaps(a, b string) bool { return containsPath(a, b) || containsPath(b, a) }

var unescapeMount = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

func mounts() ([]mount, error) {
	body, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	out := []mount{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(body)), "\n") {
		before, after, ok := strings.Cut(line, " - ")
		f, a := strings.Fields(before), strings.Fields(after)
		if !ok || len(f) < 6 || len(a) < 3 {
			return nil, fmt.Errorf("挂载记录格式无效")
		}
		out = append(out, mount{Point: unescapeMount.Replace(f[4]), Device: unescapeMount.Replace(a[1]),
			Number: f[2], FSRoot: unescapeMount.Replace(f[3]), Type: a[0],
			ReadOnly: strings.Contains(","+f[5]+","+a[2]+",", ",ro,")})
	}
	return out, nil
}

func covering(path string) (mount, error) {
	all, err := mounts()
	if err != nil {
		return mount{}, err
	}
	var result mount
	for _, at := range all {
		if containsPath(at.Point, path) && len(at.Point) >= len(result.Point) {
			result = at
		}
	}
	if result.Point == "" {
		return result, fmt.Errorf("路径 %s 没有挂载记录", path)
	}
	return result, nil
}

func rootIdentity(ctx context.Context, point string) (mount, string, error) {
	m, err := covering(point)
	if err != nil {
		return m, "", err
	}
	if m.Point != point {
		return m, "", fault("dependency_unavailable", "挂载点 %s 缺失，实际挂载点 %s", point, m.Point)
	}
	if m.Type != "ext4" || m.FSRoot != "/" || !strings.HasPrefix(m.Device, "/dev/") {
		return m, "", fault("invalid_request", "%s 须为整卷原生 ext4 挂载，实际为 %s %s", point, m.Type, m.FSRoot)
	}
	if m.ReadOnly {
		return m, "", fault("dependency_unavailable", "挂载点 %s 为只读", point)
	}
	var dev, dir unix.Stat_t
	if err = unix.Stat(m.Device, &dev); err != nil {
		return m, "", fault("dependency_unavailable", "无法读取 %s 的挂载设备 %s：%v", point, m.Device, err)
	}
	number := func(n uint64) string { return fmt.Sprintf("%d:%d", unix.Major(n), unix.Minor(n)) }
	if err = unix.Stat(point, &dir); err != nil || dev.Mode&unix.S_IFMT != unix.S_IFBLK ||
		number(dev.Rdev) != m.Number || number(dir.Dev) != m.Number {
		return m, "", fault("dependency_unavailable", "挂载点 %s 与设备 %s 的设备号不同", point, m.Device)
	}
	uuid, err := output(ctx, "blkid", "-p", "-s", "UUID", "-o", "value", "--", m.Device)
	if err != nil || uuid == "" {
		return m, "", fault("dependency_unavailable", "无法读回 %s 的 ext4 UUID：%v", point, err)
	}
	return m, strings.ToLower(uuid), nil
}

func captureRoot(ctx context.Context, id, point, relative string) (protocol.StorageRoot, error) {
	base := filepath.Join(point, relative)
	if err := trustedDir(base); err != nil {
		return protocol.StorageRoot{}, err
	}
	m, uuid, err := rootIdentity(ctx, point)
	if err != nil {
		return protocol.StorageRoot{}, err
	}
	root := protocol.StorageRoot{ID: id, MountPoint: point, RelativeDir: relative, UUID: uuid, Type: m.Type, Base: base}
	if err = noNestedMount(base, point, false); err != nil {
		return protocol.StorageRoot{}, err
	}
	return root, nil
}

func (m *Manager) checkRoot(ctx context.Context, root protocol.StorageRoot) error {
	actual, err := captureRoot(ctx, root.ID, root.MountPoint, root.RelativeDir)
	if err != nil {
		return err
	}
	if actual.UUID != root.UUID || actual.Type != root.Type || actual.Base != root.Base {
		return fault("dependency_unavailable", "挂载点 %s 的身份不同，预期 %s %s，实际 %s %s",
			root.MountPoint, root.Type, root.UUID, actual.Type, actual.UUID)
	}
	return nil
}

// 每层可由 root 创建，已有路径必须由 root 控制。
func trustedDir(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fault("invalid_request", "目录路径无效：%s", path)
	}
	for p := path; ; p = filepath.Dir(p) {
		var st unix.Stat_t
		err := unix.Lstat(p, &st)
		if err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		if err == nil {
			groupOK := st.Gid == 0
			if !groupOK && p == filepath.Dir(command.DefaultInstance) && st.Mode&07777 == 0750 {
				group, lookupErr := user.LookupGroup("opsd")
				groupOK = lookupErr == nil && group.Gid == strconv.FormatUint(uint64(st.Gid), 10)
			}
			if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || !groupOK || st.Mode&0022 != 0 {
				return fault("invalid_request", "目录 %s 须为 root 所有、使用受信任组且禁止组和其他用户写入的真实目录", p)
			}
		}
		if p == "/" {
			return nil
		}
	}
}

func noNestedMount(path, expected string, descendants bool) error {
	all, err := mounts()
	if err != nil {
		return err
	}
	for _, at := range all {
		if at.Point != expected && containsPath(expected, at.Point) &&
			(containsPath(at.Point, path) || descendants && containsPath(path, at.Point)) {
			return fault("conflict", "路径 %s 与额外挂载点 %s 重叠", path, at.Point)
		}
	}
	return nil
}

func realDir(path string, absentOK bool) error {
	if err := trustedDir(filepath.Dir(path)); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if absentOK && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fault("invalid_request", "%s 须为真实目录", path)
	}
	return nil
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	body, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s：%w", name, ctx.Err())
	}
	out := strings.TrimSpace(string(body))
	if err != nil {
		return "", fmt.Errorf("%s：%w：%s", name, err, out)
	}
	return out, nil
}

func checkWriters(paths ...string) error {
	list, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, process := range list {
		pid := process.Name()
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}
		p := filepath.Join("/proc", pid)
		fds, err := os.ReadDir(filepath.Join(p, "fd"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, fd := range fds {
			name := fd.Name()
			link, err := os.Readlink(filepath.Join(p, "fd", name))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			link = strings.TrimSuffix(link, " (deleted)")
			for _, path := range paths {
				if !filepath.IsAbs(link) || !containsPath(path, link) {
					continue
				}
				body, err := os.ReadFile(filepath.Join(p, "fdinfo", name))
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return err
				}
				for line := range strings.SplitSeq(string(body), "\n") {
					if raw, ok := strings.CutPrefix(line, "flags:"); ok {
						flags, err := strconv.ParseUint(strings.TrimSpace(raw), 8, 64)
						if err != nil {
							return err
						}
						if flags&unix.O_ACCMODE != unix.O_RDONLY {
							return fault("conflict", "进程 %s 仍打开 %s 写入", pid, link)
						}
					}
				}
			}
		}
		body, err := os.ReadFile(filepath.Join(p, "maps"))
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, unix.ESRCH) {
			return err
		}
		for line := range strings.SplitSeq(string(body), "\n") {
			fields := strings.SplitN(strings.Join(strings.Fields(line), " "), " ", 6)
			if len(fields) != 6 || len(fields[1]) != 4 || fields[1][1] != 'w' || fields[1][3] != 's' {
				continue
			}
			for _, path := range paths {
				if link := strings.TrimSuffix(fields[5], " (deleted)"); filepath.IsAbs(link) && containsPath(path, link) {
					return fault("conflict", "进程 %s 仍映射 %s 写入", pid, link)
				}
			}
		}
	}
	return nil
}
