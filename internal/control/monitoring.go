package control

import (
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/monitoring"
	"github.com/jkfujr/opsd/internal/protocol"
	"net/http"
	"slices"
	"sort"
	"time"
)

func (s *Server) GetOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, e := s.nodes(ctx)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	items := []cluster.OverviewNode{}
	readings := []monitoring.Reading{}
	for _, node := range nodes {
		if node.RevokedAt != nil {
			continue
		}
		reading := s.node.Reading(ctx, node.Id)
		items = append(items, cluster.OverviewNode{Node: node, Reading: reading})
		readings = append(readings, reading)
	}
	pending, unavailable := s.node.PendingKnown()
	s.resultError(w, r, 200, cluster.Overview{UpdatedAt: s.now(), Nodes: items, Summary: monitoring.Aggregate(readings, s.now()), PendingOperations: pending, Unavailable: unavailable}, e)
}
func (s *Server) GetMonitoringHistory(w http.ResponseWriter, r *http.Request, _ httpapi.GetMonitoringHistoryParams) {
	targets, q, e := s.historyTargets(r)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	c, e := s.node.Config(r.Context())
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	buckets := map[int64]monitoring.Summary{}
	unavailable := []string{}
	nodeHistory := []httpapi.MonitoringNodeHistory{}
	step := int64(c.IntervalMinutes * 60)
	if count := int64(q.To.Sub(q.From).Seconds()) / step; count > 720 {
		step *= (count + 719) / 720
	}
	byNode := map[string]map[int64]protocol.Metrics{}
	unavailable = s.node.ReadHistory(r.Context(), targets, q, func(id string, m protocol.Metrics) {
		if byNode[id] == nil {
			byNode[id] = map[int64]protocol.Metrics{}
		}
		bucket := m.CollectedAt.Unix() / step
		if old, ok := byNode[id][bucket]; !ok || m.CollectedAt.After(old.CollectedAt) {
			byNode[id][bucket] = m
		}
	})
	for _, id := range targets {
		metrics := []httpapi.Metrics{}
		for bucket, m := range byNode[id] {
			buckets[bucket] = monitoring.Combine(buckets[bucket], monitoring.Aggregate([]monitoring.Reading{{NodeID: id, State: "ready", Metrics: &m}}, m.CollectedAt))
			metrics = append(metrics, convertHTTP[httpapi.Metrics](m))
		}
		sort.Slice(metrics, func(i, j int) bool { return metrics[i].CollectedAt.Before(metrics[j].CollectedAt) })
		if r.URL.Query().Get("group_by") == "node" && !slices.Contains(unavailable, id) {
			nodeHistory = append(nodeHistory, httpapi.MonitoringNodeHistory{NodeId: id, Items: metrics})
		}
	}
	if len(unavailable) == len(targets) && len(targets) > 0 {
		s.operationError(w, r, &cluster.Fault{Code: "dependency_unavailable", Message: "监控历史不可用"})
		return
	}
	points := []monitoring.Point{}
	for t := q.From.Unix() / step; t <= q.To.Unix()/step; t++ {
		at := time.Unix(t*step, 0).UTC()
		summary := buckets[t]
		summary.Metrics.CollectedAt = at
		if summary.Metrics.Errors == nil {
			summary.Metrics.Errors = []string{}
		}
		points = append(points, monitoring.Point{CollectedAt: at, Summary: summary})
	}
	out := map[string]any{"items": points, "interval_minutes": step / 60, "unavailable": unavailable, "checked_at": s.now().UTC()}
	if r.URL.Query().Get("group_by") == "node" {
		out["nodes"] = nodeHistory
	}
	s.resultError(w, r, 200, out, nil)
}
