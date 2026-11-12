package control

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
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
	case "unauthenticated":
		status = 401
	case "invalid_request", "logs_unsupported":
		status = 422
	case "precondition_failed":
		status = 412
	case "secret_unavailable":
		status = 409
	case "outcome_unknown":
		status = 504
	case "conflict", "node_busy", "restart_required":
		status = 409
	case "forbidden":
		status = 403
	case "not_found":
		status = 404
	case "node_unavailable", "dependency_unavailable", "logs_read_failed":
		status = 503
	}
	if code == "secret_unavailable" {
		owner := r.PathValue("node_id")
		if owner == "" {
			owner = s.node.ID
		}
		receipt, err := s.node.RequestReceipt(r.Context(), owner, requeststate.Write{Actor: actor(r), Key: r.Header.Get("Idempotency-Key")})
		if err == nil {
			problem := newProblem(r, status, code, message)
			if url := receiptResourceURL(receipt); url != "" {
				problem.ResourceUrl = &url
			}
			writeProblem(w, problem)
			return
		}
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
	if e == nil {
		s.setETag(w, r, uint64(result.Revision))
	}
	s.resultError(w, r, 200, convertHTTP[httpapi.FirewallConfiguration](result), e)
}
func (s *Server) ChangeFirewallConfig(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangeFirewallConfigParams) {
	var body protocol.FirewallConfigPatch
	if !s.body(w, r, &body) {
		return
	}
	s.writeFirewallConfig(w, r, id, cluster.FirewallWrite{ExpectedRevision: body.ExpectedRevision, Change: &body.Change, Key: r.Header.Get("Idempotency-Key")})
}

func (s *Server) writeFirewallConfig(w http.ResponseWriter, r *http.Request, id string, request cluster.FirewallWrite) {
	request.Actor = actor(r)
	request.InputDigest = requestFingerprint(r)
	var saved cluster.FirewallSaved
	e := s.node.Call(r.Context(), id, "firewall.config.write", request, &saved)
	var op cluster.Operation
	if e == nil {
		op, e = s.node.GetOperation(r.Context(), saved.OperationID)
		w.Header().Set("Location", httpapi.APIBasePath+"/operations/"+saved.OperationID)
	}
	s.resultError(w, r, 202, convertHTTP[httpapi.Operation](op), e)
}
func (s *Server) GetOperation(w http.ResponseWriter, r *http.Request, id string) {
	op, e := s.node.GetOperation(r.Context(), id)
	s.resultError(w, r, 200, op, e)
}

func (s *Server) ListOperations(w http.ResponseWriter, r *http.Request, _ httpapi.ListOperationsParams) {
	s.operationList(w, r, r.URL.Query().Get("pending") == "true")
}
func (s *Server) operationList(w http.ResponseWriter, r *http.Request, pending bool) {
	q := cluster.ListQuery{ResourceID: r.URL.Query().Get("resource_id"), Key: r.URL.Query().Get("idempotency_key"), Pending: pending, State: r.URL.Query().Get("state"), Limit: 50}
	q.Actor = actor(r)
	if a := access(r.Context()); a != nil {
		q.Authorization = &a.Token
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		q.Limit, _ = strconv.Atoi(v)
	}
	target := r.URL.Query().Get("node_id")
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" {
		position, err := decodeCursor(r)
		if err != nil {
			s.fail(w, r, 400, "malformed_request", err.Error())
			return
		}
		cursor = position.Position
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || len(b) > 16000 || json.Unmarshal(b, &q.Positions) != nil {
			s.fail(w, r, 400, "malformed_request", "分页位置无效")
			return
		}
		for _, position := range q.Positions {
			if position.ID == "" || position.CreatedAt.IsZero() {
				s.fail(w, r, 400, "malformed_request", "分页位置无效")
				return
			}
		}
		if position, ok := q.Positions[target]; target != "" && ok {
			q.Before = &position
		}
	}
	list, e := s.node.ListOperations(r.Context(), target, q)
	out := map[string]any{"items": list.Items, "unavailable": list.Unavailable}
	if len(list.NextPositions) > 0 {
		b, _ := json.Marshal(list.NextPositions)
		out["next_cursor"] = encodeCursor(r, collectionCursor{Position: base64.RawURLEncoding.EncodeToString(b)})
	}
	if list.Next != nil {
		b, _ := json.Marshal(map[string]cluster.OperationPosition{target: *list.Next})
		out["next_cursor"] = encodeCursor(r, collectionCursor{Position: base64.RawURLEncoding.EncodeToString(b)})
	}
	s.resultError(w, r, 200, out, e)
}
