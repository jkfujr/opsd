package cluster

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
)

func poolSupportsEC(nodes []protocol.PoolNodePlan) bool {
	domains := map[string]bool{}
	for _, node := range nodes {
		if node.ECImage == "" {
			return false
		}
		domains[node.FailureDomain] = true
	}
	return len(domains) >= 4
}

func poolVolumeAddress(node protocol.PoolNodePlan) string {
	return fmt.Sprintf("%s.%d", storagepool.Address(node.PrivateIP, node.Ports.Volume), node.Ports.VolumeGRPC)
}

func (n *Node) checkPoolECSubmission(ctx context.Context, r *Request) error {
	var settings protocol.PoolECSettings
	if err := json.Unmarshal(r.Payload, &settings); err != nil || operationOwner(settings.ID) != n.ID ||
		!operations.ValidID(settings.ID) || settings.Enabled && settings.RestoreReplicas || len(settings.ParityPreferred) > 64 {
		return fault("invalid_request", "EC 设置无效")
	}
	if r.ExpectedRevision == nil || settings.Revision != *r.ExpectedRevision || settings.Revision == 0 {
		return fault("precondition_failed", "EC 设置缺少池版本")
	}
	db, err := n.poolStore(ctx)
	if err != nil {
		return err
	}
	p, err := db.Get(ctx, settings.ID)
	if err != nil {
		return err
	}
	if p.Revision != settings.Revision {
		return fault("precondition_failed", "池版本已变化")
	}
	plan, err := n.poolPlanForOperation(ctx, p.OperationID)
	if err != nil {
		return err
	}
	if !poolSupportsEC(plan.Nodes) {
		return fault("invalid_request", "EC 需要至少四个故障域和全部成员的固定镜像")
	}
	preferences := map[string]bool{}
	for _, id := range settings.ParityPreferred {
		if preferences[id] || !slices.ContainsFunc(plan.Nodes, func(m protocol.PoolNodePlan) bool { return m.NodeID == id }) {
			return fault("invalid_request", "校验片优先节点不属于此池或重复")
		}
		preferences[id] = true
	}
	if active, err := db.ActiveEC(ctx, p.ID); err != nil {
		return err
	} else if active.Operation != "" {
		return fault("conflict", "该池仍有在途转换或待清理数据")
	}
	if settings.Enabled || settings.RestoreReplicas {
		members, replies := n.poolReadMembers(ctx, plan.PoolPlan)
		for _, reply := range replies {
			if reply.out.DataMigration != "" {
				return fault("conflict", "池数据卷已有迁移 "+reply.out.DataMigration)
			}
		}
		if !poolQuorum(members) {
			return fault("dependency_unavailable", "存储池多数派不可用")
		}
		candidates := poolECCandidates(members, replies, nil)
		if settings.Enabled {
			domains := map[string]bool{}
			for _, m := range candidates {
				if m.Available > 0 {
					domains[m.FailureDomain] = true
				}
			}
			if len(domains) < 4 {
				return fault("insufficient_storage", "当前在线故障域可用容量不足")
			}
		}
		if settings.RestoreReplicas {
			current, err := db.EncodedEC(ctx, p.ID)
			if err != nil {
				return err
			}
			if len(current) > 0 {
				if len(candidates) == 0 {
					return fault("dependency_unavailable", "无法读取池普通卷槽位")
				}
				scan, err := n.poolECScan(ctx, plan, candidates[0].NodeID)
				if err != nil {
					return err
				}
				for _, w := range current {
					if _, err = poolRestoreTargets(plan.Nodes, w, candidates, scan); err != nil {
						return err
					}
				}
			}
		}
	}
	r.ResourceID = settings.ID
	return nil
}

func (n *Node) applyPoolECPolicy(ctx context.Context, body []byte, op *Operation) error {
	var input protocol.PoolECSettings
	if err := json.Unmarshal(body, &input); err != nil {
		return err
	}
	db, err := n.poolStore(ctx)
	if err != nil {
		return err
	}
	if err = db.SetECPolicy(ctx, input, op.ID); err != nil {
		return err
	}
	op.State, op.Error, op.Revision = protocol.Succeeded, nil, op.Revision+1
	if input.RestoreReplicas {
		pending, message, err := db.ECRestoreResult(ctx, input.ID)
		if err != nil || pending {
			return err
		}
		if message != "" {
			op.State, op.Error = protocol.Failed, &protocol.Error{Code: "insufficient_storage", Message: message}
		}
	}
	n.notifyChange(Change{Resource: "storage", NodeID: n.ID})
	return n.saveOperation(ctx, op, "finished")
}

func (n *Node) callPoolEC(ctx context.Context, plan poolPlanRecord, worker string, job protocol.PoolECJob) (protocol.PoolECJobResult, error) {
	var empty protocol.PoolECJobResult
	index := slices.IndexFunc(plan.Nodes, func(p protocol.PoolNodePlan) bool { return p.NodeID == worker })
	if index < 0 {
		return empty, fault("conflict", "原 EC 执行节点不属于池")
	}
	_, operation, err := n.readPoolPlan(ctx, plan.PlanID)
	if err != nil || !operation.Valid {
		return empty, fault("conflict", "EC 缺少原建池操作")
	}
	out, err := n.poolApplyHost(ctx, "ec", plan.PoolID, operation.String, plan.Nodes[index], protocol.PoolDeploy{EC: &job})
	if err != nil {
		return empty, err
	}
	if out.ECJob == nil {
		return empty, fault("dependency_unavailable", "EC 执行器应答缺少原任务状态")
	}
	return *out.ECJob, nil
}

func poolECCandidates(status []protocol.PoolMemberStatus, replies []poolHostReply, parity []string) []storagepool.ECMember {
	var out []storagepool.ECMember
	for i, m := range status {
		if m.S3Ready && replies[i].err == nil {
			out = append(out, storagepool.ECMember{NodeID: m.NodeID, FailureDomain: m.FailureDomain,
				Available: replies[i].out.DataFreeBytes - max(m.DataBytes/10, 128*protocol.PoolMiB),
				Parity:    slices.Contains(parity, m.NodeID)})
		}
	}
	return out
}

func (n *Node) poolECScan(ctx context.Context, plan poolPlanRecord, worker string) (protocol.PoolECScan, error) {
	read, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	result, err := n.callPoolEC(read, plan, worker, protocol.PoolECJob{Input: protocol.PoolECWorkerInput{Action: "scan"}})
	var scan protocol.PoolECScan
	if err != nil || json.Unmarshal(result.Data, &scan) != nil || scan.Limit <= 0 {
		return scan, fault("dependency_unavailable", "无法读取池卷及容量")
	}
	return scan, nil
}

func (n *Node) maintainPoolEC(ctx context.Context, db storagepool.Store, pool protocol.StoragePool, plan poolPlanRecord,
	status []protocol.PoolMemberStatus, replies []poolHostReply) {
	if !poolSupportsEC(plan.Nodes) || db.ReadECPolicy(ctx, &pool) != nil {
		return
	}
	active, err := db.ActiveEC(ctx, pool.ID)
	if err != nil {
		return
	}
	if active.Operation != "" {
		n.advancePoolEC(ctx, db, plan, active)
		return
	}
	if !poolQuorum(status) {
		return
	}
	for _, reply := range replies {
		if reply.out.DataMigration != "" {
			return
		}
	}
	candidates := poolECCandidates(status, replies, pool.ParityNodes)
	if len(candidates) == 0 {
		return
	}
	current, err := db.EncodedEC(ctx, pool.ID)
	if err != nil {
		return
	}
	if len(current) == 0 && pool.ECRestore {
		_ = db.ClearECRestore(ctx, pool.ID)
		return
	}
	if !pool.ECEnabled && len(current) == 0 {
		return
	}
	scan, err := n.poolECScan(ctx, plan, candidates[0].NodeID)
	if err != nil {
		return
	}
	existing := make(map[uint32][]protocol.PoolECVolume)
	regular := make(map[uint32][]protocol.PoolECVolume)
	for _, v := range scan.Volumes {
		if v.ShardBits == 0 {
			regular[v.Volume] = append(regular[v.Volume], v)
			continue
		}
		existing[v.Volume] = append(existing[v.Volume], v)
	}
	free := make(map[string]int64)
	for _, m := range plan.Nodes {
		if i := slices.IndexFunc(candidates, func(c storagepool.ECMember) bool { return c.NodeID == m.NodeID }); i >= 0 {
			free[poolVolumeAddress(m)] = candidates[i].Available
		}
	}
	var restore storagepool.ECWork
	for _, row := range current {
		observed := make(map[string]uint32)
		for _, v := range existing[row.Volume] {
			if v.Generation == row.Generation && v.Collection == row.Plan.Input.Collection {
				observed[v.Address] |= v.ShardBits
			}
		}
		count, repairable := 0, true
		for _, member := range row.Plan.Input.Members {
			missing := 0
			for _, id := range member.Shards {
				if observed[member.Address]&(1<<id) == 0 {
					missing++
				} else {
					count++
				}
			}
			repairable = repairable && (missing == 0 || free[member.Address] >= int64(missing)*row.Plan.ShardBytes)
		}
		state := "ready"
		switch {
		case count < storagepool.ECDataShards:
			state = "unavailable"
		case count < storagepool.ECShardCount:
			state = "degraded"
		}
		_, _ = db.DB.ExecContext(ctx, `UPDATE storage_pool_volumes SET state=?,checked_at=? WHERE pool_id=?
			AND volume_id=? AND layout='ec'`, state, n.Now().Unix(), pool.ID, row.Volume)
		i := slices.IndexFunc(candidates, func(c storagepool.ECMember) bool { return c.NodeID == row.Plan.Worker })
		start := state == "degraded" && repairable && row.Plan.ShardBytes > 0 && i >= 0 &&
			candidates[i].Available >= storagepool.ECShardCount*row.Plan.ShardBytes+32*protocol.PoolMiB
		if start || row.Plan.Repairing {
			n.maintainPoolECRepair(ctx, db, plan, row, start)
		}
		if restore.Operation == "" && state == "ready" && !row.Plan.Repairing {
			restore = row
		}
	}
	if pool.ECRestore {
		if restore.Operation != "" {
			n.planPoolRestore(ctx, db, plan, restore, candidates, scan)
		}
		return
	}
	if !pool.ECEnabled {
		return
	}
	var ids []uint32
	for id := range regular {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		volume := regular[id]
		if _, ok := existing[id]; ok || len(volume) != 2 {
			continue
		}
		if ec, ok := n.newPoolEncode(pool.ID, plan, scan.Limit, volume, candidates); ok {
			_ = db.BeginEC(ctx, ec, n.Now())
			return
		}
	}
}

func (n *Node) newPoolEncode(id string, plan poolPlanRecord, limit int64, copies []protocol.PoolECVolume,
	candidates []storagepool.ECMember) (storagepool.ECWork, bool) {
	var out storagepool.ECWork
	a, b := copies[0], copies[1]
	oldest := max(a.LastWrite, b.LastWrite)
	if a.Volume == 0 || a.Collection != b.Collection || a.Bytes != b.Bytes || a.ShardBytes != b.ShardBytes ||
		!storagepool.ECEligible(n.Now(), time.Unix(oldest, 0), a.Bytes, limit) || oldest <= 0 || a.ShardBytes <= 0 {
		return out, false
	}
	selected := make(map[string]protocol.PoolNodePlan)
	for _, m := range plan.Nodes {
		selected[poolVolumeAddress(m)] = m
	}
	left, right := selected[a.Address], selected[b.Address]
	if left.NodeID == "" || right.NodeID == "" || left.FailureDomain == right.FailureDomain {
		return out, false
	}
	required := a.Bytes + int64(storagepool.ECShardCount)*a.ShardBytes + max(32*protocol.PoolMiB, a.Bytes/10)
	for _, from := range []protocol.PoolNodePlan{left, right} {
		targets := slices.Clone(candidates)
		index := slices.IndexFunc(targets, func(x storagepool.ECMember) bool { return x.NodeID == from.NodeID })
		if index < 0 || targets[index].Available < required {
			continue
		}
		targets[index].Available -= required
		placements, err := storagepool.PlaceEC(targets, a.ShardBytes)
		if err != nil {
			continue
		}
		now := n.Now()
		generation := now.UnixNano()
		r := protocol.PoolECWorkerInput{Volume: a.Volume, Collection: a.Collection, Generation: generation,
			SourceBytes: a.Bytes, QuietBefore: now.Add(-time.Hour).Unix(), Sources: []string{a.Address, b.Address}}
		for _, m := range plan.Nodes {
			part := protocol.PoolECMember{Address: poolVolumeAddress(m), Domain: m.FailureDomain}
			for _, placement := range placements {
				if placement.NodeID == m.NodeID {
					part.Shards = placement.ShardIDs
				}
			}
			r.Members = append(r.Members, part)
		}
		return storagepool.ECWork{PoolID: id, Volume: a.Volume, Generation: generation, Operation: NewOperationID(n.ID),
			Direction: "encode", Plan: storagepool.ECPlan{Worker: from.NodeID, Input: r, Bytes: a.Bytes, ShardBytes: a.ShardBytes, Limit: limit,
				LastWrite: oldest, Attempt: 1}}, true
	}
	return out, false
}

func (n *Node) maintainPoolECRepair(ctx context.Context, db storagepool.Store, plan poolPlanRecord, w storagepool.ECWork, start bool) {
	now := n.Now()
	if now.Unix() < w.Plan.RetryAt {
		return
	}
	job := protocol.PoolECJob{ID: w.Operation, Attempt: w.Plan.Attempt, Input: w.Plan.Input}
	job.Input.Action = "repair"
	result, err := n.callPoolEC(ctx, plan, w.Plan.Worker, job)
	if err == nil && result.State == "absent" && start {
		job.Start = true
		result, err = n.callPoolEC(ctx, plan, w.Plan.Worker, job)
	}
	if err != nil {
		return
	}
	switch result.State {
	case "running":
		if w.Plan.Repairing {
			return
		}
		w.Plan.Repairing = true
	case "done", "failed", "unknown":
		w.Plan.Repairing = false
		w.Plan.Attempt++
		w.Plan.RetryAt = now.Add(time.Minute).Unix()
		w.Error = result.Message
	default:
		return
	}
	_ = db.SaveEC(ctx, w, now)
	n.notifyChange(Change{Resource: "storage", NodeID: n.ID})
}

func poolRestoreTargets(members []protocol.PoolNodePlan, w storagepool.ECWork,
	candidates []storagepool.ECMember, scan protocol.PoolECScan) ([]string, error) {
	const reserve = 32 * protocol.PoolMiB
	if scan.ReplicaSlots == nil {
		return nil, fault("dependency_unavailable", "无法读取池普通卷槽位")
	}
	worker := slices.IndexFunc(candidates, func(m storagepool.ECMember) bool { return m.NodeID == w.Plan.Worker })
	if worker < 0 {
		return nil, fault("dependency_unavailable", "原 EC 执行节点不在线")
	}
	pinned := w.Plan.Input.Restore
	scratch := int64(0)
	if len(pinned) == 0 {
		scratch = 2 * w.Plan.Bytes
		if candidates[worker].Available < scratch+reserve {
			return nil, fault("insufficient_storage", fmt.Sprintf("卷 %d 的 EC 执行节点无足够恢复暂存空间", w.Volume))
		}
	}
	members = slices.Clone(members)
	slots := scan.ReplicaSlots
	slices.SortStableFunc(members, func(a, b protocol.PoolNodePlan) int {
		return cmp.Compare(slots[poolVolumeAddress(b)], slots[poolVolumeAddress(a)])
	})
	domains := map[string]bool{}
	var targets []string
	for _, m := range members {
		i := slices.IndexFunc(candidates, func(v storagepool.ECMember) bool { return v.NodeID == m.NodeID })
		address := poolVolumeAddress(m)
		if i < 0 || domains[m.FailureDomain] || len(pinned) > 0 && !slices.Contains(pinned, address) {
			continue
		}
		free := candidates[i].Available
		if i == worker {
			free -= scratch
		}
		staged := len(pinned) > 0 && slices.ContainsFunc(scan.Volumes, func(v protocol.PoolECVolume) bool {
			return v.Volume == w.Volume && v.ShardBits == 0 && v.ReadOnly && v.Collection == w.Plan.Input.Collection && v.Address == address
		})
		if staged || slots[address] > 0 && free > w.Plan.Bytes+reserve {
			targets = append(targets, address)
			domains[m.FailureDomain] = true
		}
		if len(targets) == 2 {
			if len(pinned) > 0 {
				return pinned, nil
			}
			return targets, nil
		}
	}
	return nil, fault("insufficient_storage", fmt.Sprintf("卷 %d 需要两个不同故障域有普通卷空槽及字节容量，当前符合 %d 个", w.Volume, len(targets)))
}

func (n *Node) planPoolRestore(ctx context.Context, db storagepool.Store, plan poolPlanRecord, encoded storagepool.ECWork,
	candidates []storagepool.ECMember, scan protocol.PoolECScan) {
	targets, err := poolRestoreTargets(plan.Nodes, encoded, candidates, scan)
	if err != nil {
		var f *Fault
		if errors.As(err, &f) && f.Code == "insufficient_storage" {
			n.failPoolRestore(ctx, db, encoded.PoolID, nil, f.Message, false)
		}
		return
	}
	encoded.Plan.Input.Restore = targets
	encoded.Generation, encoded.Operation = n.Now().UnixNano(), NewOperationID(n.ID)
	encoded.State, encoded.Direction, encoded.Cutover, encoded.Cleanup = "", "restore", false, false
	encoded.Plan.Attempt, encoded.Plan.RetryAt, encoded.Plan.Repairing = 1, 0, false
	_ = db.BeginEC(ctx, encoded, n.Now())
}

func (n *Node) failPoolRestore(ctx context.Context, db storagepool.Store, pool string, w *storagepool.ECWork, message string, keepTargets bool) {
	if db.FailECRestore(ctx, pool, w, message, keepTargets, n.Now()) == nil {
		n.notifyChange(Change{Resource: "storage", NodeID: n.ID})
	}
}

func (n *Node) advancePoolEC(ctx context.Context, db storagepool.Store, plan poolPlanRecord, w storagepool.ECWork) {
	now := n.Now()
	if now.Unix() < w.Plan.RetryAt || w.Plan.Attempt < 1 {
		return
	}
	var action, next string
	switch {
	case w.Cleanup && w.Cutover && w.Direction == "encode":
		action = "clean-replicas"
	case w.Cleanup && w.Cutover:
		action = "clean-shards"
	case w.Direction == "restore" && w.State == "pending":
		w.State = "verifying"
		_ = db.SaveEC(ctx, w, now)
		return
	case w.Direction == "restore" && w.State == "verifying":
		action = "restore-replicas"
	case w.State == "pending":
		action, next = "seal", "encoding"
	case w.State == "encoding":
		action, next = "prepare", "verifying"
	case w.State == "verifying":
		action = "verify"
	default:
		return
	}
	job := protocol.PoolECJob{ID: w.Operation, Attempt: w.Plan.Attempt, Input: w.Plan.Input}
	job.Input.Action = action
	result, err := n.callPoolEC(ctx, plan, w.Plan.Worker, job)
	if err != nil {
		return
	}
	if result.State == "absent" {
		job.Start = true
		result, err = n.callPoolEC(ctx, plan, w.Plan.Worker, job)
		if err != nil {
			return
		}
	}
	switch result.State {
	case "done":
		w.Error, w.Plan.Attempt, w.Plan.RetryAt = "", 1, 0
		if w.Cleanup {
			w.Cleanup = false
			_ = db.SaveEC(ctx, w, now)
		} else if next != "" {
			w.State = next
			_ = db.SaveEC(ctx, w, now)
		} else {
			_ = db.CommitEC(ctx, w, now)
		}
		n.notifyChange(Change{Resource: "storage", NodeID: n.ID})
	case "failed", "unknown":
		w.Error = result.Message
		if action == "restore-replicas" && result.State == "failed" && result.Code == "insufficient_storage" {
			n.failPoolRestore(ctx, db, w.PoolID, &w, w.Error, strings.Contains(w.Error, "目标 ") && strings.Contains(w.Error, "暂存"))
			return
		}
		if result.State == "failed" && (action == "seal" || action == "prepare") && strings.Contains(w.Error, "自计划以来有新写入或容量变化") {
			w.State = "failed"
			w.Plan.Attempt = 0
		} else {
			w.Plan.Attempt++
			w.Plan.RetryAt = now.Add(time.Duration(min(w.Plan.Attempt*15, 300)) * time.Second).Unix()
		}
		_ = db.SaveEC(ctx, w, now)
		n.notifyChange(Change{Resource: "storage", NodeID: n.ID})
	}
}
