package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

func (n *Node) sharedDispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	switch method {
	case "shared.status":
		return n.SharedStatus(ctx), nil
	case "shared.prepare":
		var state SharedLocal
		if err := json.Unmarshal(body, &state); err != nil {
			return nil, err
		}
		if state.Access.Username != protocol.SharedUsername(n.ID) || state.Monitor.Username != protocol.SharedMonitorUser(n.ID) || state.Binding.Issuer != origin {
			return nil, fault("forbidden", "数据库接入资料不属于当前节点")
		}
		if existing := n.sharedState(); existing != nil && existing.Access.Username != "" {
			return n.SharedStatus(ctx), nil
		}
		if err := n.saveSharedLocal(ctx, state); err != nil {
			return nil, err
		}
		return n.SharedStatus(ctx), nil
	case "shared.export":
		if origin != n.ID {
			n.advanceMu.Lock()
			defer n.advanceMu.Unlock()
		}
		var q sharedExportRequest
		if err := json.Unmarshal(body, &q); err != nil {
			return nil, err
		}
		return n.exportShared(ctx, q)
	case "shared.bootstrap":
		return n.sharedBootstrap(ctx, origin)
	case "shared.flush":
		store, err := n.sharedStore(ctx)
		if err == nil {
			err = n.uploadShared(ctx, store)
		}
		return n.SharedStatus(ctx), err
	case "shared.recover":
		return n.SharedStatus(ctx), n.RecoverSharedState(ctx)
	case "shared.recover_steps":
		return n.SharedStatus(ctx), n.recoverSharedSteps(ctx)
	}
	return nil, fault("invalid_request", "未知共享数据操作")
}
func (n *Node) sharedBootstrap(ctx context.Context, node string) (SharedLocal, error) {
	state := n.sharedState()
	if state == nil || state.Phase != "active" {
		return SharedLocal{}, sharedsql.ErrUnavailable
	}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return SharedLocal{}, err
	}
	var access SharedLocal
	object, err := s.Object(ctx, "access", node)
	if err == sql.ErrNoRows {
		access = SharedLocal{DataKey: state.DataKey, Binding: state.Binding, Cluster: state.Cluster, Phase: "active", Access: Credentials{Username: protocol.SharedUsername(node), Password: identity.Random()}, Monitor: Credentials{Username: protocol.SharedMonitorUser(node), Password: identity.Random()}}
		current, e := n.DatabaseCluster(ctx, state.Binding.ClusterID)
		if e != nil {
			return access, e
		}
		if e = n.checkSharedConnections(ctx, current); e != nil {
			return access, e
		}
		for _, ref := range current.Members {
			if err := n.Call(ctx, ref.NodeID, "ping", nil, nil); err == nil {
				access.AccountSource = ref
				break
			}
		}
		if access.AccountSource.NodeID == "" {
			return access, fault("dependency_unavailable", "数据库成员的维护入口暂不可用")
		}
		recipients, e := n.sharedRecipients(ctx)
		if e != nil {
			return access, e
		}
		data, _ := json.Marshal(access)
		secret, e := seal(data, recipients)
		if e != nil {
			return access, e
		}
		object = sharedObject("access", node, 1, secret)
		if e = s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
			if _, e := t.Object(ctx, "revocation", node); e == nil {
				return fault("forbidden", "节点已撤销")
			} else if e != sql.ErrNoRows {
				return e
			}
			return t.Put(ctx, object, 0)
		}); e != nil {
			return access, e
		}
	} else if err != nil {
		return access, err
	}
	var secret Secret
	if err = json.Unmarshal(object.Body, &secret); err != nil {
		return access, err
	}
	key, err := n.sharedIdentity()
	if err != nil {
		return access, err
	}
	data, err := unseal(secret, key)
	if err != nil {
		return access, err
	}
	if err = json.Unmarshal(data, &access); err != nil {
		return access, err
	}
	c, err := n.DatabaseCluster(ctx, state.Binding.ClusterID)
	if err != nil {
		return access, err
	}
	access.Cluster = c
	if len(c.Members) == 0 {
		return access, fmt.Errorf("数据库集群没有成员")
	}
	ref := access.AccountSource
	if ref.NodeID == "" {
		return access, fault("dependency_unavailable", "节点账号缺少原维护目标")
	}
	a := protocol.DatabaseAction{Kind: "shared_database", ClusterID: state.Binding.GaleraID, Shared: &protocol.SharedDatabaseAction{Mode: "provision", Cluster: n.cluster, Database: state.Binding.Database, NodeID: node, PasswordHash: databaseengine.NativePassword(access.Access.Password), MonitorHash: databaseengine.NativePassword(access.Monitor.Password)}}
	ready, err := n.ensureDatabaseStep(ctx, ref, stepID(ref.NodeID, "shared-maintenance/"+n.cluster+"/"+state.Binding.Database+"/provision/"+node), a)
	if err != nil {
		return access, err
	}
	if !ready {
		return access, fault("node_busy", "节点数据库账号正在准备")
	}
	return access, nil
}
func (n *Node) RecoverSharedState(ctx context.Context) error {
	state := n.sharedState()
	if state == nil {
		return fault("conflict", "节点尚未绑定 OPSD 数据库")
	}
	if err := n.recoverSharedSteps(ctx); err != nil {
		return err
	}
	if state.Migration != nil && state.Migration.Stage != "complete" {
		for id := range state.Migration.Nodes {
			if id != n.ID {
				if err := n.Call(ctx, id, "shared.recover_steps", nil, nil); err != nil {
					return err
				}
			}
		}
		return nil
	}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return err
	}
	var high uint64
	err = s.DB.QueryRowContext(ctx, "SELECT sequence FROM shared_watermarks WHERE owner=?", n.ID).Scan(&high)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	rows, err := n.DB.QueryContext(ctx, "SELECT id FROM queue WHERE terminal=0")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		op, e := n.operation(ctx, id)
		if e != nil {
			return e
		}
		if op.Subject == "database" || op.Subject == "shared_initialize" {
			continue
		}
		if record, e := n.latestShared(ctx, n.ID, "operation", id); e == nil {
			var original Operation
			if json.Unmarshal(record.Data, &original) == nil && terminalState(original.State) {
				body, _ := json.Marshal(original)
				if _, e = n.DB.ExecContext(ctx, "UPDATE queue SET body=?,terminal=1,phase='finished' WHERE id=?", string(body), id); e != nil {
					return e
				}
				continue
			}
		}
		var result protocol.OperationQueryResult
		switch op.Subject {
		case "firewall", "component", "firewall_config", "container", "database_step", "task_run", "compose":
			reply, e := n.executeRead(ctx, &protocol.OperationQuery{OperationID: id})
			if e != nil {
				return e
			}
			value, ok := reply.(*protocol.OperationQueryResult)
			if !ok {
				return fmt.Errorf("原操作查询应答无效")
			}
			result = *value
		}
		if result.Found && result.Record != nil {
			op.OperationRecord = *result.Record
		} else {
			op.State = protocol.AttentionRequired
			op.Error = &protocol.Error{Code: "restored_state", Message: "恢复的操作需要核对原结果"}
		}
		op.Revision++
		body, _ := json.Marshal(op)
		if _, e = n.DB.ExecContext(ctx, "UPDATE queue SET body=?,phase='submitted',terminal=? WHERE id=?", string(body), terminalState(op.State), id); e != nil {
			return e
		}
	}
	if _, err = n.DB.ExecContext(ctx, "UPDATE node_identity SET sequence=MAX(sequence,?) WHERE id=1", high); err != nil {
		return err
	}
	n.notify()
	return nil
}

func (n *Node) recoverSharedSteps(ctx context.Context) error {
	n.advanceMu.Lock()
	defer n.advanceMu.Unlock()
	rows, err := n.DB.QueryContext(ctx, "SELECT body FROM queue WHERE terminal=0 AND kind IN ('database_step','shared_initialize')")
	if err != nil {
		return err
	}
	var pending []Operation
	for rows.Next() {
		var body []byte
		var op Operation
		if err = rows.Scan(&body); err != nil {
			break
		}
		if err = json.Unmarshal(body, &op); err != nil {
			break
		}
		if op.State == protocol.AttentionRequired {
			pending = append(pending, op)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, op := range pending {
		if _, err = n.decide(ctx, "operation.recover", OperationDecision{ID: op.ID, ExpectedRevision: op.Revision}); err != nil {
			return err
		}
	}
	n.notify()
	return nil
}
