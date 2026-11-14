package cluster

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"reflect"

	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) registerDatabaseDeployment(ctx context.Context, request DatabaseRPC) (DatabaseScanItem, error) {
	out := DatabaseScanItem{}
	d := request.Action.Deployment
	if d == nil || request.InstanceID != "deployment_"+d.ID || OperationOwner(request.OperationID) != n.ID {
		return out, fault("invalid_request", "部署实例标识无效")
	}
	var result protocol.OperationQueryResult
	if err := n.Call(ctx, n.ID, "database.query", DatabaseRPC{OperationID: request.OperationID}, &result); err != nil {
		return out, err
	}
	if !result.Found || result.Record == nil || result.Record.State != protocol.Succeeded {
		return out, fault("conflict", "部署步骤尚未完成")
	}
	reply, err := n.executeRead(ctx, &protocol.DatabaseRead{Connection: d.Connection, Scope: "health"})
	if err != nil {
		return out, err
	}
	read, ok := reply.(*protocol.DatabaseReadResult)
	if !ok {
		return out, fault("dependency_unavailable", "数据库状态应答无效")
	}
	out.Status = read.State
	if d.Kind == "galera" && (read.State.ClusterID == "" || d.ClusterID != "" && read.State.ClusterID != d.ClusterID || !read.State.Primary || !read.State.Ready || !read.State.Synced) {
		return out, fault("conflict", "数据库身份或同步状态不符合预期")
	}
	if d.Kind == "proxysql" && read.State.State != "ready" {
		return out, fault("conflict", "代理尚未就绪")
	}
	docker, err := n.executeRead(ctx, &protocol.DockerRead{})
	if err != nil {
		return out, err
	}
	listed, ok := docker.(*protocol.DockerReadResult)
	if !ok {
		return out, fault("dependency_unavailable", "容器读取应答无效")
	}
	id := ""
	for _, container := range listed.State.Containers {
		if container.Name == "opsd-"+d.Kind+"-"+d.ID {
			id = container.ContainerID
			break
		}
	}
	if id == "" {
		return out, fault("conflict", "原部署容器已不存在")
	}
	i := DatabaseInstance{ID: request.InstanceID, NodeID: n.ID, Name: request.Instance.Name, Kind: d.Kind, Address: d.Connection.Address, Username: d.Connection.Username, ContainerID: id, Running: true}
	if i.Name == "" {
		i.Name = d.ID
	}
	out.Instance, err = n.saveDatabaseInstance(ctx, DatabaseInstanceInput{DatabaseInstance: i, Password: &d.Connection.Password, ProtectedUsers: d.Connection.ProtectedUsers})
	return out, err
}

func (n *Node) saveDeploymentCluster(ctx context.Context, c ManagedDatabaseCluster) (ManagedDatabaseCluster, error) {
	current, err := n.DatabaseCluster(ctx, c.ID)
	if err != nil && err != sql.ErrNoRows {
		return c, err
	}
	if err == nil {
		if current.Revision != c.Revision {
			left, right := current, c
			left.Revision, right.Revision = 0, 0
			left.UpdatedAt = right.UpdatedAt
			left.Signature, right.Signature = nil, nil
			if reflect.DeepEqual(left, right) {
				return current, nil
			}
			return c, fault("conflict", "集群配置已被其他操作修改，请核对原任务")
		}
		if reflect.DeepEqual(current.Members, c.Members) && reflect.DeepEqual(current.Proxies, c.Proxies) {
			return current, nil
		}
	}
	return n.saveDatabaseCluster(ctx, c)
}

func (n *Node) finishDeploymentRouting(ctx context.Context, c ManagedDatabaseCluster, policy protocol.ProxyRoutingPolicy) error {
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var body string
	if err = tx.QueryRowContext(ctx, "SELECT body FROM managed_database_clusters WHERE id=?", c.ID).Scan(&body); err != nil {
		return err
	}
	var current ManagedDatabaseCluster
	if err = json.Unmarshal([]byte(body), &current); err != nil {
		return err
	}
	if reflect.DeepEqual(current.ProxyRouting, &policy) {
		return nil
	}
	if current.Revision != c.Revision {
		return fault("conflict", "集群配置已变化，请核对代理路由")
	}
	current.ProxyRouting = &policy
	current.Revision++
	current.UpdatedAt = n.Now().UTC()
	current.Signature = ed25519.Sign(n.signing, databaseClusterBytes(current))
	b, _ := json.Marshal(current)
	if _, err = tx.ExecContext(ctx, "UPDATE managed_database_clusters SET body=?,revision=? WHERE id=?", string(b), current.Revision, current.ID); err != nil {
		return err
	}
	if err = n.appendTx(ctx, tx, "database_cluster", current.ID, current, n.Now()); err != nil {
		return err
	}
	return tx.Commit()
}
