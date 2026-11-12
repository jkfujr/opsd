package databaseengine

import (
	"maps"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestGaleraRuntimeRolesDoNotAppearAsPendingConfiguration(t *testing.T) {
	groups := protocol.ProxyHostgroups{Writer: 10, BackupWriter: 20, Reader: 30, Offline: 40}
	first := map[string]string{"hostgroup_id": "10", "hostname": "10.0.0.1", "port": "3306", "status": "ONLINE", "weight": "1000", "max_connections": "100"}
	second := maps.Clone(first)
	second["hostname"], second["weight"] = "10.0.0.2", "990"
	shunned, backup := maps.Clone(second), maps.Clone(second)
	shunned["status"], backup["hostgroup_id"] = "SHUNNED", "20"
	memory := map[string][]map[string]string{"mysql_servers": {first, second}, "mysql_galera_hostgroups": {}}
	layers := proxyServerLayers{Memory: memory, Disk: memory, Runtime: map[string][]map[string]string{"mysql_servers": {first, shunned, backup}, "mysql_galera_hostgroups": {}}}
	if !serverLayersInSync(layers, groups) {
		t.Fatal("正常备用写节点被判断为配置未应用")
	}
	foreign := maps.Clone(first)
	foreign["hostgroup_id"] = "99"
	layers.Memory = map[string][]map[string]string{"mysql_servers": {first, second, foreign}, "mysql_galera_hostgroups": {}}
	if serverLayersInSync(layers, groups) {
		t.Fatal("其他主机组待应用变化被忽略")
	}
	action := protocol.ProxyRouteAction{Hostgroups: groups, UnmanagedFingerprint: unmanagedServersFingerprint(memory, groups)}
	if err := validateRouteLayers(layers, action); err == nil {
		t.Fatal("未阻止接管范围外的配置变化")
	}
}
