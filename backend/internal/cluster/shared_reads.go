package cluster

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) latestShared(ctx context.Context, owner, kind, object string) (Record, error) {
	s, err := n.sharedStore(ctx)
	if err != nil {
		return Record{}, err
	}
	body, err := s.Latest(ctx, owner, kind, object)
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err = json.Unmarshal(body, &r); err != nil {
		return r, err
	}
	if err = n.verifyStoredRecord(ctx, r); err != nil {
		return r, err
	}
	return r, n.observe(ctx, owner+"~"+kind+"~"+object, r.Sequence)
}
func (n *Node) readSharedHistory(ctx context.Context, targets []string, q HistoryQuery, visit func(string, protocol.Metrics)) []string {
	s, err := n.sharedStore(ctx)
	if err != nil {
		return targets
	}
	failed := map[string]bool{}
	for _, owner := range targets {
		rows, e := s.DB.QueryContext(ctx, "SELECT body FROM shared_records WHERE owner=? AND kind='metrics' AND created_at>=? AND created_at<=? ORDER BY created_at", owner, q.From.UTC(), q.To.UTC())
		if e != nil {
			failed[owner] = true
			continue
		}
		for rows.Next() {
			var body []byte
			var r Record
			var m protocol.Metrics
			if e = rows.Scan(&body); e != nil {
				break
			}
			if e = json.Unmarshal(body, &r); e != nil {
				break
			}
			if e = n.verifyStoredRecord(ctx, r); e != nil {
				break
			}
			if e = json.Unmarshal(r.Data, &m); e != nil {
				break
			}
			visit(owner, m)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			failed[owner] = true
		}
	}
	return historyFailures(failed)
}
func (n *Node) archivedSharedOperations(ctx context.Context, owner string, q ListQuery) (OperationList, error) {
	out := OperationList{Items: []Operation{}}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return out, err
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT r.body FROM shared_records r WHERE r.owner=? AND r.kind='operation' AND NOT EXISTS(SELECT 1 FROM shared_records newer WHERE newer.owner=r.owner AND newer.kind=r.kind AND newer.object_id=r.object_id AND newer.sequence>r.sequence)", owner)
	if err != nil {
		return out, err
	}
	latest := map[string]Operation{}
	for rows.Next() {
		var body []byte
		var r Record
		var op Operation
		if err = rows.Scan(&body); err != nil {
			break
		}
		if err = json.Unmarshal(body, &r); err != nil {
			break
		}
		if err = n.verifyStoredRecord(ctx, r); err != nil {
			break
		}
		if err = json.Unmarshal(r.Data, &op); err != nil {
			break
		}
		latest[op.ID] = op
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.DB.QueryContext(ctx, "SELECT body,coordinator,epoch FROM shared_flows WHERE JSON_UNQUOTE(JSON_EXTRACT(CONVERT(body USING utf8mb4),'$.operation.node_id'))=?", owner)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var body []byte
		var p sharedFlowPayload
		var coordinator string
		var epoch uint64
		if err = rows.Scan(&body, &coordinator, &epoch); err != nil {
			break
		}
		if err = json.Unmarshal(body, &p); err != nil {
			break
		}
		p.Operation.Coordinator, p.Operation.CoordinatorEpoch = coordinator, epoch
		latest[p.Operation.ID] = p.Operation
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	member, err := n.Member(ctx, owner)
	if err != nil {
		return out, err
	}
	for _, op := range latest {
		if q.Authorization != nil {
			module, id, region := protocol.OperationModule(op.Subject), owner, member.Region
			if module == "databases" {
				id, region = op.ResourceID, ""
			}
			if module == "" || !q.Authorization.Allows(module, "read", id, region) {
				continue
			}
		}
		if q.Key != "" && (op.Key != q.Key || q.Actor != "" && op.Actor != q.Actor) || q.ResourceID != "" && op.ResourceID != q.ResourceID || q.Before != nil && !q.Before.includes(op) || q.Pending && terminalState(op.State) || q.State != "" && string(op.State) != q.State {
			continue
		}
		out.Items = append(out.Items, op)
	}
	sort.Slice(out.Items, func(i, j int) bool { return operationBefore(out.Items[i], out.Items[j]) })
	if len(out.Items) > q.Limit {
		out.Items = out.Items[:q.Limit]
		last := out.Items[len(out.Items)-1]
		out.Next = &OperationPosition{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return out, nil
}

func mergeOperationLists(a, b OperationList, limit int) OperationList {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	items := map[string]Operation{}
	for _, list := range []OperationList{a, b} {
		for _, op := range list.Items {
			old, exists := items[op.ID]
			if !exists || op.CoordinatorEpoch > old.CoordinatorEpoch || op.CoordinatorEpoch == old.CoordinatorEpoch && op.Revision >= old.Revision {
				items[op.ID] = op
			}
		}
	}
	out := OperationList{Items: []Operation{}}
	for _, op := range items {
		out.Items = append(out.Items, op)
	}
	sort.Slice(out.Items, func(i, j int) bool { return operationBefore(out.Items[i], out.Items[j]) })
	more := len(out.Items) > limit || a.Next != nil || b.Next != nil
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
	}
	if more && len(out.Items) > 0 {
		last := out.Items[len(out.Items)-1]
		out.Next = &OperationPosition{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return out
}
