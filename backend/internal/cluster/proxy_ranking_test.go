package cluster

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

func rankingInput(now time.Time, latencies ...int64) (protocol.ProxyRoutingPolicy, protocol.ProxyRoutingObservation, []string) {
	policy := protocol.ProxyRoutingPolicy{Enabled: true, Automatic: true, Hostgroups: protocol.ProxyHostgroups{Writer: 10, BackupWriter: 20, Reader: 30, Offline: 40}, Backends: []protocol.ProxyBackend{}, Retired: []protocol.ProxyEndpoint{}, Accounts: []protocol.ProxyAccountBinding{}}
	observation := protocol.ProxyRoutingObservation{CheckedAt: now, Samples: []protocol.ProxyPingSample{}}
	order := []string{}
	for i, latency := range latencies {
		backend := protocol.ProxyBackend{ProxyEndpoint: protocol.ProxyEndpoint{Host: fmt.Sprintf("10.0.0.%d", i+1), Port: 3306}, NodeID: fmt.Sprint(i), InstanceID: "db"}
		policy.Backends = append(policy.Backends, backend)
		order = append(order, backend.Key())
		for j := range 3 {
			observation.Samples = append(observation.Samples, protocol.ProxyPingSample{ProxyEndpoint: backend.ProxyEndpoint, At: now.Add(-time.Duration(j) * time.Second), LatencyUS: latency})
		}
	}
	return policy, observation, order
}

func TestProxyRankingKeepsLocalAndRequiresStableImprovement(t *testing.T) {
	now := time.Now().UTC()
	policy, observation, order := rankingInput(now, 200000, 80000, 50000)
	state := ProxyRoutingState{Order: order}
	for i := range 2 {
		if got := nextProxyOrder("0", policy, observation, &state, now.Add(time.Duration(i)*30*time.Second)); !slices.Equal(got, order) {
			t.Fatal("短暂波动触发了切换", got)
		}
	}
	wanted := []string{order[0], order[2], order[1]}
	if got := nextProxyOrder("0", policy, observation, &state, now.Add(time.Minute)); !slices.Equal(got, wanted) {
		t.Fatal("未按稳定延迟排序或丢失本机优先", got)
	}
	changed := now
	state = ProxyRoutingState{Order: order, ChangedAt: &changed}
	for i := range 4 {
		if got := nextProxyOrder("0", policy, observation, &state, now.Add(time.Duration(i)*30*time.Second)); !slices.Equal(got, order) {
			t.Fatal("保持时间内发生主动切换", got)
		}
	}
	if got := nextProxyOrder("0", policy, observation, &state, now.Add(5*time.Minute)); !slices.Equal(got, wanted) {
		t.Fatal(got)
	}
}

func TestProxyRankingIgnoresSmallStaleAndMissingSamples(t *testing.T) {
	now := time.Now().UTC()
	for _, latencies := range [][]int64{{100, 80000, 75000}, {100, 80000, 70000}} {
		policy, observation, order := rankingInput(now, latencies...)
		state := ProxyRoutingState{Order: order}
		for range 5 {
			if got := nextProxyOrder("0", policy, observation, &state, now); !slices.Equal(got, order) {
				t.Fatal("未同时满足绝对与相对改善阈值", got)
			}
		}
	}
	policy, observation, order := rankingInput(now, 100, 80000, 1000)
	for i := range observation.Samples {
		if observation.Samples[i].Host == policy.Backends[2].Host {
			observation.Samples[i].At = now.Add(-90 * time.Second)
		}
	}
	state := ProxyRoutingState{Order: order}
	for range 5 {
		if got := nextProxyOrder("0", policy, observation, &state, now); !slices.Equal(got, order) {
			t.Fatal("旧样本被用于主动切换", got)
		}
	}
	observation.Samples = nil
	if got := nextProxyOrder("0", policy, observation, &state, now); !slices.Equal(got, order) {
		t.Fatal("未知延迟被当作零", got)
	}
}

func TestProxyRankingKeepsCloseRemoteMembersInTheirPreviousOrder(t *testing.T) {
	now := time.Now().UTC()
	policy, observation, previous := rankingInput(now, 100, 100000, 80000, 79000)
	state := ProxyRoutingState{Order: previous}
	for range 2 {
		nextProxyOrder("0", policy, observation, &state, now)
	}
	got := nextProxyOrder("0", policy, observation, &state, now)
	if !slices.Equal(got, []string{previous[0], previous[2], previous[3], previous[1]}) {
		t.Fatal("大幅改善顺带改变了接近延迟成员的顺序", got)
	}
}

func TestProxyRankingRemainsBoundedWithTwentyAndHundredMembers(t *testing.T) {
	for _, count := range []int{20, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			latencies := make([]int64, count)
			for i := range latencies {
				latencies[i] = int64(count-i) * 10000
			}
			policy, observation, previous := rankingInput(time.Now().UTC(), latencies...)
			order := initialProxyOrder("0", policy, observation, previous)
			action := proxyRouteAction(policy, observation, order)
			if len(order) != count || len(action.Servers) != count || order[0] != previous[0] {
				t.Fatal("成员缺失或本机优先错误", len(order))
			}
			seen := map[string]bool{}
			for _, server := range action.Servers {
				if seen[server.Key()] || server.Weight < 1 {
					t.Fatal("后端重复或权重无效", server)
				}
				seen[server.Key()] = true
			}
		})
	}
}
