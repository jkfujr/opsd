package dockerengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/client"
	"go.yaml.in/yaml/v3"
)

const monitoringLabel = "com.opsd.monitoring"
const monitoringSpecLabel = "com.opsd.monitoring-spec"

var monitoringNames = []string{"receive", "query", "store", "compact"}

func monitoringProject(d protocol.MonitoringDeployment) string {
	sum := sha256.Sum256([]byte(d.ClusterID))
	return "opsd-monitoring-" + hex.EncodeToString(sum[:6])
}
func monitoringFingerprint(d protocol.MonitoringDeployment) string {
	b, _ := json.Marshal(d)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// MonitoringFiles 只生成固定服务定义，外部参数通过 YAML 编码进入字段。
func MonitoringFiles(d protocol.MonitoringDeployment) (map[string][]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	u, _ := url.Parse(d.Storage.Endpoint)
	files := map[string][]byte{}
	httpConfig := map[string]any{"response_header_timeout": "10s", "idle_conn_timeout": "90s"}
	if d.Storage.CAPEM != "" {
		files["s3-ca.pem"] = []byte(d.Storage.CAPEM)
		httpConfig["tls_config"] = map[string]string{"ca_file": "/config/s3-ca.pem"}
	}
	for name, key := range map[string]protocol.S3Credential{"receive": d.Credentials.Receive, "store": d.Credentials.Store, "compact": d.Credentials.Compact} {
		lookup := "path"
		if d.Storage.AccessStyle == "virtual_host" {
			lookup = "virtual-hosted"
		}
		config := map[string]any{"type": "S3", "prefix": strings.TrimSuffix(d.Prefix, "/"), "config": map[string]any{"bucket": d.Storage.Bucket, "endpoint": u.Host, "region": d.Storage.Region, "access_key": key.AccessKey, "secret_key": key.SecretKey, "session_token": key.SessionToken, "insecure": u.Scheme == "http", "bucket_lookup_type": lookup, "max_retries": 1, "http_config": httpConfig}}
		data, err := yaml.Marshal(config)
		if err != nil {
			return nil, err
		}
		files[name+".yml"] = data
	}
	commands := map[string][]string{
		"receive": {"receive", "--grpc-address=0.0.0.0:10901", "--http-address=0.0.0.0:10902", "--remote-write.address=0.0.0.0:19291", "--tsdb.path=/data", "--tsdb.retention=30d", "--tsdb.out-of-order.time-window=2h", "--tsdb.too-far-in-future.time-window=1m", "--receive.replication-factor=1", "--receive.default-tenant-id=opsd", "--label=cluster_id=" + fmt.Sprintf("%q", d.ClusterID), "--label=receive_replica=" + fmt.Sprintf("%q", d.NodeID), "--objstore.config-file=/config/objstore.yml"},
		"query":   {"query", "--http-address=0.0.0.0:10902", "--endpoint=dns+receive.opsd.invalid:10901", "--endpoint=dns+store.opsd.invalid:10901", "--store.sd-dns-interval=5s", "--query.max-concurrent=2", "--query.timeout=30s", "--query.replica-label=receive_replica", "--query.auto-downsampling"},
		"store":   {"store", "--grpc-address=0.0.0.0:10901", "--http-address=0.0.0.0:10902", "--data-dir=/data", "--objstore.config-file=/config/objstore.yml", "--index-cache-size=128MB", "--chunk-pool-size=128MB", "--store.grpc.series-max-concurrency=2"},
		"compact": {"compact", "--http-address=0.0.0.0:10902", "--data-dir=/data", "--objstore.config-file=/config/objstore.yml", "--wait", "--compact.concurrency=1", "--downsample.concurrency=1", "--compact.enable-vertical-compaction", "--retention.resolution-raw=365d", "--retention.resolution-5m=365d", "--retention.resolution-1h=365d", "--delete-delay=48h"},
	}
	memories := map[string]string{"receive": "2g", "query": "512m", "store": "1g", "compact": "2g"}
	ports := map[string][]string{"receive": {"127.0.0.1:19291:19291", "127.0.0.1:19293:10902"}, "query": {"127.0.0.1:19292:10902"}, "store": {"127.0.0.1:19294:10902"}, "compact": {"127.0.0.1:19295:10902"}}
	services := map[string]any{}
	for _, name := range monitoringNames {
		service := map[string]any{"image": "quay.io/thanos/thanos:" + metricsstore.ThanosVersion, "container_name": monitoringProject(d) + "-" + name, "command": commands[name], "restart": "unless-stopped", "user": "65534:65534", "mem_limit": memories[name], "ports": ports[name], "labels": map[string]string{monitoringLabel: d.ClusterID, monitoringSpecLabel: monitoringFingerprint(d), "com.opsd.managed": "monitoring"}, "logging": map[string]any{"driver": "json-file", "options": map[string]string{"max-size": "10m", "max-file": "2"}}}
		service["networks"] = map[string]any{"default": map[string]any{"aliases": []string{name + ".opsd.invalid"}}}
		if name != "query" {
			volumes := []any{map[string]any{"type": "bind", "source": filepath.Join(d.Directory, name), "target": "/data"}, map[string]any{"type": "bind", "source": filepath.Join(d.Directory, name+".yml"), "target": "/config/objstore.yml", "read_only": true}}
			if d.Storage.CAPEM != "" {
				volumes = append(volumes, map[string]any{"type": "bind", "source": filepath.Join(d.Directory, "s3-ca.pem"), "target": "/config/s3-ca.pem", "read_only": true})
			}
			service["volumes"] = volumes
		}
		services[name] = service
	}
	compose, err := yaml.Marshal(map[string]any{"services": services})
	if err != nil {
		return nil, err
	}
	files["compose.yml"] = compose
	return files, nil
}

func monitoringOwner(d protocol.MonitoringDeployment) error {
	var owner struct {
		Cluster string `json:"cluster_id"`
		Node    string `json:"node_id"`
	}
	b, err := os.ReadFile(filepath.Join(d.Directory, ".opsd-monitoring.json"))
	if err != nil {
		return err
	}
	if json.Unmarshal(b, &owner) != nil || owner.Cluster != d.ClusterID || owner.Node != d.NodeID {
		return fmt.Errorf("监控目录归属不同")
	}
	return nil
}

func (m *Manager) PrepareMonitoring(ctx context.Context, d protocol.MonitoringDeployment) error {
	if err := d.Validate(); err != nil {
		return err
	}
	for p := d.Directory; p != string(filepath.Separator); p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("监控目录及父目录须为真实目录")
		}
	}
	if err := monitoringOwner(d); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		entries, e := os.ReadDir(d.Directory)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if len(entries) > 0 {
			return fmt.Errorf("首次监控部署需要空目录")
		}
		if e = os.MkdirAll(d.Directory, 0700); e != nil {
			return e
		}
		body, _ := json.Marshal(map[string]string{"cluster_id": d.ClusterID, "node_id": d.NodeID})
		if e = writeMonitoringFile(filepath.Join(d.Directory, ".opsd-monitoring.json"), body); e != nil {
			return e
		}
	}
	files, err := MonitoringFiles(d)
	if err != nil {
		return err
	}
	for _, name := range []string{"receive", "store", "compact"} {
		p := filepath.Join(d.Directory, name)
		if info, e := os.Lstat(p); e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("监控组件目录含有外部链接")
		}
		if err = os.MkdirAll(p, 0700); err != nil {
			return err
		}
		if err = os.Chown(p, 65534, 65534); err != nil {
			return err
		}
	}
	for name, body := range files {
		if err = writeMonitoringFile(filepath.Join(d.Directory, name), body); err != nil {
			return err
		}
	}
	_, available, err := monitoringDisk(ctx, d.Directory)
	if err != nil {
		return err
	}
	if available < 1<<30 {
		return fmt.Errorf("监控磁盘可用空间不足 1GiB")
	}
	return nil
}
func writeMonitoringFile(path string, body []byte) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("监控配置文件含有外部链接")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".opsd-write-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Chown(name, 65534, 65534); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func (m *Manager) ApplyMonitoring(ctx context.Context, d protocol.MonitoringDeployment) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if !d.Enabled {
		return m.stopMonitoring(ctx, d)
	}
	if err := monitoringOwner(d); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "docker", "--host", "unix://"+m.Socket, "compose", "--project-name", monitoringProject(d), "--project-directory", d.Directory, "-f", filepath.Join(d.Directory, "compose.yml"), "up", "-d", "--no-build", "--pull", "never")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("启动监控组件：%w：%s", err, string(output))
	}
	return nil
}
func (m *Manager) stopMonitoring(ctx context.Context, d protocol.MonitoringDeployment) error {
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	for _, name := range monitoringNames {
		r, e := c.ContainerInspect(ctx, monitoringProject(d)+"-"+name, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(e) {
			continue
		}
		if e != nil {
			return e
		}
		if r.Container.Config == nil || r.Container.Config.Labels[monitoringLabel] != d.ClusterID {
			return fmt.Errorf("同名监控容器归属冲突")
		}
		if _, e = c.ContainerStop(ctx, r.Container.ID, client.ContainerStopOptions{}); e != nil {
			return e
		}
	}
	return nil
}

func (m *Manager) MonitoringState(ctx context.Context, d protocol.MonitoringDeployment) (protocol.MonitoringState, error) {
	out := protocol.MonitoringState{State: "absent", CheckedAt: time.Now().UTC(), Components: []protocol.MonitoringComponent{}}
	check := d
	check.Enabled = false
	if err := check.Validate(); err != nil {
		return out, err
	}
	c, _, _, err := m.connect(ctx)
	if err != nil {
		return out, err
	}
	defer c.Close()
	ports := map[string]int{"receive": 19293, "query": 19292, "store": 19294, "compact": 19295}
	running, ready := 0, 0
	httpClient := &http.Client{Timeout: time.Second}
	for _, name := range monitoringNames {
		component := protocol.MonitoringComponent{Name: name, State: "absent"}
		r, e := c.ContainerInspect(ctx, monitoringProject(d)+"-"+name, client.ContainerInspectOptions{})
		if e != nil && !errdefs.IsNotFound(e) {
			return out, e
		}
		if e == nil {
			if r.Container.Config == nil || r.Container.Config.Labels[monitoringLabel] != d.ClusterID {
				return out, fmt.Errorf("同名监控容器归属冲突")
			}
			if r.Container.State != nil {
				component.State = string(r.Container.State.Status)
			}
			if d.Enabled && r.Container.Config.Labels[monitoringSpecLabel] != monitoringFingerprint(d) {
				component.Error = "组件配置尚未应用"
			}
			if component.State == "running" {
				running++
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/-/ready", ports[name]), nil)
				response, e := httpClient.Do(req)
				if e == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
					response.Body.Close()
					component.Ready = response.StatusCode == 200 && component.Error == ""
				} else {
					component.Error = "组件尚未就绪"
				}
				if component.Ready {
					ready++
				}
			}
		}
		out.Components = append(out.Components, component)
	}
	if running > 0 {
		out.State = "starting"
	} else {
		out.State = "stopped"
	}
	if ready == 4 {
		out.State = "ready"
	}
	if _, err = os.Stat(d.Directory); os.IsNotExist(err) {
		return out, nil
	} else if err != nil {
		return out, err
	}
	if err = monitoringOwner(d); err != nil {
		return out, err
	}
	out.DiskUsedBytes, out.DiskAvailableBytes, err = monitoringDisk(ctx, d.Directory)
	if err != nil {
		return out, err
	}
	var shipper struct {
		Uploaded []string `json:"uploaded"`
	}
	if data, e := os.ReadFile(filepath.Join(d.Directory, "receive", "opsd", "thanos.shipper.json")); e == nil {
		if e = json.Unmarshal(data, &shipper); e != nil {
			return out, e
		}
	} else if !os.IsNotExist(e) {
		return out, e
	}
	shipped := map[string]bool{}
	for _, id := range shipper.Uploaded {
		shipped[id] = true
	}
	entries, err := os.ReadDir(filepath.Join(d.Directory, "receive", "opsd"))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 26 || shipped[entry.Name()] {
			continue
		}
		data, e := os.ReadFile(filepath.Join(d.Directory, "receive", "opsd", entry.Name(), "meta.json"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return out, e
		}
		var meta struct {
			MinTime int64 `json:"minTime"`
		}
		if e = json.Unmarshal(data, &meta); e != nil {
			return out, e
		}
		out.ArchivePendingBlocks++
		at := time.UnixMilli(meta.MinTime).UTC()
		if out.OldestUnshippedAt == nil || at.Before(*out.OldestUnshippedAt) {
			out.OldestUnshippedAt = &at
		}
	}
	return out, nil
}
