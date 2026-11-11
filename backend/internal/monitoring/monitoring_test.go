package monitoring

import (
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func ptr[T any](v T) *T { return &v }

func TestRatesResetAndMissingCounters(t *testing.T) {
	now := time.Now()
	before := counters{at: now, boot: "boot", total: 100, idle: 40, network: map[string][2]uint64{"eth0": {100, 200}}}
	after := counters{at: now.Add(3 * time.Second), boot: "boot", total: 200, idle: 65, network: map[string][2]uint64{"eth0": {400, 800}}}
	var m protocol.Metrics
	rates(&m, before, after)
	if m.CPUUsage == nil || *m.CPUUsage != 75 || m.NetworkRX == nil || *m.NetworkRX != 100 || *m.NetworkTX != 200 {
		t.Fatalf("差值计算错误：%+v", m)
	}
	for _, change := range []func(*counters){func(c *counters) { c.boot = "new" }, func(c *counters) { c.at = now }, func(c *counters) { c.total = 1; c.idle = 1; c.network = map[string][2]uint64{"eth0": {1, 1}} }} {
		copy := after
		change(&copy)
		m = protocol.Metrics{}
		rates(&m, before, copy)
		if m.CPUUsage != nil || m.NetworkRX != nil {
			t.Fatal("重启或回退不能产生有效速率")
		}
	}
	m = protocol.Metrics{}
	before.total = 0
	before.network = nil
	rates(&m, before, after)
	if m.CPUUsage != nil || m.NetworkRX != nil {
		t.Fatal("首次采集不能补零或用启动以来计数计算速率")
	}
}

func TestAggregateWeightedAndPartial(t *testing.T) {
	a := protocol.Metrics{CPUCores: 1, CPUUsage: ptr(20.0), MemoryUsed: ptr(uint64(20)), MemoryTotal: ptr(uint64(100)), NetworkRX: ptr(10.0), NetworkTX: ptr(5.0)}
	b := protocol.Metrics{CPUCores: 3, CPUUsage: ptr(80.0), MemoryUsed: ptr(uint64(180)), MemoryTotal: ptr(uint64(300))}
	value := Aggregate([]Reading{{State: "ready", Metrics: &a}, {State: "partial", Metrics: &b}, {State: "offline", Metrics: &b}, {State: "error", Metrics: &a}}, time.Now())
	if *value.Metrics.CPUUsage != 65 || *value.Metrics.MemoryUsed != 200 || *value.Metrics.MemoryTotal != 400 || value.Metrics.DiskTotal != nil || value.ValidNodes.CPU != 2 || value.ValidNodes.Network != 1 {
		t.Fatalf("汇总口径错误：%+v", value)
	}
}
