package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
	"github.com/jkfujr/opsd/internal/storagepool"
)

const poolPlanLifetime = 30 * time.Minute

type poolState struct {
	mu    sync.Mutex
	path  string
	store storagepool.Store
}

func (n *Node) poolStore(ctx context.Context) (storagepool.Store, error) {
	n.pools.mu.Lock()
	defer n.pools.mu.Unlock()
	if n.pools.store.DB == nil {
		file, err := os.OpenFile(n.pools.path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return storagepool.Store{}, err
		}
		if err = errors.Join(file.Chmod(0600), file.Close()); err != nil {
			return storagepool.Store{}, err
		}
		db, err := storage.Open(ctx, n.pools.path, storage.Pools)
		if err != nil {
			return storagepool.Store{}, err
		}
		n.pools.store.DB = db
	}
	return n.pools.store, nil
}

type poolPlanRecord struct {
	protocol.PoolPlan
	NormalizedName string  `json:"normalized_name"`
	Work           *Secret `json:"work,omitempty"`
}

type poolQuery struct {
	ID    string
	After string
	Limit int
}

type poolHostCommand struct {
	Request protocol.PoolHostRequest
	Sealed  *Secret
}

func (n *Node) poolDispatch(ctx context.Context, origin, method string, raw []byte) (any, error) {
	switch method {
	case "storage.pool.plan":
		var plan protocol.PoolPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return nil, err
		}
		return n.savePoolPlan(ctx, plan)
	case "storage.pool.list", "storage.pool.get":
		var q poolQuery
		if err := json.Unmarshal(raw, &q); err != nil {
			return nil, err
		}
		if _, err := os.Stat(n.pools.path); errors.Is(err, os.ErrNotExist) {
			if method == "storage.pool.get" {
				return nil, fault("not_found", "存储池不存在")
			}
			return []protocol.StoragePool{}, nil
		} else if err != nil {
			return nil, err
		}
		db, err := n.poolStore(ctx)
		if err != nil {
			return nil, err
		}
		if method == "storage.pool.get" {
			return n.localPool(ctx, db, q.ID)
		}
		if q.Limit < 1 || q.Limit > 101 {
			return nil, fault("invalid_request", "列表分页范围无效")
		}
		return db.List(ctx, q.After, q.Limit)
	case "storage.pool.inspect", "storage.pool.observe", "storage.pool.deploy":
		var wire poolHostCommand
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, err
		}
		r := wire.Request
		switch method {
		case "storage.pool.inspect":
			if r.Action != "inspect" || r.Member.NodeID != n.ID {
				return nil, fault("invalid_request", "成员预检的节点身份无效")
			}
		case "storage.pool.observe":
			if r.Action != "observe" {
				return nil, fault("invalid_request", "存储池读回动作无效")
			}
		case "storage.pool.deploy":
			if origin != operationOwner(r.PoolID) || !operations.ValidID(r.PoolID) ||
				!slices.Contains([]string{"prepare", "control", "serve", "rotate", "repair"}, r.Action) ||
				wire.Sealed == nil || r.Deploy != nil {
				return nil, fault("forbidden", "存储池部署需要所属节点签发")
			}
			body, err := unseal(*wire.Sealed, n.age)
			if err != nil {
				return nil, fault("forbidden", "存储池部署密文不属于本节点")
			}
			if err = json.Unmarshal(body, &r.Deploy); err != nil || r.Deploy == nil ||
				r.PoolID != r.Deploy.PoolID || operationOwner(r.Deploy.OperationID) != origin || r.Deploy.Member.NodeID != n.ID {
				return nil, fault("forbidden", "存储池部署与原操作不一致")
			}
		}
		data, err := n.executeRead(ctx, &r)
		if err != nil {
			return nil, err
		}
		out, ok := data.(*protocol.PoolHostResult)
		if !ok {
			return nil, fault("dependency_unavailable", "存储池执行器读回格式无效")
		}
		return out, nil
	}
	return nil, fault("invalid_request", "未知存储池请求")
}

func (n *Node) inspectPoolMembers(ctx context.Context, in protocol.PoolInput) (map[string]protocol.PoolHostResult, error) {
	inputs := make([]protocol.PoolHostRequest, len(in.Nodes))
	for i, m := range in.Nodes {
		if _, err := n.Member(ctx, m.NodeID); err != nil || !n.Active(ctx, m.NodeID) {
			return nil, fault("invalid_request", "成员 "+m.NodeID+" 不存在或已撤销")
		}
		inputs[i] = protocol.PoolHostRequest{Action: "inspect", Member: m, PortOffset: in.PortOffset}
	}
	results := n.poolHostRead(ctx, "storage.pool.inspect", inputs)
	probes := make(map[string]protocol.PoolHostResult, len(in.Nodes))
	for i, r := range results {
		node := in.Nodes[i].NodeID
		if r.err != nil {
			code := poolCode(r.err)
			if !slices.Contains([]string{"invalid_request", "conflict", "forbidden", "insufficient_storage"}, code) {
				code = "dependency_unavailable"
			}
			return nil, fault(code, "成员 "+node+" 预检失败："+r.err.Error())
		}
		probes[node] = r.out
	}
	return probes, nil
}

type poolHostReply struct {
	out protocol.PoolHostResult
	err error
}

func (n *Node) poolHostRead(ctx context.Context, method string, inputs []protocol.PoolHostRequest) []poolHostReply {
	out := make([]poolHostReply, len(inputs))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i, r := range inputs {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				out[i].err = ctx.Err()
				return
			}
			out[i].err = n.Call(ctx, r.Member.NodeID, method, poolHostCommand{Request: r}, &out[i].out)
		})
	}
	wg.Wait()
	return out
}

func (n *Node) PreviewPool(ctx context.Context, input protocol.PoolInput) (protocol.PoolPlan, error) {
	in, err := storagepool.Normalize(input)
	if err != nil {
		return protocol.PoolPlan{}, err
	}
	probes, err := n.inspectPoolMembers(ctx, in)
	if err != nil {
		return protocol.PoolPlan{}, err
	}
	members, err := storagepool.Select(in, probes)
	if err != nil {
		return protocol.PoolPlan{}, err
	}
	owner := ""
	for i, m := range members {
		in.Nodes[i] = m.PoolMemberInput
		if owner == "" && m.Control() {
			owner = m.NodeID
		}
	}
	var out protocol.PoolPlan
	err = n.Call(ctx, owner, "storage.pool.plan", protocol.PoolPlan{OwnerNodeID: owner, Normalized: in, Nodes: members}, &out)
	return out, err
}

func (n *Node) savePoolPlan(ctx context.Context, plan protocol.PoolPlan) (protocol.PoolPlan, error) {
	if plan.OwnerNodeID != n.ID || len(plan.Nodes) != len(plan.Normalized.Nodes) || len(plan.Nodes) < 3 {
		return protocol.PoolPlan{}, fault("invalid_request", "预览所属节点或成员无效")
	}
	if _, err := storagepool.Normalize(plan.Normalized); err != nil {
		return protocol.PoolPlan{}, err
	}
	db, err := n.poolStore(ctx)
	if err != nil {
		return protocol.PoolPlan{}, err
	}
	if err = db.CheckName(ctx, plan.Normalized.Name, n.ID); err != nil {
		return protocol.PoolPlan{}, err
	}
	now := n.Now().UTC()
	plan.PlanID, plan.PoolID, plan.ExpiresAt = NewOperationID(n.ID), NewOperationID(n.ID), now.Add(poolPlanLifetime)
	body, err := json.Marshal(poolPlanRecord{PoolPlan: plan, NormalizedName: strings.ToLower(plan.Normalized.Name)})
	if err == nil {
		_, err = n.DB.ExecContext(ctx, "INSERT INTO plans(id,body,created_at) VALUES(?,?,?)", plan.PlanID, string(body), now.Unix())
	}
	return plan, err
}

func (n *Node) readPoolPlan(ctx context.Context, id string) (poolPlanRecord, sql.NullString, error) {
	var plan poolPlanRecord
	var operation sql.NullString
	var body string
	err := n.DB.QueryRowContext(ctx, "SELECT body,operation_id FROM plans WHERE id=?", id).Scan(&body, &operation)
	if errors.Is(err, sql.ErrNoRows) {
		return plan, operation, fault("not_found", "存储池预览不存在")
	}
	if err == nil {
		err = json.Unmarshal([]byte(body), &plan)
	}
	if err == nil && (plan.PlanID != id || plan.OwnerNodeID != n.ID || operationOwner(plan.PoolID) != n.ID ||
		!operations.ValidID(plan.PoolID) || len(plan.Nodes) < 3) {
		err = fault("not_found", "存储池预览不存在")
	}
	return plan, operation, err
}

func (n *Node) enqueuePool(ctx context.Context, origin string, r Request) (Operation, error) {
	var in protocol.PoolCreate
	if json.Unmarshal(r.Payload, &in) != nil || r.Key == "" || operationOwner(in.PlanID) != n.ID || !operations.ValidID(in.PlanID) {
		return Operation{}, fault("invalid_request", "计划标识和请求键为必填项")
	}
	if err := storagepool.ValidateCredentials(in.Credentials); err != nil {
		return Operation{}, err
	}
	d, op, err := n.readPoolPlan(ctx, in.PlanID)
	if err == nil && op.Valid {
		err = fault("conflict", "存储池预览已使用")
	}
	if err == nil && !n.Now().Before(d.ExpiresAt) {
		err = fault("precondition_failed", "存储池预览已过期，请重新预览")
	}
	if err == nil {
		err = n.recheckPoolPlan(ctx, d.PoolPlan)
	}
	if err != nil {
		if prior, hit, lookup := n.retryOperation(ctx, origin, r); lookup != nil {
			return Operation{}, lookup
		} else if hit {
			return prior, nil
		}
		return Operation{}, err
	}
	r.ResourceID = d.PoolID
	return n.enqueueTx(ctx, origin, r, func(tx *sql.Tx, op Operation) error {
		var prior sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT operation_id FROM plans WHERE id=?", d.PlanID).Scan(&prior); err != nil {
			return err
		}
		if prior.Valid {
			if prior.String == op.ID {
				return nil
			}
			return fault("conflict", "存储池预览已使用")
		}
		if !n.Now().Before(d.ExpiresAt) {
			return fault("precondition_failed", "存储池预览已过期，请重新预览")
		}
		var nameExists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM plans WHERE operation_id IS NOT NULL
			AND json_extract(body,'$.pool_id') IS NOT NULL AND json_extract(body,'$.normalized_name')=?)`,
			d.NormalizedName).Scan(&nameExists); err != nil {
			return err
		}
		if nameExists {
			return fault("conflict", "已存在同名存储池")
		}
		_, err := tx.ExecContext(ctx, "UPDATE plans SET operation_id=? WHERE id=?", op.ID, d.PlanID)
		return err
	})
}

func (n *Node) recheckPoolPlan(ctx context.Context, plan protocol.PoolPlan) error {
	input, err := storagepool.Normalize(plan.Normalized)
	if err != nil {
		return err
	}
	probes, err := n.inspectPoolMembers(ctx, input)
	if err != nil {
		if slices.Contains([]string{"conflict", "invalid_request"}, poolCode(err)) {
			return fault("precondition_failed", err.Error())
		}
		return err
	}
	for _, before := range plan.Nodes {
		p := probes[before.NodeID]
		if before.ControlDisk.UUID != p.ControlDisk.UUID || before.DataDisk.UUID != p.DataDisk.UUID ||
			before.ControlDisk.MountPoint != p.ControlDisk.MountPoint || before.DataDisk.MountPoint != p.DataDisk.MountPoint ||
			!p.PortsFree || before.Control() && !p.ControlPortsFree {
			return fault("precondition_failed", "成员 "+before.NodeID+" 的挂载身份或端口已变化，请重新预览")
		}
	}
	current, err := storagepool.Select(input, probes)
	if err != nil {
		return err
	}
	for i, now := range current {
		before := plan.Nodes[i]
		if before.PoolMemberInput != now.PoolMemberInput || !slices.Equal(before.Roles, now.Roles) ||
			before.ControlBytes != now.ControlBytes || before.DataBytes != now.DataBytes {
			return fault("precondition_failed", "存储池选点或配额已变化，请重新预览")
		}
	}
	return nil
}

func poolCode(err error) string {
	if err == nil {
		return ""
	}
	var f *Fault
	if errors.As(err, &f) {
		return f.Code
	}
	var native *firewall.Fault
	if errors.As(err, &native) {
		return native.Code
	}
	return "dependency_unavailable"
}

func (n *Node) Pool(ctx context.Context, id string) (protocol.StoragePool, error) {
	var out protocol.StoragePool
	owner := operationOwner(id)
	if owner == "" || !operations.ValidID(id) {
		return out, fault("not_found", "存储池不存在")
	}
	if _, err := n.Member(ctx, owner); err != nil {
		return out, fault("not_found", "存储池所属节点不存在")
	}
	err := n.Call(ctx, owner, "storage.pool.get", poolQuery{ID: id}, &out)
	return out, err
}

func (n *Node) Pools(ctx context.Context, after string, limit int) (protocol.PoolList, error) {
	out := protocol.PoolList{Items: []protocol.StoragePool{}, Unavailable: []string{}}
	dir, err := n.Directory(ctx)
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	type reply struct {
		id    string
		items []protocol.StoragePool
		err   error
	}
	result := make(chan reply, len(dir.Members))
	var wg sync.WaitGroup
	for _, member := range dir.Members {
		if !n.Active(ctx, member.ID) {
			continue
		}
		wg.Go(func() {
			r := reply{id: member.ID}
			r.err = n.Call(ctx, member.ID, "storage.pool.list", poolQuery{After: after, Limit: limit + 1}, &r.items)
			result <- r
		})
	}
	wg.Wait()
	close(result)
	ok := 0
	for r := range result {
		if r.err != nil {
			out.Unavailable = append(out.Unavailable, r.id)
			continue
		}
		ok++
		out.Items = append(out.Items, r.items...)
	}
	if ok == 0 {
		return out, fault("dependency_unavailable", "没有可用的存储池所属节点")
	}
	slices.Sort(out.Unavailable)
	slices.SortFunc(out.Items, func(a, b protocol.StoragePool) int { return strings.Compare(a.ID, b.ID) })
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.NextCursor = out.Items[len(out.Items)-1].ID
	}
	return out, nil
}

func (n *Node) poolPlanForOperation(ctx context.Context, operation string) (poolPlanRecord, error) {
	var id string
	if err := n.DB.QueryRowContext(ctx, `SELECT id FROM plans WHERE operation_id=? AND json_extract(body,'$.pool_id') IS NOT NULL`,
		operation).Scan(&id); err != nil {
		return poolPlanRecord{}, err
	}
	p, _, err := n.readPoolPlan(ctx, id)
	return p, err
}

func (n *Node) localPool(ctx context.Context, db storagepool.Store, id string) (protocol.StoragePool, error) {
	p, err := db.Get(ctx, id)
	if err != nil {
		return p, err
	}
	plan, err := n.poolPlanForOperation(ctx, p.OperationID)
	if err != nil {
		return p, err
	}
	read, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	p.Nodes, _ = n.poolReadMembers(read, plan.PoolPlan)
	return p, nil
}

func (n *Node) poolReadMembers(ctx context.Context, plan protocol.PoolPlan) ([]protocol.PoolMemberStatus, []poolHostReply) {
	inputs := make([]protocol.PoolHostRequest, len(plan.Nodes))
	for i, m := range plan.Nodes {
		inputs[i] = protocol.PoolHostRequest{Action: "observe", PoolID: plan.PoolID, Member: m.PoolMemberInput}
	}
	results := n.poolHostRead(ctx, "storage.pool.observe", inputs)
	status := make([]protocol.PoolMemberStatus, len(plan.Nodes))
	for i, r := range results {
		if r.err != nil || r.out.Phase == "absent" {
			m := plan.Nodes[i]
			status[i] = protocol.PoolMemberStatus{PoolNodePlan: m, Endpoint: "https://" + storagepool.Address(m.PrivateIP, m.Ports.S3), State: "pending"}
			if r.err != nil {
				status[i].State, status[i].Error = "unavailable", r.err.Error()
			}
			continue
		}
		status[i] = r.out.Status
	}
	return status, results
}

func (n *Node) setPoolDatabaseDir(nodePath string) {
	n.pools.path = filepath.Join(filepath.Dir(nodePath), "storage-pools.db")
}

func (n *Node) hasPoolDatabase() bool {
	_, err := os.Stat(n.pools.path)
	return err == nil
}
