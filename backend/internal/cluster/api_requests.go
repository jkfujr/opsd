package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jkfujr/opsd/internal/requeststate"
	"time"
)

type nameChange struct {
	Name             string `json:"name"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

func missingRequest(err error) bool {
	var f *Fault
	return errors.Is(err, sql.ErrNoRows) || errors.As(err, &f) && f.Code == "not_found"
}

type RequestReceipt struct {
	Key         string     `json:"key"`
	Owner       string     `json:"owner"`
	Actor       string     `json:"actor"`
	Kind        string     `json:"kind"`
	ResourceID  string     `json:"resource_id"`
	State       string     `json:"state"`
	OperationID string     `json:"operation_id,omitempty"`
	Secret      bool       `json:"secret"`
	CreatedAt   time.Time  `json:"created_at"`
	Operation   *Operation `json:"operation,omitempty"`
}

func (n *Node) requestReceipt(ctx context.Context, request requeststate.Write) (RequestReceipt, error) {
	out := RequestReceipt{Key: request.Key, Owner: n.ID, Actor: request.Actor}
	var body string
	var digest []byte
	e := n.DB.QueryRowContext(ctx, "SELECT body,request_digest FROM queue WHERE origin=? AND request_key=?", request.Actor, request.Key).Scan(&body, &digest)
	if e == nil {
		if request.Digest != "" && fmt.Sprintf("%x", digest) != request.Digest {
			return out, fault("conflict", "同一请求键不能用于不同参数")
		}
		var op Operation
		if e = json.Unmarshal([]byte(body), &op); e != nil {
			return out, e
		}
		out.Kind, out.ResourceID, out.OperationID, out.State, out.CreatedAt, out.Operation = op.Subject, op.ResourceID, op.ID, "accepted", op.CreatedAt, &op
		return out, nil
	}
	if e != sql.ErrNoRows {
		return out, e
	}
	r, e := requeststate.Lookup(ctx, n.DB, request)
	if errors.Is(e, sql.ErrNoRows) {
		return out, fault("not_found", "尚未找到原请求")
	}
	if errors.Is(e, requeststate.ErrConflict) {
		return out, fault("conflict", e.Error())
	}
	if e != nil {
		return out, e
	}
	out.Kind, out.ResourceID, out.State, out.Secret, out.CreatedAt = r.Kind, r.ResourceID, "completed", r.Secret, r.CreatedAt
	if r.Kind == "operation" {
		out.OperationID = r.ResourceID
		if len(r.Result) == 0 || string(r.Result) == "{}" {
			out.State = "unknown"
		}
		if op, e := n.operation(ctx, r.ResourceID); e == nil {
			out.Operation = &op
		}
	}
	return out, nil
}

type operationDecisionRequest struct {
	Method   string             `json:"method"`
	Decision OperationDecision  `json:"decision"`
	Write    requeststate.Write `json:"write"`
}

func (n *Node) CallOperationDecision(ctx context.Context, id, method string, decision OperationDecision, write *requeststate.Write) (Operation, error) {
	var out Operation
	e := n.Call(ctx, OperationOwner(id), "api.operation.decision", operationDecisionRequest{Method: method, Decision: decision, Write: *write}, &out)
	return out, e
}
func (n *Node) apiOperationDecision(ctx context.Context, request operationDecisionRequest) (Operation, error) {
	n.apiDecisionMu.Lock()
	defer n.apiDecisionMu.Unlock()
	prior, e := requeststate.Lookup(ctx, n.DB, request.Write)
	if e == nil {
		var op Operation
		if json.Unmarshal(prior.Result, &op) == nil && op.ID != "" {
			return op, nil
		}
		return op, fault("outcome_unknown", "原决定结果未知，请查询原操作")
	}
	if errors.Is(e, requeststate.ErrConflict) {
		return Operation{}, fault("conflict", e.Error())
	}
	if e != sql.ErrNoRows {
		return Operation{}, e
	}
	op, e := n.operation(ctx, request.Decision.ID)
	if e != nil {
		return op, e
	}
	if op.Revision != request.Decision.ExpectedRevision {
		return op, fault("conflict", "操作已经改变，请重新读取")
	}
	if e = requeststate.Save(ctx, n.DB, requeststate.Receipt{Write: request.Write, Kind: "operation", ResourceID: op.ID, Result: json.RawMessage(`{}`), CreatedAt: n.Now().UTC()}); e != nil {
		return op, e
	}
	ctx = requeststate.With(ctx, &request.Write)
	if request.Method == "operation.cancel" {
		if op.Subject == "task_run" {
			op, e = n.cancelTask(ctx, op.ID)
		} else {
			op, e = n.cancelQueued(ctx, op.ID)
		}
	} else {
		op, e = n.decide(ctx, request.Method, request.Decision)
	}
	if e == nil {
		data, _ := json.Marshal(op)
		e = requeststate.Finish(ctx, n.DB, requeststate.Receipt{Write: request.Write, ResourceID: op.ID, Result: data})
	}
	return op, e
}
func (n *Node) RequestReceipt(ctx context.Context, target string, request requeststate.Write) (RequestReceipt, error) {
	var out RequestReceipt
	e := n.Call(ctx, target, "api.request", request, &out)
	return out, e
}
func (n *Node) RequireAPIAuthorization(ctx context.Context, target string) error {
	var result struct {
		AuthorizationVersion int `json:"authorization_version"`
	}
	e := n.Call(ctx, target, "api.capabilities", nil, &result)
	if e != nil {
		var f *Fault
		if errors.As(e, &f) && f.Code == "invalid_request" {
			return fault("conflict", "目标节点不支持当前授权契约，请更新节点")
		}
		return e
	}
	if result.AuthorizationVersion != APIAuthorizationVersion {
		return fault("conflict", "目标节点不支持当前授权契约，请更新节点")
	}
	return nil
}
