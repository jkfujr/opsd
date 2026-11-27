package dockerengine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

type StorageBind struct {
	Source, Target string
	ReadOnly       bool
}

type StorageContainer struct {
	Name    string
	Aliases []string
	Labels  map[string]string
	Binds   []StorageBind
}

func (s StorageContainer) named(name string) bool {
	return s.Name == name || slices.Contains(s.Aliases, name)
}

type StorageContainerState struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Restart string `json:"restart"`
	Exit    int    `json:"exit_code"`
}

type StoragePeers struct {
	Specs             []StorageContainer
	MovingDirectories []string
}

func monitoringBinds(d protocol.MonitoringDeployment, name string) []StorageBind {
	if name == "query" {
		return nil
	}
	binds := []StorageBind{
		{filepath.Join(d.Directory, name), "/data", false},
		{filepath.Join(d.Directory, name+".yml"), "/config/objstore.yml", true},
	}
	if d.Storage.CAPEM != "" {
		binds = append(binds, StorageBind{filepath.Join(d.Directory, "s3-ca.pem"), "/config/s3-ca.pem", true})
	}
	return binds
}

func (m *Manager) StorageArchiveSpec(d protocol.MonitoringDeployment) ([]StorageContainer, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	project := monitoringProject(d)
	specs := make([]StorageContainer, 0, len(monitoringNames))
	for _, name := range monitoringNames {
		specs = append(specs, StorageContainer{
			Name: project + "-" + name, Binds: monitoringBinds(d, name),
			Labels: map[string]string{monitoringLabel: d.ClusterID, monitoringSpecLabel: monitoringFingerprint(d),
				"com.opsd.managed": "monitoring", "com.docker.compose.project": project, "com.docker.compose.service": name},
		})
	}
	return specs, nil
}

func VerifyStorageArchive(d protocol.MonitoringDeployment) error {
	if err := monitoringOwner(d); err != nil {
		return err
	}
	files, err := MonitoringFiles(d)
	if err != nil {
		return err
	}
	for name, expected := range files {
		if name == "compose.yml" {
			continue
		}
		path := filepath.Join(d.Directory, name)
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() || st.Size() != int64(len(expected)) {
			return fmt.Errorf("归档对象存储配置不同：%s", path)
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			return fmt.Errorf("归档对象存储配置不同：%s", path)
		}
	}
	return nil
}

func deploymentBinds(d protocol.DatabaseDeployment, dataDir string) []StorageBind {
	at := func(name, target string, ro bool) StorageBind {
		return StorageBind{filepath.Join(d.Directory, name), target, ro}
	}
	if d.Kind == "proxysql" {
		return []StorageBind{{dataDir, "/var/lib/proxysql", false}, at("database.cnf", "/etc/proxysql.cnf", true)}
	}
	binds := []StorageBind{
		{dataDir, "/var/lib/mysql", false}, at("database.cnf", "/etc/mysql/conf.d/opsd.cnf", true),
		at("init.sql", "/docker-entrypoint-initdb.d/opsd.sql", true), at("password", "/run/secrets/opsd-root", true),
		at("start.sh", "/opsd-start.sh", true), at("state", "/opsd-state", false),
	}
	if len(d.TLS) > 0 {
		binds = append(binds, at("tls", "/etc/opsd-database-tls", true))
	}
	return binds
}

func StorageDatabaseSpec(d protocol.DatabaseDeployment, dataDir string) []StorageContainer {
	return []StorageContainer{{
		Name: d.ContainerName(), Aliases: []string{d.LegacyContainerName()}, Binds: deploymentBinds(d, dataDir),
		Labels: map[string]string{deploymentLabel: d.ID, deploymentSpecLabel: deploymentFingerprint(d), sourceImageLabel: d.Image},
	}}
}

func storageMatches(actual container.InspectResponse, spec StorageContainer) bool {
	if actual.Config == nil || actual.HostConfig == nil || actual.State == nil || len(actual.Mounts) != len(spec.Binds) {
		return false
	}
	for key, value := range spec.Labels {
		if actual.Config.Labels[key] != value {
			return false
		}
	}
	for _, b := range actual.Mounts {
		if b.Type != mount.TypeBind || b.Propagation != mount.PropagationRPrivate || !slices.Contains(spec.Binds, StorageBind{b.Source, b.Destination, !b.RW}) {
			return false
		}
	}
	return true
}

func pathWithin(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// AuditStorage 同时检查已停止的容器。任一所有权证据缺失时拒绝接管。
func (m *Manager) AuditStorage(ctx context.Context, roots []string, specs []StorageContainer, allowMissing bool, peers ...StoragePeers) ([]StorageContainerState, error) {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	list, err := c.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}
	states := make([]StorageContainerState, 0, len(specs))
	for _, entry := range list.Items {
		result, err := c.ContainerInspect(ctx, entry.ID, client.ContainerInspectOptions{})
		if err != nil {
			return nil, err
		}
		actual := result.Container
		name := strings.TrimPrefix(actual.Name, "/")
		if slices.ContainsFunc(specs, func(s StorageContainer) bool { return s.named(name) }) {
			if !slices.ContainsFunc(specs, func(s StorageContainer) bool { return s.named(name) && storageMatches(actual, s) }) {
				return nil, fmt.Errorf("容器 %s（%s）的标签或绑定指纹不同", name, entry.ID)
			}
			states = append(states, StorageContainerState{entry.ID, name, actual.State.Running || actual.State.Restarting, string(actual.HostConfig.RestartPolicy.Name), actual.State.ExitCode})
			continue
		}
		if actual.Config != nil {
			project := actual.Config.Labels["com.docker.compose.project"]
			if project != "" && slices.ContainsFunc(specs, func(s StorageContainer) bool { return s.Labels["com.docker.compose.project"] == project }) {
				return nil, fmt.Errorf("非受管容器 %s（%s）占用受管 Compose 项目", name, entry.ID)
			}
		}
		checkPaths := roots
		if len(peers) > 0 && slices.ContainsFunc(peers[0].Specs, func(s StorageContainer) bool {
			return s.named(name) && storageMatches(actual, s)
		}) {
			checkPaths = peers[0].MovingDirectories
		}
		for _, b := range actual.Mounts {
			if b.Type != mount.TypeBind {
				continue
			}
			canonical, err := filepath.EvalSymlinks(b.Source)
			if err != nil {
				// 路径已消失时仍检查其可解析的父目录。
				for parent := filepath.Dir(b.Source); parent != "/"; parent = filepath.Dir(parent) {
					if real, e := filepath.EvalSymlinks(parent); e == nil {
						rel, _ := filepath.Rel(parent, b.Source)
						canonical = filepath.Join(real, rel)
						break
					}
				}
			}
			for _, source := range []string{b.Source, canonical} {
				for _, root := range checkPaths {
					if source != "" && (pathWithin(root, source) || b.RW && pathWithin(source, root)) {
						return nil, fmt.Errorf("非受管容器 %s（%s）绑定 %s 覆盖 %s", name, entry.ID, source, root)
					}
				}
			}
		}
	}
	for _, spec := range specs {
		n := 0
		for _, state := range states {
			if spec.named(state.Name) {
				n++
			}
		}
		if n > 1 || n == 0 && !allowMissing {
			return nil, fmt.Errorf("受管容器 %s 缺失或重复", spec.Name)
		}
	}
	return states, nil
}

func (m *Manager) StopStorage(ctx context.Context, states []StorageContainerState) error {
	c, _, _, err := m.connect(ctx, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	for _, s := range states {
		if _, err = c.ContainerUpdate(ctx, s.ID, client.ContainerUpdateOptions{RestartPolicy: &container.RestartPolicy{Name: "no"}}); err != nil {
			return err
		}
	}
	// 共享一个优雅停机预算。
	timeout := 32
	for _, s := range states {
		if s.Running {
			if _, err = c.ContainerStop(ctx, s.ID, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) RemoveStoppedStorage(ctx context.Context, states []StorageContainerState) error {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	for _, s := range states {
		if s.Running || s.Restart != "no" {
			return fmt.Errorf("容器 %s 尚未停写", s.ID)
		}
		if _, err = c.ContainerRemove(ctx, s.ID, client.ContainerRemoveOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) StartStorage(ctx context.Context, states, before []StorageContainerState) error {
	c, _, _, err := m.connect(ctx, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	for _, s := range states {
		index := slices.IndexFunc(before, func(old StorageContainerState) bool { return old.Name == s.Name })
		if index == -1 && len(before) == 1 && len(states) == 1 {
			index = 0
		}
		if before != nil && index == -1 {
			return fmt.Errorf("原容器记录缺少 %s", s.Name)
		}
		if !s.Running && (index == -1 || before[index].Running) {
			if _, err = c.ContainerStart(ctx, s.ID, client.ContainerStartOptions{}); err != nil {
				return err
			}
		}
		if index >= 0 && s.Restart != before[index].Restart && before[index].Restart != "" {
			_, err = c.ContainerUpdate(ctx, s.ID, client.ContainerUpdateOptions{RestartPolicy: &container.RestartPolicy{Name: container.RestartPolicyMode(before[index].Restart)}})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
