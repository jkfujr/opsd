package cluster

import (
	"context"
	"fmt"
	"slices"
	"strings"
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

type routingObservationExecutor struct {
	lostExecutor
	observation protocol.ProxyRoutingObservation
}

func (e *routingObservationExecutor) Call(ctx context.Context, value protocol.Payload) (protocol.Payload, error) {
	if _, ok := value.(*protocol.ProxyRoutingRead); ok {
		return &protocol.ProxyRoutingReadResult{Routing: e.observation}, nil
	}
	return e.lostExecutor.Call(ctx, value)
}

func TestAutomaticProxyRankingCreatesNewOperationsWhenOrderCycles(t *testing.T) {
	n, _ := openTestNode(t)
	now := time.Now().UTC()
	n.Now = func() time.Time { return now }
	password := "test-password"
	instance, err := n.saveDatabaseInstance(t.Context(), DatabaseInstanceInput{DatabaseInstance: DatabaseInstance{ID: "proxy", Name: "代理", Kind: "proxysql", Address: "127.0.0.1:6032", Username: "admin"}, Password: &password})
	if err != nil {
		t.Fatal(err)
	}
	policy, observation, order := rankingInput(now, 100, 80000, 50000)
	observation.Fingerprint, observation.UnmanagedFingerprint = strings.Repeat("a", 64), strings.Repeat("f", 64)
	runtime := &routingObservationExecutor{observation: observation}
	n.Executor = runtime
	ref := DatabaseRef{NodeID: n.ID, InstanceID: instance.ID, InstanceRevision: instance.Revision}
	c := ManagedDatabaseCluster{ID: NewOperationID(n.ID), Coordinator: n.ID, ProxyRouting: &policy, Proxies: []DatabaseRef{ref}}
	for _, backend := range policy.Backends {
		c.Members = append(c.Members, DatabaseRef{NodeID: backend.NodeID, InstanceID: backend.InstanceID})
	}
	state := ProxyRoutingState{NodeID: n.ID, InstanceID: ref.InstanceID, State: "ready", PolicyVersion: routingVersion(policy), ConfigFingerprint: observation.Fingerprint, Order: order, Entries: []ProxyRankEntry{}}
	if err = n.saveProxyRoutingState(t.Context(), c.ID, state); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for cycle := range 3 {
		latencies := []int64{100, 80000, 50000}
		if cycle == 1 {
			latencies = []int64{100, 50000, 80000}
		}
		_, samples, _ := rankingInput(now, latencies...)
		runtime.observation.Samples, runtime.observation.CheckedAt = samples.Samples, now
		for range 3 {
			n.reconcileProxyRouting(t.Context(), c, ref)
		}
		state, err = n.proxyRoutingState(t.Context(), c.ID, ref.InstanceID)
		if err != nil || state.PendingAction == nil || state.OperationID == "" || seen[state.OperationID] {
			t.Fatal("排序循环复用了已完成操作", cycle, state, err)
		}
		seen[state.OperationID] = true
		op, err := n.operation(t.Context(), state.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		op.State, op.Revision = protocol.Succeeded, op.Revision+1
		if err = n.saveOperation(t.Context(), &op, "finished"); err != nil {
			t.Fatal(err)
		}
		runtime.observation.Fingerprint = strings.Repeat("b", 64)
		if cycle == 1 {
			runtime.observation.Fingerprint = strings.Repeat("a", 64)
		}
		setProxyApplied(&state, runtime.observation, *state.PendingAction, op.ID, now)
		if err = n.saveProxyRoutingState(t.Context(), c.ID, state); err != nil {
			t.Fatal(err)
		}
		now = now.Add(10 * time.Minute)
	}
	var count int
	if err = n.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM queue WHERE kind='database_step'").Scan(&count); err != nil || count != 3 {
		t.Fatal("排序操作数量不同", count, err)
	}
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
