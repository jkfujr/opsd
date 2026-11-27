package dockerengine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// StoragePoolTemplate 来自本机 root 部署记录。模板中的 data 相对绑定随数据镜像迁移。
type StoragePoolTemplate struct {
	Name     string                    `json:"name"`
	Config   container.Config          `json:"config"`
	Host     container.HostConfig      `json:"host"`
	Networks *network.NetworkingConfig `json:"networks,omitempty"`
}

func (t StoragePoolTemplate) StorageSpec(poolID, directory string) (StorageContainer, error) {
	out := StorageContainer{Name: t.Name}
	if t.Name == "" || !(strings.HasPrefix(t.Config.Image, "sha256:") || strings.Contains(t.Config.Image, "@sha256:")) ||
		t.Host.AutoRemove || len(t.Host.Binds) > 0 || len(t.Host.VolumesFrom) > 0 {
		return out, fmt.Errorf("存储池数据容器模板无效")
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(raw)
	out.Labels = map[string]string{"com.opsd.storage.pool": poolID, "com.opsd.storage.pool-spec": fmt.Sprintf("%x", sum[:])}
	writable := false
	for _, b := range t.Host.Mounts {
		if b.Type != mount.TypeBind || b.Target == "/" || !filepath.IsAbs(b.Target) || filepath.Clean(b.Target) != b.Target {
			return out, fmt.Errorf("池数据容器仅支持明确的目录绑定")
		}
		switch {
		case filepath.IsLocal(b.Source) && filepath.Clean(b.Source) == b.Source && pathWithin("data", b.Source):
			b.Source = filepath.Join(directory, b.Source)
			writable = writable || !b.ReadOnly
		case filepath.IsAbs(b.Source) && b.ReadOnly:
		default:
			return out, fmt.Errorf("池数据容器只写入其受管数据镜像")
		}
		out.Binds = append(out.Binds, StorageBind{b.Source, b.Target, b.ReadOnly})
	}
	if !writable {
		return out, fmt.Errorf("池数据容器缺少可写数据绑定")
	}
	return out, nil
}

func (m *Manager) StageStoragePool(ctx context.Context, poolID, directory string, templates []StoragePoolTemplate) error {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	for _, t := range templates {
		spec, err := t.StorageSpec(poolID, directory)
		if err != nil {
			return err
		}
		image, err := c.ImageInspect(ctx, t.Config.Image)
		if err != nil {
			return err
		}
		config, host := t.Config, t.Host
		config.Image = image.ID
		config.Labels = make(map[string]string, len(t.Config.Labels)+len(spec.Labels))
		maps.Copy(config.Labels, t.Config.Labels)
		maps.Copy(config.Labels, spec.Labels)
		host.RestartPolicy = container.RestartPolicy{Name: "no"}
		host.Mounts = make([]mount.Mount, 0, len(spec.Binds))
		for _, b := range spec.Binds {
			host.Mounts = append(host.Mounts, mount.Mount{Type: mount.TypeBind, Source: b.Source, Target: b.Target, ReadOnly: b.ReadOnly})
		}
		if _, err = c.ContainerCreate(ctx, client.ContainerCreateOptions{Name: spec.Name, Config: &config, HostConfig: &host,
			NetworkingConfig: t.Networks}); err != nil {
			return err
		}
	}
	return nil
}
