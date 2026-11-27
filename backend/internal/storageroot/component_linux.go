package storageroot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

type definition struct {
	Archive  *protocol.MonitoringDeployment `json:"archive,omitempty"`
	Database *protocol.DatabaseDeployment   `json:"database,omitempty"`
	Pool     *poolDeployment                `json:"pool,omitempty"`
}

func (d definition) key() (string, string) {
	if d.Archive != nil {
		return protocol.StorageArchive, d.Archive.ClusterID
	}
	if d.Database != nil {
		return protocol.StorageDatabase, d.Database.ID
	}
	if d.Pool != nil {
		return protocol.StoragePoolData, d.Pool.ID
	}
	return "", ""
}

func (d definition) enabled() bool {
	return d.Archive != nil && d.Archive.Enabled || d.Database != nil || d.Pool != nil
}

func (d definition) directory() string {
	if d.Archive != nil {
		return d.Archive.Directory
	}
	if d.Pool != nil {
		return d.Pool.Directory
	}
	return filepath.Join(d.Database.Directory, "data")
}

func (d definition) target(base string) string {
	if d.Archive != nil {
		return filepath.Join(base, "archive")
	}
	if d.Pool != nil {
		return filepath.Join(base, "pools", d.Pool.ID)
	}
	return filepath.Join(base, "databases", d.Database.ID, "data")
}

func (d definition) archiveAt(path string) protocol.MonitoringDeployment {
	out := *d.Archive
	out.Directory = path
	return out
}

func (m *Manager) specs(d definition, b binding) ([]dockerengine.StorageContainer, error) {
	if d.Archive != nil {
		return m.Docker.StorageArchiveSpec(d.archiveAt(b.Directory))
	}
	if d.Database != nil {
		if err := d.Database.Validate(); err != nil {
			return nil, err
		}
		return dockerengine.StorageDatabaseSpec(*d.Database, b.Directory), nil
	}
	if d.Pool != nil {
		return d.Pool.specs(b.Directory)
	}
	return nil, fmt.Errorf("受管组件定义缺失")
}

func (m *Manager) verifyDefinition(ctx context.Context, d definition, b binding) error {
	if d.Archive != nil {
		return dockerengine.VerifyStorageArchive(d.archiveAt(b.Directory))
	}
	if d.Pool != nil {
		return d.Pool.image(ctx, b.Directory)
	}
	return databaseengine.VerifyDeploymentRecord(*d.Database)
}

func (m *Manager) stage(ctx context.Context, d definition, b binding) error {
	if err := m.validateBinding(ctx, b, false); err != nil {
		return err
	}
	if d.Archive != nil {
		return m.Docker.StageStorageArchive(ctx, d.archiveAt(b.Directory), b.Guarded)
	}
	if d.Pool != nil {
		if err := d.Pool.mount(ctx, b.Directory); err != nil {
			return err
		}
		return m.Docker.StageStoragePool(ctx, d.Pool.ID, b.Directory, d.Pool.Templates)
	}
	return m.Docker.StageStorageDatabase(ctx, *d.Database, b.Directory, b.Guarded)
}

func (m *Manager) healthy(ctx context.Context, d definition, b binding, stopping bool) error {
	if d.Archive != nil {
		s, err := m.Docker.MonitoringState(ctx, d.archiveAt(b.Directory))
		if err != nil {
			return err
		}
		if s.State != "ready" {
			return fmt.Errorf("归档组件状态 %s，等待查询与收发组件就绪", s.State)
		}
		return nil
	}
	if d.Pool != nil {
		if dev, err := d.Pool.activeLoop(ctx, b.Directory); err != nil || dev == "" {
			return fault("conflict", "池数据镜像未挂载：%s %v", b.Directory, err)
		}
		specs, err := d.Pool.specs(b.Directory)
		if err != nil {
			return err
		}
		states, err := m.Docker.AuditStorage(ctx, nil, specs, false)
		if err != nil {
			return err
		}
		for _, s := range states {
			if !s.Running {
				return fault("dependency_unavailable", "池数据容器 %s 未运行", s.ID)
			}
		}
		return nil
	}
	s, err := (databaseengine.Manager{}).Read(ctx, d.Database.Connection, "health")
	if err != nil {
		return err
	}
	if s.State != "ready" || d.Database.Kind == "galera" && (!s.Primary || !s.Ready || !s.Synced ||
		d.Database.ClusterID != "" && s.ClusterID != d.Database.ClusterID) {
		return fmt.Errorf("数据库状态 %s，成员未就绪", s.State)
	}
	if stopping && d.Database.Kind == "galera" {
		configured := len(strings.Split(strings.TrimPrefix(s.ConfiguredMembers, "gcomm://"), ","))
		if configured < 3 || s.ClusterSize-1 <= configured/2 {
			return fault("conflict", "停止本机后 Galera 不能维持多数派，当前成员 %d，已配置 %d", s.ClusterSize, configured)
		}
	}
	return nil
}

func (m *Manager) scanRoots(r *record) []string {
	roots := []string{r.From.Directory, r.To.Directory}
	for _, b := range []binding{r.From, r.To} {
		if b.RootID != "" {
			roots = append(roots, b.Identity.Base)
		} else {
			roots = append(roots, "/var/lib/opsd")
			if containsPath(storage.DefaultDatasetDir, b.Directory) {
				roots = append(roots, storage.DefaultDatasetDir)
			}
		}
	}
	return roots
}

func (m *Manager) audit(ctx context.Context, r *record, b binding, missingOK bool) ([]dockerengine.StorageContainerState, error) {
	specs, err := m.specs(r.Definition, b)
	if err != nil {
		return nil, err
	}
	return m.auditStorage(ctx, r, specs, missingOK)
}

func (m *Manager) auditBoth(ctx context.Context, r *record) ([]dockerengine.StorageContainerState, error) {
	from, err := m.specs(r.Definition, r.From)
	if err != nil {
		return nil, err
	}
	to, err := m.specs(r.Definition, r.To)
	if err != nil {
		return nil, err
	}
	return m.auditStorage(ctx, r, append(from, to...), true)
}

func (m *Manager) auditStorage(ctx context.Context, req *record, specs []dockerengine.StorageContainer, missingOK bool) ([]dockerengine.StorageContainerState, error) {
	peers := dockerengine.StoragePeers{MovingDirectories: []string{req.From.Directory, req.To.Directory}}
	r, err := m.paths(ctx)
	if err != nil {
		return nil, err
	}
	deployments, err := m.deployed(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range deployments {
		component, id := d.key()
		if component == req.From.Component && id == req.From.ComponentID {
			continue
		}
		b, found := r.lookup(component, id)
		if found && b.Archive != nil {
			d.Archive = b.Archive
		}
		if !d.enabled() {
			continue
		}
		if !found {
			b.Directory = d.directory()
		}
		if err := m.verifyDefinition(ctx, d, b); err == nil {
			if other, err := m.specs(d, b); err == nil {
				peers.Specs = append(peers.Specs, other...)
			}
		}
	}
	return m.Docker.AuditStorage(ctx, m.scanRoots(req), specs, missingOK, peers)
}
