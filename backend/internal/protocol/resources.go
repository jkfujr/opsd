package protocol

import (
	"net"
	"sort"
	"strconv"
)

func OperationModule(subject string) string {
	switch subject {
	case "monitoring_config", "monitoring_deploy":
		return "monitoring"
	case "name":
		return "nodes"
	case "container":
		return "containers"
	case "firewall", "firewall_config", "component":
		return "firewall"
	case "database":
		return "databases"
	case "task_save", "task_run":
		return "tasks"
	case "compose", "compose_register":
		return "compose"
	}
	return ""
}

// ResourceClaim 的共享声明只用于父资源，具体写入资源始终独占。
type ResourceClaim struct {
	Key    string `json:"key"`
	Shared bool   `json:"shared,omitempty"`
}

func ResourcesConflict(a, b []ResourceClaim) bool {
	for _, x := range a {
		for _, y := range b {
			if x.Key == y.Key && (!x.Shared || !y.Shared) {
				return true
			}
		}
	}
	return false
}

func ActionResources(a OperationAction) []ResourceClaim {
	claims := map[string]bool{}
	add := func(key string, shared bool) {
		old, exists := claims[key]
		claims[key] = shared && (!exists || old)
	}
	switch {
	case a.Monitoring != nil:
		add("docker", true)
		add("monitoring", false)
		add("directory/"+a.Monitoring.Directory, false)
		for port := 19291; port <= 19295; port++ {
			add("port/"+strconv.Itoa(port), false)
		}
	case a.Shell != nil:
		add("task/"+a.Shell.TaskID, false)
	case a.Container != nil:
		c := a.Container
		add("docker", true)
		add("engine/"+c.EngineID, c.ProjectKnown || c.Project != "")
		if c.Project != "" {
			add("project/"+c.EngineID+"/"+c.Project, true)
		}
		add("container/"+c.ContainerID, false)
	case a.Compose != nil:
		p := a.Compose.Project
		add("docker", true)
		add("engine/"+p.EngineID, true)
		add("project/"+p.EngineID+"/"+p.Name, false)
	case a.Database != nil:
		d := a.Database
		if d.Repair != nil {
			add("docker", true)
			add("engine/"+d.Repair.EngineID, true)
			add("container/"+d.Repair.ContainerID, false)
		}
		if d.Deployment != nil {
			p := d.Deployment
			add("docker", true)
			add("directory/"+p.Directory, false)
			add("container-name/"+p.ContainerName(), false)
			for _, port := range []int{p.SQLPort, p.AdminPort, p.ReplicationPort, p.ISTPort, p.SSTPort} {
				if port > 0 {
					add("port/"+strconv.Itoa(port), false)
				}
			}
		} else if d.SeedUpdate != nil {
			add("docker", true)
			add("container/"+d.SeedUpdate.ContainerID, false)
		} else {
			if d.Connection.ContainerID != "" {
				add("docker", true)
				add("container/"+d.Connection.ContainerID, false)
			}
			endpoint := d.Connection.Socket
			if endpoint == "" {
				host, port, err := net.SplitHostPort(d.Connection.Address)
				if err == nil && net.ParseIP(host).IsLoopback() {
					endpoint = "loopback:" + port
				} else {
					endpoint = d.Connection.Address
				}
			}
			add("sql/"+endpoint, false)
			if d.Connection.Kind == "galera" {
				add("galera/"+d.ClusterID, false)
			}
		}
	default:
		add("firewall", false)
	}
	out := make([]ResourceClaim, 0, len(claims))
	for key, shared := range claims {
		out = append(out, ResourceClaim{Key: key, Shared: shared})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
