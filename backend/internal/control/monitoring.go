package control

import (
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/monitoring"
	"github.com/jkfujr/opsd/internal/protocol"
	"net/http"
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
		if f, ok := e.(*cluster.Fault); ok && f.Code == "invalid_request" {
			s.fail(w, r, 400, f.Code, f.Message)
		} else {
			s.operationError(w, r, e)
		}
		return
	}
	c, e := s.node.Config(r.Context())
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	type bucketValue struct {
		summary monitoring.Summary
		count   int
	}
	buckets := map[int64]bucketValue{}
	unavailable := []string{}
	nodeHistory := []httpapi.MonitoringNodeHistory{}
	base := int64(c.IntervalMinutes * 60)
	start, end := q.From.Unix(), q.To.Add(-time.Nanosecond).Unix()
	step := base * max(1, (end-start+1+720*base-1)/(720*base))
	for end/step-start/step+1 > 720 {
		step += base
	}
	status := s.node.ReadHistoryWindow(r.Context(), targets, q)
	for _, node := range status {
		if node.State != "ready" && node.State != "empty" {
			unavailable = append(unavailable, node.NodeID)
			continue
		}
		byBucket := map[int64]protocol.Metrics{}
		for _, m := range node.Samples {
			bucket := m.CollectedAt.Unix() / step
			if old, ok := byBucket[bucket]; !ok || m.CollectedAt.After(old.CollectedAt) {
				byBucket[bucket] = m
			}
		}
		metrics := []httpapi.Metrics{}
		for bucket, m := range byBucket {
			value := buckets[bucket]
			value.count++
			value.summary = monitoring.Combine(value.summary, monitoring.Aggregate([]monitoring.Reading{{NodeID: node.NodeID, State: "ready", Metrics: &m}}, m.CollectedAt))
			buckets[bucket] = value
			metrics = append(metrics, convertHTTP[httpapi.Metrics](m))
		}
		sort.Slice(metrics, func(i, j int) bool { return metrics[i].CollectedAt.Before(metrics[j].CollectedAt) })
		if r.URL.Query().Get("group_by") == "node" {
			nodeHistory = append(nodeHistory, httpapi.MonitoringNodeHistory{NodeId: node.NodeID, Items: metrics})
		}
	}
	if len(unavailable) == len(targets) && len(targets) > 0 {
		s.operationError(w, r, &cluster.Fault{Code: "dependency_unavailable", Message: "监控历史不可用"})
		return
	}
	points := []monitoring.Point{}
	for t := start / step; t <= end/step; t++ {
		at := time.Unix(t*step, 0).UTC()
		value := buckets[t]
		summary := value.summary
		summary.Metrics.CollectedAt = at
		if summary.Metrics.Errors == nil {
			summary.Metrics.Errors = []string{}
		}
		points = append(points, monitoring.Point{CollectedAt: at, Summary: summary, SampleCount: value.count})
	}
	out := map[string]any{"items": points, "interval_minutes": step / 60, "unavailable": unavailable, "checked_at": s.now().UTC(), "from": q.From.UTC(), "to": q.To.UTC(), "node_status": status}
	if r.URL.Query().Get("group_by") == "node" {
		out["nodes"] = nodeHistory
	}
	s.resultError(w, r, 200, out, nil)
}

func (s *Server) GetMonitoringLocal(w http.ResponseWriter, r *http.Request, id string) {
	var out cluster.LocalHistoryStatus
	err := s.node.Call(r.Context(), id, "history.local", nil, &out)
	if err == nil {
		s.setETag(w, r, out.Revision)
	}
	s.resultError(w, r, 200, out, err)
}
