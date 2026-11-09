package monitoring

import (
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type Settings struct {
	IntervalMinutes int `json:"interval_minutes"`
	RetentionDays   int `json:"retention_days"`
}
type Target struct {
	ID, Connection       string
	Connected, Supported bool
}
type Reading struct {
	Read          protocol.ReadStatus `json:"read"`
	NodeID        string              `json:"node_id"`
	State         string              `json:"state"`
	Metrics       *protocol.Metrics   `json:"metrics,omitempty"`
	Error         string              `json:"error,omitempty"`
	Firewall      string              `json:"firewall"`
	FirewallError string              `json:"firewall_error,omitempty"`
}
type Coverage struct {
	CPU     int `json:"cpu"`
	Memory  int `json:"memory"`
	Disk    int `json:"disk"`
	Network int `json:"network"`
}
type Summary struct {
	Metrics    protocol.Metrics `json:"metrics"`
	ValidNodes Coverage         `json:"valid_nodes"`
}
type Point struct {
	CollectedAt time.Time `json:"collected_at"`
	Summary     Summary   `json:"summary"`
}

func Aggregate(readings []Reading, now time.Time) Summary {
	r := Summary{Metrics: protocol.Metrics{CollectedAt: now, Errors: []string{}}}
	var cpu float64
	var cores int
	var mu, mt, du, dt uint64
	var rx, tx float64
	for _, reading := range readings {
		if reading.Metrics == nil || (reading.State != "ready" && reading.State != "partial") {
			continue
		}
		m := reading.Metrics
		if m.CPUUsage != nil && m.CPUCores > 0 {
			cpu += *m.CPUUsage * float64(m.CPUCores)
			cores += m.CPUCores
			r.ValidNodes.CPU++
		}
		if m.MemoryUsed != nil && m.MemoryTotal != nil {
			mu += *m.MemoryUsed
			mt += *m.MemoryTotal
			r.ValidNodes.Memory++
		}
		if m.DiskUsed != nil && m.DiskTotal != nil {
			du += *m.DiskUsed
			dt += *m.DiskTotal
			r.ValidNodes.Disk++
		}
		if m.NetworkRX != nil && m.NetworkTX != nil {
			rx += *m.NetworkRX
			tx += *m.NetworkTX
			r.ValidNodes.Network++
		}
	}
	r.Metrics.CPUCores = cores
	if cores > 0 {
		v := cpu / float64(cores)
		r.Metrics.CPUUsage = &v
	}
	if r.ValidNodes.Memory > 0 {
		r.Metrics.MemoryUsed = &mu
		r.Metrics.MemoryTotal = &mt
	}
	if r.ValidNodes.Disk > 0 {
		r.Metrics.DiskUsed = &du
		r.Metrics.DiskTotal = &dt
	}
	if r.ValidNodes.Network > 0 {
		r.Metrics.NetworkRX = &rx
		r.Metrics.NetworkTX = &tx
	}
	return r
}

// Combine 合并已按节点去重的历史分组，不保留全部节点的原始历史。
func Combine(a, b Summary) Summary {
	cores := a.Metrics.CPUCores + b.Metrics.CPUCores
	if cores > 0 {
		var total float64
		if a.Metrics.CPUUsage != nil {
			total += *a.Metrics.CPUUsage * float64(a.Metrics.CPUCores)
		}
		if b.Metrics.CPUUsage != nil {
			total += *b.Metrics.CPUUsage * float64(b.Metrics.CPUCores)
		}
		value := total / float64(cores)
		a.Metrics.CPUUsage = &value
	}
	a.Metrics.CPUCores = cores
	add := func(dst **uint64, src *uint64) {
		if src != nil {
			var v uint64
			if *dst != nil {
				v = **dst
			}
			v += *src
			*dst = &v
		}
	}
	add(&a.Metrics.MemoryUsed, b.Metrics.MemoryUsed)
	add(&a.Metrics.MemoryTotal, b.Metrics.MemoryTotal)
	add(&a.Metrics.DiskUsed, b.Metrics.DiskUsed)
	add(&a.Metrics.DiskTotal, b.Metrics.DiskTotal)
	addRate := func(dst **float64, src *float64) {
		if src != nil {
			var v float64
			if *dst != nil {
				v = **dst
			}
			v += *src
			*dst = &v
		}
	}
	addRate(&a.Metrics.NetworkRX, b.Metrics.NetworkRX)
	addRate(&a.Metrics.NetworkTX, b.Metrics.NetworkTX)
	a.ValidNodes.CPU += b.ValidNodes.CPU
	a.ValidNodes.Memory += b.ValidNodes.Memory
	a.ValidNodes.Disk += b.ValidNodes.Disk
	a.ValidNodes.Network += b.ValidNodes.Network
	return a
}
