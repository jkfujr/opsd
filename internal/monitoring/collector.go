// Package monitoring 负责主机资源采集、共享读数与监控历史。
package monitoring

import (
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type counters struct {
	at          time.Time
	boot        string
	total, idle uint64
	network     map[string][2]uint64
}

type Collector struct {
	mu       sync.Mutex
	previous map[string]counters
}

func (c *Collector) Collect(stream string) protocol.Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.previous == nil {
		c.previous = make(map[string]counters)
	}
	m, raw := collect()
	if before, ok := c.previous[stream]; ok {
		if stream != "live" || raw.at.Sub(before.at) <= 10*time.Second {
			rates(&m, before, raw)
		}
	}
	c.previous[stream] = raw
	return m
}

func rates(m *protocol.Metrics, before, after counters) {
	seconds := after.at.Sub(before.at).Seconds()
	if seconds <= 0 || before.boot == "" || before.boot != after.boot {
		return
	}
	if before.total > 0 && after.total > before.total && after.idle >= before.idle {
		delta, idle := after.total-before.total, after.idle-before.idle
		if idle <= delta {
			v := 100 * float64(delta-idle) / float64(delta)
			m.CPUUsage = &v
		}
	}
	if len(before.network) == 0 || len(before.network) != len(after.network) {
		return
	}
	var rx, tx float64
	for name, current := range after.network {
		old, ok := before.network[name]
		if !ok || current[0] < old[0] || current[1] < old[1] {
			return
		}
		rx += float64(current[0]-old[0]) / seconds
		tx += float64(current[1]-old[1]) / seconds
	}
	m.NetworkRX, m.NetworkTX = &rx, &tx
}
