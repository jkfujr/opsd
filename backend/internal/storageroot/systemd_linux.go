package storageroot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
)

const unitDirectory = "/etc/systemd/system"
const stopBudget = 45 * time.Second
const drainBudget = 60 * time.Second
const reloadBudget = 10 * time.Second
const startBudget = 75 * time.Second

var systemdTimeout = errors.New("systemd 阶段超时")

func unitName(b binding) string {
	name := b.Component + "/" + b.ComponentID
	if b.Instance != "" {
		name += "/" + b.Instance
	}
	hash := sha256.Sum256([]byte(name))
	return fmt.Sprintf("opsd-storage-root-%x.service", hash[:10])
}

func unitArg(s string) string {
	return strconv.Quote(strings.ReplaceAll(strings.ReplaceAll(s, "%", "%%"), "$", "$$"))
}

func changeSystemd(ctx context.Context, budget time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	_, err := output(ctx, "systemctl", args...)
	if ctx.Err() != nil {
		return fmt.Errorf("%w：%s，预算 %s", systemdTimeout, strings.Join(args, " "), budget)
	}
	return err
}

func drain(ctx context.Context, budget time.Duration) error {
	return drainExcept(ctx, budget, "")
}

func drainExcept(ctx context.Context, budget time.Duration, caller string) error {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		command, stop := context.WithTimeout(ctx, 2*time.Second)
		out, err := output(command, "systemctl", "list-jobs", "--no-legend", "--no-pager", "--plain")
		stop()
		if err == nil {
			idle := out == "" || out == "No jobs running." || out == "No jobs running"
			if !idle && caller != "" {
				idle = true
				for line := range strings.SplitSeq(out, "\n") {
					fields := strings.Fields(line)
					if len(fields) < 3 || fields[1] != caller || fields[2] != "start" {
						idle = false
						break
					}
				}
			}
			if idle {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w：等待所有 systemd job 清空，预算 %s", systemdTimeout, budget)
		case <-ticker.C:
		}
	}
}

func operationUnit() string {
	body, _ := os.ReadFile("/proc/self/cgroup")
	for line := range strings.SplitSeq(string(body), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) == 3 {
			unit := filepath.Base(fields[2])
			if strings.HasPrefix(unit, "opsd-operation@") && strings.HasSuffix(unit, ".service") {
				return unit
			}
		}
	}
	return ""
}

func (m *Manager) stop(ctx context.Context, b binding, states []dockerengine.StorageContainerState) error {
	ctx, cancel := context.WithTimeout(ctx, stopBudget)
	defer cancel()
	if b.Guarded {
		if err := changeSystemd(ctx, stopBudget, "stop", unitName(b)); err != nil {
			return err
		}
	}
	if err := m.Docker.StopStorage(ctx, states); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w：组件停止，预算 %s", systemdTimeout, stopBudget)
		}
		return err
	}
	return nil
}

func (m *Manager) installUnit(ctx context.Context, b binding) error {
	name := unitName(b)
	binary := m.Binary
	if binary == "" {
		var err error
		binary, err = os.Executable()
		if err != nil {
			return err
		}
	}
	if !filepath.IsAbs(binary) {
		return fault("conflict", "组件启动程序须使用绝对路径")
	}
	point := b.Identity.MountPoint
	dependency := ""
	if point != "/" {
		mount, err := output(ctx, "systemd-escape", "--path", "--suffix=mount", point)
		if err != nil {
			return err
		}
		dependency = fmt.Sprintf("RequiresMountsFor=%s\nBindsTo=%s\nAfter=%s\n", unitArg(point), mount, mount)
	}
	if b.Component == protocol.StoragePoolData && b.Instance != "" {
		d, err := m.readPoolNode(b.ComponentID)
		if err != nil {
			return err
		}
		controlUnit := unitName(m.poolControlBinding(d))
		dependency += fmt.Sprintf("BindsTo=%s\nAfter=%s\n", controlUnit, controlUnit)
	}
	cmd := func(action string) string {
		return fmt.Sprintf("%s storage-root %s --config %s --component %s --component-id %s",
			unitArg(binary), action, unitArg(m.Config), b.Component, b.ComponentID)
	}
	owner := "# OPSD storage-root " + name + "\n"
	path := filepath.Join(unitDirectory, name)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && !strings.HasPrefix(string(old), owner) {
		return fault("conflict", "已有非受管 systemd 单元 %s", name)
	}
	body := fmt.Sprintf("%s[Unit]\nDescription=OPSD 受管组件存储 %s\nBindsTo=docker.service\nAfter=docker.service\nPartOf=docker.service\n%s\n[Service]\nType=oneshot\nRemainAfterExit=yes\nTimeoutStartSec=70\nTimeoutStopSec=40\nExecStartPre=%s\nExecStart=%s\nExecStop=%s\n\n[Install]\nWantedBy=multi-user.target docker.service\n",
		owner, b.Component, dependency, cmd("component-check"), cmd("component-start"), cmd("component-stop"))
	file, err := os.CreateTemp(unitDirectory, ".opsd-storage-root-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0644); err == nil {
		_, err = file.WriteString(body)
	}
	if err == nil {
		err = file.Sync()
	}
	closed := file.Close()
	if err != nil || closed != nil {
		return errors.Join(err, closed)
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	for _, target := range []string{"multi-user.target", "docker.service"} {
		dir := filepath.Join(unitDirectory, target+".wants")
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		link := filepath.Join(dir, name)
		if err = os.Symlink("../"+name, link); err != nil {
			actual, e := os.Readlink(link)
			if e != nil || actual != "../"+name {
				return fmt.Errorf("组件启动链接不可用：%s：%w", link, err)
			}
		}
		if err = localstate.SyncDirectory(dir); err != nil {
			return err
		}
	}
	return localstate.SyncDirectory(unitDirectory)
}

func (m *Manager) removeUnit(b binding) error {
	name := unitName(b)
	path := filepath.Join(unitDirectory, name)
	old, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(old), "# OPSD storage-root "+name+"\n") {
		return fault("conflict", "已有非受管 systemd 单元 %s", name)
	}
	for _, target := range []string{"multi-user.target", "docker.service"} {
		link := filepath.Join(unitDirectory, target+".wants", name)
		actual, err := os.Readlink(link)
		if err == nil && actual == "../"+name {
			if err = os.Remove(link); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fault("conflict", "组件启动链接已改变：%s", link)
		}
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	return localstate.SyncDirectory(unitDirectory)
}

func (m *Manager) Component(ctx context.Context, cmd, component, id string) error {
	if component == poolControlComponent {
		return m.poolControlComponent(ctx, cmd, id)
	}
	r, err := m.paths(ctx)
	if err != nil {
		return err
	}
	b, ok := r.lookup(component, id)
	if !ok || !b.Guarded {
		return fault("conflict", "未登记受管组件启动绑定")
	}
	// 停机时仍可核对绑定。拔盘触发的停止不能依赖盘内文件。
	if cmd != "component-stop" {
		p, err := m.pending(r)
		if err != nil {
			return err
		}
		if p != nil && p.From.Component == component && p.From.ComponentID == id &&
			!(p.WriteIntent && p.To.Directory == b.Directory && (p.Phase == "switch" || p.Phase == "cleanup")) &&
			!(p.Phase == "restore_old" && !p.WriteIntent && p.From.Directory == b.Directory) {
			return fault("node_busy", "组件迁移 %s 尚未放行写入", p.ID)
		}
		if err = m.validateBinding(ctx, b, false); err != nil {
			return err
		}
		if component == protocol.StoragePoolData && b.Instance != "" {
			d, err := m.readPoolNode(id)
			if err != nil {
				return err
			}
			if err := m.checkPoolControl(ctx, d); err != nil {
				return err
			}
			if device, err := activePoolLoop(ctx, m.poolDir(id), "control", d.ControlUUID); err != nil || device == "" {
				return fault("dependency_unavailable", "池控制卷未就绪")
			}
		}
	}
	if cmd == "component-check" {
		return nil
	}
	d := definition{Archive: b.Archive}
	if b.Archive == nil {
		d, err = m.findDeployment(ctx, component, id)
		if err != nil {
			return err
		}
	}
	if b.Paused || !d.enabled() {
		if cmd != "component-stop" {
			return nil
		}
		if b.Archive == nil {
			p, err := m.readRecord(b.Record)
			if err != nil {
				return err
			}
			d = p.Definition
		}
	}
	specs, err := m.specs(d, b)
	if err != nil {
		return err
	}
	states, err := m.Docker.AuditStorage(ctx, nil, specs, cmd == "component-stop")
	if err != nil {
		return err
	}
	if cmd == "component-stop" {
		if err := m.Docker.StopStorage(ctx, states); err != nil {
			return err
		}
		if d.Pool != nil {
			return d.Pool.unmount(ctx, b.Directory)
		}
		return nil
	}
	if d.Pool != nil {
		if err := d.Pool.mount(ctx, b.Directory); err != nil {
			return err
		}
	}
	for _, s := range states {
		if s.Restart != "no" {
			return fault("conflict", "受管容器 %s 的重启策略已改变", s.ID)
		}
	}
	if d.Database != nil {
		if err = dockerengine.VerifyDeploymentMounts(*d.Database, b.Directory); err != nil {
			return err
		}
	}
	return m.Docker.StartStorage(ctx, states, nil)
}
