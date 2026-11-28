package databaseengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/deploymentdir"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/api/types/container"
)

// managedDeployment 只读取本系统创建且定义仍相符的容器。
func (m Manager) managedDeployment(ctx context.Context, raw container.InspectResponse) (protocol.DatabaseDeployment, error) {
	var d protocol.DatabaseDeployment
	if !dockerengine.IsDatabaseDeployment(raw) {
		return d, fmt.Errorf("此实例缺少本系统的部署资料，保留原启动配置")
	}
	for _, mount := range raw.Mounts {
		if mount.Destination != "/etc/mysql/conf.d/opsd.cnf" || filepath.Base(mount.Source) != "database.cnf" {
			continue
		}
		dir, err := deploymentdir.Open(filepath.Dir(mount.Source), deploymentdir.Existing)
		if err != nil {
			return d, err
		}
		file, err := deploymentdir.OpenFile(dir, "profile.json", os.O_RDONLY, deploymentdir.GaleraUID)
		dir.Close()
		if err != nil {
			return d, err
		}
		const limit = 256 * 1024
		data, err := io.ReadAll(io.LimitReader(file, limit+1))
		file.Close()
		if err != nil || len(data) > limit || json.Unmarshal(data, &d) != nil || d.Validate() != nil ||
			filepath.Join(d.Directory, "database.cnf") != mount.Source {
			break
		}
		actualID, _, err := m.Docker.DeploymentState(ctx, d)
		if err != nil {
			return d, err
		}
		if actualID != raw.ID {
			break
		}
		return d, nil
	}
	return d, fmt.Errorf("此实例缺少本系统的部署资料，保留原启动配置")
}

func (m Manager) deploymentSeedDirectory(ctx context.Context, u protocol.DatabaseSeedUpdate) (*os.File, error) {
	if err := u.Validate(); err != nil {
		return nil, err
	}
	if m.Docker == nil {
		return nil, fmt.Errorf("Docker 入口不可用")
	}
	raw, err := m.Docker.DeploymentContainer(ctx, u.ContainerID)
	if err != nil {
		return nil, err
	}
	d, err := m.managedDeployment(ctx, raw)
	if err != nil {
		return nil, err
	}
	return deploymentdir.Open(filepath.Join(d.Directory, "state"), deploymentdir.Existing)
}

func readDeploymentSeeds(dir *os.File) ([]byte, error) {
	file, err := deploymentdir.OpenFile(dir, "seeds", os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const limit = 8192
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if len(data) > limit {
		return nil, fmt.Errorf("数据库启动地址文件已经变化")
	}
	return data, err
}

func (m Manager) observeDeploymentSeeds(ctx context.Context, u protocol.DatabaseSeedUpdate) (Observation, error) {
	out := Observation{CheckedAt: time.Now().UTC()}
	dir, err := m.deploymentSeedDirectory(ctx, u)
	if err != nil {
		return out, err
	}
	defer dir.Close()
	data, err := readDeploymentSeeds(dir)
	if err != nil {
		return out, err
	}
	out.Fingerprint = fingerprint(string(data))
	out.Satisfied = strings.TrimSpace(string(data)) == strings.Join(u.Seeds, ",")
	return out, nil
}

func (m Manager) applyDeploymentSeeds(ctx context.Context, u protocol.DatabaseSeedUpdate) error {
	dir, err := m.deploymentSeedDirectory(ctx, u)
	if err != nil {
		return err
	}
	defer dir.Close()
	if _, err = readDeploymentSeeds(dir); err != nil {
		return err
	}
	name := ".seeds-" + identity.Random()
	file, err := deploymentdir.OpenFile(dir, name, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer deploymentdir.Remove(dir, name)
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
	return deploymentdir.Replace(dir, name, "seeds")
}
