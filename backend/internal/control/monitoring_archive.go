package control

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/objectstore"
)

func (s *Server) ListStorageTargets(w http.ResponseWriter, r *http.Request) {
	items, err := s.node.StorageTargets(r.Context())
	if err == nil {
		for i := range items {
			var current objectstore.Target
			s.node.CurrentRead(r.Context(), items[i].NodeID, "storage.get", cluster.StorageCommand{ID: items[i].ID}, &current, 5*time.Second)
			if current.Revision == items[i].Revision {
				items[i].Check = current.Check
			}
		}
	}
	s.resultError(w, r, 200, map[string]any{"items": items}, err)
}
func (s *Server) GetStorageTarget(w http.ResponseWriter, r *http.Request, id string) {
	item, err := s.node.StorageTarget(r.Context(), id)
	if err == nil {
		err = s.node.Call(r.Context(), item.NodeID, "storage.get", cluster.StorageCommand{ID: id}, &item)
	}
	if err == nil {
		s.setETag(w, r, item.Revision)
	}
	s.resultError(w, r, 200, item, err)
}
func (s *Server) SaveStorageTarget(w http.ResponseWriter, r *http.Request, id string, _ httpapi.SaveStorageTargetParams) {
	var body httpapi.StorageTargetInput
	if !s.body(w, r, &body) {
		return
	}
	input := convertHTTP[objectstore.Input](body)
	input.ID, input.Revision = id, expectedRevision(r)
	var out objectstore.Target
	err := s.node.Call(r.Context(), input.NodeID, "storage.save", cluster.StorageCommand{Input: &input, Write: s.writeIdentity(r)}, &out)
	if err == nil {
		s.setETag(w, r, out.Revision)
	}
	s.resultError(w, r, 200, out, err)
}
func (s *Server) CheckStorageTarget(w http.ResponseWriter, r *http.Request, id string, _ httpapi.CheckStorageTargetParams) {
	var body httpapi.StorageCheckInput
	if !s.body(w, r, &body) {
		return
	}
	target, err := s.node.StorageTarget(r.Context(), id)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	if !s.checkRevision(w, r, target.Revision) {
		return
	}
	var out objectstore.Check
	err = s.node.Call(r.Context(), target.NodeID, "storage.check", cluster.StorageCommand{ID: id, Prefix: body.Prefix, Revision: target.Revision, Write: s.writeIdentity(r)}, &out)
	s.setETag(w, r, target.Revision)
	s.resultError(w, r, 200, out, err)
}
func (s *Server) GetMonitoringConfig(w http.ResponseWriter, r *http.Request) {
	out, err := s.node.ArchiveConfig(r.Context())
	if err == nil {
		s.setETag(w, r, out.Revision)
	}
	s.resultError(w, r, 200, out, err)
}
func (s *Server) SaveMonitoringConfig(w http.ResponseWriter, r *http.Request, _ httpapi.SaveMonitoringConfigParams) {
	var body httpapi.MonitoringArchiveInput
	if !s.body(w, r, &body) {
		return
	}
	config := convertHTTP[metricsstore.Config](body)
	config.Revision = expectedRevision(r)
	target := config.NodeID
	if !config.Enabled {
		previous, err := s.node.ArchiveConfig(r.Context())
		if err != nil {
			s.operationError(w, r, err)
			return
		}
		target = previous.NodeID
	}
	if target == "" {
		target = s.node.ID
	}
	s.submit(w, r, target, "monitoring_config", config, r.Header.Get("Idempotency-Key"), "")
}
func (s *Server) GetMonitoringArchiveStatus(w http.ResponseWriter, r *http.Request, _ httpapi.GetMonitoringArchiveStatusParams) {
	nodes, err := s.nodes(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	out := httpapi.MonitoringArchiveStatusList{Items: []httpapi.MonitoringArchiveStatus{}, Unavailable: []httpapi.UnavailableNode{}}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	slots := make(chan struct{}, 8)
	for _, node := range nodes {
		if node.RevokedAt != nil || r.URL.Query().Get("node_id") != "" && r.URL.Query().Get("node_id") != node.Id {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var status cluster.ArchiveStatus
			var err error
			select {
			case slots <- struct{}{}:
				err = s.node.Call(ctx, node.Id, "archive.status", nil, &status)
				<-slots
			case <-ctx.Done():
				err = ctx.Err()
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Unavailable = append(out.Unavailable, httpapi.UnavailableNode{NodeId: node.Id, NodeName: node.Name, Message: err.Error()})
			} else {
				status.Buffer.Gaps = slices.DeleteFunc(status.Buffer.Gaps, func(g metricsstore.Gap) bool { return g.NodeID != node.Id })
				out.Items = append(out.Items, convertHTTP[httpapi.MonitoringArchiveStatus](status))
			}
		}()
	}
	wg.Wait()
	slices.SortFunc(out.Items, func(a, b httpapi.MonitoringArchiveStatus) int {
		if a.NodeId < b.NodeId {
			return -1
		}
		if a.NodeId > b.NodeId {
			return 1
		}
		return 0
	})
	write(w, 200, out)
}
func (s *Server) GetMonitoringSeries(w http.ResponseWriter, r *http.Request, p httpapi.GetMonitoringSeriesParams) {
	q := metricsstore.Query{Metric: p.Metric}
	if p.From != nil {
		q.From = *p.From
	}
	if p.To != nil {
		q.To = *p.To
	}
	if p.StepSeconds != nil {
		q.StepSeconds = *p.StepSeconds
	}
	if p.Aggregate != nil {
		q.Aggregate = string(*p.Aggregate)
	}
	module := metricsstore.MetricModules[q.Metric]
	if module == "" {
		s.fail(w, r, 422, "invalid_request", "请选择受支持的指标")
		return
	}
	a := access(r.Context())
	if a != nil && !a.Token.Has(module, "read") {
		s.fail(w, r, 403, "forbidden", "令牌没有此指标读取权限")
		return
	}
	directory, err := s.node.Directory(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	resources := map[string][]string{}
	if module == "databases" {
		definitions, err := s.databaseDefinitions(r.Context())
		if err != nil {
			s.operationError(w, r, err)
			return
		}
		for _, c := range definitions {
			for _, item := range append(append([]cluster.DatabaseRef{}, c.Members...), c.Proxies...) {
				if !slices.Contains(resources[item.NodeID], item.InstanceID) {
					resources[item.NodeID] = append(resources[item.NodeID], item.InstanceID)
				}
			}
		}
	}
	for _, m := range directory.Members {
		if !s.node.Active(r.Context(), m.ID) || p.NodeId != nil && m.ID != *p.NodeId {
			continue
		}
		if module != "databases" && a != nil && !a.Token.Allows(module, "read", m.ID, m.Region) {
			continue
		}
		ids := resources[m.ID]
		if p.ResourceId != nil && *p.ResourceId != "" {
			if module == "databases" && !slices.Contains(ids, *p.ResourceId) {
				continue
			}
			if module == "monitoring" || q.Metric == "opsd_docker_up" {
				s.fail(w, r, 422, "invalid_request", "主机指标无需资源标识")
				return
			}
			ids = []string{*p.ResourceId}
		}
		if module == "databases" && len(ids) == 0 {
			continue
		}
		q.Targets = append(q.Targets, metricsstore.QueryTarget{NodeID: m.ID, ResourceIDs: ids})
	}
	if err = q.Normalize(s.now()); err != nil {
		s.fail(w, r, 422, "invalid_request", err.Error())
		return
	}
	config, err := s.node.ArchiveConfig(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	if config.NodeID == "" {
		s.fail(w, r, 409, "conflict", "集中监控尚未配置")
		return
	}
	var out metricsstore.QueryResult
	err = s.node.Call(r.Context(), config.NodeID, "archive.series", q, &out)
	s.resultError(w, r, 200, out, err)
}
