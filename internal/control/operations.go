package control

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"net/http"
	"strconv"
)

func (s *Server) operationError(w http.ResponseWriter, r *http.Request, e error) {
	code, message, status := "internal_error", "操作未完成，请查询原操作", 500
	var f *cluster.Fault
	var native *firewall.Fault
	if errors.As(e, &f) {
		code, message = f.Code, f.Message
	} else if errors.As(e, &native) {
		code, message = native.Code, native.Message
	} else if errors.Is(e, sql.ErrNoRows) {
		code, message = "not_found", "记录不存在"
	}
	switch code {
	case "invalid_request":
		status = 422
	case "conflict", "node_busy":
		status = 409
	case "forbidden":
		status = 403
	case "not_found":
		status = 404
	case "node_unavailable", "dependency_unavailable":
		status = 503
	}
	s.fail(w, r, status, code, message)
}
func (s *Server) ReadFirewall(w http.ResponseWriter, r *http.Request, id string) {
	var state protocol.ManagedFirewallState
	e := s.node.Call(r.Context(), id, "firewall.config.read", nil, &state)
	var result httpapi.ManagedFirewallState
	if e == nil {
		b, _ := json.Marshal(state)
		e = json.Unmarshal(b, &result)
	}
	s.resultError(w, r, 200, result, e)
}
func (s *Server) ChangeFirewallConfig(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangeFirewallConfigParams) {
	var body protocol.FirewallConfigPatch
	if !s.body(w, r, &body) {
		return
	}
	s.writeFirewallConfig(w, r, id, cluster.FirewallWrite{ExpectedRevision: body.ExpectedRevision, Change: &body.Change, Key: r.Header.Get("Idempotency-Key")})
}
func (s *Server) SyncFirewallConfig(w http.ResponseWriter, r *http.Request, id string, _ httpapi.SyncFirewallConfigParams) {
	var body struct {
		ExpectedRevision uint64 `json:"expected_revision"`
	}
	if !s.body(w, r, &body) {
		return
	}
	s.writeFirewallConfig(w, r, id, cluster.FirewallWrite{ExpectedRevision: body.ExpectedRevision, Key: r.Header.Get("Idempotency-Key")})
}
func (s *Server) writeFirewallConfig(w http.ResponseWriter, r *http.Request, id string, request cluster.FirewallWrite) {
	var saved cluster.FirewallSaved
	e := s.node.Call(r.Context(), id, "firewall.config.write", request, &saved)
	var result httpapi.FirewallSaved
	if e == nil {
		b, _ := json.Marshal(saved)
		e = json.Unmarshal(b, &result)
	}
	s.resultError(w, r, 202, result, e)
}
func (s *Server) GetOperation(w http.ResponseWriter, r *http.Request, id string) {
	op, e := s.node.GetOperation(r.Context(), id)
	s.resultError(w, r, 200, op, e)
}
func (s *Server) ListPendingOperations(w http.ResponseWriter, r *http.Request, _ httpapi.ListPendingOperationsParams) {
	s.operationList(w, r, true)
}
func (s *Server) ListOperations(w http.ResponseWriter, r *http.Request, _ httpapi.ListOperationsParams) {
	s.operationList(w, r, false)
}
func (s *Server) operationList(w http.ResponseWriter, r *http.Request, pending bool) {
	q := cluster.ListQuery{Key: r.URL.Query().Get("idempotency_key"), Pending: pending, State: r.URL.Query().Get("state"), Limit: 50}
	if v := r.URL.Query().Get("limit"); v != "" {
		q.Limit, _ = strconv.Atoi(v)
	}
	target := r.URL.Query().Get("node_id")
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" {
		if target != "" {
			q.Cursor, _ = strconv.ParseUint(cursor, 10, 64)
		} else {
			b, e := base64.RawURLEncoding.DecodeString(cursor)
			if e != nil || len(b) > 16000 || json.Unmarshal(b, &q.Positions) != nil {
				s.fail(w, r, 422, "invalid_request", "分页位置无效")
				return
			}
		}
	}
	list, e := s.node.ListOperations(r.Context(), target, q)
	out := map[string]any{"items": list.Items, "unavailable": list.Unavailable}
	if len(list.NextPositions) > 0 {
		b, _ := json.Marshal(list.NextPositions)
		out["next_cursor"] = base64.RawURLEncoding.EncodeToString(b)
	}
	if list.Next > 0 {
		out["next_cursor"] = fmt.Sprint(list.Next)
	}
	s.resultError(w, r, 200, out, e)
}
func (s *Server) ConfirmOperation(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ConfirmOperationParams) {
	s.operationDecision(w, r, id, "operation.confirm")
}
func (s *Server) RecoverOperation(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RecoverOperationParams) {
	s.operationDecision(w, r, id, "operation.recover")
}
func (s *Server) ResolveOperation(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ResolveOperationParams) {
	s.operationDecision(w, r, id, "operation.resolve")
}
func (s *Server) operationDecision(w http.ResponseWriter, r *http.Request, id, method string) {
	var body struct {
		ExpectedRevision uint64 `json:"expected_revision"`
		AcceptCurrent    bool   `json:"accept_current"`
	}
	if !s.body(w, r, &body) {
		return
	}
	op, e := s.node.Decide(r.Context(), method, cluster.OperationDecision{ID: id, ExpectedRevision: body.ExpectedRevision, AcceptCurrent: body.AcceptCurrent})
	s.resultError(w, r, 200, op, e)
}
func (s *Server) CancelOperation(w http.ResponseWriter, r *http.Request, id string, _ httpapi.CancelOperationParams) {
	var op cluster.Operation
	e := s.node.Call(r.Context(), cluster.OperationOwner(id), "operation.cancel", id, &op)
	s.resultError(w, r, 200, op, e)
}
func (s *Server) InstallFirewall(w http.ResponseWriter, r *http.Request, id string, _ httpapi.InstallFirewallParams) {
	var body struct {
		Backend protocol.Backend `json:"backend"`
	}
	if !s.body(w, r, &body) {
		return
	}
	s.submit(w, r, id, "component", protocol.ComponentAction{Component: "firewall", Action: "install", Backend: body.Backend}, r.Header.Get("Idempotency-Key"), "")
}
