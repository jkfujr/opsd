package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

type sharedFlowPayload struct {
	Operation Operation `json:"operation"`
	Input     Secret    `json:"input"`
	Progress  *Secret   `json:"progress,omitempty"`
}

func (n *Node) sharedRecipients(ctx context.Context) ([]string, error) {
	key, err := n.sharedIdentity()
	if err != nil {
		return nil, err
	}
	return []string{key.Recipient().String()}, nil
}
func (n *Node) createSharedWorkflow(ctx context.Context, r Request) (Operation, error) {
	var input DatabaseSubmission
	if err := json.Unmarshal(r.Payload, &input); err != nil {
		return Operation{}, err
	}
	if err := validateDatabaseSubmission(input); err != nil {
		return Operation{}, err
	}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return Operation{}, err
	}
	actor := r.Actor
	if actor == "" {
		actor = n.ID
	}
	key := r.Key
	if key == "" {
		key = r.ID
	}
	if key == "" {
		return Operation{}, fault("invalid_request", "共享流程缺少请求标识")
	}
	digest := r.InputDigest
	if digest == "" {
		sum := sha256.Sum256(r.Payload)
		digest = hex.EncodeToString(sum[:])
	}
	prior, err := s.Receipt(ctx, sharedsql.Receipt{Actor: actor, Key: key, Digest: digest})
	if err == nil {
		var op Operation
		err = json.Unmarshal(prior.Result, &op)
		return op, err
	}
	if err != sql.ErrNoRows {
		return Operation{}, sharedFault(err)
	}
	coordinator := input.Cluster.Coordinator
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	err = n.Call(probe, coordinator, "ping", nil, nil)
	cancel()
	if err != nil {
		c, e := n.Config(ctx)
		if e != nil {
			return Operation{}, e
		}
		if !c.CoordinatorCandidate && !c.Console {
			return Operation{}, fault("node_unavailable", "原协调节点离线，请通过备用管理入口提交")
		}
		coordinator = n.ID
	}
	recipients, err := n.sharedRecipients(ctx)
	if err != nil {
		return Operation{}, err
	}
	id := stepID(n.ID, "shared-workflow/"+actor+"/"+key)
	flow, err := n.buildDatabaseWorkflow(ctx, id, input)
	if err != nil {
		return Operation{}, err
	}
	flow.SharedEpoch = 1
	if flow.Cluster != nil {
		flow.Cluster.Coordinator = coordinator
	}
	progress, _ := json.Marshal(flow)
	progressSecret, err := seal(progress, recipients)
	if err != nil {
		return Operation{}, err
	}
	op := Operation{OperationRecord: protocol.OperationRecord{ID: id, State: protocol.Accepted, Revision: 1, Decision: "undecided", Steps: []protocol.OperationStep{}}, NodeID: n.ID, Subject: "database", ResourceID: input.Cluster.ID, Actor: actor, Key: key, CreatedAt: n.Now().UTC(), UpdatedAt: n.Now().UTC(), Executor: "node", Coordinator: coordinator, CoordinatorEpoch: 1}
	if err = n.describeOperation(ctx, n.DB, r, "", &op); err != nil {
		return Operation{}, err
	}
	err = s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		if receipt, e := t.Receipt(ctx, sharedsql.Receipt{Actor: actor, Key: key, Digest: digest}); e == nil {
			return json.Unmarshal(receipt.Result, &op)
		} else if e != sql.ErrNoRows {
			return e
		}
		current, e := t.Object(ctx, "database", input.Cluster.ID)
		creating := input.Change.Deployment != nil && input.Cluster.Revision == 0
		if !creating {
			if e != nil {
				return e
			}
			if current.Revision != input.Change.ExpectedRevision {
				return sharedsql.ErrConflict
			}
			var c ManagedDatabaseCluster
			if e = json.Unmarshal(current.Body, &c); e != nil {
				return e
			}
			if c.Coordinator != coordinator {
				c.Coordinator = coordinator
				c.CoordinatorEpoch++
				c.Revision++
				c.Signature = ed25519.Sign(n.signing, databaseClusterBytes(c))
				object := sharedObject("database", c.ID, c.Revision, c)
				object.Identity = c.ClusterID
				if e = t.Put(ctx, object, current.Revision); e != nil {
					return e
				}
				input.Cluster = c
				input.Change.ExpectedRevision = c.Revision
			}
		}
		input.Cluster.Coordinator = coordinator
		body, _ := json.Marshal(input)
		secret, e := seal(body, recipients)
		if e != nil {
			return e
		}
		payload, _ := json.Marshal(sharedFlowPayload{Operation: op, Input: secret, Progress: &progressSecret})
		if e = t.CreateFlow(ctx, sharedsql.Flow{ID: id, ResourceID: op.ResourceID, Coordinator: coordinator, Epoch: 1, Revision: op.Revision, Body: payload}); e != nil {
			return e
		}
		result, _ := json.Marshal(op)
		return t.SaveReceipt(ctx, sharedsql.Receipt{Actor: actor, Key: key, Digest: digest, Kind: "operation", ResourceID: op.ID, Result: result})
	})
	if err == nil {
		n.notifyChange(Change{Resource: "operations"})
	}
	return op, sharedFault(err)
}

func (n *Node) restoreSharedWorkflow(ctx context.Context, f sharedsql.Flow) (Operation, error) {
	var p sharedFlowPayload
	if err := json.Unmarshal(f.Body, &p); err != nil {
		return Operation{}, err
	}
	op := p.Operation
	op.Coordinator = f.Coordinator
	op.CoordinatorEpoch = f.Epoch
	key, err := n.sharedIdentity()
	if err != nil {
		return op, err
	}
	input, err := unseal(p.Input, key)
	if err != nil {
		return op, err
	}
	secret, err := seal(input, []string{n.age.Recipient().String()})
	if err != nil {
		return op, err
	}
	encrypted, _ := json.Marshal(secret)
	var progressBody []byte
	if p.Progress != nil {
		plain, e := unseal(*p.Progress, key)
		if e != nil {
			return op, e
		}
		var flow databaseWorkflow
		if e = json.Unmarshal(plain, &flow); e != nil {
			return op, e
		}
		flow.SharedEpoch = f.Epoch
		if flow.Cluster != nil {
			current, e := n.DatabaseCluster(ctx, flow.Cluster.ID)
			if e == nil && current.Revision > flow.Cluster.Revision {
				flow.Cluster = &current
			} else if e != nil && e != sql.ErrNoRows {
				return op, e
			}
			flow.Cluster.Coordinator = n.ID
		}
		data, _ := json.Marshal(flow)
		local, e := seal(data, []string{n.age.Recipient().String()})
		if e != nil {
			return op, e
		}
		progressBody, _ = json.Marshal(struct {
			Secret Secret `json:"secret"`
		}{local})
	}
	body, _ := json.Marshal(op)
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return op, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,body,terminal,phase) VALUES(?,?,'database',?,'',?, ?,?,'submitted') ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,body=excluded.body,terminal=excluded.terminal,phase='submitted'", op.ID, op.Actor, string(encrypted), []byte{}, string(body), f.Terminal); err != nil {
		return op, err
	}
	if p.Progress != nil {
		if _, err = tx.ExecContext(ctx, "INSERT INTO database_workflows(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", op.ID, string(progressBody)); err != nil {
			return op, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, "DELETE FROM database_workflows WHERE id=?", op.ID); err != nil {
			return op, err
		}
	}
	if err = tx.Commit(); err != nil {
		return op, err
	}
	return op, nil
}
func (n *Node) saveSharedWorkflow(ctx context.Context, op Operation, flow databaseWorkflow, before uint64) error {
	s, err := n.sharedStore(ctx)
	if err != nil {
		return err
	}
	f, err := s.Flow(ctx, op.ID)
	if err != nil {
		return err
	}
	if f.Coordinator != n.ID || f.Epoch != op.CoordinatorEpoch || f.Revision != before {
		return sharedsql.ErrConflict
	}
	var p sharedFlowPayload
	if err = json.Unmarshal(f.Body, &p); err != nil {
		return err
	}
	recipients, err := n.sharedRecipients(ctx)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(flow)
	secret, err := seal(data, recipients)
	if err != nil {
		return err
	}
	p.Progress = &secret
	p.Operation = op
	f.Body, _ = json.Marshal(p)
	f.Revision = op.Revision
	f.Terminal = operations.Terminal(op.State)
	return s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		if op.State == protocol.Succeeded && op.Action == "configure_proxy_routes" {
			if err := n.sharedRoutingFinished(ctx, t, p); err != nil {
				return err
			}
		}
		if err := t.SaveFlow(ctx, f, before); err != nil {
			return err
		}
		if w := requeststate.Current(ctx); w != nil {
			body, _ := json.Marshal(op)
			return t.SaveReceipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest, Kind: "operation", ResourceID: op.ID, Result: body})
		}
		return nil
	})
}
func (n *Node) reconcileSharedFlows(ctx context.Context, s *sharedsql.Store) error {
	n.advanceMu.Lock()
	defer n.advanceMu.Unlock()
	c, err := n.Config(ctx)
	if err != nil {
		return err
	}
	flows, err := s.Flows(ctx)
	if err != nil {
		return err
	}
	for _, f := range flows {
		if f.Coordinator != n.ID && !c.CoordinatorCandidate && !c.Console {
			continue
		}
		if err = s.Claim(ctx, f.ID, n.ID); err != nil {
			if err == sharedsql.ErrConflict {
				continue
			}
			return err
		}
		f, err = s.Flow(ctx, f.ID)
		if err != nil {
			return err
		}
		if err = n.syncSharedFlowCoordinator(ctx, s, f); err != nil {
			return err
		}
		if _, err = n.restoreSharedWorkflow(ctx, f); err != nil {
			return err
		}
	}
	return nil
}
func (n *Node) sharedOperation(ctx context.Context, id string) (Operation, error) {
	s, err := n.sharedStore(ctx)
	if err != nil {
		return Operation{}, err
	}
	f, err := s.Flow(ctx, id)
	if err != nil {
		return Operation{}, err
	}
	var p sharedFlowPayload
	if err = json.Unmarshal(f.Body, &p); err != nil {
		return Operation{}, err
	}
	p.Operation.Coordinator = f.Coordinator
	p.Operation.CoordinatorEpoch = f.Epoch
	return p.Operation, nil
}
func (n *Node) checkSharedStep(ctx context.Context, origin string, r DatabaseRPC) error {
	if !n.sharedActive() || r.Action.Shared != nil {
		return nil
	}
	if origin == n.ID && r.Action.Deployment != nil {
		if state := n.sharedState(); state != nil && state.Deployment != nil && r.Action.Deployment.ID == state.Deployment.ID {
			return nil
		}
	}
	if r.FlowID == "" || r.CoordinatorEpoch == 0 {
		return fault("invalid_request", "数据库步骤缺少协调代际")
	}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return err
	}
	return sharedFault(s.CheckFlow(ctx, r.FlowID, origin, r.CoordinatorEpoch))
}

func (n *Node) TransferDatabaseCoordinator(ctx context.Context, id, target string, expected uint64) (ManagedDatabaseCluster, error) {
	s, err := n.sharedStore(ctx)
	if err != nil {
		return ManagedDatabaseCluster{}, err
	}
	if w := requeststate.Current(ctx); w != nil {
		prior, e := s.Receipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest})
		if e == nil {
			var result ManagedDatabaseCluster
			e = json.Unmarshal(prior.Result, &result)
			return result, e
		}
		if e != sql.ErrNoRows {
			return ManagedDatabaseCluster{}, sharedFault(e)
		}
	}
	if !n.Active(ctx, target) {
		return ManagedDatabaseCluster{}, fault("forbidden", "目标节点无效")
	}
	if err = n.Call(ctx, target, "ping", nil, nil); err != nil {
		return ManagedDatabaseCluster{}, err
	}
	var result ManagedDatabaseCluster
	err = s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		if w := requeststate.Current(ctx); w != nil {
			prior, e := t.Receipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest})
			if e == nil {
				return json.Unmarshal(prior.Result, &result)
			}
			if e != sql.ErrNoRows {
				return e
			}
		}
		candidate, e := t.Object(ctx, "candidate", target)
		if e != nil || string(candidate.Body) != "true" {
			return fault("invalid_request", "目标节点尚未登记为协调候选")
		}
		o, e := t.Object(ctx, "database", id)
		if e != nil {
			return e
		}
		if o.Revision != expected {
			return sharedsql.ErrConflict
		}
		if e = json.Unmarshal(o.Body, &result); e != nil {
			return e
		}
		result.Coordinator = target
		result.CoordinatorEpoch++
		result.Revision++
		result.Signature = ed25519.Sign(n.signing, databaseClusterBytes(result))
		updated := sharedObject("database", id, result.Revision, result)
		updated.Identity = result.ClusterID
		if e = t.Put(ctx, updated, o.Revision); e != nil {
			return e
		}
		_, e = t.ExecContext(ctx, "UPDATE shared_flows SET coordinator=?,epoch=epoch+1,lease_until=CURRENT_TIMESTAMP(6) WHERE resource_id=? AND terminal=0", target, id)
		if e == nil {
			if w := requeststate.Current(ctx); w != nil {
				body, _ := json.Marshal(result)
				e = t.SaveReceipt(ctx, sharedsql.Receipt{Actor: w.Actor, Key: w.Key, Digest: w.Digest, Kind: "database", ResourceID: id, Result: body})
			}
		}
		return e
	})
	return result, sharedFault(err)
}

func (n *Node) sharedWorkflowDecision(ctx context.Context, method string, d OperationDecision) (Operation, error) {
	n.advanceMu.Lock()
	defer n.advanceMu.Unlock()
	s, err := n.sharedStore(ctx)
	if err != nil {
		return Operation{}, err
	}
	f, err := s.Flow(ctx, d.ID)
	if err != nil {
		return Operation{}, err
	}
	if f.Coordinator != n.ID {
		return Operation{}, fault("conflict", "协调节点已变化，请查询原操作")
	}
	if f.Terminal {
		return n.sharedOperation(ctx, d.ID)
	}
	if err = s.CheckFlow(ctx, f.ID, n.ID, f.Epoch); err != nil {
		return Operation{}, err
	}
	if method == "operation.cancel" {
		var p sharedFlowPayload
		if err = json.Unmarshal(f.Body, &p); err != nil {
			return Operation{}, err
		}
		if f.Revision != d.ExpectedRevision || f.Revision != 1 || p.Operation.State != protocol.Accepted {
			return p.Operation, fault("conflict", "流程已经开始，请查询原操作")
		}
		op := p.Operation
		op.Coordinator, op.CoordinatorEpoch = f.Coordinator, f.Epoch
		op.State, op.UpdatedAt = protocol.Rejected, n.Now().UTC()
		op.Revision++
		err = n.saveSharedWorkflow(ctx, op, databaseWorkflow{}, f.Revision)
		return op, sharedFault(err)
	}
	_, err = n.restoreSharedWorkflow(ctx, f)
	if err != nil {
		return Operation{}, err
	}
	return n.decide(ctx, method, d)
}
