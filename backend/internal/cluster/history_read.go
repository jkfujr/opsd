package cluster

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type NodeHistory struct {
	NodeID       string             `json:"node_id"`
	State        string             `json:"state"`
	LastSampleAt *time.Time         `json:"last_sample_at,omitempty"`
	Samples      []protocol.Metrics `json:"-"`
}

// ReadHistoryWindow 汇总左闭右开的窗口。只有明确读到旧能力版本时才查询共享副本。
func (n *Node) ReadHistoryWindow(ctx context.Context, targets []string, q HistoryQuery) []NodeHistory {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	results := make([]NodeHistory, len(targets))
	slots := make(chan struct{}, 8)
	var group sync.WaitGroup
	for i, id := range targets {
		results[i].NodeID = id
		group.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				results[i].State = "timeout"
				return
			}
			if id == n.ID {
				var err error
				result := &results[i]
				if h, _, _, _ := n.history.snapshot(); h != nil {
					result.Samples, err = n.historySlice(ctx, h, q)
				} else {
					result.Samples, err = n.legacyLocalHistory(ctx, inclusiveHistory(q))
				}
				result.complete(ctx, err, q)
				return
			}
			check, stop := context.WithTimeout(ctx, 2*time.Second)
			var capability historyCapability
			err := n.Call(check, id, "api.capabilities", nil, &capability)
			checkErr := check.Err()
			stop()
			result := &results[i]
			if err != nil {
				result.complete(ctx, errors.Join(err, checkErr), q)
				return
			}
			if capability.LocalHistoryVersion == 0 {
				result.State = "legacy"
				return
			}
			for from := q.From; from.Before(q.To); {
				to := from.Add(24 * time.Hour)
				if to.After(q.To) {
					to = q.To
				}
				var samples []protocol.Metrics
				page := inclusiveHistory(HistoryQuery{From: from, To: to})
				if err = n.Call(ctx, id, "history", page, &samples); err != nil {
					break
				}
				result.Samples = append(result.Samples, samples...)
				from = to
			}
			result.complete(ctx, err, q)
		})
	}
	group.Wait()
	var legacy []string
	indexes := map[string]int{}
	for i, r := range results {
		if r.State == "legacy" {
			legacy = append(legacy, r.NodeID)
			indexes[r.NodeID] = i
		}
	}
	if len(legacy) > 0 {
		legacyQuery := HistoryQuery{From: q.From, To: q.To.Add(-time.Nanosecond)}
		failed := n.readLegacyHistory(ctx, legacy, legacyQuery, func(id string, m protocol.Metrics) {
			out := &results[indexes[id]]
			out.Samples = append(out.Samples, m)
		})
		for _, id := range legacy {
			var err error
			if slices.Contains(failed, id) {
				err = fault("dependency_unavailable", "旧监控历史不可用")
			}
			results[indexes[id]].complete(ctx, err, q)
		}
	}
	return results
}

func inclusiveHistory(q HistoryQuery) HistoryQuery {
	_, to := historyBounds(q)
	return HistoryQuery{From: time.Unix(q.From.Unix(), 0).UTC(), To: time.Unix(to-1, 0).UTC()}
}

func (r *NodeHistory) complete(ctx context.Context, err error, q HistoryQuery) {
	if err != nil {
		r.Samples = nil
		r.State = "error"
		var remote *Fault
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			r.State = "timeout"
		} else if errors.As(err, &remote) && remote.Code == "node_unavailable" {
			r.State = "offline"
		}
		return
	}
	r.State = "empty"
	r.Samples = slices.DeleteFunc(r.Samples, func(m protocol.Metrics) bool {
		return m.CollectedAt.Before(q.From) || !m.CollectedAt.Before(q.To)
	})
	for _, m := range r.Samples {
		if r.LastSampleAt == nil || m.CollectedAt.After(*r.LastSampleAt) {
			r.LastSampleAt = &m.CollectedAt
		}
	}
	if r.LastSampleAt != nil {
		r.State = "ready"
	}
}
