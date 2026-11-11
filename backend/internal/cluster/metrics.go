package cluster

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jkfujr/opsd/internal/monitoring"
	"github.com/jkfujr/opsd/internal/protocol"
)

type liveRead struct {
	value protocol.MonitoringResult
	at    time.Time
	done  chan struct{}
	err   error
}

func (n *Node) metrics(ctx context.Context, stream string) (protocol.MonitoringResult, error) {
	if stream == "history" {
		return n.collect(ctx, stream)
	}
	for {
		n.liveMu.Lock()
		cached := n.live
		if cached != nil && cached.done != nil {
			done := cached.done
			n.liveMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return protocol.MonitoringResult{}, ctx.Err()
			}
		}
		if cached != nil && n.Now().Sub(cached.at) < 3*time.Second {
			v, e := cached.value, cached.err
			n.liveMu.Unlock()
			return v, e
		}
		entry := &liveRead{at: n.Now(), done: make(chan struct{})}
		n.live = entry
		n.liveMu.Unlock()
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2500*time.Millisecond)
		v, e := n.collect(readCtx, stream)
		cancel()
		n.liveMu.Lock()
		entry.value = v
		entry.err = e
		entry.at = n.Now()
		close(entry.done)
		entry.done = nil
		n.liveMu.Unlock()
		return v, e
	}
}
func (n *Node) collect(ctx context.Context, stream string) (protocol.MonitoringResult, error) {
	if n.Executor != nil {
		reply, e := n.Executor.Call(ctx, &protocol.MonitoringRead{Stream: stream})
		if e == nil {
			if p, ok := reply.(*protocol.MonitoringResult); ok {
				if state, err := n.FirewallConfig(ctx); err == nil && state.CheckedAt != nil {
					p.Firewall = "unknown"
					if state.Backend == "none" {
						p.Firewall = "none"
					} else if state.ServiceActive != nil {
						if *state.ServiceActive {
							p.Firewall = "enabled"
						} else {
							p.Firewall = "disabled"
						}
					}
				}
				return *p, nil
			}
		}
	}
	return protocol.MonitoringResult{Metrics: n.collector.Collect(stream), Firewall: "unknown", FirewallError: "防火墙状态读取失败"}, nil
}
func (n *Node) sample(ctx context.Context) error {
	v, e := n.metrics(ctx, "history")
	if e != nil {
		return e
	}
	b, _ := json.Marshal(v.Metrics)
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	at := v.Metrics.CollectedAt.UTC()
	if _, e = tx.ExecContext(ctx, "INSERT INTO monitoring_samples(collected_at,body) VALUES(?,?) ON CONFLICT(collected_at) DO NOTHING", at.Unix(), string(b)); e != nil {
		return e
	}
	if e = n.appendTx(ctx, tx, "metrics", strconv.FormatInt(at.Unix(), 10), v.Metrics, at); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM monitoring_samples WHERE collected_at<?", n.Now().Add(-7*24*time.Hour).Unix()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM records WHERE kind='metrics' AND database_name<?", "opsd_metrics_"+n.Now().UTC().AddDate(0, 0, -6).Format("20060102")); e != nil {
		return e
	}
	return tx.Commit()
}

type HistoryQuery struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func (n *Node) localHistory(ctx context.Context, q HistoryQuery) ([]protocol.Metrics, error) {
	if q.From.Before(n.Now().Add(-7 * 24 * time.Hour)) {
		q.From = n.Now().Add(-7 * 24 * time.Hour)
	}
	rows, e := n.DB.QueryContext(ctx, "SELECT body FROM monitoring_samples WHERE collected_at>=? AND collected_at<=? ORDER BY collected_at LIMIT 10081", q.From.Unix(), q.To.Unix())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []protocol.Metrics{}
	for rows.Next() {
		var b string
		var m protocol.Metrics
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &m); e != nil {
			return nil, e
		}
		items = append(items, m)
	}
	return items, rows.Err()
}
func (n *Node) History(ctx context.Context, target string, q HistoryQuery) ([]protocol.Metrics, error) {
	if target == n.ID {
		return n.localHistory(ctx, q)
	}
	items := []protocol.Metrics{}
	failed := n.ReadHistory(ctx, []string{target}, q, func(_ string, m protocol.Metrics) { items = append(items, m) })
	if len(failed) > 0 {
		return nil, fault("dependency_unavailable", "监控历史不可用")
	}
	return items, nil
}

type Overview struct {
	UpdatedAt         time.Time          `json:"updated_at"`
	Nodes             []OverviewNode     `json:"nodes"`
	Summary           monitoring.Summary `json:"summary"`
	PendingOperations []Operation        `json:"pending_operations"`
	Unavailable       []string           `json:"unavailable,omitempty"`
}
type OverviewNode struct {
	Node    any                `json:"node"`
	Reading monitoring.Reading `json:"reading"`
}

func (n *Node) Reading(ctx context.Context, id string) monitoring.Reading {
	r := monitoring.Reading{NodeID: id, State: "offline", Firewall: "unknown"}
	var m struct {
		protocol.MonitoringResult
		Pending []Operation `json:"pending_operations"`
	}
	status := n.CurrentRead(ctx, id, "metrics", nil, &m, 3*time.Second)
	r.Read = status
	if status.DataAt == nil || status.State != "ready" {
		r.State = status.State
		r.Error = ""
		if status.Error != nil {
			r.Error = status.Error.Message
		}
		if status.DataAt != nil {
			r.Metrics = &m.Metrics
		}
		n.liveMu.Lock()
		if n.readings == nil {
			n.readings = map[string]monitoring.Reading{}
			n.pending = map[string][]Operation{}
		}
		n.readings[id] = r
		if _, ok := n.pending[id]; !ok {
			n.pending[id] = nil
		}
		n.liveMu.Unlock()
		return r
	}
	r.State = "ready"
	if len(m.Metrics.Errors) > 0 {
		r.State = "partial"
	}
	r.Metrics = &m.Metrics
	r.Firewall = m.Firewall
	r.FirewallError = m.FirewallError
	n.liveMu.Lock()
	if n.readings == nil {
		n.readings = map[string]monitoring.Reading{}
		n.pending = map[string][]Operation{}
	}
	n.readings[id] = r
	n.pending[id] = m.Pending
	n.liveMu.Unlock()
	return r
}
func (n *Node) PendingKnown() ([]Operation, []string) {
	n.liveMu.Lock()
	pending := map[string][]Operation{}
	states := map[string]string{}
	for id, r := range n.readings {
		states[id] = r.Read.State
	}
	for id, ops := range n.pending {
		pending[id] = append([]Operation(nil), ops...)
	}
	n.liveMu.Unlock()
	out := []Operation{}
	unavailable := []string{}
	for id, ops := range pending {
		for _, op := range ops {
			op.ResultPendingSync = !n.Connected(id) || states[id] != "ready"
			out = append(out, op)
		}
		if !n.Connected(id) || states[id] != "ready" {
			unavailable = append(unavailable, id)
		}
	}
	return out, unavailable
}
