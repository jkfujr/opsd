package databaseengine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

// managedDeployment 只读取本系统创建且定义仍相符的容器。
func (m Manager) managedDeployment(ctx context.Context, id string) (protocol.DatabaseDeployment, error) {
	var d protocol.DatabaseDeployment
	if m.Docker == nil {
		return d, fmt.Errorf("Docker 入口不可用")
	}
	container, err := m.Docker.DeploymentContainer(ctx, id)
	if err != nil {
		return d, err
	}
	for _, mount := range container.Mounts {
		if mount.Destination != "/etc/mysql/conf.d/opsd.cnf" {
			continue
		}
		path := filepath.Join(filepath.Dir(mount.Source), "profile.json")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 256*1024 {
			break
		}
		data, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(data, &d) != nil || d.Validate() != nil {
			break
		}
		if _, err = deploymentParent(d.Directory); err != nil {
			return d, err
		}
		actualID, _, err := m.Docker.DeploymentState(ctx, d)
		if err != nil {
			return d, err
		}
		if actualID != id {
			break
		}
		return d, nil
	}
	return d, fmt.Errorf("此实例缺少本系统的部署资料，保留原启动配置")
}

func (m Manager) observeDeploymentSeeds(ctx context.Context, u protocol.DatabaseSeedUpdate) (Observation, error) {
	out := Observation{CheckedAt: time.Now().UTC()}
	if err := u.Validate(); err != nil {
		return out, err
	}
	d, err := m.managedDeployment(ctx, u.ContainerID)
	if err != nil {
		return out, err
	}
	path := filepath.Join(d.Directory, "state", "seeds")
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Size() > 8192) {
		return out, fmt.Errorf("数据库启动地址文件已经变化")
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return out, err
	}
	out.Fingerprint = fingerprint(string(data))
	out.Satisfied = strings.TrimSpace(string(data)) == strings.Join(u.Seeds, ",")
	return out, nil
}

func (m Manager) applyDeploymentSeeds(ctx context.Context, u protocol.DatabaseSeedUpdate) error {
	if _, err := m.observeDeploymentSeeds(ctx, u); err != nil {
		return err
	}
	d, err := m.managedDeployment(ctx, u.ContainerID)
	if err != nil {
		return err
	}
	dir := filepath.Join(d.Directory, "state")
	file, err := os.CreateTemp(dir, ".seeds-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.WriteString(strings.Join(u.Seeds, ",") + "\n")
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), filepath.Join(dir, "seeds")); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
