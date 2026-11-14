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
	"github.com/moby/moby/client"
)

const deploymentLabel = "com.opsd.database-deployment"
const deploymentSpecLabel = "com.opsd.database-spec"

func deploymentName(d protocol.DatabaseDeployment) string { return "opsd-" + d.Kind + "-" + d.ID }

func deploymentFingerprint(d protocol.DatabaseDeployment) string {
	// 集群身份在首次启动后取得，不改变已经批准的容器定义。
	d.ClusterID = ""
	b, _ := json.Marshal(d)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) DeploymentState(ctx context.Context, d protocol.DatabaseDeployment) (id, state string, err error) {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return "", "", err
	}
	defer c.Close()
	result, err := c.ContainerInspect(ctx, deploymentName(d), client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return "", "absent", nil
	}
	if err != nil {
		return "", "", err
	}
	raw := result.Container
	if raw.Config == nil || raw.Config.Labels[deploymentLabel] != d.ID || raw.Config.Labels[deploymentSpecLabel] != deploymentFingerprint(d) {
		return "", "", fmt.Errorf("同名容器已存在或部署配置已经变化")
	}
	if raw.State == nil {
		return "", "", fmt.Errorf("容器状态缺失")
	}
	return raw.ID, string(raw.State.Status), nil
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
	bind := func(name, target string, readOnly bool) {
		mode := ":rw"
		if readOnly {
			mode = ":ro"
		}
		host.Binds = append(host.Binds, filepath.Join(d.Directory, name)+":"+target+mode)
	}
	if d.Kind == "galera" {
		bind("data", "/var/lib/mysql", false)
		bind("database.cnf", "/etc/mysql/conf.d/opsd.cnf", true)
		bind("init.sql", "/docker-entrypoint-initdb.d/opsd.sql", true)
		bind("password", "/run/secrets/opsd-root", true)
		bind("start.sh", "/opsd-start.sh", true)
		bind("state", "/opsd-state", false)
		if len(d.TLS) > 0 {
			bind("tls", "/etc/opsd-database-tls", true)
		}
		config.Env = []string{"MARIADB_ROOT_PASSWORD_FILE=/run/secrets/opsd-root", "MARIADB_ROOT_HOST=localhost"}
		config.Entrypoint = []string{"/bin/sh", "/opsd-start.sh"}
	} else {
		bind("data", "/var/lib/proxysql", false)
		bind("database.cnf", "/etc/proxysql.cnf", true)
		config.Entrypoint = []string{"/usr/bin/proxysql"}
		config.Cmd = []string{"-f", "-c", "/etc/proxysql.cnf"}
	}
	_, err = c.ContainerCreate(ctx, client.ContainerCreateOptions{Name: deploymentName(d), Config: config, HostConfig: host})
	return err
}

func (m *Manager) StartDeployment(ctx context.Context, d protocol.DatabaseDeployment) error {
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
