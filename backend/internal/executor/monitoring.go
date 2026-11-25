package executor

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (s *Services) resourceMetrics(ctx context.Context, r protocol.ResourceMetricsRead) (protocol.Payload, error) {
	if r.DatabasesOnly {
		// 先结束慢查询，给已完成的数据库指标留出回传时间。
		read, cancel := context.WithTimeout(ctx, 550*time.Millisecond)
		defer cancel()
		ctx = read
	}
	out := &protocol.ResourceMetrics{At: time.Now().UTC(), Metrics: []protocol.ResourceMetric{}, Errors: []string{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	if s.Docker != nil && !r.DatabasesOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := s.Docker.List(ctx)
			mu.Lock()
			defer mu.Unlock()
			out.Docker = &state
			if state.State != "ready" {
				out.Errors = append(out.Errors, "containers: "+state.State)
				return
			}
			out.Metrics = append(out.Metrics, protocol.ResourceMetric{Metric: "opsd_docker_up", Value: 1})
			for _, c := range state.Containers {
				v := 0.0
				if c.State == "running" {
					v = 1
				}
				out.Metrics = append(out.Metrics, protocol.ResourceMetric{Metric: "opsd_container_running", ResourceID: c.ContainerID, Value: v})
			}
		}()
	}
	for id, c := range r.Connections {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				mu.Lock()
				out.Errors = append(out.Errors, "database: "+id+": 采集超时")
				mu.Unlock()
				return
			}
			values, err := (databaseengine.Manager{}).Metrics(ctx, id, c)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("database: %s: %v", id, err))
				return
			}
			out.Metrics = append(out.Metrics, values...)
		}()
	}
	wg.Wait()
	sort.Slice(out.Metrics, func(i, j int) bool {
		a, b := out.Metrics[i], out.Metrics[j]
		return a.Metric+a.ResourceID+a.Backend < b.Metric+b.ResourceID+b.Backend
	})
	sort.Strings(out.Errors)
	if len(out.Errors) > 32 {
		out.Errors = out.Errors[:32]
	}
	return out, nil
}
