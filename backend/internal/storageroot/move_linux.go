package storageroot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
)

type Status struct {
	protocol.StorageRootProgress
	Active      *protocol.StorageBinding             `json:"active,omitempty"`
	SourceError string                               `json:"source_error,omitempty"`
	TargetError string                               `json:"target_error,omitempty"`
	Containers  []dockerengine.StorageContainerState `json:"containers,omitempty"`
	AuditError  string                               `json:"audit_error,omitempty"`
	StepTimes   map[string]int64                     `json:"step_ms,omitempty"`
	Copied      inventory                            `json:"verified_data,omitempty"`
	Unit        string                               `json:"unit,omitempty"`
	Reverse     bool                                 `json:"reverse,omitempty"`
}

func (m *Manager) Status(ctx context.Context, id string) (Status, error) {
	r, err := m.readRecord(id)
	if err != nil {
		return Status{}, err
	}
	out := Status{StorageRootProgress: r.StorageRootProgress, StepTimes: r.StepTimes, Copied: r.Copied, Reverse: r.Reverse}
	if r.Phase == "registered" {
		return out, nil
	}
	reg, err := m.paths(ctx)
	if err != nil {
		return out, err
	}
	if b, found := reg.lookup(r.From.Component, r.From.ComponentID); found {
		out.Active = &b.StorageBinding
		if b.Guarded {
			out.Unit = unitName(b)
		}
	} else {
		out.Active = &r.Original.StorageBinding
	}
	if err = m.validateBinding(ctx, r.From, false); err != nil {
		out.SourceError = err.Error()
	}
	if err = m.validateBinding(ctx, r.To, !r.WriteIntent); err != nil {
		out.TargetError = err.Error()
	}
	out.Containers, err = m.auditBoth(ctx, &r)
	if err != nil {
		out.AuditError = err.Error()
	}
	return out, nil
}

func (m *Manager) Run(ctx context.Context, id, action, mode string) error {
	unlock, err := localstate.Lock(filepath.Join(m.stateDir(), "lock"))
	if err != nil {
		return err
	}
	defer unlock()
	unlockDocker, err := operations.LockDocker(m.ExecutorDir)
	if err != nil {
		return err
	}
	defer unlockDocker()
	r, err := m.readRecord(id)
	if err != nil {
		return err
	}
	if r.Input.Action != "move" {
		return fault("conflict", "该记录没有组件迁移")
	}
	reg, err := m.paths(ctx)
	if err != nil {
		return err
	}
	if p, err := m.pending(reg); err != nil {
		return err
	} else if p != nil && p.ID != id {
		return fault("node_busy", "节点已有存储迁移 %s", p.ID)
	}
	if r.Phase == "rolled_back" || action == "apply" && r.Phase == "complete" {
		return nil
	}
	switch action {
	case "apply":
		if r.Phase == "pending_restart" && reg.Pending == id {
			return m.move(ctx, &reg, &r, false)
		}
		if r.Phase != "needs_recovery" {
			r.Phase = "needs_recovery"
			r.Error = "上次迁移在阶段提交前退出，请读回并恢复原申请"
			if err = m.saveRecord(&r); err != nil {
				return err
			}
		}
		return fault("conflict", "申请 %s 需要运行 storage-root recover", id)
	case "rollback":
		if r.Phase != "complete" {
			return fault("conflict", "rollback 只适用于已完成的迁移，其他状态使用 recover")
		}
		return m.reverse(ctx, &reg, &r, false)
	case "recover":
		if r.Phase == "complete" || r.Phase == "pending_restart" {
			return fault("conflict", "申请 %s 当前状态 %s 无需恢复", id, r.Phase)
		}
		if err = drain(ctx, drainBudget); err != nil {
			return m.needs(&r, err)
		}
		if !r.WriteIntent {
			if mode == "repair-target" {
				return fault("conflict", "目标尚未开放写入，请使用 reverse 恢复源组件")
			}
			return m.restoreOld(ctx, &reg, &r)
		}
		if mode == "repair-target" {
			return m.repairTarget(ctx, &reg, &r)
		}
		if r.Reverse {
			return fault("conflict", "反迁目标已开放写入，请使用 repair-target 完成当前反迁")
		}
		return m.reverse(ctx, &reg, &r, true)
	}
	return fmt.Errorf("未知存储迁移命令")
}

func (m *Manager) capacity(ctx context.Context, r *record) error {
	var bytes int64
	if r.Definition.Pool != nil {
		size, err := r.Definition.Pool.capacityBytes(r.From.Directory)
		if err != nil {
			return err
		}
		bytes = size
	} else {
		current, err := inspectTree(ctx, r.From.Directory, false)
		if err != nil {
			return err
		}
		bytes = current.Bytes
	}
	if r.Definition.Archive != nil {
		bytes += 1 << 30
	}
	return checkCapacity(r.To.Identity.MountPoint, bytes)
}

func (m *Manager) preflight(ctx context.Context, r *record) error {
	if err := m.validateBinding(ctx, r.From, false); err != nil {
		return err
	}
	if err := m.validateBinding(ctx, r.To, true); err != nil {
		return err
	}
	if err := m.verifyDefinition(ctx, r.Definition, r.From); err != nil {
		return fault("conflict", "部署记录校验失败：%v", err)
	}
	ino, err := inodeOf(r.To.Directory)
	if err == nil && (!r.Reverse || ino != r.OriginalInode) {
		return fault("conflict", "目标目录已存在：%s", r.To.Directory)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err = m.audit(ctx, r, r.From, false); err != nil {
		return fault("conflict", "%v", err)
	}
	return m.capacity(ctx, r)
}

func (m *Manager) step(r *record, name string, fn func() error) error {
	r.Phase = name
	if r.StepTimes == nil {
		r.StepTimes = make(map[string]int64)
	}
	if err := m.saveRecord(r); err != nil {
		return err
	}
	start := time.Now()
	err := fn()
	prefix := ""
	if r.Reverse {
		prefix = "reverse_"
	}
	r.StepTimes[prefix+name] = time.Since(start).Milliseconds()
	return errors.Join(err, m.saveRecord(r))
}

func (m *Manager) move(ctx context.Context, reg *registry, r *record, recovering bool) error {
	r.Error = ""
	if err := m.step(r, "prepare", func() error { return m.prepare(ctx, r, recovering) }); err != nil {
		return m.failed(ctx, reg, r, err)
	}
	if err := m.step(r, "copy", func() error { return m.copy(ctx, r) }); err != nil {
		return m.failed(ctx, reg, r, err)
	}
	if err := m.step(r, "verify", func() error { return m.verifyCopy(ctx, r) }); err != nil {
		return m.failed(ctx, reg, r, err)
	}
	if err := m.step(r, "switch", func() error { return m.switchBinding(ctx, reg, r) }); err != nil {
		return m.failed(ctx, reg, r, err)
	}
	return m.finish(ctx, reg, r)
}

func (m *Manager) prepare(ctx context.Context, r *record, recovering bool) error {
	if err := m.preflight(ctx, r); err != nil {
		return err
	}
	d, err := m.findDeployment(ctx, r.From.Component, r.From.ComponentID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(d, r.Definition) {
		return fault("conflict", "组件部署记录已经变化，请重新申请")
	}
	// 执行器接受 Docker 写操作时同样会持有 Docker 资源锁。
	pending, err := m.Store.List(ctx, true)
	if err != nil {
		return err
	}
	for _, op := range pending {
		if op.Action != nil && slices.ContainsFunc(protocol.ActionResources(*op.Action), func(c protocol.ResourceClaim) bool { return c.Key == "docker" }) {
			return fault("node_busy", "仍有容器或数据库操作 %s", op.Record.ID)
		}
	}
	r.Before, err = m.audit(ctx, r, r.From, false)
	if err != nil {
		return err
	}
	if !recovering || d.Database != nil && d.Database.Kind == "galera" {
		if slices.ContainsFunc(r.Before, func(s dockerengine.StorageContainerState) bool { return !s.Running }) {
			return fault("conflict", "原组件存在未运行容器，请先恢复业务")
		}
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = m.waitReady(check, d, r.From, true)
		cancel()
		if err != nil {
			return err
		}
	}
	if !r.Reverse {
		r.OriginalInode, err = inodeOf(r.Original.Directory)
		if err != nil {
			return err
		}
	}
	if _, err = output(ctx, "sync", "-f", r.From.Directory); err != nil {
		return err
	}
	if d.Pool != nil {
		if err = d.Pool.sync(ctx, r.From.Directory); err != nil {
			return err
		}
	}
	r.StopIntent = true
	if err = m.saveRecord(r); err != nil {
		return err
	}
	start := time.Now()
	if err = m.stop(ctx, r.From, r.Before); err != nil {
		return err
	}
	if d.Pool != nil {
		if err = d.Pool.unmount(ctx, r.From.Directory); err != nil {
			return err
		}
	}
	r.StepTimes["stop"] = time.Since(start).Milliseconds()
	start = time.Now()
	if err = drain(ctx, drainBudget); err != nil {
		return err
	}
	r.StepTimes["drain"] = time.Since(start).Milliseconds()
	states, err := m.audit(ctx, r, r.From, false)
	if err != nil {
		return err
	}
	for _, state := range states {
		if state.Running || state.Restart != "no" || state.Exit != 0 && (r.Definition.Database != nil || state.Exit != 143) {
			return fault("conflict", "容器 %s 未完成干净停机（退出码 %d）", state.ID, state.Exit)
		}
	}
	return checkWriters(r.From.Directory, r.To.Directory)
}

func (m *Manager) switchBinding(ctx context.Context, reg *registry, r *record) error {
	if err := m.validateBinding(ctx, r.To, true); err != nil {
		return err
	}
	if err := m.publishCopy(r); err != nil {
		return err
	}
	if err := m.installUnit(ctx, r.To); err != nil {
		return err
	}
	reg.setBinding(r.To)
	if err := m.save(reg); err != nil {
		return err
	}
	start := time.Now()
	if err := drain(ctx, drainBudget); err != nil {
		return err
	}
	if err := changeSystemd(ctx, reloadBudget, "daemon-reload"); err != nil {
		return err
	}
	r.StepTimes["reload"] = time.Since(start).Milliseconds()
	states, err := m.audit(ctx, r, r.From, false)
	if err != nil {
		return err
	}
	if err = checkWriters(r.From.Directory, r.To.Directory); err != nil {
		return err
	}
	if err = m.Docker.RemoveStoppedStorage(ctx, states); err != nil {
		return err
	}
	if err = m.stage(ctx, r.Definition, r.To); err != nil {
		return err
	}
	return m.openTarget(ctx, r)
}

func (m *Manager) openTarget(ctx context.Context, r *record) error {
	states, err := m.audit(ctx, r, r.To, false)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(states, func(s dockerengine.StorageContainerState) bool { return s.Running || s.Restart != "no" }) {
		return fault("conflict", "待切换容器在放行前已运行或重启策略改变")
	}
	if err = m.validateBinding(ctx, r.To, false); err != nil {
		return err
	}
	r.WriteIntent = true
	if err = m.saveRecord(r); err != nil {
		return err
	}
	return changeSystemd(ctx, startBudget, "start", unitName(r.To))
}

func (m *Manager) finish(ctx context.Context, reg *registry, r *record) error {
	err := m.step(r, "cleanup", func() error {
		if err := m.waitReady(ctx, r.Definition, r.To, false); err != nil {
			return err
		}
		states, err := m.audit(ctx, r, r.To, false)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(states, func(s dockerengine.StorageContainerState) bool { return !s.Running || s.Restart != "no" }) {
			return fault("conflict", "新根上运行中的受管容器不完整")
		}
		return checkWriters(r.From.Directory)
	})
	if err != nil {
		return m.needs(r, err)
	}
	final := "complete"
	if r.Reverse {
		final = "rolled_back"
	}
	return m.end(reg, r, final)
}

func (m *Manager) waitReady(ctx context.Context, d definition, b binding, stopping bool) error {
	ctx, cancel := context.WithTimeout(ctx, startBudget)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := m.validateBinding(ctx, b, false); err != nil {
			return err
		}
		err := m.healthy(ctx, d, b, stopping)
		if err == nil {
			return nil
		}
		var problem *firewall.Fault
		if errors.As(err, &problem) && problem.Code == "conflict" {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("组件就绪等待结束：%w", err)
		case <-ticker.C:
		}
	}
}

func (m *Manager) failed(ctx context.Context, reg *registry, r *record, err error) error {
	r.Error = err.Error()
	if r.WriteIntent || errors.Is(err, systemdTimeout) || ctx.Err() != nil {
		return m.needs(r, err)
	}
	if !r.StopIntent {
		if r.Reverse {
			r.Reverse = false
			r.From, r.To = r.Original, r.From
			r.WriteIntent = true
			if r.ReversePhase == "needs_recovery" {
				return m.needs(r, err)
			}
			return errors.Join(err, m.end(reg, r, "complete"))
		}
		// 缺盘等预检失败保留同一待应用申请。
		r.Phase = "pending_restart"
		return errors.Join(err, m.saveRecord(r))
	}
	return errors.Join(err, m.restoreOld(ctx, reg, r))
}

func (m *Manager) needs(r *record, err error) error {
	r.Phase = "needs_recovery"
	r.Error = err.Error()
	return errors.Join(err, m.saveRecord(r))
}

func (m *Manager) end(reg *registry, r *record, phase string) error {
	r.Phase = phase
	if err := m.saveRecord(r); err != nil {
		return err
	}
	reg.Pending = ""
	return m.save(reg)
}

func (m *Manager) restoreOld(ctx context.Context, reg *registry, r *record) error {
	if err := m.restoreSource(ctx, reg, r); err != nil {
		return m.needs(r, err)
	}
	r.Restored = true
	if r.Reverse {
		r.Reverse = false
		r.From, r.To = r.Original, r.From
		r.WriteIntent = true
		return m.end(reg, r, "complete")
	}
	return m.end(reg, r, "rolled_back")
}

func (m *Manager) restoreSource(ctx context.Context, reg *registry, r *record) error {
	if r.WriteIntent {
		return fault("conflict", "目标可能有新写入，需从实际新根反迁")
	}
	if err := drain(ctx, drainBudget); err != nil {
		return err
	}
	if err := m.validateBinding(ctx, r.From, false); err != nil {
		return err
	}
	if !r.StopIntent {
		return nil
	}
	all, err := m.auditBoth(ctx, r)
	if err != nil {
		return err
	}
	source, sourceErr := m.audit(ctx, r, r.From, false)
	if sourceErr != nil {
		if err = checkWriters(r.To.Directory); err != nil {
			return err
		}
		if err = m.Docker.RemoveStoppedStorage(ctx, all); err != nil {
			return err
		}
		if r.Definition.Pool != nil {
			if err = r.Definition.Pool.unmount(ctx, r.To.Directory); err != nil {
				return err
			}
		}
		if err = m.stage(ctx, r.Definition, r.From); err != nil {
			return err
		}
		source, err = m.audit(ctx, r, r.From, false)
		if err != nil {
			return err
		}
	}
	if err = m.unpublishBeforeOpen(ctx, r); err != nil {
		return err
	}
	if b, found := reg.lookup(r.From.Component, r.From.ComponentID); found && b.Directory != r.From.Directory {
		if r.From.Guarded {
			reg.setBinding(r.From)
		} else {
			reg.Bindings = slices.DeleteFunc(reg.Bindings, func(b binding) bool { return b.Component == r.From.Component && b.ComponentID == r.From.ComponentID })
		}
		if err = m.save(reg); err != nil {
			return err
		}
	}
	r.Phase = "restore_old"
	if err = m.saveRecord(r); err != nil {
		return err
	}
	if r.From.Guarded {
		if err = m.installUnit(ctx, r.From); err != nil {
			return err
		}
	} else if err = m.removeUnit(r.From); err != nil {
		return err
	}
	if err = changeSystemd(ctx, reloadBudget, "daemon-reload"); err != nil {
		return err
	}
	if r.From.Guarded {
		err = changeSystemd(ctx, startBudget, "start", unitName(r.From))
	} else {
		if r.Definition.Pool != nil {
			if err = r.Definition.Pool.mount(ctx, r.From.Directory); err != nil {
				return err
			}
		}
		err = m.Docker.StartStorage(ctx, source, r.Before)
	}
	if err != nil {
		return err
	}
	return m.waitReady(ctx, r.Definition, r.From, false)
}

func (m *Manager) reverse(ctx context.Context, reg *registry, r *record, recovering bool) error {
	b, found := reg.lookup(r.To.Component, r.To.ComponentID)
	if !found || b.Directory != r.To.Directory || b.Record != r.ID {
		return fault("conflict", "当前组件绑定已被后续操作修改")
	}
	rev := *r
	rev.Reverse = true
	rev.ReversePhase = r.Phase
	rev.From, rev.To = b, r.Original
	rev.To.Guarded, rev.To.Record = true, r.ID
	rev.WriteIntent, rev.StopIntent, rev.StageInode = false, false, 0
	rev.Error = ""
	if err := m.preflight(ctx, &rev); err != nil {
		return err
	}
	if reg.Pending != r.ID {
		// 完整保存反迁意图后才会触及写入方。
		reg.Pending = r.ID
		if err := m.save(reg); err != nil {
			return err
		}
	}
	return m.move(ctx, reg, &rev, recovering)
}

func (m *Manager) repairTarget(ctx context.Context, reg *registry, r *record) error {
	b, found := reg.lookup(r.To.Component, r.To.ComponentID)
	if !found || !reflect.DeepEqual(b, r.To) {
		return m.needs(r, fault("conflict", "实际绑定与目标申请不同"))
	}
	err := m.step(r, "switch", func() error {
		if err := m.validateBinding(ctx, b, false); err != nil {
			return err
		}
		if err := m.verifyDefinition(ctx, r.Definition, b); err != nil {
			return err
		}
		states, err := m.audit(ctx, r, b, true)
		if err != nil {
			return err
		}
		if err = m.stop(ctx, b, states); err != nil {
			return err
		}
		if err = drain(ctx, drainBudget); err != nil {
			return err
		}
		if err = checkWriters(r.From.Directory, r.To.Directory); err != nil {
			return err
		}
		complete, check := m.audit(ctx, r, b, false)
		if check != nil {
			states, err = m.audit(ctx, r, b, true)
			if err != nil {
				return err
			}
			if err = m.Docker.RemoveStoppedStorage(ctx, states); err != nil {
				return err
			}
			if err = m.stage(ctx, r.Definition, b); err != nil {
				return err
			}
		} else if slices.ContainsFunc(complete, func(s dockerengine.StorageContainerState) bool { return s.Running }) {
			return fault("conflict", "目标组件仍在运行")
		}
		if err = m.installUnit(ctx, b); err != nil {
			return err
		}
		if err = changeSystemd(ctx, reloadBudget, "daemon-reload"); err != nil {
			return err
		}
		return m.openTarget(ctx, r)
	})
	if err != nil {
		return m.needs(r, err)
	}
	r.Error = ""
	return m.finish(ctx, reg, r)
}
