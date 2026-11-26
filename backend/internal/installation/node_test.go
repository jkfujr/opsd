package installation

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/storage"
)

func TestUnifiedServicesKeepRecoveryIndependent(t *testing.T) {
	units := Units(NodeFile{Binary: "/opt/opsd/bin/opsd", DataDir: "/var/lib/opsd/node"}, "/etc/opsd/node.json")
	for name, body := range units {
		if strings.Contains(body, "opsd-agent") || !strings.Contains(body, "/opt/opsd/bin/opsd") {
			t.Fatal(name, body)
		}
	}
	node := units["opsd.service"]
	root := units["opsd-executor.service"]
	operation := units["opsd-operation@.service"]
	if !strings.Contains(node, "User=opsd") || !strings.Contains(root, "User=root") || !strings.Contains(root, "RuntimeDirectory=opsd") || !strings.Contains(operation, "TimeoutStartSec=infinity") || strings.Contains(operation, "BindsTo=opsd.service") {
		t.Fatal("本机恢复依赖 Web 或节点服务")
	}
}

func TestMetadataRootUnits(t *testing.T) {
	f := NodeFile{Binary: "/opt/opsd/bin/opsd", DataDir: storage.NodeDir, ExecutorDir: storage.ExecutorDir}
	for _, uuid := range []string{"", "12345678-abcd"} {
		f.StorageMountUUID = uuid
		units := Units(f, "/etc/opsd/instance.json")
		check := units["opsd-storage-check.service"]
		if !strings.Contains(check, quote(f.storageCheckPath())+" check "+quote(uuid)) ||
			strings.Contains(check, "Wants=opsd.mount") != (uuid != "") {
			t.Fatal(check)
		}
		for name, directory := range map[string]string{
			"opsd.service": f.DataDir, "opsd-executor.service": f.ExecutorDir,
			"opsd-recover.service": f.ExecutorDir, "opsd-operation@.service": f.ExecutorDir,
		} {
			body := units[name]
			if !strings.Contains(body, "Requires=opsd-storage-check.service\nAfter=opsd-storage-check.service") ||
				!strings.Contains(body, "RequiresMountsFor="+quote(directory)) ||
				strings.Contains(body, "BindsTo=opsd.mount\nAfter=opsd.mount") != (uuid != "") ||
				strings.Contains(body, "ReadWritePaths=") != (name == "opsd.service") {
				t.Fatal(name, body)
			}
		}
		if !strings.Contains(units["opsd.service"], "ReadWritePaths="+quote(storage.NodeDir)+"\n") {
			t.Fatal(units["opsd.service"])
		}
	}
}

func TestNodeNameAndRegionKeepExistingConfiguration(t *testing.T) {
	for _, name := range []string{"自定义节点", ""} {
		path := filepath.Join(t.TempDir(), "node.db")
		n, err := cluster.Open(t.Context(), path, cluster.Options{Config: cluster.Config{Name: name, Region: "JP", Address: "http://100.100.1.2:8444"}, Username: "admin", Password: "test-password", Entrance: "/signin"})
		if err != nil {
			t.Fatal(err)
		}
		id := n.ID
		wantName := name
		if wantName == "" {
			wantName, _ = os.Hostname()
		}
		config, err := n.Config(t.Context())
		if err != nil || config.Name != wantName || config.Region != "JP" {
			t.Fatal(config, err)
		}
		n.Close()
		n, err = cluster.Open(t.Context(), path, cluster.Options{Config: cluster.Config{Name: "替换名称", Region: "US", Address: "http://100.100.2.2:8444"}})
		if err != nil {
			t.Fatal(err)
		}
		after, err := n.Config(t.Context())
		if err != nil || !reflect.DeepEqual(after, config) || n.ID != id {
			t.Fatal("重新打开覆盖了现有配置或身份", after, err)
		}
		n.Close()
	}
}

// TestNewInstallDefaultsToPolicyClosed 覆盖 A2 的默认值：
// 新装实例显式写入关闭值，只有显式开启才允许任意代码。
func TestNewInstallDefaultsToPolicyClosed(t *testing.T) {
	if raw := policyJSON(false); string(raw) != `{"arbitrary_code":false}` {
		t.Fatalf("新装默认应为关闭，实际 %s", raw)
	}
	if raw := policyJSON(true); string(raw) != `{"arbitrary_code":true}` {
		t.Fatalf("显式开启应写入 true，实际 %s", raw)
	}
}
