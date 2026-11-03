//go:build linux

package monitoring

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"golang.org/x/sys/unix"
)

func collect() (protocol.Metrics, counters) {
	now := time.Now().UTC()
	m := protocol.Metrics{CollectedAt: now, Errors: []string{}}
	raw := counters{at: now, network: map[string][2]uint64{}}
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	raw.boot = strings.TrimSpace(string(boot))
	stat, err := os.ReadFile("/proc/stat")
	if err == nil {
		for _, line := range strings.Split(string(stat), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			if fields[0] == "cpu" {
				// guest 和 guest_nice 已包含于 user 和 nice，不重复累计。
				for i, field := range fields[1:min(len(fields), 9)] {
					v, e := strconv.ParseUint(field, 10, 64)
					if e != nil {
						err = e
						break
					}
					raw.total += v
					if i == 3 || i == 4 {
						raw.idle += v
					}
				}
			} else if strings.HasPrefix(fields[0], "cpu") {
				if _, e := strconv.Atoi(fields[0][3:]); e == nil {
					m.CPUCores++
				}
			}
		}
	}
	if err != nil || m.CPUCores == 0 {
		m.Errors = append(m.Errors, "CPU 采集失败")
		raw.total = 0
		raw.idle = 0
	}
	mem, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		values := map[string]uint64{}
		for _, line := range strings.Split(string(mem), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				v, e := strconv.ParseUint(f[1], 10, 64)
				if e == nil {
					values[strings.TrimSuffix(f[0], ":")] = v * 1024
				}
			}
		}
		total, ok := values["MemTotal"]
		available, valid := values["MemAvailable"]
		if ok && valid && total >= available {
			used := total - available
			m.MemoryUsed, m.MemoryTotal = &used, &total
		}
	}
	if m.MemoryTotal == nil {
		m.Errors = append(m.Errors, "内存采集失败")
	}
	used, total, e := disks("/proc/self/mountinfo")
	if total > 0 && e == nil {
		m.DiskUsed, m.DiskTotal = &used, &total
	}
	if e != nil || total == 0 {
		m.Errors = append(m.Errors, "磁盘采集不完整")
	}
	net, err := os.ReadFile("/proc/net/dev")
	if err == nil {
		for _, line := range strings.Split(string(net), "\n") {
			name, rest, ok := strings.Cut(line, ":")
			name = strings.TrimSpace(name)
			if !ok || name == "lo" {
				continue
			}
			// 有设备归属的主机网卡包含云主机的 virtio 网卡，排除 bridge、veth、tun 等虚拟接口。
			if _, e := os.Stat(filepath.Join("/sys/class/net", name, "device")); e != nil {
				continue
			}
			f := strings.Fields(rest)
			if len(f) < 16 {
				err = strconv.ErrSyntax
				break
			}
			rx, e1 := strconv.ParseUint(f[0], 10, 64)
			tx, e2 := strconv.ParseUint(f[8], 10, 64)
			if e1 != nil || e2 != nil {
				err = strconv.ErrSyntax
				break
			}
			raw.network[name] = [2]uint64{rx, tx}
		}
	}
	if err != nil || len(raw.network) == 0 {
		m.Errors = append(m.Errors, "网络采集失败")
		raw.network = nil
	}
	return m, raw
}

func disks(path string) (uint64, uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	var used, total uint64
	var result error
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		left, right, ok := strings.Cut(scanner.Text(), " - ")
		if !ok {
			continue
		}
		a, b := strings.Fields(left), strings.Fields(right)
		if len(a) < 5 || len(b) < 2 {
			continue
		}
		// 只计入持久文件系统；绑定挂载用设备号去重。
		switch b[0] {
		case "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs", "f2fs", "jfs", "reiserfs", "bcachefs", "nfs", "nfs4", "cifs":
		default:
			continue
		}
		key := a[2]
		if b[0] == "nfs" || b[0] == "nfs4" || b[0] == "cifs" {
			key = b[1]
		}
		if seen[key] {
			continue
		}
		mount := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(a[4])
		var stat unix.Statfs_t
		if e := unix.Statfs(mount, &stat); e != nil {
			result = e
			continue
		}
		seen[key] = true
		total += stat.Blocks * uint64(stat.Bsize)
		used += (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	}
	if err := scanner.Err(); err != nil {
		result = err
	}
	return used, total, result
}
