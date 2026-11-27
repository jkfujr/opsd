package cluster

import (
	"context"
	"encoding/json"
	"path"

	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
)

func archiveStorage(view protocol.StorageRootView, id string) protocol.StorageBinding {
	for _, b := range view.Bindings {
		if b.Component == protocol.StorageArchive && b.ComponentID == id {
			return b
		}
	}
	return protocol.StorageBinding{}
}

func (n *Node) EffectiveArchiveConfig(ctx context.Context) (metricsstore.Config, error) {
	c, err := n.ArchiveConfig(ctx)
	if err != nil || c.NodeID == "" {
		return c, err
	}
	var b protocol.StorageBinding
	if n.Call(ctx, c.NodeID, "monitoring.storage", nil, &b) == nil && b.Directory != "" {
		c.Directory, c.StorageRootID = b.Directory, b.RootID
	}
	return c, nil
}

func (n *Node) normalizeArchiveStorage(ctx context.Context, raw []byte, fromAPI bool) ([]byte, error) {
	var c metricsstore.Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fault("invalid_request", "监控配置无效")
	}
	old, err := n.ArchiveConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !c.Enabled {
		c.Directory, c.StorageRootID = old.Directory, old.StorageRootID
		return json.Marshal(c)
	}
	if old.Enabled && old.NodeID == c.NodeID {
		b := protocol.StorageBinding{Directory: old.Directory, RootID: old.StorageRootID}
		if v, err := n.storageRoots(ctx); err == nil {
			if actual := archiveStorage(v, n.cluster); actual.Directory != "" {
				b = actual
			}
		}
		if c.Directory != "" && c.Directory != b.Directory || c.StorageRootID != "" && c.StorageRootID != b.RootID {
			return nil, fault("relocation_required", "已启用的归档须通过存储根迁移目录")
		}
		c.Directory, c.StorageRootID = old.Directory, old.StorageRootID
		return json.Marshal(c)
	}
	if !fromAPI && c.StorageRootID == "" && c.Directory != "" {
		return raw, nil
	}
	if c.StorageRootID == "" {
		c.StorageRootID = "default"
	}
	v, err := n.storageRoots(ctx)
	if err != nil {
		return nil, fault("dependency_unavailable", "无法确认归档目标存储根")
	}
	for _, root := range v.Roots {
		if root.ID != c.StorageRootID {
			continue
		}
		if root.State != "ready" {
			return nil, fault("dependency_unavailable", "归档目标存储根当前不可用")
		}
		dir := path.Join(root.Base, "archive")
		if c.Directory != "" && c.Directory != dir && c.Directory != old.Directory {
			return nil, fault("relocation_required", "归档目录由目标存储根派生")
		}
		c.Directory = dir
		return json.Marshal(c)
	}
	return nil, fault("invalid_request", "归档目标存储根未登记")
}

func (n *Node) ArchiveConfig(ctx context.Context) (metricsstore.Config, error) {
	return n.telemetry.Config(ctx)
}
func (n *Node) cacheArchiveConfig(ctx context.Context, c metricsstore.Config) error {
	return n.telemetry.CacheConfig(ctx, c)
}
func (n *Node) validateArchiveRequest(ctx context.Context, body []byte) error {
	return n.telemetry.ValidateRequest(ctx, body)
}
func (n *Node) advanceArchive(ctx context.Context, op *Operation, body []byte) error {
	return n.telemetry.Advance(ctx, telemetryParent{n, op}, body)
}
func (n *Node) decideArchive(ctx context.Context, method string, d OperationDecision, op Operation) (Operation, error) {
	err := n.telemetry.Decide(ctx, method, d.ExpectedRevision, telemetryParent{n, &op})
	return op, err
}
func (n *Node) archiveDeployment(ctx context.Context, c metricsstore.Config) (protocol.MonitoringDeployment, error) {
	return n.telemetry.Deployment(ctx, c)
}
