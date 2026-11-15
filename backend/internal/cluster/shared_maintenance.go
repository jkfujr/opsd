package cluster

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) sharedProxyAddress(ctx context.Context, c protocol.DatabaseConnection) (string, error) {
	address, err := (databaseengine.Manager{}).ProxySQLAddress(ctx, c)
	if err != nil || c.ContainerID == "" {
		return address, err
	}
	reply, err := n.executeRead(ctx, &protocol.DockerRead{})
	if err != nil {
		return "", err
	}
	state, ok := reply.(*protocol.DockerReadResult)
	if !ok {
		return "", fmt.Errorf("Docker 状态应答无效")
	}
	_, port, _ := net.SplitHostPort(address)
	for _, item := range state.State.Containers {
		if item.ContainerID != c.ContainerID {
			continue
		}
		for _, binding := range item.Ports {
			if strconv.Itoa(int(binding.ContainerPort)) == port && binding.HostPort != 0 && (binding.HostIP == "0.0.0.0" || binding.HostIP == "::" || net.ParseIP(binding.HostIP).IsLoopback()) {
				return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(binding.HostPort))), nil
			}
		}
	}
	return address, nil
}

func (n *Node) revokeSharedProxy(ctx context.Context, state *SharedLocal, id string) error {
	instance, err := n.databaseInstance(ctx, state.ProxyID)
	if err != nil {
		return err
	}
	ref := DatabaseRef{NodeID: n.ID, InstanceID: state.ProxyID, InstanceRevision: instance.Revision}
	action := protocol.DatabaseAction{Kind: "shared_proxy", ClusterID: state.Binding.GaleraID, Shared: &protocol.SharedDatabaseAction{Mode: "revoke", Cluster: n.cluster, Database: state.Binding.Database, NodeID: id}}
	_, err = n.ensureDatabaseStep(ctx, ref, stepID(n.ID, "shared-proxy-revoke/"+state.Binding.ClusterID+"/"+id), action)
	return err
}

func (n *Node) reconcileSharedRevocations(ctx context.Context, state *SharedLocal) error {
	directory, err := n.Directory(ctx)
	if err != nil {
		return err
	}
	for _, revoked := range directory.Revocations {
		if err = n.revokeSharedProxy(ctx, state, revoked.ID); err != nil {
			return err
		}
		for _, ref := range state.Cluster.Members {
			if ref.NodeID != n.ID {
				continue
			}
			action := protocol.DatabaseAction{Kind: "shared_database", ClusterID: state.Binding.GaleraID, Shared: &protocol.SharedDatabaseAction{Mode: "revoke", Cluster: n.cluster, Database: state.Binding.Database, NodeID: revoked.ID}}
			if _, err = n.ensureDatabaseStep(ctx, ref, stepID(n.ID, "shared-database-revoke/"+state.Binding.ClusterID+"/"+revoked.ID), action); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *Node) recoverSharedProxy(ctx context.Context, state *SharedLocal) (bool, error) {
	if state.Admin.ContainerID == "" {
		return true, nil
	}
	reply, err := n.executeRead(ctx, &protocol.DockerRead{})
	if err != nil {
		return false, err
	}
	read, ok := reply.(*protocol.DockerReadResult)
	if !ok {
		return false, fmt.Errorf("Docker 状态应答无效")
	}
	for _, item := range read.State.Containers {
		if item.ContainerID != state.Admin.ContainerID {
			continue
		}
		if item.State == "running" {
			return true, nil
		}
		if state.ProxyReady {
			state.ProxyReady = false
			if err = n.saveSharedLocal(ctx, *state); err != nil {
				return false, err
			}
		}
		if item.State == "restarting" {
			return false, nil
		}
		if item.State != "exited" && item.State != "created" && item.State != "dead" {
			return false, fmt.Errorf("代理容器当前状态需要处理：%s", item.State)
		}
		instance, err := n.databaseInstance(ctx, state.ProxyID)
		if err != nil {
			return false, err
		}
		ref := DatabaseRef{NodeID: n.ID, InstanceID: instance.ID, InstanceRevision: instance.Revision}
		action := protocol.DatabaseAction{Kind: "shared_proxy_repair", Repair: &item.ContainerRef, ClusterID: state.Binding.GaleraID, Shared: &protocol.SharedDatabaseAction{Mode: "repair", Cluster: n.cluster, Database: state.Binding.Database, NodeID: n.ID}}
		return n.ensureDatabaseStep(ctx, ref, stepID(n.ID, "shared-proxy-repair/"+protocol.ContainerRevision(item)), action)
	}
	return false, fmt.Errorf("原代理容器不存在，请核对节点数据")
}
