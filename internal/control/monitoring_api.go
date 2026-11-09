package control

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/monitoring"
)

func convertHTTP[T any](v any) T {
	var out T
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, &out)
	return out
}
func withModule(ctx context.Context, module string) context.Context {
	if a := access(ctx); a != nil {
		return context.WithValue(ctx, apiAccessKey{}, &apiAccess{Token: a.Token, Module: module})
	}
	return ctx
}
func (s *Server) currentReadings(ctx context.Context, target string) (httpapi.MonitoringCurrent, error) {
	out := httpapi.MonitoringCurrent{CheckedAt: s.now().UTC(), Items: []httpapi.MonitoringNode{}}
	nodes, e := s.nodes(withModule(ctx, "monitoring"))
	if e != nil {
		return out, e
	}
	selected := []httpapi.Node{}
	for _, n := range nodes {
		if n.RevokedAt == nil && (target == "" || n.Id == target) {
			selected = append(selected, n)
		}
	}
	if target != "" && len(selected) == 0 {
		return out, &cluster.Fault{Code: "not_found", Message: "节点不存在或已撤销"}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Id < selected[j].Id })
	out.Items = make([]httpapi.MonitoringNode, len(selected))
	readings := make([]monitoring.Reading, len(selected))
	for i, n := range selected {
		reading := s.node.Reading(ctx, n.Id)
		item := httpapi.MonitoringNode{NodeId: n.Id, Name: n.Name, State: httpapi.MonitoringNodeState(reading.State), CheckedAt: out.CheckedAt, Read: convertHTTP[httpapi.ReadStatus](reading.Read)}
		if n.Region != nil {
			item.Region = *n.Region
		}
		if reading.Metrics != nil {
			m := convertHTTP[httpapi.Metrics](reading.Metrics)
			item.Metrics = &m
		}
		out.Items[i] = item
		readings[i] = reading
	}
	out.Summary = convertHTTP[httpapi.MonitoringSummary](monitoring.Aggregate(readings, out.CheckedAt))
	return out, nil
}
func (s *Server) GetMonitoringCurrent(w http.ResponseWriter, r *http.Request, _ httpapi.GetMonitoringCurrentParams) {
	out, e := s.currentReadings(r.Context(), r.URL.Query().Get("node_id"))
	s.resultError(w, r, 200, out, e)
}

func (s *Server) historyTargets(r *http.Request) ([]string, cluster.HistoryQuery, error) {
	now := s.now()
	duration := 24 * time.Hour
	if r.URL.Query().Get("range") == "7d" {
		duration = 7 * 24 * time.Hour
	}
	q := cluster.HistoryQuery{From: now.Add(-duration), To: now}
	var e error
	if v := r.URL.Query().Get("from"); v != "" {
		q.From, e = time.Parse(time.RFC3339, v)
		if e != nil {
			return nil, q, &cluster.Fault{Code: "invalid_request", Message: "起始时间无效"}
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		q.To, e = time.Parse(time.RFC3339, v)
		if e != nil {
			return nil, q, &cluster.Fault{Code: "invalid_request", Message: "结束时间无效"}
		}
	}
	if !q.From.Before(q.To) || q.To.After(now.Add(time.Minute)) || q.From.Before(now.Add(-7*24*time.Hour-time.Second)) {
		return nil, q, &cluster.Fault{Code: "invalid_request", Message: "查询范围须在最近七天内"}
	}
	if v := r.URL.Query().Get("group_by"); v != "" && v != "node" {
		return nil, q, &cluster.Fault{Code: "invalid_request", Message: "分组参数无效"}
	}
	nodes, e := s.nodes(r.Context())
	if e != nil {
		return nil, q, e
	}
	allowed := map[string]bool{}
	targets := []string{}
	for _, n := range nodes {
		if n.RevokedAt == nil {
			allowed[n.Id] = true
			targets = append(targets, n.Id)
		}
	}
	wanted := r.URL.Query().Get("node_ids")
	if id := r.URL.Query().Get("node_id"); id != "" {
		if wanted != "" {
			return nil, q, &cluster.Fault{Code: "invalid_request", Message: "请使用一种节点筛选方式"}
		}
		wanted = id
	}
	if wanted != "" {
		targets = []string{}
		for _, id := range strings.Split(wanted, ",") {
			if !allowed[id] {
				code := "not_found"
				if access(r.Context()) != nil {
					code = "forbidden"
				}
				return nil, q, &cluster.Fault{Code: code, Message: "节点不存在或超出授权范围"}
			}
			if !slices.Contains(targets, id) {
				targets = append(targets, id)
			}
		}
	}
	return targets, q, nil
}
