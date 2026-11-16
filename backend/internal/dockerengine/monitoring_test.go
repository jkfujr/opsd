//go:build linux

package dockerengine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
	"go.yaml.in/yaml/v3"
)

func TestMonitoringFixedComponentsAndCredentialIsolation(t *testing.T) {
	d := protocol.MonitoringDeployment{ClusterID: strings.Repeat("a", 43), NodeID: strings.Repeat("b", 32), Enabled: true, Directory: "/var/lib/opsd-monitoring", Prefix: "opsd/metrics/cluster/", DiskBudgetBytes: 80 << 30, Storage: protocol.S3Connection{Endpoint: "https://s3.example.test", Bucket: "metrics", AccessStyle: "path"}, Credentials: protocol.S3Credentials{Receive: protocol.S3Credential{AccessKey: "receive-key", SecretKey: "receive-secret"}, Store: protocol.S3Credential{AccessKey: "store-key", SecretKey: "store-secret"}, Compact: protocol.S3Credential{AccessKey: "compact-key", SecretKey: "compact-secret"}}}
	files, err := MonitoringFiles(d)
	if err != nil || len(files) != 4 {
		t.Fatal(files, err)
	}
	var compose struct {
		Services map[string]struct {
			Image    string            `yaml:"image"`
			Ports    []string          `yaml:"ports"`
			Command  []string          `yaml:"command"`
			Memory   string            `yaml:"mem_limit"`
			Labels   map[string]string `yaml:"labels"`
			Networks map[string]struct {
				Aliases []string `yaml:"aliases"`
			} `yaml:"networks"`
		} `yaml:"services"`
	}
	if err = yaml.Unmarshal(files["compose.yml"], &compose); err != nil || len(compose.Services) != 4 {
		t.Fatal(compose, err)
	}
	for name, component := range compose.Services {
		if component.Image != "quay.io/thanos/thanos:"+metricsstore.ThanosVersion || component.Memory == "" || component.Labels[monitoringSpecLabel] != monitoringFingerprint(d) {
			t.Fatal("组件版本或资源限制缺失", name, component)
		}
		for _, port := range component.Ports {
			if !strings.HasPrefix(port, "127.0.0.1:") {
				t.Fatal("内部服务暴露到外部网卡", port)
			}
		}
		if strings.Join(component.Networks["default"].Aliases, ",") != name+".opsd.invalid" {
			t.Fatal("组件内部别名缺失", name)
		}
		if name == "query" {
			if !strings.Contains(strings.Join(component.Command, " "), "--endpoint=dns+receive.opsd.invalid:10901") || !strings.Contains(strings.Join(component.Command, " "), "--endpoint=dns+store.opsd.invalid:10901") {
				t.Fatal("查询组件未使用内部别名")
			}
			continue
		}
		text := string(files[name+".yml"])
		if !strings.Contains(text, name+"-secret") || !strings.Contains(text, "prefix: opsd/metrics/cluster") {
			t.Fatal("组件凭据或前缀缺失", name)
		}
		for _, other := range []string{"receive", "store", "compact"} {
			if other != name && strings.Contains(text, other+"-secret") {
				t.Fatal("组件获得其他角色凭据", name, other)
			}
		}
	}
	raw, _ := json.Marshal(compose)
	if strings.Contains(string(raw), "secret") {
		t.Fatal("Compose 内嵌了凭据")
	}
	if !strings.Contains(strings.Join(compose.Services["receive"].Command, " "), "--tsdb.out-of-order.time-window=2h") || !strings.Contains(strings.Join(compose.Services["compact"].Command, " "), "--retention.resolution-raw=365d") {
		t.Fatal("补传或原始历史保留参数缺失")
	}
}
