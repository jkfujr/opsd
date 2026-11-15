package cluster

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type DatabaseScanItem struct {
	Instance DatabaseInstance       `json:"instance"`
	Status   protocol.DatabaseState `json:"status"`
}
type DatabaseScanGroup struct {
	ClusterID    string             `json:"cluster_id"`
	Name         string             `json:"name"`
	RegisteredID string             `json:"registered_id"`
	Members      []DatabaseScanItem `json:"members"`
	Proxies      []DatabaseScanItem `json:"proxies"`
}
type DatabaseScanResult struct {
	Groups     []DatabaseScanGroup `json:"groups"`
	Unresolved []DatabaseScanItem  `json:"unresolved"`
	Problems   []string            `json:"problems"`
}
type DatabaseScanInput struct {
	Nodes       []string                `json:"nodes"`
	Credentials []DatabaseInstanceInput `json:"credentials"`
}

func (n *Node) databaseCandidates(ctx context.Context) ([]protocol.DatabaseCandidate, error) {
	reply, err := n.executeRead(ctx, &protocol.DatabaseDiscovery{})
	if err != nil {
		return nil, err
	}
	found, ok := reply.(*protocol.DatabaseDiscovered)
	if !ok {
		return nil, fmt.Errorf("数据库发现应答无效")
	}
	items := found.Instances
	rows, err := n.DB.QueryContext(ctx, "SELECT id FROM managed_database_instances ORDER BY id")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		instance, e := n.databaseInstance(ctx, id)
		if e != nil {
			return nil, e
		}
		connection, e := n.databaseConnection(ctx, id)
		if e != nil {
			return nil, e
		}
		index := slices.IndexFunc(items, func(c protocol.DatabaseCandidate) bool {
			return c.ID == id || instance.ContainerID != "" && c.ContainerID == instance.ContainerID
		})
		if index >= 0 {
			items[index].ID = id
			items[index].Name = instance.Name
			items[index].Connection = connection
		} else {
			items = append(items, protocol.DatabaseCandidate{ID: id, Name: instance.Name, Connection: connection, ContainerID: instance.ContainerID, Running: instance.Running && instance.ContainerID == ""})
		}
	}
	return items, nil
}

func (n *Node) inspectDatabaseCandidate(ctx context.Context, candidate protocol.DatabaseCandidate, input DatabaseInstanceInput) (DatabaseScanItem, protocol.DatabaseConnection) {
	c := candidate.Connection
	if input.Password != nil {
		c.Password = *input.Password
		if input.Username != "" {
			c.Username = input.Username
		}
	}
	i := DatabaseInstance{ID: candidate.ID, NodeID: n.ID, Name: candidate.Name, Kind: c.Kind, Address: c.Address, Socket: c.Socket, Username: c.Username, ContainerID: candidate.ContainerID, Running: candidate.Running, CredentialsAvailable: c.Password != "", ProtectedUsers: c.ProtectedUsers, BackupRoot: c.BackupRoot}
	if i.ProtectedUsers == nil {
		i.ProtectedUsers = []string{}
	}
	if old, err := n.databaseInstance(ctx, i.ID); err == nil {
		i.Revision = old.Revision
	}
	out := DatabaseScanItem{Instance: i}
	if i.ContainerID != "" && !i.Running {
		out.Status = protocol.DatabaseState{State: "unavailable", CheckedAt: n.Now().UTC(), Error: &protocol.Error{Code: "dependency_unavailable", Message: "数据库容器未运行或已移除，请恢复实例后重新扫描"}}
		return out, c
	}
	scope := "health"
	if c.Kind == "proxysql" {
		scope = "proxies"
	}
	reply, err := n.executeRead(ctx, &protocol.DatabaseRead{Connection: c, Scope: scope})
	if err == nil {
		if result, ok := reply.(*protocol.DatabaseReadResult); ok {
			out.Status = result.State
		} else {
			err = fmt.Errorf("数据库状态应答无效")
		}
	}
	if err != nil {
		out.Status = protocol.DatabaseState{State: "unavailable", CheckedAt: n.Now().UTC(), Error: &protocol.Error{Code: "dependency_unavailable", Message: "连接失败，请补充管理凭据或恢复实例"}}
	}
	return out, c
}

func (n *Node) inspectDiscoveredDatabase(ctx context.Context, input DatabaseInstanceInput, register bool, expected string) (DatabaseScanItem, error) {
	items, err := n.databaseCandidates(ctx)
	if err != nil {
		return DatabaseScanItem{}, err
	}
	index := slices.IndexFunc(items, func(c protocol.DatabaseCandidate) bool { return c.ID == input.ID })
	if index < 0 {
		return DatabaseScanItem{}, fault("not_found", "数据库实例已不存在，请重新扫描")
	}
	out, connection := n.inspectDatabaseCandidate(ctx, items[index], input)
	if !register {
		return out, nil
	}
	if out.Status.Error != nil {
		return out, fault("dependency_unavailable", out.Status.Error.Message)
	}
	if out.Instance.Kind == "galera" && (out.Status.ClusterID != expected || !out.Status.Primary || !out.Status.Ready || !out.Status.Synced) {
		return out, fault("conflict", "实例集群身份或同步状态已变化")
	}
	save := DatabaseInstanceInput{DatabaseInstance: out.Instance, Password: &connection.Password, ProtectedUsers: connection.ProtectedUsers, BackupRoot: connection.BackupRoot}
	out.Instance, err = n.saveDatabaseInstance(ctx, save)
	return out, err
}

func (n *Node) scanLocalDatabases(ctx context.Context, input DatabaseScanInput) ([]DatabaseScanItem, error) {
	items, err := n.databaseCandidates(ctx)
	if err != nil {
		return nil, err
	}
	out := []DatabaseScanItem{}
	for _, item := range items {
		if item.ContainerID != "" && !item.Running {
			continue
		}
		var override DatabaseInstanceInput
		for _, value := range input.Credentials {
			if value.NodeID == n.ID && value.ID == item.ID {
				override = value
				break
			}
		}
		value, _ := n.inspectDatabaseCandidate(ctx, item, override)
		out = append(out, value)
	}
	return out, nil
}

func proxyMatchesScan(state protocol.DatabaseState, clusterID string, items []DatabaseScanItem) bool {
	if state.Error != nil {
		return false
	}
	endpoints := map[string]bool{}
	for _, item := range items {
		if item.Instance.Kind != "galera" || item.Status.ClusterID != clusterID {
			continue
		}
		for _, address := range strings.Split(item.Status.ActualMembers, ",") {
			endpoints[strings.TrimSpace(address)] = true
		}
		host := item.Status.MemberAddress
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(host, "3306")
		}
		endpoints[host] = true
	}
	for _, server := range state.Servers {
		if endpoints[net.JoinHostPort(server.Host, fmt.Sprint(server.Port))] {
			return true
		}
	}
	return false
}

func (n *Node) ScanDatabases(ctx context.Context, input DatabaseScanInput) (DatabaseScanResult, error) {
	out := DatabaseScanResult{Groups: []DatabaseScanGroup{}, Unresolved: []DatabaseScanItem{}, Problems: []string{}}
	if len(input.Nodes) > 100 || len(input.Credentials) > 200 {
		return out, fault("invalid_request", "扫描节点过多")
	}
	if len(input.Nodes) == 0 {
		dir, err := n.Directory(ctx)
		if err != nil {
			return out, err
		}
		for _, m := range dir.Members {
			if n.Active(ctx, m.ID) {
				input.Nodes = append(input.Nodes, m.ID)
			}
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	all := []DatabaseScanItem{}
	for _, node := range input.Nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			var items []DatabaseScanItem
			local := DatabaseScanInput{Nodes: []string{node}}
			for _, credential := range input.Credentials {
				if credential.NodeID == node {
					local.Credentials = append(local.Credentials, credential)
				}
			}
			err := n.Call(readCtx, node, "database.discovery.scan", local, &items)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Problems = append(out.Problems, node+"："+err.Error())
			} else {
				all = append(all, items...)
			}
		}()
	}
	wg.Wait()
	slices.SortFunc(all, func(a, b DatabaseScanItem) int {
		return strings.Compare(a.Instance.NodeID+a.Instance.ID, b.Instance.NodeID+b.Instance.ID)
	})
	registered, err := n.DatabaseClusters(ctx)
	if err != nil {
		return out, err
	}
	for _, item := range all {
		if item.Instance.Kind != "galera" {
			continue
		}
		if item.Status.ClusterID == "" || item.Status.Error != nil {
			out.Unresolved = append(out.Unresolved, item)
			continue
		}
		at := slices.IndexFunc(out.Groups, func(g DatabaseScanGroup) bool { return g.ClusterID == item.Status.ClusterID })
		if at < 0 {
			g := DatabaseScanGroup{ClusterID: item.Status.ClusterID, Name: item.Status.ClusterName, Members: []DatabaseScanItem{}, Proxies: []DatabaseScanItem{}}
			for _, c := range registered {
				if !c.Deleted && c.ClusterID == g.ClusterID {
					g.RegisteredID = c.ID
				}
			}
			out.Groups = append(out.Groups, g)
			at = len(out.Groups) - 1
		}
		out.Groups[at].Members = append(out.Groups[at].Members, item)
	}
	for _, item := range all {
		if item.Instance.Kind != "proxysql" {
			continue
		}
		matched := false
		for i := range out.Groups {
			if proxyMatchesScan(item.Status, out.Groups[i].ClusterID, all) {
				out.Groups[i].Proxies = append(out.Groups[i].Proxies, item)
				matched = true
			}
		}
		if !matched {
			out.Unresolved = append(out.Unresolved, item)
		}
	}
	return out, nil
}

func (n *Node) advanceDeploymentImport(ctx context.Context, op *Operation, flow *databaseWorkflow) {
	step := &flow.Steps[flow.Index]
	if flow.Request == "resolve" {
		op.State = protocol.ResolvedManually
		op.Error = nil
		_ = n.persistDatabaseWorkflow(ctx, op, *flow)
		return
	}
	var found DatabaseScanItem
	err := n.Call(ctx, step.Ref.NodeID, "database.discovery.register", DatabaseRPC{Instance: *step.Import, ExpectedClusterID: flow.Cluster.ClusterID}, &found)
	if err == nil {
		step.Ref.InstanceRevision = found.Instance.Revision
		refs := &flow.Cluster.Members
		if found.Instance.Kind == "proxysql" {
			refs = &flow.Cluster.Proxies
		}
		if !slices.ContainsFunc(*refs, func(r DatabaseRef) bool { return r.NodeID == step.Ref.NodeID && r.InstanceID == step.Ref.InstanceID }) {
			*refs = append(*refs, step.Ref)
		}
		// 协调成员先接入后即可登记，其余成员分步补齐。
		if slices.ContainsFunc(flow.Cluster.Members, func(r DatabaseRef) bool { return r.NodeID == n.ID }) {
			var saved ManagedDatabaseCluster
			saved, err = n.saveDeploymentCluster(ctx, *flow.Cluster)
			if err == nil {
				*flow.Cluster = saved
			}
		}
	}
	if err != nil {
		op.State = protocol.AttentionRequired
		op.Error = &protocol.Error{Code: "dependency_unavailable", Message: err.Error()}
		flow.Request = ""
	} else {
		step.State = "completed"
		flow.Index++
		op.State = protocol.Applying
		op.Error = nil
		flow.Request = ""
	}
	_ = n.persistDatabaseWorkflow(ctx, op, *flow)
}
