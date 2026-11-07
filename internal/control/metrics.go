package control

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	a := access(r.Context())
	if a == nil {
		s.fail(w, r, 401, "unauthenticated", "请提供 Bearer 令牌")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
	defer cancel()
	families := map[string][]string{}
	var mu sync.Mutex
	emit := func(name, labels string, value float64) {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return
		}
		line := name + "{" + labels + "} " + strconv.FormatFloat(value, 'g', -1, 64) + "\n"
		mu.Lock()
		families[name] = append(families[name], line)
		mu.Unlock()
	}
	replacer := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"")
	label := func(key, value string) string { return key + "=\"" + replacer.Replace(value) + "\"" }
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	wg.Add(3)
	go func() {
		defer wg.Done()
		if a.Token.Has("monitoring", "read") {
			if e := s.nodeMetrics(ctx, emit, label); e != nil {
				failures <- e
			}
		}
	}()
	go func() {
		defer wg.Done()
		if a.Token.Has("containers", "read") {
			if e := s.containerMetrics(ctx, emit, label); e != nil {
				failures <- e
			}
		}
	}()
	go func() { defer wg.Done(); s.databaseMetrics(ctx, emit, label) }()
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			s.operationError(w, r, e)
			return
		}
	}
	var body strings.Builder
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&body, "# TYPE %s gauge\n", name)
		sort.Strings(families[name])
		for _, line := range families[name] {
			body.WriteString(line)
		}
	}
	if strings.Contains(r.Header.Get("Accept"), "application/openmetrics-text") {
		w.Header().Set("Content-Type", "application/openmetrics-text; version=1.0.0; charset=utf-8")
		body.WriteString("# EOF\n")
	} else {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	}
	w.Write([]byte(body.String()))
}
func (s *Server) nodeMetrics(ctx context.Context, emit func(string, string, float64), label func(string, string) string) error {
	out, e := s.currentReadings(ctx, "")
	if e != nil {
		return e
	}
	for _, n := range out.Items {
		labels := label("node_id", n.NodeId) + "," + label("name", n.Name) + "," + label("region", n.Region)
		up := 0.0
		if n.State != "offline" {
			up = 1
		}
		emit("opsd_node_up", labels, up)
		if n.Metrics == nil {
			continue
		}
		m := n.Metrics
		emit("opsd_node_collected_timestamp_seconds", labels, float64(m.CollectedAt.Unix()))
		if n.State == "offline" {
			continue
		}
		emit("opsd_node_cpu_cores", labels, float64(m.CpuCores))
		if m.CpuUsage != nil {
			emit("opsd_node_cpu_usage_percent", labels, *m.CpuUsage)
		}
		for _, v := range []struct {
			name  string
			value *float64
		}{{"memory_used_bytes", m.MemoryUsed}, {"memory_total_bytes", m.MemoryTotal}, {"disk_used_bytes", m.DiskUsed}, {"disk_total_bytes", m.DiskTotal}, {"network_receive_bytes_per_second", m.NetworkRx}, {"network_transmit_bytes_per_second", m.NetworkTx}} {
			if v.value != nil {
				emit("opsd_node_"+v.name, labels, *v.value)
			}
		}
	}
	return nil
}
func (s *Server) containerMetrics(ctx context.Context, emit func(string, string, float64), label func(string, string) string) error {
	ctx, cancel := context.WithTimeout(withModule(ctx, "containers"), 5*time.Second)
	defer cancel()
	nodes, e := s.nodes(ctx)
	if e != nil {
		return e
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, node := range nodes {
		if node.RevokedAt != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var state protocol.DockerState
			var e error
			select {
			case slots <- struct{}{}:
				e = s.node.Call(ctx, node.Id, "containers.read", nil, &state)
				<-slots
			case <-ctx.Done():
				e = ctx.Err()
			}
			up := 0.0
			if e == nil && state.State == "ready" {
				up = 1
			}
			emit("opsd_docker_up", label("node_id", node.Id), up)
			if up == 0 {
				return
			}
			emit("opsd_docker_checked_timestamp_seconds", label("node_id", node.Id), float64(state.CheckedAt.Unix()))
			for _, c := range state.Containers {
				running := 0.0
				if c.State == "running" {
					running = 1
				}
				emit("opsd_container_running", label("node_id", node.Id)+","+label("container_id", c.ContainerID)+","+label("name", c.Name)+","+label("state", c.State), running)
			}
		}()
	}
	wg.Wait()
	return nil
}
