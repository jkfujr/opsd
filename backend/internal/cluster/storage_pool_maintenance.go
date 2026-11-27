package cluster

import (
	"context"
	"slices"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
)

type poolObservation struct {
	state string
	count int
}

func poolObservedState(members []protocol.PoolMemberStatus) string {
	if !poolQuorum(members) {
		return "unavailable"
	}
	serving, all := 0, true
	writable := make(map[string]bool)
	for _, m := range members {
		if m.S3Ready {
			serving++
		}
		if m.Writable && m.S3Ready {
			writable[m.FailureDomain] = true
		}
		all = all && m.State == "ready"
	}
	switch {
	case serving == 0:
		return "unavailable"
	case len(writable) < 2:
		return "read_only"
	case !all:
		return "degraded"
	default:
		return "ready"
	}
}

func (n *Node) poolLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	observed := map[string]poolObservation{}
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			if n.hasPoolDatabase() {
				n.maintainPools(observed)
			}
		}
	}
}

func (n *Node) maintainPools(observed map[string]poolObservation) {
	ctx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
	defer cancel()
	db, err := n.poolStore(ctx)
	if err != nil {
		return
	}
	for after := ""; ctx.Err() == nil; {
		pools, err := db.List(ctx, after, 100)
		if err != nil || len(pools) == 0 {
			return
		}
		for _, p := range pools {
			if !slices.Contains([]string{"ready", "degraded", "read_only", "unavailable"}, p.State) {
				continue
			}
			plan, err := n.poolPlanForOperation(ctx, p.OperationID)
			if err != nil {
				continue
			}
			work, err := n.loadPoolWork(plan)
			if err != nil || work.Phase != "done" {
				continue
			}
			read, stop := context.WithTimeout(ctx, 5*time.Second)
			values, replies := n.poolReadMembers(read, plan.PoolPlan)
			stop()
			state := poolObservedState(values)
			o := observed[p.ID]
			if state == p.State {
				delete(observed, p.ID)
			} else if state == o.state {
				o.count++
				observed[p.ID] = o
			} else {
				o = poolObservation{state: state, count: 1}
				observed[p.ID] = o
			}
			required := 3
			if state == "ready" || p.State == "unavailable" && state != "unavailable" {
				required = 2
			}
			if o.count < required {
				state = p.State
			}
			changed := state != p.State || p.CAPEM != work.publicCA()
			if err = db.Update(ctx, p.ID, state, work.publicCA(), values, n.Now()); err != nil {
				continue
			}
			if changed {
				n.notifyChange(Change{Resource: "storage", NodeID: n.ID})
			}
			n.maintainPoolEC(ctx, db, p, plan, values, replies)
			if poolObservedState(values) == "ready" {
				if !work.DegradedSince.IsZero() {
					work.DegradedSince, work.RepairAfter = time.Time{}, time.Time{}
					_ = n.savePoolWork(ctx, plan, work)
				}
				n.advancePoolRotation(ctx, plan, p.OperationID, work, values, replies)
			} else if state == "degraded" && work.Rotation == nil {
				n.maintainPoolReplicas(ctx, plan, p.OperationID, work, values)
			}
		}
		after = pools[len(pools)-1].ID
	}
}

func (n *Node) maintainPoolReplicas(ctx context.Context, plan poolPlanRecord, operation string, work poolWork, values []protocol.PoolMemberStatus) {
	if poolObservedState(values) != "degraded" || !slices.ContainsFunc(values, func(m protocol.PoolMemberStatus) bool { return !m.S3Ready }) {
		return
	}
	now := n.Now().UTC()
	if work.DegradedSince.IsZero() {
		work.DegradedSince, work.RepairAfter = now, now.Add(10*time.Minute)
		_ = n.savePoolWork(ctx, plan, work)
		return
	}
	if now.Before(work.RepairAfter) {
		return
	}
	for _, member := range values {
		if !member.S3Ready || !member.Writable {
			continue
		}
		// 持久记录本次派发。中断后等待观察窗口，再向原池发起下一次修复。
		work.RepairAfter = now.Add(15 * time.Minute)
		if n.savePoolWork(ctx, plan, work) != nil {
			return
		}
		_, err := n.poolApplyHost(ctx, "repair", plan.PoolID, operation, member.PoolNodePlan, protocol.PoolDeploy{})
		if n.Log != nil {
			n.Log("存储池 %s 副本修复返回：%v", plan.PoolID, err)
		}
		return
	}
}

type poolRotation struct {
	Stage     string
	Cursor    int
	Next      *storagepool.Authority
	Materials map[string]protocol.PoolMaterial
	RetryAt   time.Time
}

func (w poolWork) publicCA() string {
	if w.Rotation != nil && w.Rotation.Next != nil {
		return w.Authority.Cert + w.Rotation.Next.Cert
	}
	return w.Authority.Cert
}

func (n *Node) beginPoolRotation(ctx context.Context, plan poolPlanRecord, work *poolWork, status []protocol.PoolMemberStatus) (bool, error) {
	next := poolRotation{Stage: "leaf", Materials: map[string]protocol.PoolMaterial{}}
	now := n.Now()
	auth := work.Authority
	expires, err := storagepool.CertificateExpiry(auth.Cert)
	if err != nil {
		return false, err
	}
	rotateCA := expires.Sub(now) <= storagepool.RenewBefore
	if rotateCA {
		auth, err = storagepool.NewAuthority(plan.PoolID, now)
		if err != nil {
			return false, err
		}
		next.Stage, next.Next = "trust", &auth
	}
	for _, member := range status {
		if !rotateCA && (member.CertExpiry.IsZero() || member.CertExpiry.Sub(now) > storagepool.RenewBefore) {
			continue
		}
		old := work.Materials[member.NodeID]
		key := old.CASeal
		if rotateCA {
			if key, err = n.poolCASeal(ctx, auth, member.PoolNodePlan); err != nil {
				return false, err
			}
		}
		m, err := auth.Issue(member.NodeID, member.PrivateIP, old.FilerKey, key, now)
		if err != nil {
			return false, err
		}
		next.Materials[member.NodeID] = m
	}
	if len(next.Materials) == 0 {
		return false, nil
	}
	work.Rotation = &next
	return true, n.savePoolWork(ctx, plan, *work)
}

func (n *Node) advancePoolRotation(ctx context.Context, plan poolPlanRecord, operation string, work poolWork,
	status []protocol.PoolMemberStatus, observed []poolHostReply) {
	if work.Rotation == nil {
		if _, err := n.beginPoolRotation(ctx, plan, &work, status); err != nil && n.Log != nil {
			n.Log("存储池 %s 无法开始 TLS 轮换：%v", plan.PoolID, err)
		}
		return
	}
	r := work.Rotation
	// 一轮只切换一个节点。下一轮先读回多数派与所有入口，再轮到下一台。
	for r.Cursor < len(plan.Nodes) {
		member := plan.Nodes[r.Cursor]
		want, ok := r.Materials[member.NodeID]
		if !ok {
			r.Cursor++
			continue
		}
		if r.Stage == "trust" {
			want = work.Materials[member.NodeID]
		}
		if r.Next != nil && r.Stage != "retire" {
			want.CA = work.publicCA()
		}
		generation := storagepool.TLSGeneration(want)
		if observed[r.Cursor].out.TLSGeneration != generation {
			if n.Now().Before(r.RetryAt) {
				return
			}
			out, err := n.poolApplyHost(ctx, "rotate", plan.PoolID, operation, member, protocol.PoolDeploy{Material: want})
			if err != nil || out.TLSGeneration != generation {
				r.RetryAt = n.Now().Add(15 * time.Minute)
				keep, cancel := context.WithTimeout(n.ctx, 5*time.Second)
				defer cancel()
				_ = n.savePoolWork(keep, plan, work)
				if n.Log != nil {
					n.Log("存储池 %s 成员 %s TLS 轮换未完成，期望 %s，读回 %s：%v", plan.PoolID, member.NodeID, generation, out.TLSGeneration, err)
				}
				return
			}
		}
		r.Cursor++
		r.RetryAt = time.Time{}
		_ = n.savePoolWork(ctx, plan, work)
		return
	}
	switch {
	case r.Stage == "trust":
		r.Stage, r.Cursor = "leaf", 0
	case r.Stage == "leaf" && r.Next != nil:
		r.Stage, r.Cursor = "retire", 0
	default:
		for id, m := range r.Materials {
			work.Materials[id] = m
		}
		if r.Next != nil {
			work.Authority = *r.Next
		}
		work.Rotation = nil
	}
	_ = n.savePoolWork(ctx, plan, work)
}
