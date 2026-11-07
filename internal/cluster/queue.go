package cluster

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/account"
	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) dispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	if !n.Active(ctx, origin) {
		return nil, fault("forbidden", "节点未加入或已撤销")
	}
	if strings.HasPrefix(method, "api_token.") {
		return n.tokenDispatch(ctx, method, body)
	}
	if validateManagedDatabaseKind(method) {
		return n.databaseDispatch(ctx, origin, method, body)
	}
	switch method {
	case "containers.read", "container.inspect", "container.logs":
		return n.containerDispatch(ctx, method, body)
	case "public_addresses.read":
		return n.PublicAddresses(ctx, n.ID)
	case "public_addresses.refresh":
		return n.RefreshPublic(ctx)
	case "public_addresses.patch":
		var p PublicAddressesPatch
		if e := json.Unmarshal(body, &p); e != nil {
			return nil, e
		}
		return n.PatchPublic(ctx, p)
	case "peer_trust.rules":
		return n.LocalTrustRules(ctx)
	case "firewall.config.read":
		return n.FirewallConfig(ctx)
	case "firewall.config.write":
		var request FirewallWrite
		if e := json.Unmarshal(body, &request); e != nil {
			return nil, e
		}
		return n.writeFirewall(ctx, origin, request, false)
	case "ping":
		return n.Self(ctx)
	case "directory":
		var d Directory
		if e := json.Unmarshal(body, &d); e != nil {
			return nil, e
		}
		if e := n.merge(ctx, d, false); e != nil {
			return nil, e
		}
		return n.Directory(ctx)
	case "config.read":
		return n.Config(ctx)
	case "account.read":
		return n.Account(ctx)
	case "submit":
		var r Request
		if e := json.Unmarshal(body, &r); e != nil {
			return nil, e
		}
		return n.enqueue(ctx, origin, r)
	case "operation.get":
		var id string
		if e := json.Unmarshal(body, &id); e != nil {
			return nil, e
		}
		return n.operation(ctx, id)
	case "operation.list":
		var q ListQuery
		if e := json.Unmarshal(body, &q); e != nil {
			return nil, e
		}
		return n.localOperations(ctx, q)
	case "operation.cancel":
		var id string
		if e := json.Unmarshal(body, &id); e != nil {
			return nil, e
		}
		return n.cancelQueued(ctx, id)
	case "operation.confirm", "operation.recover", "operation.resolve":
		var request OperationDecision
		if e := json.Unmarshal(body, &request); e != nil {
			return nil, e
		}
		return n.decide(ctx, method, request)
	case "firewall.read":
		var p protocol.FirewallRead
		if e := json.Unmarshal(body, &p); e != nil {
			return nil, e
		}
		return n.executeRead(ctx, &p)
	case "firewall.preview":
		var p protocol.FirewallPreview
		if e := json.Unmarshal(body, &p); e != nil {
			return nil, e
		}
		reply, e := n.executeRead(ctx, &p)
		if e != nil {
			return nil, e
		}
		result, ok := reply.(*protocol.FirewallPreviewResult)
		if !ok {
			return nil, fmt.Errorf("预览应答无效")
		}
		result.Plan.ID = NewOperationID(n.ID)
		b, _ := json.Marshal(result.Plan)
		_, e = n.DB.ExecContext(ctx, "INSERT INTO plans(id,body,created_at) VALUES(?,?,?)", result.Plan.ID, string(b), n.Now().Unix())
		return result.Plan, e
	case "firewall.plan":
		var id string
		if e := json.Unmarshal(body, &id); e != nil {
			return nil, e
		}
		var b string
		var p protocol.ChangePlan
		var operationID sql.NullString
		e := n.DB.QueryRowContext(ctx, "SELECT body,operation_id FROM plans WHERE id=?", id).Scan(&b, &operationID)
		if e == nil {
			e = json.Unmarshal([]byte(b), &p)
		}
		return Plan{ChangePlan: p, OperationID: operationID.String}, e
	case "metrics":
		v, e := n.metrics(ctx, "live")
		if e != nil {
			return nil, e
		}
		pending, e := n.localOperations(ctx, ListQuery{Pending: true, Limit: 100})
		return struct {
			protocol.MonitoringResult
			Pending []Operation `json:"pending_operations"`
		}{v, pending.Items}, e
	case "history":
		var q HistoryQuery
		if e := json.Unmarshal(body, &q); e != nil {
			return nil, e
		}
		return n.localHistory(ctx, q)
	case "storage.credentials":
		return n.databaseCredentials(ctx, origin)
	case "storage.status":
		return n.StorageStatus(ctx)
	default:
		return nil, fault("invalid_request", "未知节点操作")
	}
}
func (n *Node) executeRead(ctx context.Context, p protocol.Payload) (protocol.Payload, error) {
	if n.Executor == nil {
		return nil, fault("dependency_unavailable", "本机执行器不可用")
	}
	return n.Executor.Call(ctx, p)
}
func (n *Node) Submit(ctx context.Context, target string, r Request) (Operation, error) {
	var out Operation
	e := n.Call(ctx, target, "submit", r, &out)
	return out, e
}
func (n *Node) enqueue(ctx context.Context, origin string, r Request) (Operation, error) {
	if strings.HasPrefix(r.Actor, "token:") {
		origin = r.Actor
	}
	if r.ID == "" && r.Key != "" {
		sum := sha256.Sum256([]byte(origin + "\x00" + r.Key))
		r.ID = n.ID + "_" + hex.EncodeToString(sum[:])
	}
	if operationOwner(r.ID) != n.ID || !operations.ValidID(r.ID) || len(r.Key) > 128 || len(r.Payload) > 256*1024 {
		return Operation{}, fault("invalid_request", "请求标识或内容无效")
	}
	switch r.Kind {
	case "database_step":
		var a protocol.DatabaseAction
		if e := json.Unmarshal(r.Payload, &a); e != nil {
			return Operation{}, e
		}
		if e := a.Validate(); e != nil {
			return Operation{}, fault("invalid_request", e.Error())
		}
		if r.ResourceID == "" {
			return Operation{}, fault("invalid_request", "数据库步骤缺少所属集群")
		}
	case "database":
		var input DatabaseSubmission
		if e := json.Unmarshal(r.Payload, &input); e != nil {
			return Operation{}, e
		}
		if input.Cluster.Coordinator != n.ID {
			return Operation{}, fault("forbidden", "数据库变更须由所属协调节点受理")
		}
		if e := validateDatabaseSubmission(input); e != nil {
			return Operation{}, e
		}
	case "container":
		var action protocol.ContainerAction
		if e := json.Unmarshal(r.Payload, &action); e != nil {
			return Operation{}, e
		}
		if e := action.Validate(); e != nil {
			return Operation{}, fault("invalid_request", e.Error())
		}
	case "firewall":
		var p protocol.ChangePlan
		if e := json.Unmarshal(r.Payload, &p); e != nil {
			return Operation{}, e
		}
		var body string
		if e := n.DB.QueryRowContext(ctx, "SELECT body FROM plans WHERE id=?", p.ID).Scan(&body); e != nil {
			return Operation{}, e
		}
		b, _ := json.Marshal(p)
		if string(b) != body || !p.Executable {
			return Operation{}, fault("conflict", "预览内容不匹配或不可执行")
		}
	case "component":
		var c protocol.ComponentAction
		if e := json.Unmarshal(r.Payload, &c); e != nil {
			return Operation{}, e
		}
		if c.Component != "firewall" || c.Action != "install" {
			return Operation{}, fault("invalid_request", "不支持此组件操作")
		}
	case "config":
		var c Config
		if e := json.Unmarshal(r.Payload, &c); e != nil {
			return Operation{}, e
		}
		if e := ValidateConfig(c); e != nil {
			return Operation{}, e
		}
		if c.Cached || c.LastSyncedAt != nil {
			return Operation{}, fault("conflict", "请重新读取节点配置")
		}
	case "account":
		var a AccountChange
		if e := json.Unmarshal(r.Payload, &a); e != nil {
			return Operation{}, e
		}
		if a.Password != "" && (len(a.Password) < 8 || len(a.Password) > 1024) {
			return Operation{}, fault("invalid_request", "密码长度须为 8 至 1024 字节")
		}
	case "name":
		var name string
		if e := json.Unmarshal(r.Payload, &name); e != nil {
			return Operation{}, e
		}
		if name == "" || len(name) > 255 {
			return Operation{}, fault("invalid_request", "名称须为 1 至 255 字节")
		}
	default:
		return Operation{}, fault("invalid_request", "未知修改类型")
	}
	digest := sha256.Sum256(append([]byte(r.Kind+"\x00"), r.Payload...))
	if r.Kind == "database" {
		var input DatabaseSubmission
		_ = json.Unmarshal(r.Payload, &input)
		fixed, _ := json.Marshal(input.Change)
		digest = sha256.Sum256(append([]byte(r.Kind+"\x00"), fixed...))
	}
	encrypted, e := seal(r.Payload, []string{n.age.Recipient().String()})
	if e != nil {
		return Operation{}, e
	}
	payload, _ := json.Marshal(encrypted)
	n.mu.Lock()
	defer n.mu.Unlock()
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return Operation{}, e
	}
	defer tx.Rollback()
	var existing string
	var priorDigest []byte
	e = tx.QueryRowContext(ctx, "SELECT body,request_digest FROM queue WHERE id=? OR (origin=? AND request_key=? AND request_key<>'')", r.ID, origin, r.Key).Scan(&existing, &priorDigest)
	if e == nil {
		if string(digest[:]) != string(priorDigest) {
			return Operation{}, fault("conflict", "同一请求不能使用不同参数")
		}
		var op Operation
		e = json.Unmarshal([]byte(existing), &op)
		return op, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Operation{}, e
	}
	op := Operation{Key: r.Key, OperationRecord: protocol.OperationRecord{ID: r.ID, State: protocol.Accepted, Revision: 1, Decision: "undecided", Steps: []protocol.OperationStep{}}, NodeID: n.ID, Subject: r.Kind, Actor: origin, CreatedAt: n.Now().UTC(), UpdatedAt: n.Now().UTC(), Executor: "node"}
	if r.Kind == "database_step" {
		op.ResourceID = r.ResourceID
	}
	if r.Kind == "database" {
		var input DatabaseSubmission
		_ = json.Unmarshal(r.Payload, &input)
		op.ResourceID = input.Cluster.ID
	}
	if r.Kind == "firewall" {
		var p protocol.ChangePlan
		_ = json.Unmarshal(r.Payload, &p)
		op.PlanID = p.ID
	}
	b, _ := json.Marshal(op)
	result, e := tx.ExecContext(ctx, "INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,body) VALUES(?,?,?,?,?,?,?)", r.ID, origin, r.Kind, string(payload), r.Key, digest[:], string(b))
	if e != nil {
		return op, e
	}
	seq, e := result.LastInsertId()
	if e != nil {
		return op, e
	}
	op.Sequence = uint64(seq)
	b, _ = json.Marshal(op)
	if _, e = tx.ExecContext(ctx, "UPDATE queue SET body=? WHERE id=?", string(b), r.ID); e != nil {
		return op, e
	}
	if op.PlanID != "" {
		if _, e = tx.ExecContext(ctx, "UPDATE plans SET operation_id=COALESCE(operation_id,?) WHERE id=?", op.ID, op.PlanID); e != nil {
			return op, e
		}
	}
	if e = n.appendTx(ctx, tx, "operation", op.ID, op, n.Now()); e != nil {
		return op, e
	}
	if e = tx.Commit(); e != nil {
		return op, e
	}
	n.notifyChange(Change{Resource: "operations", NodeID: n.ID})
	return op, nil
}
func (n *Node) operation(ctx context.Context, id string) (Operation, error) {
	var op Operation
	var b string
	e := n.DB.QueryRowContext(ctx, "SELECT body FROM queue WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal([]byte(b), &op)
	}
	if e == nil && op.State == protocol.AwaitingConfirmation && n.Executor != nil {
		reply, err := n.Executor.Call(ctx, &protocol.OperationQuery{OperationID: id})
		if q, ok := reply.(*protocol.OperationQueryResult); err == nil && ok && q.Record != nil && q.Record.Revision+1 == op.Revision && q.Record.State == op.State {
			op.ConfirmationRemainingSeconds = q.Record.ConfirmationRemainingSeconds
		} else {
			op.ResultPendingSync = true
			op.ConfirmationRemainingSeconds = nil
		}
	}
	return op, e
}
func (n *Node) GetOperation(ctx context.Context, id string) (Operation, error) {
	target := operationOwner(id)
	if target == "" {
		return Operation{}, fault("invalid_request", "操作标识无效")
	}
	var op Operation
	e := n.Call(ctx, target, "operation.get", id, &op)
	if e != nil && target != n.ID {
		var r Record
		r, err := n.latest(ctx, target, "operation", id)
		if err == nil {
			err = json.Unmarshal(r.Data, &op)
			op.ResultPendingSync = true
			e = err
		}
	}
	if e == nil {
		e = n.observe(ctx, "operation:"+op.ID, op.Revision)
	}
	return op, e
}
func (n *Node) saveOperation(ctx context.Context, op *Operation, phase string) error {
	if op.Error != nil {
		op.Error.RequestID = op.ID
	}
	op.UpdatedAt = n.Now().UTC()
	b, e := json.Marshal(op)
	if e != nil {
		return e
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, "UPDATE queue SET body=?,phase=?,terminal=? WHERE id=? AND terminal=0 AND json_extract(body,'$.revision')<=?", string(b), phase, operations.Terminal(op.State), op.ID, op.Revision)
	if e != nil {
		return e
	}
	count, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if count != 1 {
		return fault("conflict", "操作状态已改变")
	}
	if e = n.firewallOperation(ctx, tx, *op); e != nil {
		return e
	}
	if e = n.appendTx(ctx, tx, "operation", op.ID, op, n.Now()); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		n.notifyChange(Change{Resource: "operations", NodeID: n.ID})
		if op.Subject == "container" {
			n.invalidateContainers()
		}
		if op.Subject == "firewall_config" {
			n.notifyChange(Change{Resource: "firewall", NodeID: n.ID})
		}
	}
	return e
}
func (n *Node) queueLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		n.advance()
		select {
		case <-n.ctx.Done():
			return
		case <-n.wake:
		case <-ticker.C:
		}
	}
}
func (n *Node) advance() {
	ctx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
	defer cancel()
	var id, origin, kind, encrypted, phase string
	e := n.DB.QueryRowContext(ctx, "SELECT id,origin,kind,payload,phase FROM queue WHERE terminal=0 ORDER BY seq LIMIT 1").Scan(&id, &origin, &kind, &encrypted, &phase)
	if e != nil {
		return
	}
	op, e := n.operation(ctx, id)
	if e != nil {
		return
	}
	finish := func(state protocol.OperationState, e error) {
		op.State = state
		op.Revision++
		if e != nil {
			op.Error = &protocol.Error{Code: "conflict", Message: e.Error()}
		}
		_ = n.saveOperation(ctx, &op, "finished")
	}
	if phase == "queued" && !strings.HasPrefix(origin, "token:") && !n.Active(ctx, origin) {
		finish(protocol.Rejected, fault("forbidden", "请求来源已撤销"))
		return
	}
	claimed := false
	if phase == "queued" {
		result, err := n.DB.ExecContext(ctx, "UPDATE queue SET phase='dispatching' WHERE id=? AND phase='queued' AND terminal=0", id)
		if err != nil {
			return
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return
		}
		claimed = true
		phase = "dispatching"
	}

	var secret Secret
	if e = json.Unmarshal([]byte(encrypted), &secret); e != nil {
		return
	}
	plain, e := unseal(secret, n.age)
	if e != nil {
		finish(protocol.Rejected, e)
		return
	}
	if kind == "database" {
		n.advanceDatabase(ctx, &op, plain)
		return
	}
	if kind != "firewall" && kind != "component" && kind != "firewall_config" && kind != "container" && kind != "database_step" {
		e = n.applyLocal(ctx, kind, plain, &op)
		if e != nil {
			finish(protocol.Rejected, e)
		}
		return
	}
	if n.Executor == nil {
		return
	}
	var result protocol.Payload
	if claimed {
		// 先记下发送意图。进程中断后只查询/封存同一 ID，不重新发送未知结果的动作。
		action := protocol.OperationAction{Kind: kind}
		if kind == "database_step" {
			action.Kind = "database"
			action.Database = &protocol.DatabaseAction{}
			e = json.Unmarshal(plain, action.Database)
		} else if kind == "container" {
			action.Container = &protocol.ContainerAction{}
			e = json.Unmarshal(plain, action.Container)
		} else if kind == "firewall_config" {
			action.FirewallConfig = &protocol.FirewallTarget{}
			e = json.Unmarshal(plain, action.FirewallConfig)
		} else if kind == "firewall" {
			action.Firewall = &protocol.ChangePlan{}
			e = json.Unmarshal(plain, action.Firewall)
		} else {
			action.Component = &protocol.ComponentAction{}
			e = json.Unmarshal(plain, action.Component)
		}
		if e != nil {
			finish(protocol.Rejected, e)
			return
		}
		result, e = n.Executor.Call(ctx, &protocol.OperationSubmit{OperationID: id, ContractVersion: protocol.Version, Action: action})
	} else {
		result, e = n.Executor.Call(ctx, &protocol.OperationQuery{OperationID: id})
		if e == nil {
			if q, ok := result.(*protocol.OperationQueryResult); ok && !q.Found {
				result, e = n.Executor.Call(ctx, &protocol.OperationReconcile{OperationID: id})
			}
		}
	}
	if e != nil {
		return
	}
	var record *protocol.OperationRecord
	switch p := result.(type) {
	case *protocol.OperationUpdated:
		record = &p.Record
	case *protocol.OperationQueryResult:
		record = p.Record
	case *protocol.OperationReconcileResult:
		record = &p.Record
	}
	if record == nil || record.ID != id {
		return
	}
	if record.Revision+1 == op.Revision && record.State == op.State && phase == "submitted" {
		return
	}
	op.OperationRecord = *record
	op.Revision++
	op.ResultPendingSync = false
	_ = n.saveOperation(ctx, &op, "submitted")
}
func (n *Node) applyLocal(ctx context.Context, kind string, plain []byte, op *Operation) error {
	c, e := n.Config(ctx)
	if e != nil {
		return e
	}
	var credentials *Credentials
	var accountChange *AccountChange
	var passwordHash string
	var secretJSON []byte
	switch kind {
	case "name":
		if e = json.Unmarshal(plain, &c.Name); e != nil {
			return e
		}
	case "config":
		var next Config
		if e = json.Unmarshal(plain, &next); e != nil {
			return e
		}
		if next.Revision != c.Revision {
			return fault("conflict", "节点配置已改变，请重新读取")
		}
		if next.Listen != c.Listen {
			return fault("invalid_request", "节点通信监听地址须通过本机配置修改")
		}
		if e = ValidateConfig(next); e != nil {
			return e
		}
		if next.Storage != c.Storage {
			if n.Executor == nil {
				return fault("dependency_unavailable", "本机执行器不可用")
			}
			value, err := n.Executor.Storage(ctx, next.Storage)
			if err != nil {
				return err
			}
			credentials = &value
		}
		c = next
	case "account":
		accountChange = &AccountChange{}
		if e = json.Unmarshal(plain, accountChange); e != nil {
			return e
		}
		a, err := n.Account(ctx)
		if err != nil {
			return err
		}
		if accountChange.Username == "" {
			accountChange.Username = a.Username
		}
		if accountChange.Entrance == "" {
			accountChange.Entrance = a.Entrance
		}
		if e = account.Validate(accountChange.Username, accountChange.Entrance); e != nil {
			return e
		}
		if accountChange.Password == "" {
			b, err := unseal(a.Secret, n.age)
			if err != nil {
				return err
			}
			accountChange.Password = string(b)
		}
		passwordHash, e = account.Hash(accountChange.Password)
		if e != nil {
			return e
		}
		recipients, err := n.recipients(ctx)
		if err != nil {
			return err
		}
		secret, err := seal([]byte(accountChange.Password), recipients)
		if err != nil {
			return err
		}
		secretJSON, _ = json.Marshal(secret)
	default:
		return fault("invalid_request", "未知修改类型")
	}
	var complete func(bool)
	if accountChange == nil && n.Configure != nil {
		complete, e = n.Configure(ctx, c)
		if e != nil {
			return e
		}
	}
	committed := false
	if complete != nil {
		defer func() { complete(committed) }()
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if accountChange != nil {
		query := "UPDATE account SET username=?,password_hash=?,entrance=?,secret=?,revision=revision+1 WHERE id=1"
		args := []any{accountChange.Username, passwordHash, accountChange.Entrance, string(secretJSON)}
		if accountChange.Revision > 0 {
			query += " AND revision=?"
			args = append(args, accountChange.Revision)
		}
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fault("conflict", "账号已改变，请重新读取")
		}
		if _, e = tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=? WHERE revoked_at IS NULL", n.Now().Unix()); e != nil {
			return e
		}
		if e = n.publishAccountTx(ctx, tx); e != nil {
			return e
		}
		if e = n.publishMemberTx(ctx, tx, c); e != nil {
			return e
		}
	} else {
		c.Revision++
		b, _ := json.Marshal(c)
		if _, e = tx.ExecContext(ctx, "UPDATE node_identity SET config=? WHERE id=1", string(b)); e != nil {
			return e
		}
		if credentials != nil {
			b, _ = json.Marshal(credentials)
			encrypted, err := seal(b, []string{n.age.Recipient().String()})
			if err != nil {
				return err
			}
			b, _ = json.Marshal(encrypted)
			if _, e = tx.ExecContext(ctx, "INSERT INTO storage_credentials(id,secret) VALUES('admin',?) ON CONFLICT(id) DO UPDATE SET secret=excluded.secret", string(b)); e != nil {
				return e
			}
		}
		if e = n.publishMemberTx(ctx, tx, c); e != nil {
			return e
		}
		if e = n.appendTx(ctx, tx, "config", n.ID, c, n.Now()); e != nil {
			return e
		}
	}
	op.State = protocol.Succeeded
	op.Revision++
	op.UpdatedAt = n.Now().UTC()
	b, _ := json.Marshal(op)
	if _, e = tx.ExecContext(ctx, "UPDATE queue SET body=?,phase='finished',terminal=1 WHERE id=?", string(b), op.ID); e != nil {
		return e
	}
	if e = n.appendTx(ctx, tx, "operation", op.ID, op, n.Now()); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		committed = true
		n.notifyChange(Change{Resource: "nodes"})
		resource := "settings"
		if accountChange != nil {
			resource = "account"
		}
		n.notifyChange(Change{Resource: resource, NodeID: n.ID})
	}
	return e
}

type OperationDecision struct {
	ID               string `json:"id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	AcceptCurrent    bool   `json:"accept_current"`
}

func (n *Node) decide(ctx context.Context, method string, d OperationDecision) (Operation, error) {
	op, e := n.operation(ctx, d.ID)
	if e != nil {
		return op, e
	}
	if op.Subject == "database" {
		return n.decideDatabase(ctx, method, d, op)
	}
	var p protocol.Payload
	switch method {
	case "operation.confirm":
		p = &protocol.OperationConfirm{OperationID: d.ID, ExpectedRevision: d.ExpectedRevision - 1}
	case "operation.recover":
		p = &protocol.OperationRecover{OperationID: d.ID, ExpectedRevision: d.ExpectedRevision}
		if op.Subject == "container" || op.Subject == "database_step" {
			p = &protocol.OperationRecover{OperationID: d.ID, ExpectedRevision: d.ExpectedRevision - 1}
		}
	case "operation.resolve":
		p = &protocol.OperationResolve{OperationID: d.ID, ExpectedRevision: d.ExpectedRevision - 1, AcceptCurrent: d.AcceptCurrent}
	}
	if op.State == protocol.Accepted {
		return op, fault("conflict", "请求尚未开始")
	}
	reply, e := n.executeRead(ctx, p)
	if e != nil {
		return op, e
	}
	if result, ok := reply.(*protocol.OperationUpdated); ok {
		op.OperationRecord = result.Record
		op.Revision++
		e = n.saveOperation(ctx, &op, "submitted")
	}
	return op, e
}
func (n *Node) Decide(ctx context.Context, method string, d OperationDecision) (Operation, error) {
	var op Operation
	e := n.Call(ctx, operationOwner(d.ID), method, d, &op)
	return op, e
}
func (n *Node) cancelQueued(ctx context.Context, id string) (Operation, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return Operation{}, e
	}
	defer tx.Rollback()
	var b, phase string
	e = tx.QueryRowContext(ctx, "SELECT body,phase FROM queue WHERE id=?", id).Scan(&b, &phase)
	var op Operation
	if e == nil {
		e = json.Unmarshal([]byte(b), &op)
	}
	if e != nil {
		return op, e
	}
	if phase != "queued" {
		return op, fault("conflict", "请求已经开始")
	}
	op.State = protocol.Rejected
	op.Revision++
	op.UpdatedAt = n.Now().UTC()
	op.Error = &protocol.Error{Code: "cancelled", Message: "请求已取消"}
	b2, _ := json.Marshal(op)
	result, e := tx.ExecContext(ctx, "UPDATE queue SET body=?,phase='finished',terminal=1 WHERE id=? AND phase='queued'", string(b2), id)
	if e != nil {
		return op, e
	}
	count, e := result.RowsAffected()
	if e != nil || count != 1 {
		return op, fault("conflict", "请求已经开始")
	}
	if e = n.firewallOperation(ctx, tx, op); e != nil {
		return op, e
	}
	if e = n.appendTx(ctx, tx, "operation", id, op, n.Now()); e != nil {
		return op, e
	}
	e = tx.Commit()
	n.notifyChange(Change{Resource: "operations", NodeID: n.ID})
	if op.Subject == "firewall_config" {
		n.notifyChange(Change{Resource: "firewall", NodeID: n.ID})
	}
	return op, e
}

type ListQuery struct {
	Authorization *apiauth.Token    `json:"authorization,omitempty"`
	Key           string            `json:"key,omitempty"`
	Positions     map[string]uint64 `json:"positions,omitempty"`
	Pending       bool              `json:"pending"`
	State         string            `json:"state"`
	Cursor        uint64            `json:"cursor"`
	Limit         int               `json:"limit"`
}
type OperationList struct {
	NextPositions map[string]uint64 `json:"next_positions,omitempty"`
	Items         []Operation       `json:"items"`
	Next          uint64            `json:"next,omitempty"`
	Unavailable   []string          `json:"unavailable,omitempty"`
}

func (n *Node) localOperations(ctx context.Context, q ListQuery) (OperationList, error) {
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 50
	}
	query := "SELECT body FROM queue WHERE kind<>'database_step'"
	args := []any{}
	if q.Authorization != nil {
		m, e := n.Member(ctx, n.ID)
		if e != nil {
			return OperationList{}, e
		}
		parts := []string{}
		for _, pair := range [][2]string{{"name", "nodes"}, {"container", "containers"}, {"firewall", "firewall"}, {"firewall_config", "firewall"}, {"component", "firewall"}} {
			if q.Authorization.Allows(pair[1], "read", n.ID, m.Region) {
				parts = append(parts, "kind=?")
				args = append(args, pair[0])
			}
		}
		for _, g := range q.Authorization.Grants {
			if g.Module == "databases" {
				if g.Scope == "all" {
					parts = append(parts, "kind='database'")
				} else {
					for _, id := range g.Resources {
						parts = append(parts, "(kind='database' AND json_extract(body,'$.resource_id')=?)")
						args = append(args, id)
					}
				}
			}
		}
		if len(parts) == 0 {
			return OperationList{Items: []Operation{}}, nil
		}
		query += " AND (" + strings.Join(parts, " OR ") + ")"
	}
	if q.Key != "" {
		query += " AND request_key=?"
		args = append(args, q.Key)
		if q.Authorization != nil {
			query += " AND json_extract(body,'$.actor')=?"
			args = append(args, q.Authorization.Actor())
		}
	}
	if q.Pending {
		query += " AND terminal=0"
	}
	if q.State != "" {
		query += " AND json_extract(body,'$.state')=?"
		args = append(args, q.State)
	}
	if q.Cursor > 0 {
		query += " AND seq<?"
		args = append(args, q.Cursor)
	}
	query += " ORDER BY seq DESC LIMIT ?"
	args = append(args, q.Limit)
	rows, e := n.DB.QueryContext(ctx, query, args...)
	out := OperationList{Items: []Operation{}}
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var op Operation
		if e = rows.Scan(&b); e != nil {
			return out, e
		}
		if e = json.Unmarshal([]byte(b), &op); e != nil {
			return out, e
		}
		out.Items = append(out.Items, op)
	}
	if len(out.Items) == q.Limit {
		out.Next = out.Items[len(out.Items)-1].Sequence
	}
	return out, rows.Err()
}
func (n *Node) ListOperations(ctx context.Context, target string, q ListQuery) (OperationList, error) {
	if target != "" {
		if q.Authorization != nil {
			m, e := n.Member(ctx, target)
			if e != nil {
				return OperationList{}, e
			}
			clusters, e := n.DatabaseClusters(ctx)
			if e != nil {
				return OperationList{}, e
			}
			if !operationTargetAllowed(*q.Authorization, m, clusters) {
				return OperationList{}, fault("forbidden", "节点超出操作授权范围")
			}
		}
		var out OperationList
		e := n.Call(ctx, target, "operation.list", q, &out)
		if e != nil && target != n.ID && q.Authorization == nil {
			return n.archivedOperations(ctx, target, q)
		}
		return out, e
	}
	d, e := n.Directory(ctx)
	out := OperationList{Items: []Operation{}, Unavailable: []string{}}
	if e != nil {
		return out, e
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 50
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	slots := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	more := false
	revoked := map[string]bool{}
	for _, r := range d.Revocations {
		revoked[r.ID] = true
	}
	for _, m := range d.Members {
		if revoked[m.ID] {
			continue
		}
		if q.Authorization != nil && !operationTargetAllowed(*q.Authorization, m, d.Databases) {
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			mu.Lock()
			out.Unavailable = append(out.Unavailable, m.ID)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			local := q
			local.Cursor = q.Positions[m.ID]
			local.Positions = nil
			list, err := n.ListOperations(ctx, m.ID, local)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Unavailable = append(out.Unavailable, m.ID)
				return
			}
			if list.Next > 0 {
				more = true
			}
			out.Items = append(out.Items, list.Items...)
			sort.Slice(out.Items, func(i, j int) bool {
				a, b := out.Items[i], out.Items[j]
				if a.NodeID == b.NodeID {
					return a.Sequence > b.Sequence
				}
				if a.CreatedAt.Equal(b.CreatedAt) {
					return a.ID > b.ID
				}
				return a.CreatedAt.After(b.CreatedAt)
			})
			if len(out.Items) > q.Limit {
				more = true
				out.Items = out.Items[:q.Limit]
			}
		}()
	}
	wg.Wait()
	if more {
		out.NextPositions = map[string]uint64{}
		for id, cursor := range q.Positions {
			out.NextPositions[id] = cursor
		}
		for _, op := range out.Items {
			prior, ok := out.NextPositions[op.NodeID]
			if !ok || prior == 0 || op.Sequence < prior {
				out.NextPositions[op.NodeID] = op.Sequence
			}
		}
	}
	return out, nil
}

func operationError(e error) error {
	var f *firewall.Fault
	if errors.As(e, &f) {
		return &Fault{f.Code, f.Message}
	}
	return e
}
