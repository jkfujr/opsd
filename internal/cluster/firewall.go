package cluster

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
)

type FirewallWrite struct {
	ExpectedRevision uint64                   `json:"expected_revision"`
	Change           *protocol.FirewallChange `json:"change,omitempty"`
	Key              string                   `json:"key"`
	migration        bool
}

type FirewallSaved struct {
	Configuration protocol.ManagedFirewallState `json:"configuration"`
	OperationID   string                        `json:"operation_id"`
}

type firewallQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func firewallState(ctx context.Context, q firewallQuerier) (protocol.ManagedFirewallState, protocol.FirewallDefinition, error) {
	var body, confirmed string
	var state protocol.ManagedFirewallState
	var definition protocol.FirewallDefinition
	e := q.QueryRowContext(ctx, "SELECT body,confirmed FROM firewall_config WHERE id=1").Scan(&body, &confirmed)
	if errors.Is(e, sql.ErrNoRows) {
		return protocol.ManagedFirewallState{FirewallDefinition: protocol.FirewallDefinition{Backend: "unknown", Rules: []protocol.ManagedRule{}, DefaultPolicies: []protocol.ManagedPolicy{}}, State: "importing"}, definition, nil
	}
	if e == nil {
		e = json.Unmarshal([]byte(body), &state)
	}
	if e == nil {
		e = json.Unmarshal([]byte(confirmed), &definition)
	}
	return state, definition, e
}

// FirewallConfig 是页面读取入口，只访问节点自己的 SQLite。
func (n *Node) FirewallConfig(ctx context.Context) (protocol.ManagedFirewallState, error) {
	state, _, e := firewallState(ctx, n.DB)
	return state, e
}

func (n *Node) writeFirewall(ctx context.Context, origin string, request FirewallWrite, maintenance bool) (FirewallSaved, error) {
	var out FirewallSaved
	if request.Key == "" || len(request.Key) > 128 {
		return out, fault("invalid_request", "缺少有效请求标识")
	}
	b, _ := json.Marshal(request)
	digest := sha256.Sum256(b)
	n.mu.Lock()
	defer n.mu.Unlock()
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var existing string
	var oldDigest []byte
	e = tx.QueryRowContext(ctx, "SELECT body,request_digest FROM queue WHERE origin=? AND request_key=?", origin, request.Key).Scan(&existing, &oldDigest)
	if e == nil {
		if string(oldDigest) != string(digest[:]) {
			return out, fault("conflict", "同一请求不能使用不同参数")
		}
		var op Operation
		if e = json.Unmarshal([]byte(existing), &op); e != nil {
			return out, e
		}
		out.Configuration, _, e = firewallState(ctx, tx)
		out.OperationID = op.ID
		return out, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	state, confirmed, e := firewallState(ctx, tx)
	if e != nil {
		return out, e
	}
	if state.Revision == 0 || state.Backend == "none" {
		return out, fault("conflict", "防火墙尚未完成导入")
	}
	if state.Revision != request.ExpectedRevision {
		return out, fault("conflict", "配置已经改变，请刷新")
	}
	var pending int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE kind='firewall_config' AND terminal=0").Scan(&pending); e != nil {
		return out, e
	}
	if pending > 0 {
		return out, fault("node_busy", "防火墙仍有未结束操作")
	}
	target := protocol.FirewallTarget{Revision: state.Revision, Configuration: state.FirewallDefinition}
	if maintenance {
		target = protocol.FirewallTarget{Revision: state.ConfirmedRevision, Configuration: confirmed, Automatic: true}
	} else if request.migration || request.Change == nil && state.Migration != nil && state.Migration.State == "failed" {
		target, e = n.trustMigrationTarget(ctx, tx, state, confirmed)
		if e != nil {
			return out, e
		}
		if request.migration && request.Key != trustMigrationKey(target) {
			return out, fault("conflict", "互信目标已经改变")
		}
	} else if request.Change != nil {
		target.Configuration, e = firewall.ChangeConfiguration(state.FirewallDefinition, *request.Change)
		if e != nil {
			return out, e
		}
		target.Revision++
		state.FirewallDefinition = target.Configuration
		state.Revision = target.Revision
		state.Error = nil
	} else {
		if state.State != "failed" && state.State != "unknown" {
			return out, fault("conflict", "当前配置无需重试")
		}
		target.Automatic = target.Revision == state.ConfirmedRevision
	}
	if target.Revision == 0 {
		return out, fault("conflict", "没有已确认配置")
	}
	data, _ := json.Marshal(target)
	encrypted, e := seal(data, []string{n.age.Recipient().String()})
	if e != nil {
		return out, e
	}
	payload, _ := json.Marshal(encrypted)
	idSum := sha256.Sum256([]byte(origin + "\x00" + request.Key))
	id := n.ID + "_" + hex.EncodeToString(idSum[:])
	op := Operation{Key: request.Key, OperationRecord: protocol.OperationRecord{ID: id, State: protocol.Accepted, Revision: 1, Decision: "undecided", Steps: []protocol.OperationStep{}}, NodeID: n.ID, Subject: "firewall_config", Actor: origin, CreatedAt: n.Now().UTC(), UpdatedAt: n.Now().UTC(), Executor: "node"}
	opBody, _ := json.Marshal(op)
	r, e := tx.ExecContext(ctx, "INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,body) VALUES(?,?,'firewall_config',?,?,?,?)", id, origin, string(payload), request.Key, digest[:], string(opBody))
	if e != nil {
		return out, e
	}
	sequence, e := r.LastInsertId()
	if e != nil {
		return out, e
	}
	op.Sequence = uint64(sequence)
	opBody, _ = json.Marshal(op)
	if _, e = tx.ExecContext(ctx, "UPDATE queue SET body=? WHERE id=?", string(opBody), id); e != nil {
		return out, e
	}
	state.State = "pending"
	state.OperationID = id
	state.Error = nil
	if target.PeerTrustMigration != nil {
		state.Migration = &protocol.PeerTrustMigrationStatus{State: "pending", OperationID: id}
	}
	if e = n.persistFirewall(ctx, tx, state, confirmed); e != nil {
		return out, e
	}
	if e = n.appendTx(ctx, tx, "operation", id, op, n.Now()); e != nil {
		return out, e
	}
	if e = tx.Commit(); e != nil {
		return out, e
	}
	n.notifyChange(Change{Resource: "firewall", NodeID: n.ID, Revision: state.Revision})
	out.Configuration, out.OperationID = state, id
	return out, nil
}

func (n *Node) persistFirewall(ctx context.Context, tx *sql.Tx, state protocol.ManagedFirewallState, confirmed protocol.FirewallDefinition) error {
	b, e := json.Marshal(state)
	if e != nil {
		return e
	}
	c, _ := json.Marshal(confirmed)
	if _, e = tx.ExecContext(ctx, "INSERT INTO firewall_config(id,body,confirmed) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,confirmed=excluded.confirmed", string(b), string(c)); e != nil {
		return e
	}
	return n.appendTx(ctx, tx, "firewall", n.ID, state, n.Now())
}

func (n *Node) firewallOperation(ctx context.Context, tx *sql.Tx, op Operation) error {
	if op.Subject != "firewall_config" {
		return nil
	}
	state, confirmed, e := firewallState(ctx, tx)
	if e != nil {
		return e
	}
	if state.OperationID != op.ID {
		return nil
	}
	var payload string
	if e = tx.QueryRowContext(ctx, "SELECT payload FROM queue WHERE id=?", op.ID).Scan(&payload); e != nil {
		return e
	}
	var secret Secret
	if e = json.Unmarshal([]byte(payload), &secret); e != nil {
		return e
	}
	plain, e := unseal(secret, n.age)
	if e != nil {
		return e
	}
	var target protocol.FirewallTarget
	if e = json.Unmarshal(plain, &target); e != nil {
		return e
	}
	if op.LastObservedAt != nil {
		state.CheckedAt = op.LastObservedAt
	}
	if target.PeerTrustMigration != nil {
		state.Migration = &protocol.PeerTrustMigrationStatus{State: "pending", OperationID: op.ID, Error: op.Error}
		switch {
		case op.State == protocol.Succeeded:
			state.FirewallDefinition = target.Configuration
			state.Revision, state.ConfirmedRevision = target.Revision, target.Revision
			confirmed = target.Configuration
			state.State, state.Error = "in_sync", nil
			state.Migration.State = "in_sync"
		case operations.Terminal(op.State):
			state.FirewallDefinition = confirmed
			state.Revision = state.ConfirmedRevision
			state.State, state.Error = "failed", op.Error
			if state.Error == nil {
				state.Error = &protocol.Error{Code: "conflict", Message: "互信迁移未完成，原 IP 规则保留"}
			}
			state.Migration.State, state.Migration.Error = "failed", state.Error
		case op.State == protocol.AttentionRequired:
			state.State, state.Error = "unknown", op.Error
		case op.State == protocol.Prepared || op.State == protocol.Applying || op.State == protocol.Reverting:
			state.State = "applying"
		default:
			state.State = "pending"
		}
		return n.persistFirewall(ctx, tx, state, confirmed)
	}
	switch op.State {
	case protocol.Accepted:
		state.State = "pending"
	case protocol.Prepared, protocol.Applying, protocol.Reverting:
		state.State = "applying"
	case protocol.AwaitingConfirmation:
		state.State = "awaiting_confirmation"
	case protocol.AttentionRequired:
		state.State = "unknown"
		state.Error = op.Error
	case protocol.Succeeded:
		if !target.Automatic {
			state.ConfirmedRevision = target.Revision
			confirmed = target.Configuration
			state.Migration = nil
		}
		if state.Revision == state.ConfirmedRevision {
			state.State = "in_sync"
			state.Error = nil
		} else {
			state.State = "failed"
		}
	default:
		if operations.Terminal(op.State) {
			state.State = "failed"
			state.Error = op.Error
		}
	}
	return n.persistFirewall(ctx, tx, state, confirmed)
}

func (n *Node) firewallLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
		n.checkFirewall(ctx)
		cancel()
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *Node) checkFirewall(ctx context.Context) {
	if n.Executor == nil {
		return
	}
	var pending int
	if e := n.DB.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE terminal=0").Scan(&pending); e != nil || pending > 0 {
		return
	}
	state, _, e := firewallState(ctx, n.DB)
	if e != nil {
		return
	}
	reply, e := n.Executor.Call(ctx, &protocol.FirewallInspect{Import: state.Revision == 0})
	if e != nil {
		if firewall.Code(e) != "node_busy" {
			n.firewallCheckError(ctx, e)
		}
		return
	}
	inspection, ok := reply.(*protocol.FirewallInspected)
	if !ok {
		n.firewallCheckError(ctx, fault("conflict", "防火墙检查应答无效"))
		return
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return
	}
	defer tx.Rollback()
	state, confirmed, e := firewallState(ctx, tx)
	if e != nil {
		return
	}
	// 保存、受理和后台检查不能相互覆盖。
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE terminal=0").Scan(&pending); e != nil || pending > 0 {
		return
	}
	if state.Revision == 0 && inspection.Target != nil {
		state.FirewallDefinition = inspection.Target.Configuration
		state.Revision = inspection.Target.Revision
		state.ConfirmedRevision = inspection.Target.Revision
		confirmed = inspection.Target.Configuration
	}
	state.Backend = inspection.Backend
	state.ServiceActive = inspection.ServiceActive
	state.Capabilities = inspection.Capabilities
	state.CheckedAt = &inspection.CheckedAt
	if state.Revision == 0 {
		state.State = "unavailable"
		state.Error = nil
	} else if state.Revision == state.ConfirmedRevision {
		state.State = "in_sync"
		state.Error = nil
	}
	if inspection.Error != nil {
		state.State = "failed"
		state.Error = inspection.Error
	}
	if e = n.persistFirewall(ctx, tx, state, confirmed); e != nil {
		return
	}
	if e = tx.Commit(); e != nil {
		return
	}
	n.notifyChange(Change{Resource: "firewall", NodeID: n.ID, Revision: state.Revision})
	if inspection.Target != nil && inspection.Error == nil && !inspection.InSync {
		_, _ = n.writeFirewall(ctx, n.ID, FirewallWrite{ExpectedRevision: state.Revision, Key: "firewall-maintenance-" + identity.Random()}, true)
	}
}

func (n *Node) firewallCheckError(ctx context.Context, cause error) {
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return
	}
	defer tx.Rollback()
	state, confirmed, e := firewallState(ctx, tx)
	if e != nil {
		return
	}
	var pending int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE terminal=0").Scan(&pending); e != nil || pending > 0 {
		return
	}
	state.State = "failed"
	state.Error = &protocol.Error{Code: firewall.Code(cause), Message: cause.Error()}
	if e = n.persistFirewall(ctx, tx, state, confirmed); e == nil {
		e = tx.Commit()
	}
	if e == nil {
		n.notifyChange(Change{Resource: "firewall", NodeID: n.ID, Revision: state.Revision})
	}
}
