package cluster

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestOlderDataPreservesReadFailureAndServiceHealth(t *testing.T) {
	n, _ := openTestNode(t)
	at := n.Now().Add(-time.Hour).UTC()
	ref := DatabaseRef{NodeID: n.ID, InstanceID: "member"}
	request, _ := json.Marshal(DatabaseRPC{InstanceID: ref.InstanceID, Scope: "health"})
	value, _ := json.Marshal(protocol.DatabaseReadResult{State: protocol.DatabaseState{State: "ready", ClusterID: "cluster", Version: "11.4", Ready: true, Synced: true, Primary: true, CheckedAt: at}})
	for _, failure := range []string{"error", "timeout", "offline"} {
		key := n.ID + "/database.read/" + string(request)
		n.current = map[string]*currentRead{key: {value: value, status: ReadStatus{State: failure, DataAt: &at, Error: &protocol.Error{Code: "dependency_unavailable", Message: "实际读取失败"}}, running: true}}
		members, _ := n.readManagedDatabaseGroup(t.Context(), ManagedDatabaseCluster{ClusterID: "cluster", Members: []DatabaseRef{ref}}, "health")
		status := members[0].State
		if status.State != "ready" || status.Error != nil || status.Read.State != failure || status.Read.Error == nil || !status.Read.DataAt.Equal(at) || status.Version != "11.4" {
			t.Fatal("读取状态覆盖了服务结果", status)
		}
	}
	n.current = map[string]*currentRead{n.ID + "/database.read/" + string(request): {status: ReadStatus{State: "queued"}, running: true}}
	members, _ := n.readManagedDatabaseGroup(t.Context(), ManagedDatabaseCluster{Members: []DatabaseRef{ref}}, "health")
	if members[0].State.State != "unknown" || members[0].State.Read.DataAt != nil {
		t.Fatal("冷缓存伪造健康", members)
	}
}

func TestCurrentDataTimeUsesSourceAcrossCachedResponses(t *testing.T) {
	at, response := time.Now().Add(-time.Minute).UTC(), time.Now().UTC()
	for _, value := range []any{protocol.DatabaseReadResult{State: protocol.DatabaseState{CheckedAt: at}}, protocol.DockerState{State: "ready", CheckedAt: at}, protocol.MonitoringResult{Metrics: protocol.Metrics{CollectedAt: at}}} {
		encoded, _ := json.Marshal(value)
		if got := currentDataTime(encoded, response); !got.Equal(at) {
			t.Fatal("缓存被标为新采集", string(encoded), got, at)
		}
	}
}
