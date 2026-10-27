package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/opsd-labs/opsd/internal/firewall"
	"github.com/opsd-labs/opsd/internal/httpapi"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/operations"
	"github.com/opsd-labs/opsd/internal/protocol"
)

type operationRow struct {
	ID, NodeID, Subject, Action, Actor string
	PlanID                             sql.NullString
	Created, Updated                   int64
	Record                             protocol.OperationRecord
	Pending                            bool
}
type rowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readOperation(ctx context.Context, q rowReader, id string) (operationRow, error) {
	var o operationRow
	var record string
	e := q.QueryRowContext(ctx, "SELECT id,node_id,plan_id,subject,action,actor,created_at,updated_at,record,pending_sync FROM operations WHERE id=?", id).Scan(&o.ID, &o.NodeID, &o.PlanID, &o.Subject, &o.Action, &o.Actor, &o.Created, &o.Updated, &record, &o.Pending)
	if e == nil {
		e = json.Unmarshal([]byte(record), &o.Record)
	}
	return o, e
}
func (s *Server) spawn(work func(context.Context)) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.workers.Add(1)
	s.mu.Unlock()
	go func() { defer s.workers.Done(); work(s.background) }()
}
func (s *Server) pendingIDs(ctx context.Context, nodeID string) ([]string, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT id FROM operations WHERE node_id=? AND terminal=0 ORDER BY created_at,id", nodeID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Server) currentConnection(nodeID string) *connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connections[nodeID]
}
func (s *Server) rpc(ctx context.Context, nodeID string, conn *connection, payload protocol.Payload) (protocol.Payload, error) {
	if conn == nil || s.currentConnection(nodeID) != conn {
		return nil, &firewall.Fault{Code: "node_unavailable", Message: "节点离线或连接已更换"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	message, e := conn.calls.Call(ctx, conn.channel, payload)
	if e != nil {
		return nil, &firewall.Fault{Code: "node_unavailable", Message: "节点应答未取得，请查询原操作或稍后重试读取"}
	}
	if s.currentConnection(nodeID) != conn {
		return nil, &firewall.Fault{Code: "node_unavailable", Message: "节点连接已更换，结果待核对"}
	}
	if failed, ok := message.Payload.(*protocol.RequestFailed); ok {
		return nil, &firewall.Fault{Code: failed.Error.Code, Message: failed.Error.Message}
	}
	return message.Payload, nil
}
func (s *Server) updateWritable(ctx context.Context, nodeID string, conn *connection) {
	ids, e := s.pendingIDs(ctx, nodeID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connections[nodeID] != conn {
		return
	}
	if e != nil {
		conn.writable = false
		conn.blocked = "未决状态无法读取"
		return
	}
	conn.writable = conn.ready && len(ids) == 0
	if !conn.ready {
		if conn.blocked == "" {
			conn.blocked = "正在核对未决操作"
		}
	} else if len(ids) > 0 {
		conn.blocked = "节点存在未决操作"
	} else {
		conn.blocked = ""
	}
}
func (s *Server) reconcileConnection(ctx context.Context, nodeID string, conn *connection, known, remote []string) {
	unknown := false
	knownSet := map[string]bool{}
	for _, id := range known {
		knownSet[id] = true
	}
	for _, id := range remote {
		if !knownSet[id] {
			row, e := readOperation(ctx, s.db, id)
			if e != nil || row.NodeID != nodeID {
				unknown = true
			} else {
				known = append(known, id)
				knownSet[id] = true
			}
		}
	}
	for _, id := range known {
		payload, e := s.rpc(ctx, nodeID, conn, &protocol.OperationReconcile{OperationID: id})
		if e != nil {
			return
		}
		reply, ok := payload.(*protocol.OperationReconcileResult)
		if !ok || reply.Record.ID != id {
			return
		}
		if e = s.saveRecord(ctx, nodeID, reply.Record); e != nil {
			return
		}
	}
	s.mu.Lock()
	if s.connections[nodeID] == conn {
		conn.ready = !unknown
		if unknown {
			conn.blocked = "Agent 存在控制端未知的未决操作，须先处理本机记录"
		}
	}
	s.mu.Unlock()
	s.updateWritable(ctx, nodeID, conn)
	s.notify("operations")
}
func (s *Server) saveRecord(ctx context.Context, nodeID string, record protocol.OperationRecord) error {
	row, e := readOperation(ctx, s.db, record.ID)
	if e != nil {
		return e
	}
	if row.NodeID != nodeID {
		return firewall.Conflict("操作不属于当前认证节点")
	}
	if operations.Terminal(row.Record.State) || record.Revision <= row.Record.Revision {
		return nil
	}
	if record.State == protocol.Accepted || record.Revision == 0 {
		return firewall.Conflict("Agent 操作结果无效")
	}
	switch record.State {
	case protocol.Prepared, protocol.Applying, protocol.AwaitingConfirmation, protocol.Reverting, protocol.AttentionRequired, protocol.Succeeded, protocol.Rejected, protocol.Failed, protocol.Reverted, protocol.ResolvedManually:
	default:
		return firewall.Conflict("Agent 返回了未知操作状态")
	}
	data, e := json.Marshal(record)
	if e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, "UPDATE operations SET record=?,updated_at=?,terminal=?,pending_sync=0 WHERE id=? AND json_extract(record,'$.revision')<? AND terminal=0", string(data), s.now().Unix(), operations.Terminal(record.State), record.ID, record.Revision)
	return e
}
func (s *Server) operationHTTP(row operationRow) httpapi.Operation {
	var result httpapi.Operation
	data, _ := json.Marshal(row.Record)
	json.Unmarshal(data, &result)
	result.Id = row.ID
	result.NodeId = row.NodeID
	result.Executor = "agent"
	result.Subject = httpapi.OperationSubject(row.Subject)
	result.CreatedAt = time.Unix(row.Created, 0).UTC()
	result.UpdatedAt = time.Unix(row.Updated, 0).UTC()
	result.ResultPendingSync = row.Pending
	if row.PlanID.Valid {
		result.PlanId = &row.PlanID.String
	}
	if row.Record.Error != nil {
		code := publicErrorCode(row.Record.Error.Code)
		result.Error = &httpapi.APIError{Code: httpapi.APIErrorCode(code), Message: row.Record.Error.Message, RequestId: identity.Random()}
	}
	return result
}
func publicErrorCode(code string) string {
	switch code {
	case "invalid_request", "unauthenticated", "forbidden", "csrf_rejected", "conflict", "node_busy", "node_unavailable", "dependency_unavailable", "not_found", "rate_limited", "internal_error":
		return code
	}
	return "conflict"
}
func (s *Server) operationError(w http.ResponseWriter, r *http.Request, e error) {
	if errors.Is(e, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "记录不存在")
		return
	}
	code := publicErrorCode(firewall.Code(e))
	status := 503
	switch code {
	case "invalid_request":
		status = 422
	case "node_busy", "conflict":
		status = 409
	case "not_found":
		status = 404
	}
	s.fail(w, r, status, code, e.Error())
}

func (s *Server) ReadFirewall(w http.ResponseWriter, r *http.Request, id string, params httpapi.ReadFirewallParams) {
	if _, e := s.findNode(r.Context(), id); e != nil {
		s.operationError(w, r, e)
		return
	}
	request := &protocol.FirewallRead{}
	if params.Family != nil {
		request.Family = protocol.AddressFamily(*params.Family)
	}
	if params.Scope != nil {
		request.Scope = protocol.ConfigScope(*params.Scope)
	}
	payload, e := s.rpc(r.Context(), id, s.currentConnection(id), request)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	reply, ok := payload.(*protocol.FirewallReadResult)
	if !ok {
		s.operationError(w, r, fmt.Errorf("节点没有返回防火墙读数"))
		return
	}
	var result httpapi.FirewallState
	data, e := json.Marshal(reply.State)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	if e = json.Unmarshal(data, &result); e != nil {
		s.operationError(w, r, e)
		return
	}
	result.Errors = []httpapi.ReadError{}
	for _, failure := range reply.State.Errors {
		result.Errors = append(result.Errors, httpapi.ReadError{Scope: failure.Scope, Code: failure.Error.Code, Message: failure.Error.Message})
	}
	write(w, 200, result)
}
func (s *Server) PreviewFirewallChange(w http.ResponseWriter, r *http.Request, id string, _ httpapi.PreviewFirewallChangeParams) {
	var change protocol.FirewallChange
	if !s.body(w, r, &change) {
		return
	}
	if _, e := protocol.Encode(protocol.Message{RequestID: "preview", Payload: &protocol.FirewallPreview{Change: change}}); e != nil {
		s.operationError(w, r, firewall.Unsupported(e.Error()))
		return
	}
	payload, e := s.rpc(r.Context(), id, s.currentConnection(id), &protocol.FirewallPreview{Change: change})
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	reply, ok := payload.(*protocol.FirewallPreviewResult)
	if !ok {
		s.operationError(w, r, fmt.Errorf("节点没有返回预览"))
		return
	}
	data, e := json.Marshal(reply.Plan)
	if e == nil {
		_, e = s.db.ExecContext(r.Context(), "INSERT INTO firewall_plans(id,node_id,body,created_at) VALUES(?,?,?,?)", reply.Plan.ID, id, string(data), s.now().Unix())
	}
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	var result httpapi.ChangePlan
	json.Unmarshal(data, &result)
	result.NodeId = id
	write(w, 201, result)
}
func (s *Server) GetFirewallPlan(w http.ResponseWriter, r *http.Request, id string) {
	var node, body string
	var operation sql.NullString
	e := s.db.QueryRowContext(r.Context(), "SELECT node_id,body,operation_id FROM firewall_plans WHERE id=?", id).Scan(&node, &body, &operation)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	var result httpapi.ChangePlan
	if e = json.Unmarshal([]byte(body), &result); e != nil {
		s.operationError(w, r, e)
		return
	}
	result.NodeId = node
	if operation.Valid {
		result.OperationId = &operation.String
	}
	write(w, 200, result)
}
func (s *Server) ApplyFirewallPlan(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ApplyFirewallPlanParams) {
	var node, body string
	var prior sql.NullString
	e := s.db.QueryRowContext(r.Context(), "SELECT node_id,body,operation_id FROM firewall_plans WHERE id=?", id).Scan(&node, &body, &prior)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	if prior.Valid {
		row, e := readOperation(r.Context(), s.db, prior.String)
		if e != nil {
			s.operationError(w, r, e)
			return
		}
		write(w, 202, s.operationHTTP(row))
		return
	}
	var plan protocol.ChangePlan
	if e = json.Unmarshal([]byte(body), &plan); e != nil {
		s.operationError(w, r, e)
		return
	}
	if !plan.Executable {
		s.operationError(w, r, firewall.Unsupported("此预览不可执行，请处理原因后重新预览"))
		return
	}
	// 受理前发现的原生冲突直接返回给编辑器；执行进程仍在副作用前复核。
	conn := s.currentConnection(node)
	s.mu.Lock()
	ready := conn != nil && conn.ready
	writable := ready && conn.writable
	s.mu.Unlock()
	if !ready {
		s.operationError(w, r, &firewall.Fault{Code: "node_unavailable", Message: "节点离线或未完成未决核对"})
		return
	}
	if !writable {
		s.operationError(w, r, &firewall.Fault{Code: "node_busy", Message: "节点存在未决操作"})
		return
	}
	payload, e := s.rpc(r.Context(), node, conn, &protocol.FirewallPreview{Change: plan.Change})
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	fresh, ok := payload.(*protocol.FirewallPreviewResult)
	if !ok {
		s.operationError(w, r, fmt.Errorf("节点没有返回当前预览"))
		return
	}
	before := func(p protocol.ChangePlan) firewall.View {
		return firewall.View{Rules: p.Baseline.Rules, Policies: p.Baseline.DefaultPolicies, Enabled: p.Baseline.ServiceActive}
	}
	if fresh.Plan.Backend != plan.Backend || !fresh.Plan.Executable || fresh.Plan.RequiresConfirmation != plan.RequiresConfirmation || !firewall.Equivalent(before(plan), before(fresh.Plan)) {
		s.operationError(w, r, firewall.Conflict("受影响的原生配置已改变，请保留草稿并重新预览"))
		return
	}
	row, e := s.accept(r.Context(), node, id, "", protocol.OperationAction{Kind: "firewall", Firewall: &plan}, current(r).session.Username)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	write(w, 202, s.operationHTTP(row))
}
func (s *Server) accept(ctx context.Context, nodeID, planID, key string, action protocol.OperationAction, actor string) (operationRow, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return operationRow{}, e
	}
	defer tx.Rollback()
	if planID != "" {
		var prior sql.NullString
		if e = tx.QueryRowContext(ctx, "SELECT operation_id FROM firewall_plans WHERE id=? AND node_id=?", planID, nodeID).Scan(&prior); e != nil {
			return operationRow{}, e
		}
		if prior.Valid {
			return readOperation(ctx, tx, prior.String)
		}
	}
	data, e := json.Marshal(action)
	if e != nil {
		return operationRow{}, e
	}
	if key != "" {
		var id string
		e = tx.QueryRowContext(ctx, "SELECT id FROM operations WHERE idempotency_key=?", key).Scan(&id)
		if e == nil {
			row, e := readOperation(ctx, tx, id)
			if e != nil {
				return row, e
			}
			if row.NodeID != nodeID || row.Action != string(data) {
				return row, firewall.Conflict("同一幂等键不能使用不同参数")
			}
			return row, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return operationRow{}, e
		}
	}
	s.mu.Lock()
	conn := s.connections[nodeID]
	ready := conn != nil && conn.ready
	writable := ready && conn.writable
	s.mu.Unlock()
	if !ready {
		return operationRow{}, &firewall.Fault{Code: "node_unavailable", Message: "节点离线或未完成未决核对"}
	}
	if !writable {
		return operationRow{}, &firewall.Fault{Code: "node_busy", Message: "节点存在未决操作，请先处理"}
	}
	var pending int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM operations WHERE node_id=? AND terminal=0", nodeID).Scan(&pending); e != nil {
		return operationRow{}, e
	}
	if pending > 0 {
		return operationRow{}, &firewall.Fault{Code: "node_busy", Message: "节点存在未决操作"}
	}
	id := identity.Random()
	now := s.now().Unix()
	record := protocol.OperationRecord{ID: id, State: protocol.Accepted, Revision: 0, Decision: "undecided", Steps: []protocol.OperationStep{}}
	raw, _ := json.Marshal(record)
	s.mu.Lock()
	s.dispatching[id] = true
	s.mu.Unlock()
	committed := false
	defer func() {
		if !committed {
			s.mu.Lock()
			delete(s.dispatching, id)
			s.mu.Unlock()
		}
	}()
	var plan, keyValue any
	if planID != "" {
		plan = planID
	}
	if key != "" {
		keyValue = key
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO operations(id,node_id,plan_id,idempotency_key,action,subject,actor,created_at,updated_at,record) VALUES(?,?,?,?,?,?,?,?,?,?)", id, nodeID, plan, keyValue, string(data), action.Kind, actor, now, now, string(raw)); e != nil {
		return operationRow{}, e
	}
	if planID != "" {
		if _, e = tx.ExecContext(ctx, "UPDATE firewall_plans SET operation_id=? WHERE id=? AND operation_id IS NULL", id, planID); e != nil {
			return operationRow{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return operationRow{}, e
	}
	committed = true
	s.mu.Lock()
	if s.connections[nodeID] == conn {
		conn.writable = false
		conn.blocked = "节点存在未决操作"
	}
	s.mu.Unlock()
	s.spawn(func(ctx context.Context) {
		defer func() { s.mu.Lock(); delete(s.dispatching, id); s.mu.Unlock(); s.notify("operations") }()
		payload, e := s.rpc(ctx, nodeID, conn, &protocol.OperationSubmit{OperationID: id, ContractVersion: protocol.Version, Action: action})
		if e == nil {
			if result, ok := payload.(*protocol.OperationUpdated); ok && result.Record.ID == id {
				s.lifecycle.Lock()
				s.saveRecord(ctx, nodeID, result.Record)
				s.lifecycle.Unlock()
			}
		}
		s.updateWritable(ctx, nodeID, conn)
		s.notify("operations")
	})
	s.notify("operations")
	return readOperation(ctx, s.db, id)
}

func (s *Server) refreshOperation(ctx context.Context, row *operationRow) error {
	if operations.Terminal(row.Record.State) {
		return nil
	}
	conn := s.currentConnection(row.NodeID)
	if conn == nil {
		row.Pending = true
		return nil
	}
	s.mu.Lock()
	dispatching := s.dispatching[row.ID]
	s.mu.Unlock()
	if dispatching {
		return nil
	}
	var payload protocol.Payload
	var e error
	if row.Pending {
		payload, e = s.rpc(ctx, row.NodeID, conn, &protocol.OperationReconcile{OperationID: row.ID})
	} else {
		payload, e = s.rpc(ctx, row.NodeID, conn, &protocol.OperationQuery{OperationID: row.ID})
	}
	if e != nil {
		row.Pending = true
		return nil
	}
	var record *protocol.OperationRecord
	switch result := payload.(type) {
	case *protocol.OperationQueryResult:
		if result.Found {
			record = result.Record
		}
	case *protocol.OperationReconcileResult:
		record = &result.Record
	}
	if record == nil || record.ID != row.ID {
		row.Pending = true
		return nil
	}
	s.lifecycle.Lock()
	e = s.saveRecord(ctx, row.NodeID, *record)
	s.lifecycle.Unlock()
	if e != nil {
		return e
	}
	updated, e := readOperation(ctx, s.db, row.ID)
	if e != nil {
		return e
	}
	*row = updated
	if record.Revision == row.Record.Revision {
		row.Record.ConfirmationRemainingSeconds = record.ConfirmationRemainingSeconds
	}
	s.updateWritable(ctx, row.NodeID, conn)
	return nil
}
func (s *Server) GetOperation(w http.ResponseWriter, r *http.Request, id string) {
	row, e := readOperation(r.Context(), s.db, id)
	if e == nil {
		e = s.refreshOperation(r.Context(), &row)
	}
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	write(w, 200, s.operationHTTP(row))
}
func (s *Server) ListPendingOperations(w http.ResponseWriter, r *http.Request, _ httpapi.ListPendingOperationsParams) {
	items, _, e := s.listOperationRows(r.Context(), r.URL.Query().Get("node_id"), "", "", 0, true, "")
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	result := []httpapi.Operation{}
	for _, row := range items {
		if e = s.refreshOperation(r.Context(), &row); e != nil {
			s.operationError(w, r, e)
			return
		}
		if !operations.Terminal(row.Record.State) {
			result = append(result, s.operationHTTP(row))
		}
	}
	write(w, 200, httpapi.PendingOperations{Items: result})
}
func (s *Server) ListOperations(w http.ResponseWriter, r *http.Request, _ httpapi.ListOperationsParams) {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > 200 {
			s.operationError(w, r, firewall.Unsupported("每页数量须为 1 至 200"))
			return
		}
		limit = n
	}
	items, next, e := s.listOperationRows(r.Context(), r.URL.Query().Get("node_id"), r.URL.Query().Get("state"), r.URL.Query().Get("cursor"), limit, false, r.URL.Query().Get("idempotency_key"))
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	result := httpapi.OperationList{Items: []httpapi.Operation{}}
	for _, row := range items {
		result.Items = append(result.Items, s.operationHTTP(row))
	}
	if next != "" {
		result.NextCursor = &next
	}
	write(w, 200, result)
}
func (s *Server) listOperationRows(ctx context.Context, node, state, cursor string, limit int, pending bool, key string) ([]operationRow, string, error) {
	conditions := []string{"1=1"}
	args := []any{}
	if key != "" {
		conditions = append(conditions, "idempotency_key=?")
		args = append(args, key)
	}
	if node != "" {
		conditions = append(conditions, "node_id=?")
		args = append(args, node)
	}
	if state != "" {
		conditions = append(conditions, "json_extract(record,'$.state')=?")
		args = append(args, state)
	}
	if pending {
		conditions = append(conditions, "terminal=0")
	}
	if cursor != "" {
		conditions = append(conditions, "(created_at,id)<(SELECT created_at,id FROM operations WHERE id=?)")
		args = append(args, cursor)
	}
	query := "SELECT id FROM operations WHERE " + strings.Join(conditions, " AND ") + " ORDER BY created_at DESC,id DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit+1)
	}
	rows, e := s.db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, "", e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, "", e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, "", e
	}
	next := ""
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
		next = ids[len(ids)-1]
	}
	items := []operationRow{}
	for _, id := range ids {
		o, e := readOperation(ctx, s.db, id)
		if e != nil {
			return nil, "", e
		}
		items = append(items, o)
	}
	return items, next, nil
}
func (s *Server) ConfirmOperation(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ConfirmOperationParams) {
	var body httpapi.OperationRevision
	if !s.body(w, r, &body) {
		return
	}
	s.requestOperation(w, r, id, "confirm", body.ExpectedRevision)
}
func (s *Server) RecoverOperation(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RecoverOperationParams) {
	var body httpapi.OperationRevision
	if !s.body(w, r, &body) {
		return
	}
	s.requestOperation(w, r, id, "recover", body.ExpectedRevision)
}
func (s *Server) ResolveOperation(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ResolveOperationParams) {
	var body httpapi.ResolveOperation
	if !s.body(w, r, &body) {
		return
	}
	if !body.AcceptCurrent {
		s.operationError(w, r, firewall.Unsupported("必须明确接受现状"))
		return
	}
	s.requestOperation(w, r, id, "resolve", body.ExpectedRevision)
}
func (s *Server) requestOperation(w http.ResponseWriter, r *http.Request, id, kind string, revision int64) {
	row, e := readOperation(r.Context(), s.db, id)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	if revision < 1 {
		s.operationError(w, r, firewall.Unsupported("缺少有效的阶段修订号"))
		return
	}
	var payload protocol.Payload
	switch kind {
	case "confirm":
		payload = &protocol.OperationConfirm{OperationID: id, ExpectedRevision: uint64(revision)}
	case "recover":
		payload = &protocol.OperationRecover{OperationID: id, ExpectedRevision: uint64(revision)}
	case "resolve":
		payload = &protocol.OperationResolve{OperationID: id, ExpectedRevision: uint64(revision), AcceptCurrent: true}
	}
	result, e := s.rpc(r.Context(), row.NodeID, s.currentConnection(row.NodeID), payload)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	reply, ok := result.(*protocol.OperationUpdated)
	if !ok || reply.Record.ID != id {
		s.operationError(w, r, fmt.Errorf("节点没有返回原操作"))
		return
	}
	s.lifecycle.Lock()
	e = s.saveRecord(r.Context(), row.NodeID, reply.Record)
	s.lifecycle.Unlock()
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	row, e = readOperation(r.Context(), s.db, id)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	s.notify("operations")
	write(w, 202, s.operationHTTP(row))
}
func (s *Server) InstallFirewall(w http.ResponseWriter, r *http.Request, id string, params httpapi.InstallFirewallParams) {
	var body httpapi.FirewallInstallRequest
	if !s.body(w, r, &body) {
		return
	}
	backend := protocol.Backend(body.Backend)
	if backend != "ufw" && backend != "firewalld" && backend != "nftables" {
		s.operationError(w, r, firewall.Unsupported("管理器不受支持"))
		return
	}
	if params.IdempotencyKey == "" || len(params.IdempotencyKey) > 128 {
		s.operationError(w, r, firewall.Unsupported("缺少有效幂等键"))
		return
	}
	action := protocol.OperationAction{Kind: "component", Component: &protocol.ComponentAction{Component: "firewall", Action: "install", Backend: backend}}
	row, e := s.accept(r.Context(), id, "", params.IdempotencyKey, action, current(r).session.Username)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	write(w, 202, s.operationHTTP(row))
}
