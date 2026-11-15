package cluster

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

func sharedProxyConnections(observation protocol.ProxyRoutingObservation) map[string]int {
	limits := map[string]int{}
	for _, server := range observation.Servers {
		limits[server.Key()] = max(limits[server.Key()], server.MaxConnections)
	}
	for key := range limits {
		limits[key] += sharedsql.MonitorConnections
	}
	return limits
}

func (n *Node) checkSharedConnections(ctx context.Context, c ManagedDatabaseCluster) error {
	directory, err := n.Directory(ctx)
	if err != nil {
		return err
	}
	withProxy := map[string]bool{}
	demand := map[string]int{}
	for _, ref := range c.Proxies {
		var observed protocol.ProxyRoutingObservation
		if err = n.Call(ctx, ref.NodeID, "database.proxy_routing.read", DatabaseRPC{InstanceID: ref.InstanceID}, &observed); err != nil {
			return err
		}
		withProxy[ref.NodeID] = true
		for endpoint, count := range sharedProxyConnections(observed) {
			demand[endpoint] += count
		}
	}
	newProxies := 0
	for _, member := range directory.Members {
		if n.Active(ctx, member.ID) && !withProxy[member.ID] {
			newProxies++
		}
	}
	for _, ref := range c.Members {
		host, port, e := net.SplitHostPort(ref.Address)
		if e != nil {
			return e
		}
		number, e := strconv.Atoi(port)
		if e != nil {
			return e
		}
		endpoint := protocol.ProxyEndpoint{Host: host, Port: number}
		var observed protocol.DatabaseReadResult
		if err = n.Call(ctx, ref.NodeID, "database.read", DatabaseRPC{InstanceID: ref.InstanceID, ExpectedClusterID: c.ClusterID, Scope: "health"}, &observed); err != nil {
			return err
		}
		required := demand[endpoint.Key()] + newProxies*(sharedsql.MaxConnections+sharedsql.MonitorConnections) + 2
		if observed.State.MaxConnections == 0 || int64(required) > observed.State.MaxConnections {
			return fault("conflict", fmt.Sprintf("数据库 %s 的代理、监控与管理连接需求为 %d，上限为 %d", ref.Name, required, observed.State.MaxConnections))
		}
	}
	return nil
}
