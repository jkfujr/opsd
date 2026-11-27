package cluster

import (
	"encoding/json"
	"github.com/jkfujr/opsd/internal/protocol"
)

func requestResources(r Request) []protocol.ResourceClaim {
	a := protocol.OperationAction{Kind: r.Kind}
	switch r.Kind {
	case "storage_root":
		return []protocol.ResourceClaim{{Key: "storage-root"}}
	case "monitoring_config":
		return []protocol.ResourceClaim{{Key: "monitoring-config"}}
	case "monitoring_deploy":
		a.Monitoring = &protocol.MonitoringDeployment{}
		_ = json.Unmarshal(r.Payload, a.Monitoring)
	case "shared_initialize":
		return []protocol.ResourceClaim{{Key: "shared-migration"}}
	case "database":
		var input DatabaseSubmission
		if json.Unmarshal(r.Payload, &input) == nil {
			return []protocol.ResourceClaim{{Key: "workflow/" + input.Cluster.ID}}
		}
	case "database_step":
		a.Database = &protocol.DatabaseAction{}
		_ = json.Unmarshal(r.Payload, a.Database)
	case "container":
		a.Container = &protocol.ContainerAction{}
		_ = json.Unmarshal(r.Payload, a.Container)
	case "compose":
		a.Compose = &protocol.ComposeAction{}
		_ = json.Unmarshal(r.Payload, a.Compose)
	case "task_run":
		a.Shell = &protocol.ShellAction{}
		_ = json.Unmarshal(r.Payload, a.Shell)
	case "firewall", "firewall_config", "component":
	default:
		return []protocol.ResourceClaim{{Key: "node-config"}}
	}
	return protocol.ActionResources(a)
}
