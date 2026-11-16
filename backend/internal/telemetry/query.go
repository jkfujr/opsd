package telemetry

import (
	"context"
	"net/http"
	"time"

	"github.com/jkfujr/opsd/internal/metricsstore"
)

func (s *Service) Query(ctx context.Context, q metricsstore.Query) (metricsstore.QueryResult, error) {
	if err := q.Normalize(s.Now()); err != nil {
		return metricsstore.QueryResult{}, s.Errors.New("invalid_request", err.Error())
	}
	s.mu.Lock()
	config := s.runtime.Config
	s.mu.Unlock()
	if !config.Enabled || config.NodeID != s.NodeID {
		return metricsstore.QueryResult{}, s.Errors.New("conflict", "集中监控尚未启用")
	}
	for _, target := range q.Targets {
		if !s.Peer.Active(ctx, target.NodeID) {
			return metricsstore.QueryResult{}, s.Errors.New("forbidden", "查询包含已撤销节点")
		}
	}
	select {
	case s.querySlots <- struct{}{}:
		defer func() { <-s.querySlots }()
	case <-ctx.Done():
		return metricsstore.QueryResult{}, ctx.Err()
	}
	return metricsstore.QueryRange(ctx, &http.Client{Timeout: 28 * time.Second}, "http://127.0.0.1:19292", s.ClusterID(), q, s.Now())
}
