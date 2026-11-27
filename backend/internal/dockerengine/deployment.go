package dockerengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/containerd/errdefs"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const deploymentLabel = "com.opsd.database-deployment"
const deploymentSpecLabel = "com.opsd.database-spec"

func deploymentFingerprint(d protocol.DatabaseDeployment) string {
	// 集群身份在首次启动后取得，不改变已经批准的容器定义。
	d.ClusterID = ""
	b, _ := json.Marshal(d)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) DeploymentState(ctx context.Context, d protocol.DatabaseDeployment) (id, state string, err error) {
	path, err := m.deploymentStorage(ctx, d, false)
	if err != nil {
		return "", "", err
	}
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return "", "", err
	}
	defer c.Close()
	state = "absent"
	for _, name := range []string{d.ContainerName(), d.LegacyContainerName()} {
		result, e := c.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(e) {
			continue
		}
		if e != nil {
			return "", "", e
		}
		raw := result.Container
		if raw.Config == nil || raw.Config.Labels[deploymentLabel] != d.ID || raw.Config.Labels[deploymentSpecLabel] != deploymentFingerprint(d) {
			return "", "", fmt.Errorf("同名容器已存在或部署配置已经变化")
		}
		if path != "" && !storageMatches(raw, StorageDatabaseSpec(d, path)[0]) {
			return "", "", fmt.Errorf("数据库存储绑定不同")
		}
		if id != "" || raw.State == nil {
			return "", "", fmt.Errorf("部署容器重复或状态缺失")
		}
		id, state = raw.ID, string(raw.State.Status)
	}
	return id, state, nil
}

func (m *Manager) DeploymentImageReady(ctx context.Context, image string) (bool, error) {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return false, err
	}
	defer c.Close()
	_, err = c.ImageInspect(ctx, image)
	if errdefs.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (m *Manager) PullDeploymentImage(ctx context.Context, image string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	c, _, _, err := m.connect(ctx, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	response, err := c.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	for message, err := range response.JSONMessages(ctx) {
		if err != nil {
			return err
		}
		if message.Error != nil {
			return fmt.Errorf("镜像拉取失败：%s", message.Error.Message)
		}
	}
	return nil
}

func (m *Manager) CreateDeployment(ctx context.Context, d protocol.DatabaseDeployment) error {
	if err := d.Validate(); err != nil {
		return err
	}
	_, state, err := m.DeploymentState(ctx, d)
	if err != nil {
		return err
	}
	if state != "absent" {
		return fmt.Errorf("原部署容器已经存在，请核对原操作")
	}
	dataDir, err := m.deploymentStorage(ctx, d, true)
	if err != nil {
		return err
	}
	return m.StageStorageDatabase(ctx, d, dataDir, dataDir != "")
}

func (m *Manager) deploymentStorage(ctx context.Context, d protocol.DatabaseDeployment, write bool) (string, error) {
	if m.StorageBinding == nil {
		return "", nil
	}
	return m.StorageBinding(ctx, protocol.StorageDatabase, d.ID, write)
}

func (m *Manager) StageStorageDatabase(ctx context.Context, d protocol.DatabaseDeployment, dataDir string, guarded bool) error {
	if dataDir == "" {
		dataDir = filepath.Join(d.Directory, "data")
	}
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	image, err := c.ImageInspect(ctx, d.Image)
	if err != nil {
		return err
	}
	config := &container.Config{Image: image.ID, Labels: map[string]string{deploymentLabel: d.ID, deploymentSpecLabel: deploymentFingerprint(d), sourceImageLabel: d.Image}}
	host := &container.HostConfig{NetworkMode: "host", RestartPolicy: container.RestartPolicy{Name: "unless-stopped"}}
	if guarded {
		host.RestartPolicy.Name = "no"
	}
	for _, b := range deploymentBinds(d, dataDir) {
		host.Mounts = append(host.Mounts, mount.Mount{Type: mount.TypeBind, Source: b.Source, Target: b.Target, ReadOnly: b.ReadOnly})
	}
	if d.Kind == "galera" {
		config.Env = []string{"MARIADB_ROOT_PASSWORD_FILE=/run/secrets/opsd-root", "MARIADB_ROOT_HOST=127.0.0.1"}
		config.Entrypoint = []string{"/bin/sh", "/opsd-start.sh"}
	} else {
		config.Entrypoint = []string{"/usr/bin/proxysql"}
		config.Cmd = []string{"-f", "-c", "/etc/proxysql.cnf"}
	}
	_, err = c.ContainerCreate(ctx, client.ContainerCreateOptions{Name: d.ContainerName(), Config: config, HostConfig: host})
	return err
}

func (m *Manager) StartDeployment(ctx context.Context, d protocol.DatabaseDeployment) error {
	if m.StorageDatabaseStart != nil {
		if handled, err := m.StorageDatabaseStart(ctx, d.ID); handled || err != nil {
			return err
		}
	}
	if _, err := m.deploymentStorage(ctx, d, true); err != nil {
		return err
	}
	id, state, err := m.DeploymentState(ctx, d)
	if err != nil {
		return err
	}
	if state != "created" {
		return fmt.Errorf("原部署容器已启动或状态已经改变，请核对原操作")
	}
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.ContainerStart(ctx, id, client.ContainerStartOptions{})
	return err
}

// DeploymentContainer 只供本机执行器提取部署资料，环境变量不返回浏览器。
func (m *Manager) DeploymentContainer(ctx context.Context, id string) (container.InspectResponse, error) {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return container.InspectResponse{}, err
	}
	defer c.Close()
	r, err := c.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	return r.Container, err
}
