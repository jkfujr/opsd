package installation

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/cluster"
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
