package dockerengine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"sync"

	"github.com/containerd/errdefs"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func (m *Manager) StoragePoolECImage(ctx context.Context) (string, error) {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()
	info, err := c.ImageInspect(ctx, protocol.PoolECImageTag)
	if errdefs.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.Config == nil || info.Config.Labels["org.opencontainers.image.version"] != protocol.PoolECVersion ||
		info.Config.Labels["org.opencontainers.image.revision"] != protocol.PoolECUpstreamRevision ||
		!strings.HasPrefix(info.ID, "sha256:") {
		return "", fmt.Errorf("本机 EC 镜像与 OPSD 固定构建版本不符")
	}
	return info.ID, nil
}

// StoragePoolTemplate 来自本机 root 部署记录。模板中的 data 相对绑定随数据镜像迁移。
type StoragePoolTemplate struct {
	Name     string                    `json:"name"`
	Config   container.Config          `json:"config"`
	Host     container.HostConfig      `json:"host"`
	Networks *network.NetworkingConfig `json:"networks,omitempty"`
}

func (m *Manager) RestartStoragePool(ctx context.Context, states []StorageContainerState) error {
	c, _, _, err := m.connect(ctx, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	// 同节点的角色彼此独立停机，共享一次等待窗口。
	timeout := 16
	results := make([]error, len(states))
	var wg sync.WaitGroup
	for i, state := range states {
		wg.Go(func() {
			_, results[i] = c.ContainerStop(ctx, state.ID, client.ContainerStopOptions{Timeout: &timeout})
		})
	}
	wg.Wait()
	if err := errors.Join(results...); err != nil {
		return err
	}
	for _, role := range []string{"etcd", "master", "volume", "filer", "s3"} {
		for _, state := range states {
			if strings.HasSuffix(state.Name, "-"+role) {
				if _, err := c.ContainerStart(ctx, state.ID, client.ContainerStartOptions{}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (t StoragePoolTemplate) StorageSpec(poolID, directory string) (StorageContainer, error) {
	return t.storageSpec(poolID, directory, "data")
}

func (t StoragePoolTemplate) StorageControlSpec(poolID, directory string) (StorageContainer, error) {
	return t.storageSpec(poolID, directory, "control")
}

func (t StoragePoolTemplate) storageSpec(poolID, directory, volume string) (StorageContainer, error) {
	out := StorageContainer{Name: t.Name, User: t.Config.User}
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
		case filepath.IsLocal(b.Source) && filepath.Clean(b.Source) == b.Source && pathWithin(volume, b.Source):
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
	return m.stageStoragePool(ctx, poolID, directory, "data", templates)
}

func (m *Manager) StageStoragePoolControl(ctx context.Context, poolID, directory string, templates []StoragePoolTemplate) error {
	return m.stageStoragePool(ctx, poolID, directory, "control", templates)
}

func (m *Manager) stageStoragePool(ctx context.Context, poolID, directory, volume string, templates []StoragePoolTemplate) error {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	for _, t := range templates {
		spec, err := t.storageSpec(poolID, directory, volume)
		if err != nil {
			return err
		}
		image, err := c.ImageInspect(ctx, t.Config.Image)
		if err != nil {
			return err
		}
		existing, err := c.ContainerInspect(ctx, spec.Name, client.ContainerInspectOptions{})
		if err == nil {
			if !storageMatches(existing.Container, spec) || existing.Container.Image != image.ID ||
				existing.Container.HostConfig.RestartPolicy.Name != "no" {
				return fmt.Errorf("池容器 %s 与已登记的模板不同", spec.Name)
			}
			continue
		}
		if !errdefs.IsNotFound(err) {
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
