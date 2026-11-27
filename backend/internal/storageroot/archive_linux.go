package storageroot

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (m *Manager) PrepareArchive(ctx context.Context, d protocol.MonitoringDeployment) error {
	if err := d.Validate(); err != nil {
		return err
	}
	unlock, err := localstate.Lock(filepath.Join(m.stateDir(), "lock"))
	if err != nil {
		return err
	}
	defer unlock()
	r, err := m.paths(ctx)
	if err != nil {
		return err
	}
	if _, ok := r.lookup(protocol.StorageArchive, d.ClusterID); ok {
		return nil
	}
	root, err := r.root(d.StorageRootID)
	if err != nil {
		return err
	}
	b := binding{StorageBinding: protocol.StorageBinding{Component: protocol.StorageArchive,
		ComponentID: d.ClusterID, RootID: root.ID, Directory: filepath.Join(root.Base, "archive")}, Identity: root, Guarded: true}
	if d.Directory != b.Directory {
		return fault("relocation_required", "归档目录须由登记根派生")
	}
	if err = m.validateBinding(ctx, b, true); err != nil {
		return err
	}
	if _, err := os.Lstat(b.Directory); err == nil || !errors.Is(err, os.ErrNotExist) {
		return fault("conflict", "归档初始目录已存在：%s", b.Directory)
	}
	for _, prior := range r.Bindings {
		if overlaps(prior.Directory, b.Directory) {
			return fault("conflict", "归档目录与现有组件重叠")
		}
	}
	if _, err = m.Docker.AuditStorage(ctx, []string{b.Directory}, nil, true); err != nil {
		return fault("conflict", "%v", err)
	}
	r.setBinding(b)
	return m.save(&r)
}

func (m *Manager) ApplyArchive(ctx context.Context, d protocol.MonitoringDeployment) (bool, error) {
	r, err := m.paths(ctx)
	if err != nil {
		return true, err
	}
	if _, ok := r.lookup(protocol.StorageArchive, d.ClusterID); !ok {
		return false, nil
	}
	unlock, err := localstate.Lock(filepath.Join(m.stateDir(), "lock"))
	if err != nil {
		return true, err
	}
	defer unlock()
	r, err = m.paths(ctx)
	if err != nil {
		return true, err
	}
	b, ok := r.lookup(protocol.StorageArchive, d.ClusterID)
	if !ok {
		return false, nil
	}
	p, err := m.pending(r)
	if err != nil {
		return true, err
	}
	if p != nil && p.From.Component == b.Component && p.From.ComponentID == b.ComponentID {
		return true, fault("node_busy", "归档存在待处理的迁移 %s", p.ID)
	}
	d.Directory = b.Directory
	previous := b.Archive
	if previous == nil && b.Record != "" {
		rec, err := m.readRecord(b.Record)
		if err != nil {
			return true, err
		}
		previous = rec.Definition.Archive
	}
	var specs []dockerengine.StorageContainer
	if previous != nil {
		old := *previous
		old.Directory = b.Directory
		specs, err = m.Docker.StorageArchiveSpec(old)
		if err != nil {
			return true, err
		}
	}
	if d.Enabled {
		if err = m.validateBinding(ctx, b, false); err != nil {
			return true, err
		}
		next, err := m.Docker.StorageArchiveSpec(d)
		if err != nil {
			return true, err
		}
		specs = append(specs, next...)
	}
	states, err := m.Docker.AuditStorage(ctx, []string{b.Directory}, specs, true)
	if err != nil {
		return true, err
	}
	if previous != nil {
		b.Archive, b.Paused = previous, true
		r.setBinding(b)
		if err = m.save(&r); err != nil {
			return true, err
		}
		if err = m.stop(ctx, b, states); err != nil {
			return true, err
		}
	}
	if !d.Enabled {
		return true, nil
	}
	// 归档部署由独立操作单元执行，其启动作业会等待当前函数返回。
	if err = drainExcept(ctx, drainBudget, operationUnit()); err != nil {
		return true, err
	}
	states, err = m.Docker.AuditStorage(ctx, []string{b.Directory}, specs, true)
	if err != nil {
		return true, err
	}
	if err = m.Docker.RemoveStoppedStorage(ctx, states); err != nil {
		return true, err
	}
	if err = m.stage(ctx, definition{Archive: &d}, b); err != nil {
		return true, err
	}
	if err = m.installUnit(ctx, b); err != nil {
		return true, err
	}
	if err = changeSystemd(ctx, reloadBudget, "daemon-reload"); err != nil {
		return true, err
	}
	b.Archive, b.Paused = &d, false
	r.setBinding(b)
	if err = m.save(&r); err != nil {
		return true, err
	}
	return true, changeSystemd(ctx, startBudget, "start", unitName(b))
}

func (m *Manager) StartDatabase(ctx context.Context, id string) (bool, error) {
	dir, err := m.Directory(ctx, protocol.StorageDatabase, id, true)
	if err != nil || dir == "" {
		return dir != "", err
	}
	b := binding{StorageBinding: protocol.StorageBinding{Component: protocol.StorageDatabase, ComponentID: id}}
	return true, changeSystemd(ctx, startBudget, "start", unitName(b))
}
