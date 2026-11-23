package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
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

func (n *Node) requestReceipt(ctx context.Context, origin string, request requeststate.Write) (RequestReceipt, error) {
	// 可信来源由目标端的成员通道给出，不能取自请求体（Write.Origin 不随 JSON 传输）。
	request.Origin = origin
	out := RequestReceipt{Key: request.Key, Owner: n.ID, Actor: request.Actor}
	var body string
	var digest []byte
	// 归属规则与受理一致：已认证来源 + 声明操作者 + 请求键。
	e := n.DB.QueryRowContext(ctx, "SELECT body,request_digest FROM queue WHERE origin=? AND actor=? AND request_key=?", origin, request.Actor, request.Key).Scan(&body, &digest)
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
	// 队列里没有记录时按回执表查，同样只用**已认证来源**：
	// 迁移前的旧记录没有可信来源，不再按声明操作者猜测归属。
	r, e := requeststate.Lookup(ctx, n.DB, request)
	if errors.Is(e, requeststate.ErrConflict) {
		return out, fault("conflict", e.Error())
	}
	if e != nil {
		return out, e
	}
	out.Kind, out.ResourceID, out.State, out.Secret, out.CreatedAt = r.Kind, r.ResourceID, "completed", r.Secret, r.CreatedAt
	if r.Kind == "storage_check" {
		var check struct {
			State string `json:"state"`
		}
		if json.Unmarshal(r.Result, &check) == nil && check.State == "checking" {
			out.State = "unknown"
		}
	}
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
	target := OperationOwner(id)
	if n.sharedActive() {
		if op, err := n.GetOperation(ctx, id); err == nil && op.CoordinatorEpoch > 0 {
			target = op.Coordinator
		}
	}
	e := n.Call(ctx, target, "api.operation.decision", operationDecisionRequest{Method: method, Decision: decision, Write: *write}, &out)
	return out, e
}
func (n *Node) apiOperationDecision(ctx context.Context, request operationDecisionRequest) (Operation, error) {
	n.apiDecisionMu.Lock()
	defer n.apiDecisionMu.Unlock()
	local, localErr := n.operation(ctx, request.Decision.ID)
	if n.sharedActive() && (localErr != nil || local.CoordinatorEpoch > 0) {
		if _, err := n.sharedOperation(ctx, request.Decision.ID); err == nil {
			s, err := n.sharedStore(ctx)
			if err != nil {
				return Operation{}, err
			}
			prior, err := s.Receipt(ctx, sharedsql.Receipt{Actor: request.Write.Actor, Key: request.Write.Key, Digest: request.Write.Digest})
			if err == nil {
				var op Operation
				err = json.Unmarshal(prior.Result, &op)
				return op, err
			}
			if err != sql.ErrNoRows {
				return Operation{}, sharedFault(err)
			}
			return n.sharedWorkflowDecision(requeststate.With(ctx, &request.Write), request.Method, request.Decision)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Operation{}, sharedFault(err)
		}
	}
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
	// 归属以**已认证来源**为准：本地入口用本节点，远端入口由调度层填入。
	if request.Origin == "" {
		request.Origin = n.ID
	}
	if n.sharedActive() {
		if store, err := n.sharedStore(ctx); err == nil {
			prior, err := store.Receipt(ctx, sharedsql.Receipt{Actor: request.Actor, Key: request.Key, Digest: request.Digest})
			if err == nil {
				out := RequestReceipt{Key: request.Key, Owner: n.ID, Actor: request.Actor, State: "completed", CreatedAt: prior.CreatedAt, Kind: prior.Kind, ResourceID: prior.ResourceID, Secret: prior.Kind == "enrollment"}
				var op Operation
				if err = json.Unmarshal(prior.Result, &op); err != nil {
					return out, err
				}
				if op.Subject != "" {
					if current, e := n.sharedOperation(ctx, op.ID); e == nil {
						op = current
					}
					out.OperationID, out.Operation, out.Kind, out.State = op.ID, &op, "operation", "accepted"
				}
				return out, nil
			}
			if errors.Is(err, sharedsql.ErrConflict) {
				return RequestReceipt{}, sharedFault(err)
			}
		}
	}
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
