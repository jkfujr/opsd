package cluster

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
	"github.com/minio/minio-go/v7"
)

type poolWork struct {
	Authority     storagepool.Authority
	Materials     map[string]protocol.PoolMaterial
	Phase         string
	Since         time.Time
	Rotation      *poolRotation
	DegradedSince time.Time
	RepairAfter   time.Time
}

var poolSteps = []string{"prepare", "control", "quorum", "serve", "verify", "done"}

func (n *Node) poolCASeal(ctx context.Context, auth storagepool.Authority, member protocol.PoolNodePlan) (string, error) {
	if !member.Control() {
		return "", nil
	}
	m, err := n.Member(ctx, member.NodeID)
	if err != nil {
		return "", err
	}
	out, err := seal(mustJSON(auth), []string{m.Recipient})
	if err != nil {
		return "", err
	}
	return string(mustJSON(out)), nil
}

func (n *Node) newPoolWork(ctx context.Context, plan protocol.PoolPlan) (poolWork, error) {
	work := poolWork{Phase: "prepare", Since: n.Now().UTC(), Materials: make(map[string]protocol.PoolMaterial, len(plan.Nodes))}
	var err error
	if work.Authority, err = storagepool.NewAuthority(plan.PoolID, n.Now()); err != nil {
		return work, err
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return work, err
	}
	for _, member := range plan.Nodes {
		key, err := n.poolCASeal(ctx, work.Authority, member)
		if err != nil {
			return work, err
		}
		m, err := work.Authority.Issue(member.NodeID, member.PrivateIP, hex.EncodeToString(secret[:]), key, n.Now())
		if err != nil {
			return work, err
		}
		work.Materials[member.NodeID] = m
	}
	return work, nil
}

func (n *Node) savePoolWork(ctx context.Context, plan poolPlanRecord, work poolWork) error {
	value, err := seal(mustJSON(work), []string{n.age.Recipient().String()})
	if err != nil {
		return err
	}
	plan.Work = &value
	_, err = n.DB.ExecContext(ctx, "UPDATE plans SET body=? WHERE id=?", string(mustJSON(plan)), plan.PlanID)
	return err
}

func (n *Node) loadPoolWork(plan poolPlanRecord) (poolWork, error) {
	var work poolWork
	if plan.Work == nil {
		return work, fault("conflict", "存储池缺少工作流记录")
	}
	raw, err := unseal(*plan.Work, n.age)
	if err == nil {
		err = json.Unmarshal(raw, &work)
	}
	return work, err
}

func (n *Node) poolApplyHost(ctx context.Context, action, id, operation string, member protocol.PoolNodePlan, d protocol.PoolDeploy) (protocol.PoolHostResult, error) {
	var out protocol.PoolHostResult
	m, err := n.Member(ctx, member.NodeID)
	if err != nil {
		return out, err
	}
	d.PoolID, d.Member, d.OperationID = id, member, operation
	secret, err := seal(mustJSON(d), []string{m.Recipient})
	if err != nil {
		return out, err
	}
	err = n.Call(ctx, member.NodeID, "storage.pool.deploy", poolHostCommand{
		Request: protocol.PoolHostRequest{Action: action, PoolID: id},
		Sealed:  &secret,
	}, &out)
	return out, err
}

func (n *Node) progressPoolMembers(ctx context.Context, plan protocol.PoolPlan, op Operation, work poolWork, cred protocol.PoolCredentials) error {
	_, observed := n.poolReadMembers(ctx, plan)
	wanted := map[string]string{"prepare": "prepared", "control": "control", "serve": "serving"}[work.Phase]
	rank := []string{"absent", "allocating", "prepared", "control", "serving"}
	got := slices.Index(rank, wanted)
	errs := make([]error, len(plan.Nodes))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 6)
	for i, member := range plan.Nodes {
		seen := observed[i]
		if seen.err != nil {
			errs[i] = seen.err
			continue
		}
		if slices.Index(rank, seen.out.Phase) >= got {
			continue
		}
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			// pause/recover 均先查原操作和目标记录，续跑已确认的阶段。
			d := protocol.PoolDeploy{}
			if work.Phase == "prepare" {
				d.Members, d.Material, d.Credentials = plan.Nodes, work.Materials[member.NodeID], cred
			}
			out, err := n.poolApplyHost(ctx, work.Phase, plan.PoolID, op.ID, member, d)
			if err == nil && slices.Index(rank, out.Phase) < got {
				err = fault("dependency_unavailable", "目标存储池阶段尚未完成")
			}
			errs[i] = err
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return fault(poolCode(err), fmt.Sprintf("成员 %s 阶段 %s：%v", plan.Nodes[i].NodeID, work.Phase, err))
		}
	}
	return nil
}

func poolQuorum(members []protocol.PoolMemberStatus) bool {
	leader := make(map[string]int)
	etcd := 0
	for _, m := range members {
		if !m.Control() {
			continue
		}
		if m.MasterLeader != "" {
			leader[m.MasterLeader]++
		}
		if m.EtcdRead {
			etcd++
		}
	}
	for _, count := range leader {
		if count >= 2 && etcd >= 2 {
			return true
		}
	}
	return false
}

func (n *Node) advancePool(ctx context.Context, op *Operation, plain []byte) {
	if op.State == protocol.AttentionRequired {
		return
	}
	db, err := n.poolStore(ctx)
	if err != nil {
		n.poolAttention(ctx, op, storagepool.Store{}, "", err)
		return
	}
	var input protocol.PoolCreate
	if err = json.Unmarshal(plain, &input); err != nil {
		n.poolAttention(ctx, op, db, "", err)
		return
	}
	plan, locked, err := n.readPoolPlan(ctx, input.PlanID)
	if err != nil || !locked.Valid || locked.String != op.ID || plan.PoolID != op.ResourceID {
		n.poolAttention(ctx, op, db, "", fault("conflict", "建池操作与原计划锁不一致"))
		return
	}
	work, err := n.loadPoolWork(plan)
	if plan.Work == nil {
		work, err = n.newPoolWork(ctx, plan.PoolPlan)
		if err == nil {
			err = n.savePoolWork(ctx, plan, work)
		}
	}
	if err != nil {
		n.poolAttention(ctx, op, db, "", err)
		return
	}
	if err = db.Begin(ctx, plan.PoolPlan, op.ID, work.Authority.Cert, n.Now()); err != nil {
		n.poolAttention(ctx, op, db, work.Authority.Cert, err)
		return
	}
	phase := work.Phase
	i := slices.Index(poolSteps, phase)
	if i < 0 {
		n.poolAttention(ctx, op, db, work.Authority.Cert, fault("conflict", "存储池工作流阶段无效"))
		return
	}
	if phase == "done" {
		op.State, op.Error, op.Revision = protocol.Succeeded, nil, op.Revision+1
		op.Steps = poolOperationSteps(i)
		_ = n.saveOperation(ctx, op, "finished")
		return
	}
	state := "deploying"
	if phase == "prepare" {
		state = "creating"
	}
	if op.State == protocol.Accepted || len(op.Steps) == 0 || op.Steps[i].State != "applying" {
		op.State, op.Error, op.Revision = protocol.Applying, nil, op.Revision+1
		op.Steps = poolOperationSteps(i)
		if err = n.saveOperation(ctx, op, "submitted"); err != nil {
			return
		}
		if err = db.Update(ctx, plan.PoolID, state, work.Authority.Cert, nil, n.Now()); err != nil {
			return
		}
	}
	var members []protocol.PoolMemberStatus
	switch phase {
	case "prepare", "control", "serve":
		err = n.progressPoolMembers(ctx, plan.PoolPlan, *op, work, input.Credentials)
	case "quorum":
		members, _ = n.poolReadMembers(ctx, plan.PoolPlan)
		if !poolQuorum(members) {
			err = fault("dependency_unavailable", "Master 或 etcd 尚未形成多数派")
		}
	case "verify":
		members, _ = n.poolReadMembers(ctx, plan.PoolPlan)
		if !poolQuorum(members) || slices.ContainsFunc(members, func(m protocol.PoolMemberStatus) bool { return m.State != "ready" }) {
			err = fault("dependency_unavailable", "等待多数派与所有 S3 入口首验")
		} else {
			err = verifyPoolEntries(ctx, plan.PoolPlan, *op, work.Authority.Cert, input.Credentials, members)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			if n.ctx.Err() == nil && n.Now().Sub(work.Since) >= 3*time.Minute {
				record, cancel := context.WithTimeout(n.ctx, 5*time.Second)
				defer cancel()
				n.poolAttention(record, op, db, work.Authority.Cert, fault("dependency_unavailable", "建池阶段超时，请读回原操作"))
			}
			return
		}
		if slices.Contains([]string{"dependency_unavailable", "node_unavailable", "node_busy"}, poolCode(err)) && n.Now().Sub(work.Since) < 3*time.Minute {
			return
		}
		n.poolAttention(ctx, op, db, work.Authority.Cert, err)
		return
	}
	work.Phase, work.Since = poolSteps[i+1], n.Now().UTC()
	if work.Phase == "done" {
		if err = db.Update(ctx, plan.PoolID, "ready", work.Authority.Cert, members, n.Now()); err != nil {
			return
		}
	}
	if err = n.savePoolWork(ctx, plan, work); err != nil {
		return
	}
	n.notifyChange(Change{Resource: "storage", NodeID: n.ID})
}

func poolOperationSteps(i int) []protocol.OperationStep {
	names := []string{"预分配镜像", "启动控制组件", "读回多数派", "启动数据和 S3 入口", "跨入口读写"}
	steps := make([]protocol.OperationStep, len(names))
	for j, name := range names {
		state := "pending"
		if j < i {
			state = "completed"
		} else if j == i {
			state = "applying"
		}
		steps[j] = protocol.OperationStep{Name: name, State: state}
	}
	return steps
}

func (n *Node) poolAttention(ctx context.Context, op *Operation, db storagepool.Store, ca string, err error) {
	if db.DB != nil && ca != "" {
		_ = db.Update(ctx, op.ResourceID, "attention_required", ca, nil, n.Now())
	}
	op.State, op.Error, op.Revision = protocol.AttentionRequired, &protocol.Error{Code: poolCode(err), Message: err.Error()}, op.Revision+1
	_ = n.saveOperation(ctx, op, "submitted")
	n.notifyChange(Change{Resource: "storage", NodeID: n.ID})
}

func (n *Node) decidePool(ctx context.Context, method string, d OperationDecision, op Operation) (Operation, error) {
	if op.State != protocol.AttentionRequired || d.ExpectedRevision != op.Revision {
		return op, fault("conflict", "存储池阶段已经变化")
	}
	switch method {
	case "operation.recover":
		plan, err := n.poolPlanForOperation(ctx, op.ID)
		if err != nil {
			return op, err
		}
		if plan.Work != nil {
			work, err := n.loadPoolWork(plan)
			if err != nil {
				return op, err
			}
			work.Since = n.Now().UTC()
			if err = n.savePoolWork(ctx, plan, work); err != nil {
				return op, err
			}
		}
		op.State, op.Error, op.Revision = protocol.Applying, nil, op.Revision+1
		op.Steps = nil
		err = n.saveOperation(ctx, &op, "submitted")
		return op, err
	case "operation.resolve":
		if d.AcceptCurrent {
			op.State, op.Error, op.Revision = protocol.ResolvedManually, nil, op.Revision+1
			return op, n.saveOperation(ctx, &op, "finished")
		}
	}
	return op, fault("invalid_request", "存储池只允许核对恢复或接受当前状态")
}

func poolProbeRead(ctx context.Context, c *minio.Client, bucket, key string, data []byte) (bool, error) {
	got, err := c.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return false, err
	}
	defer got.Close()
	body, err := io.ReadAll(io.LimitReader(got, int64(len(data)+1)))
	if code := minio.ToErrorResponse(err).Code; code == "NoSuchKey" || code == "NoSuchBucket" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !bytes.Equal(body, data) {
		return false, fault("conflict", "S3 首验对象摘要不同")
	}
	return true, nil
}

func verifyPoolEntries(ctx context.Context, plan protocol.PoolPlan, op Operation, ca string, key protocol.PoolCredentials, status []protocol.PoolMemberStatus) error {
	clients := make([]*minio.Client, len(status))
	sum := sha256.Sum256([]byte(plan.PoolID))
	bucket := fmt.Sprintf("opsd-pool-check-%x", sum[:8])
	for i, member := range status {
		var err error
		if clients[i], err = storagepool.S3(member.Endpoint, ca, key); err != nil {
			return err
		}
	}
	exists, err := clients[0].BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		err = clients[0].MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"})
		if err != nil && minio.ToErrorResponse(err).Code != "BucketAlreadyOwnedByYou" {
			return err
		}
	}
	for i, c := range clients {
		key := status[i].NodeID
		body := []byte(op.ID + "/" + key)
		exists, err := poolProbeRead(ctx, c, bucket, key, body)
		if err != nil {
			return err
		}
		if !exists {
			if _, err = c.PutObject(ctx, bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
				return err
			}
		}
		ok, err := poolProbeRead(ctx, clients[(i+1)%len(clients)], bucket, key, body)
		if err != nil {
			return err
		}
		if !ok {
			return fault("dependency_unavailable", "另一个 S3 入口尚未读到首验对象")
		}
	}
	for _, m := range status {
		if err := clients[0].RemoveObject(ctx, bucket, m.NodeID, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return clients[0].RemoveBucket(ctx, bucket)
}
