package control

import (
	"context"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/monitoring"
	"github.com/jkfujr/opsd/internal/protocol"
	"net/http"
	"sync"
	"time"
)

func (s *Server) GetOverview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2600*time.Millisecond)
	defer cancel()
	nodes, e := s.nodes(ctx)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	items := []cluster.OverviewNode{}
	readings := []monitoring.Reading{}
	slots := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, node := range nodes {
		if node.RevokedAt != nil {
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			mu.Lock()
			items = append(items, cluster.OverviewNode{Node: node, Reading: monitoring.Reading{NodeID: node.Id, State: "offline", Firewall: "unknown"}})
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
			defer cancel()
			reading := s.node.Reading(ctx, node.Id)
			node.Connected = reading.State == "ready" || reading.State == "partial"
			node.Writable = node.Connected
			mu.Lock()
			items = append(items, cluster.OverviewNode{Node: node, Reading: reading})
			readings = append(readings, reading)
			mu.Unlock()
		}()
	}
	wg.Wait()
	pending, unavailable := s.node.PendingKnown()
	s.resultError(w, r, 200, cluster.Overview{UpdatedAt: s.now(), Nodes: items, Summary: monitoring.Aggregate(readings, s.now()), PendingOperations: pending, Unavailable: unavailable}, e)
}
func (s *Server) GetMonitoringHistory(w http.ResponseWriter, r *http.Request, _ httpapi.GetMonitoringHistoryParams) {
	duration := 24 * time.Hour
	if r.URL.Query().Get("range") == "7d" {
		duration = 7 * 24 * time.Hour
	}
	now := s.now()
	q := cluster.HistoryQuery{From: now.Add(-duration), To: now}
	target := r.URL.Query().Get("node_id")
	targets := []string{target}
	if target == "" {
		targets = nil
		nodes, e := s.nodes(r.Context())
		if e != nil {
			s.operationError(w, r, e)
			return
		}
		for _, n := range nodes {
			if n.RevokedAt == nil {
				targets = append(targets, n.Id)
			}
		}
	}
	c, e := s.node.Config(r.Context())
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	buckets := map[int64]monitoring.Summary{}
	unavailable := []string{}
	for _, id := range targets {
		metrics, err := s.node.History(r.Context(), id, q)
		if err != nil {
			unavailable = append(unavailable, id)
			continue
		}
		latest := map[int64]protocol.Metrics{}
		for _, m := range metrics {
			bucket := m.CollectedAt.Unix() / int64(c.IntervalMinutes*60)
			if old, ok := latest[bucket]; !ok || m.CollectedAt.After(old.CollectedAt) {
				latest[bucket] = m
			}
		}
		for bucket, m := range latest {
			buckets[bucket] = monitoring.Combine(buckets[bucket], monitoring.Aggregate([]monitoring.Reading{{NodeID: id, State: "ready", Metrics: &m}}, m.CollectedAt))
		}
	}
	if len(unavailable) == len(targets) && len(targets) > 0 {
		s.operationError(w, r, &cluster.Fault{Code: "dependency_unavailable", Message: "监控历史不可用"})
		return
	}
	points := []monitoring.Point{}
	step := int64(c.IntervalMinutes * 60)
	for t := q.From.Unix() / step; t <= q.To.Unix()/step; t++ {
		at := time.Unix(t*step, 0).UTC()
		summary := buckets[t]
		summary.Metrics.CollectedAt = at
		if summary.Metrics.Errors == nil {
			summary.Metrics.Errors = []string{}
		}
		points = append(points, monitoring.Point{CollectedAt: at, Summary: summary})
	}
	s.resultError(w, r, 200, map[string]any{"items": points, "interval_minutes": c.IntervalMinutes, "unavailable": unavailable}, nil)
}
