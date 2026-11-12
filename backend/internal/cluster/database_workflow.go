package cluster

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
)

type DatabaseProxySelection struct {
	NodeID     string `json:"node_id"`
	InstanceID string `json:"instance_id"`
	Hostgroup  int    `json:"hostgroup"`
}
type DatabaseChange struct {
	Routing          *protocol.ProxyRoutingPolicy `json:"routing,omitempty"`
	RoutingTargets   []ProxyRoutePreview          `json:"routing_targets,omitempty"`
	Cluster          string                       `json:"cluster_id"`
	ExpectedRevision uint64                       `json:"expected_revision"`
	Kind             string                       `json:"kind"`
	Database         string                       `json:"database,omitempty"`
	Charset          string                       `json:"charset,omitempty"`
	User             string                       `json:"user,omitempty"`
	Host             string                       `json:"host,omitempty"`
	Password         string                       `json:"password,omitempty"`
	Privileges       []string                     `json:"privileges,omitempty"`
	Grants           []protocol.DatabaseGrant     `json:"grants,omitempty"`
	Proxies          []DatabaseProxySelection     `json:"proxies"`
}
type DatabaseSubmission struct {
	Change  DatabaseChange         `json:"change"`
	Cluster ManagedDatabaseCluster `json:"cluster"`
}
type databaseWorkflowStep struct {
	Ref      DatabaseRef             `json:"ref"`
	Action   protocol.DatabaseAction `json:"action"`
	ID       string                  `json:"id"`
	State    string                  `json:"state"`
	Revision uint64                  `json:"revision"`
	Attempts uint64                  `json:"attempts"`
}
type databaseWorkflow struct {
	Built   bool                   `json:"built"`
	Index   int                    `json:"index"`
	Steps   []databaseWorkflowStep `json:"steps"`
	Request string                 `json:"request"`
}

func validateDatabaseSubmission(input DatabaseSubmission) error {
	c, d := input.Cluster, input.Change
	if d.Kind == "configure_proxy_routes" {
		return validateRoutingSubmission(input)
	}
	if c.ID == "" || c.Deleted || d.Cluster != c.ID || d.ExpectedRevision != c.Revision || !slices.Contains([]string{"create_database", "delete_database", "create_user", "password", "delete_user", "grant", "revoke", "sync_user"}, d.Kind) || len(d.Password) > 1024 || len(d.Grants) > 100 || len(d.Proxies) > 100 {
		return fault("invalid_request", "数据库操作参数无效")
	}
	if d.Kind == "create_user" || d.Kind == "password" {
		if len(d.Password) < 8 {
			return fault("invalid_request", "密码至少需要 8 字节")
		}
	}
	for _, p := range d.Proxies {
		if p.Hostgroup < 0 || !slices.ContainsFunc(c.Proxies, func(ref DatabaseRef) bool { return ref.NodeID == p.NodeID && ref.InstanceID == p.InstanceID }) {
			return fault("invalid_request", "代理超出数据库集群范围")
		}
		if c.ProxyRouting != nil && c.ProxyRouting.Enabled && d.Kind != "delete_user" && p.Hostgroup != c.ProxyRouting.Hostgroups.Writer {
			return fault("invalid_request", "受管代理账号须使用写入主机组")
		}
	}
	if d.Kind == "sync_user" {
		if d.User == "" || d.Host == "" || len(d.Proxies) == 0 {
			return fault("invalid_request", "请选择账号和代理")
		}
		return nil
	}
	a := protocol.DatabaseAction{Connection: protocol.DatabaseConnection{Kind: "galera", Address: "127.0.0.1:3306", Username: "__opsd_validation__"}, ClusterID: c.ClusterID, Kind: d.Kind, Database: d.Database, Charset: d.Charset, User: d.User, Host: d.Host, Privileges: d.Privileges}
	if d.Password != "" {
		a.PasswordHash = databaseengine.NativePassword(d.Password)
	}
	if e := a.Validate(); e != nil {
		return fault("invalid_request", e.Error())
	}
	for _, g := range d.Grants {
		a.Kind = "grant"
		a.Database = g.Database
		a.Privileges = g.Privileges
		if e := a.Validate(); e != nil {
			return fault("invalid_request", e.Error())
		}
	}
	return nil
}
func (n *Node) SubmitDatabaseChange(ctx context.Context, change DatabaseChange, actor, key string, fingerprint ...string) (Operation, error) {
	c, e := n.DatabaseCluster(ctx, change.Cluster)
	if e != nil {
		return Operation{}, e
	}
	digest := ""
	if len(fingerprint) > 0 {
		digest = fingerprint[0]
	}
	if digest != "" {
		prior, e := n.RequestReceipt(ctx, c.Coordinator, requeststate.Write{Actor: actor, Key: key, Digest: digest})
		if e == nil && prior.Operation != nil {
			return *prior.Operation, nil
		}
		if e != nil && !missingRequest(e) {
			return Operation{}, e
		}
		if e = n.RequireAPIAuthorization(ctx, c.Coordinator); e != nil {
			return Operation{}, e
		}
	}
	if c.Revision != change.ExpectedRevision {
		return Operation{}, fault("conflict", "数据库集群配置已经变化")
	}
	if change.Kind == "configure_proxy_routes" && c.ProxyRouting == nil {
		if e = n.requireProxyRoutingSupport(ctx); e != nil {
			return Operation{}, e
		}
	}
	var pins sync.WaitGroup
	for groupIndex, group := range [][]DatabaseRef{c.Members, c.Proxies} {
		for index := range group {
			ref := &group[index]
			needed := groupIndex == 0 && ref.NodeID == c.Coordinator || groupIndex == 1 && slices.ContainsFunc(change.Proxies, func(p DatabaseProxySelection) bool { return p.NodeID == ref.NodeID && p.InstanceID == ref.InstanceID })
			if !needed {
				continue
			}
			pins.Add(1)
			go func() {
				defer pins.Done()
				pinCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				var instance DatabaseInstance
				if err := n.Call(pinCtx, ref.NodeID, "database.instance.get", DatabaseRPC{InstanceID: ref.InstanceID}, &instance); err == nil {
					ref.InstanceRevision = instance.Revision
				}
			}()
		}
	}
	pins.Wait()
	input := DatabaseSubmission{Change: change, Cluster: c}
	if e = validateDatabaseSubmission(input); e != nil {
		return Operation{}, e
	}
	body, _ := json.Marshal(input)
	return n.Submit(ctx, c.Coordinator, Request{Actor: actor, Kind: "database", Payload: body, Key: key, InputDigest: digest})
}
func (n *Node) buildDatabaseWorkflow(ctx context.Context, id string, input DatabaseSubmission) (databaseWorkflow, error) {
	c, d := input.Cluster, input.Change
	out := databaseWorkflow{Steps: []databaseWorkflowStep{}}
	var source DatabaseRef
	for _, ref := range c.Members {
		if ref.NodeID == n.ID {
			source = ref
			break
		}
	}
	if source.InstanceID == "" {
		return out, fault("conflict", "协调节点缺少 Galera 成员")
	}
	hash := ""
	if d.Password != "" {
		hash = databaseengine.NativePassword(d.Password)
	}
	if d.Kind == "sync_user" {
		var result protocol.DatabaseReadResult
		e := n.Call(ctx, source.NodeID, "database.read", DatabaseRPC{InstanceID: source.InstanceID, InstanceRevision: source.InstanceRevision, ExpectedClusterID: c.ClusterID, User: d.User, Host: d.Host}, &result)
		if e != nil {
			return out, e
		}
		hash = result.PasswordHash
	}
	add := func(ref DatabaseRef, a protocol.DatabaseAction) {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", id, len(out.Steps))))
		out.Steps = append(out.Steps, databaseWorkflowStep{Ref: ref, Action: a, ID: ref.NodeID + "_" + hex.EncodeToString(sum[:]), State: "pending"})
	}
	if d.Kind == "configure_proxy_routes" {
		for _, preview := range d.RoutingTargets {
			for _, ref := range c.Proxies {
				if ref.NodeID == preview.NodeID && ref.InstanceID == preview.InstanceID {
					ref.InstanceRevision = preview.InstanceRevision
					action := preview.Action
					add(ref, protocol.DatabaseAction{Kind: "proxy_route", Routing: &action})
				}
			}
		}
		out.Built = true
		return out, nil
	}
	proxy := func(kind string) {
		for _, p := range d.Proxies {
			for _, ref := range c.Proxies {
				if p.NodeID == ref.NodeID && p.InstanceID == ref.InstanceID {
					add(ref, protocol.DatabaseAction{Kind: kind, User: d.User, Host: d.Host, PasswordHash: hash, Hostgroup: p.Hostgroup})
				}
			}
		}
	}
	if d.Kind == "delete_user" {
		proxy("proxy_disable")
	}
	if d.Kind != "sync_user" {
		add(source, protocol.DatabaseAction{ClusterID: c.ClusterID, Kind: d.Kind, Database: d.Database, Charset: d.Charset, User: d.User, Host: d.Host, PasswordHash: hash, Privileges: databaseengine.SortedPrivileges(d.Privileges)})
	}
	if d.Kind == "create_user" {
		for _, g := range d.Grants {
			add(source, protocol.DatabaseAction{ClusterID: c.ClusterID, Kind: "grant", User: d.User, Host: d.Host, Database: g.Database, Privileges: databaseengine.SortedPrivileges(g.Privileges)})
		}
	}
	if d.Kind == "create_user" || d.Kind == "password" || d.Kind == "sync_user" {
		proxy("proxy_upsert")
	}
	if d.Kind == "delete_user" {
		proxy("proxy_delete")
	}
	out.Built = true
	return out, nil
}
func (n *Node) loadDatabaseWorkflow(ctx context.Context, id string) (databaseWorkflow, error) {
	var out databaseWorkflow
	var b string
	e := n.DB.QueryRowContext(ctx, "SELECT body FROM database_workflows WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal([]byte(b), &out)
	}
	return out, e
}
func (n *Node) persistDatabaseWorkflow(ctx context.Context, op *Operation, flow databaseWorkflow) error {
	before := op.Revision
	op.Revision++
	op.UpdatedAt = n.Now().UTC()
	op.Steps = []protocol.OperationStep{}
	for _, step := range flow.Steps {
		name := step.Ref.Name
		if name == "" {
			name = step.Ref.NodeID
		}
		op.Steps = append(op.Steps, protocol.OperationStep{Name: name + " · " + databaseActionName(step.Action.Kind), State: step.State})
	}
	opBody, _ := json.Marshal(op)
	flowBody, _ := json.Marshal(flow)
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, "UPDATE queue SET body=?,phase='submitted',terminal=? WHERE id=? AND json_extract(body,'$.revision')=?", string(opBody), operations.Terminal(op.State), op.ID, before)
	if e != nil {
		return e
	}
	count, _ := r.RowsAffected()
	if count != 1 {
		return fault("conflict", "数据库操作阶段已经变化")
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO database_workflows(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", op.ID, string(flowBody)); e != nil {
		return e
	}
	if e = n.appendTx(ctx, tx, "operation", op.ID, op, n.Now()); e != nil {
		return e
	}
	if op.State == protocol.Succeeded && op.Action == "configure_proxy_routes" {
		if e = n.finishProxyRoutingPolicy(ctx, tx, op.ID); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e == nil {
		n.notifyChange(Change{Resource: "operations", NodeID: n.ID})
		n.notifyChange(Change{Resource: "databases"})
	}
	return e
}
func databaseActionName(kind string) string {
	return map[string]string{"create_database": "创建库", "delete_database": "删除库", "create_user": "创建账号", "password": "修改密码", "delete_user": "删除账号", "grant": "授权", "revoke": "撤权", "proxy_upsert": "同步代理账号", "proxy_disable": "停用代理账号", "proxy_delete": "删除代理账号", "proxy_route": "应用代理路由"}[kind]
}
func (n *Node) advanceDatabase(ctx context.Context, op *Operation, plain []byte) {
	flow, e := n.loadDatabaseWorkflow(ctx, op.ID)
	attention := func(err error) {
		op.State = protocol.AttentionRequired
		op.Error = &protocol.Error{Code: "dependency_unavailable", Message: err.Error()}
		flow.Request = ""
		_ = n.persistDatabaseWorkflow(ctx, op, flow)
	}
	if e == sql.ErrNoRows || e == nil && !flow.Built && flow.Request == "recover" {
		var input DatabaseSubmission
		if json.Unmarshal(plain, &input) != nil {
			return
		}
		current, err := n.DatabaseCluster(ctx, input.Cluster.ID)
		if err != nil || current.Revision != input.Change.ExpectedRevision {
			op.State = protocol.Rejected
			op.Error = &protocol.Error{Code: "conflict", Message: "数据库集群配置已经改变，未执行原请求"}
			_ = n.persistDatabaseWorkflow(ctx, op, flow)
			return
		}
		flow, e = n.buildDatabaseWorkflow(ctx, op.ID, input)
		if e != nil {
			attention(e)
			return
		}
		op.State = protocol.Applying
		if n.persistDatabaseWorkflow(ctx, op, flow) != nil {
			return
		}
	} else if e != nil {
		return
	}
	if op.State == protocol.AttentionRequired && flow.Request == "" {
		return
	}
	if !flow.Built && flow.Request == "resolve" {
		op.State = protocol.ResolvedManually
		op.Error = nil
		_ = n.persistDatabaseWorkflow(ctx, op, flow)
		return
	}
	if flow.Index >= len(flow.Steps) {
		op.State = protocol.Succeeded
		op.Error = nil
		if err := n.persistDatabaseWorkflow(ctx, op, flow); err != nil {
			if current, readErr := n.operation(ctx, op.ID); readErr == nil {
				*op = current
				attention(err)
			}
		}
		return
	}
	step := &flow.Steps[flow.Index]
	request := DatabaseRPC{Actor: op.Actor, ResourceID: op.ResourceID, InstanceID: step.Ref.InstanceID, InstanceRevision: step.Ref.InstanceRevision, OperationID: step.ID, Action: step.Action}
	if step.State == "pending" {
		step.State = "intent"
		op.State = protocol.Applying
		if n.persistDatabaseWorkflow(ctx, op, flow) != nil {
			return
		}
		var reply protocol.OperationUpdated
		e = n.Call(ctx, step.Ref.NodeID, "database.execute", request, &reply)
		if e != nil {
			attention(e)
			return
		}
		step.State = "applying"
		step.Revision = reply.Record.Revision
		_ = n.persistDatabaseWorkflow(ctx, op, flow)
		return
	}
	var result protocol.OperationQueryResult
	e = n.Call(ctx, step.Ref.NodeID, "database.query", request, &result)
	if e != nil {
		attention(e)
		return
	}
	if !result.Found || result.Record == nil {
		if flow.Request == "resolve" {
			if e = n.Call(ctx, step.Ref.NodeID, "database.reconcile", request, nil); e != nil {
				attention(e)
				return
			}
			op.State = protocol.ResolvedManually
			op.Error = nil
			_ = n.persistDatabaseWorkflow(ctx, op, flow)
			return
		}
		if flow.Request == "recover" {
			step.State = "pending"
			flow.Request = ""
			op.State = protocol.Applying
			op.Error = nil
			_ = n.persistDatabaseWorkflow(ctx, op, flow)
			return
		}
		attention(fmt.Errorf("目标尚未登记该步骤，请重新处理原操作"))
		return
	}
	record := *result.Record
	priorRevision := step.Revision
	step.Revision = record.Revision
	op.LastObservedAt = record.LastObservedAt
	if (flow.Request == "resolve" || flow.Request == "resolving") && operations.Terminal(record.State) {
		op.State = protocol.ResolvedManually
		op.Error = nil
		_ = n.persistDatabaseWorkflow(ctx, op, flow)
		return
	}
	if record.State == protocol.Succeeded {
		if step.Action.Kind == "proxy_route" {
			if e = n.Call(ctx, step.Ref.NodeID, "database.proxy_routing.applied", request, nil); e != nil {
				attention(e)
				return
			}
		}
		step.State = "completed"
		flow.Index++
		flow.Request = ""
		op.State = protocol.Applying
		op.Error = nil
		_ = n.persistDatabaseWorkflow(ctx, op, flow)
		return
	}
	if flow.Request == "resolve" {
		if record.State != protocol.AttentionRequired && record.State != protocol.Accepted && !operations.Terminal(record.State) {
			attention(fmt.Errorf("目标步骤仍在执行"))
			return
		}
		if record.State == protocol.AttentionRequired || record.State == protocol.Accepted {
			request.Revision = record.Revision
			var reply protocol.OperationUpdated
			if e = n.Call(ctx, step.Ref.NodeID, "database.resolve", request, &reply); e != nil {
				attention(e)
				return
			}
			flow.Request = "resolving"
			op.State = protocol.Applying
			_ = n.persistDatabaseWorkflow(ctx, op, flow)
			return
		}
	}
	if record.State == protocol.ResolvedManually && flow.Request == "resolving" {
		op.State = protocol.ResolvedManually
		op.Error = nil
		_ = n.persistDatabaseWorkflow(ctx, op, flow)
		return
	}
	if record.State == protocol.AttentionRequired && flow.Request == "recover" {
		request.Revision = record.Revision
		var reply protocol.OperationUpdated
		if e = n.Call(ctx, step.Ref.NodeID, "database.recover", request, &reply); e != nil {
			attention(e)
			return
		}
		flow.Request = "recovering"
		op.State = protocol.Applying
		op.Error = nil
		_ = n.persistDatabaseWorkflow(ctx, op, flow)
		return
	}
	if record.State == protocol.AttentionRequired {
		if (flow.Request == "recovering" || flow.Request == "resolving") && priorRevision == record.Revision {
			return
		}
		message := "数据库步骤需要处理"
		if record.Error != nil {
			message = record.Error.Message
		}
		step.State = "unknown"
		attention(fmt.Errorf("%s：%s", step.Ref.Name, message))
		return
	}
	if flow.Request == "recover" && record.State == protocol.Rejected && record.Error != nil && record.Error.Code == "not_accepted" {
		step.Attempts++
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%d", op.ID, flow.Index, step.Attempts)))
		step.ID = step.Ref.NodeID + "_" + hex.EncodeToString(sum[:])
		step.State = "pending"
		step.Revision = 0
		flow.Request = ""
		op.State = protocol.Applying
		op.Error = nil
		_ = n.persistDatabaseWorkflow(ctx, op, flow)
		return
	}
	if operations.Terminal(record.State) {
		attention(fmt.Errorf("数据库步骤已结束，请核对原操作"))
		return
	}
}
func (n *Node) decideDatabase(ctx context.Context, method string, d OperationDecision, op Operation) (Operation, error) {
	if op.Revision != d.ExpectedRevision || op.State != protocol.AttentionRequired {
		return op, fault("conflict", "数据库操作阶段已经变化")
	}
	flow, e := n.loadDatabaseWorkflow(ctx, op.ID)
	if e != nil {
		return op, e
	}
	switch method {
	case "operation.recover":
		flow.Request = "recover"
	case "operation.resolve":
		if !d.AcceptCurrent {
			return op, fault("invalid_request", "请确认接受当前状态")
		}
		flow.Request = "resolve"
	default:
		return op, fault("invalid_request", "数据库操作无需确认")
	}
	e = n.persistDatabaseWorkflow(ctx, &op, flow)
	if e == nil {
		n.notify()
	}
	return op, e
}
