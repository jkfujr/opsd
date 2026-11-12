package cluster

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"time"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
)

type ProxyRankEntry struct {
	protocol.ProxyBackend
	Rank      int        `json:"rank"`
	Local     bool       `json:"local"`
	LatencyUS *int64     `json:"latency_us,omitempty"`
	Samples   int        `json:"samples"`
	SampledAt *time.Time `json:"sampled_at,omitempty"`
}

type ProxyRoutingState struct {
	NodeID            string                     `json:"node_id"`
	InstanceID        string                     `json:"instance_id"`
	State             string                     `json:"state"`
	PolicyVersion     string                     `json:"policy_version"`
	ConfigFingerprint string                     `json:"config_fingerprint"`
	Order             []string                   `json:"order"`
	Entries           []ProxyRankEntry           `json:"entries"`
	CurrentWriter     *protocol.ProxyEndpoint    `json:"current_writer,omitempty"`
	CheckedAt         *time.Time                 `json:"checked_at,omitempty"`
	ChangedAt         *time.Time                 `json:"changed_at,omitempty"`
	OperationID       string                     `json:"operation_id,omitempty"`
	Error             *protocol.Error            `json:"error,omitempty"`
	Candidate         []string                   `json:"candidate,omitempty"`
	CandidateRounds   int                        `json:"candidate_rounds,omitempty"`
	PendingAction     *protocol.ProxyRouteAction `json:"pending_action,omitempty"`
}

func (n *Node) proxyRoutingState(ctx context.Context, cluster, id string) (ProxyRoutingState, error) {
	state := ProxyRoutingState{NodeID: n.ID, InstanceID: id, State: "unmanaged", Entries: []ProxyRankEntry{}, Order: []string{}}
	var body string
	err := n.DB.QueryRowContext(ctx, "SELECT body FROM proxy_routing_state WHERE cluster_id=? AND instance_id=?", cluster, id).Scan(&body)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(body), &state)
	}
	return state, err
}

func (n *Node) saveProxyRoutingState(ctx context.Context, cluster string, state ProxyRoutingState) error {
	previous, err := n.proxyRoutingState(ctx, cluster, state.InstanceID)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(state)
	_, err = n.DB.ExecContext(ctx, "INSERT INTO proxy_routing_state(cluster_id,instance_id,body) VALUES(?,?,?) ON CONFLICT(cluster_id,instance_id) DO UPDATE SET body=excluded.body", cluster, state.InstanceID, string(b))
	before, after := previous, state
	before.CheckedAt, after.CheckedAt = nil, nil
	if err == nil && !reflect.DeepEqual(before, after) {
		n.notifyChange(Change{Resource: "databases", NodeID: n.ID})
	}
	return err
}

func proxyRankEntries(node string, policy protocol.ProxyRoutingPolicy, observation protocol.ProxyRoutingObservation, order []string) []ProxyRankEntry {
	out := make([]ProxyRankEntry, 0, len(order))
	for i, key := range order {
		for _, backend := range policy.Backends {
			if backend.Key() != key {
				continue
			}
			entry := ProxyRankEntry{ProxyBackend: backend, Rank: i + 1, Local: backend.NodeID == node}
			values := []int64{}
			var latest time.Time
			for _, sample := range observation.Samples {
				if sample.ProxyEndpoint != backend.ProxyEndpoint || sample.At.Before(observation.CheckedAt.Add(-120*time.Second)) || sample.At.After(observation.CheckedAt.Add(time.Second)) || sample.LatencyUS <= 0 {
					continue
				}
				values = append(values, sample.LatencyUS)
				if sample.At.After(latest) {
					latest = sample.At
				}
			}
			entry.Samples = len(values)
			if !latest.IsZero() {
				entry.SampledAt = &latest
			}
			if len(values) >= 3 && !latest.Before(observation.CheckedAt.Add(-60*time.Second)) {
				slices.Sort(values)
				median := values[len(values)/2]
				if len(values)%2 == 0 {
					median = values[len(values)/2-1] + (median-values[len(values)/2-1])/2
				}
				entry.LatencyUS = &median
			}
			out = append(out, entry)
		}
	}
	return out
}

func initialProxyOrder(node string, policy protocol.ProxyRoutingPolicy, observation protocol.ProxyRoutingObservation, previous []string) []string {
	order := []string{}
	for _, key := range previous {
		if slices.ContainsFunc(policy.Backends, func(backend protocol.ProxyBackend) bool { return backend.Key() == key }) && !slices.Contains(order, key) {
			order = append(order, key)
		}
	}
	for _, backend := range policy.Backends {
		if !slices.Contains(order, backend.Key()) {
			order = append(order, backend.Key())
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		local := func(key string) bool {
			return slices.ContainsFunc(policy.Backends, func(backend protocol.ProxyBackend) bool { return backend.Key() == key && backend.NodeID == node })
		}
		return local(order[i]) && !local(order[j])
	})
	entries := proxyRankEntries(node, policy, observation, order)
	known := []ProxyRankEntry{}
	for _, entry := range entries {
		if !entry.Local && entry.LatencyUS != nil {
			known = append(known, entry)
		}
	}
	sort.SliceStable(known, func(i, j int) bool { return *known[i].LatencyUS < *known[j].LatencyUS })
	index := 0
	for i, entry := range entries {
		if !entry.Local && entry.LatencyUS != nil {
			order[i] = known[index].Key()
			index++
		}
	}
	return order
}

func nextProxyOrder(node string, policy protocol.ProxyRoutingPolicy, observation protocol.ProxyRoutingObservation, state *ProxyRoutingState, now time.Time) []string {
	current := slices.Clone(state.Order)
	entries := proxyRankEntries(node, policy, observation, current)
	state.Entries = entries
	wanted := slices.Clone(current)
	positions := []int{}
	known := []ProxyRankEntry{}
	for i, entry := range entries {
		if !entry.Local && entry.LatencyUS != nil {
			positions, known = append(positions, i), append(known, entry)
		}
	}
	// 每一对跨越都满足改善阈值，保留未知样本位置及接近延迟的原顺序。
	for i := 1; i < len(known); i++ {
		for j := i; j > 0; j-- {
			previous, next := *known[j-1].LatencyUS, *known[j].LatencyUS
			if previous-next < 10000 || (previous-next)*100 < previous*20 {
				break
			}
			known[j-1], known[j] = known[j], known[j-1]
		}
	}
	for i, position := range positions {
		wanted[position] = known[i].Key()
	}
	if slices.Equal(wanted, current) {
		state.Candidate, state.CandidateRounds = nil, 0
		return current
	}
	if slices.Equal(state.Candidate, wanted) {
		state.CandidateRounds++
	} else {
		state.Candidate, state.CandidateRounds = wanted, 1
	}
	if state.CandidateRounds < 3 || state.ChangedAt != nil && now.Sub(*state.ChangedAt) < 5*time.Minute {
		return current
	}
	return wanted
}

func (n *Node) noteProxyRouteApplied(ctx context.Context, cluster, id, operation string, action *protocol.ProxyRouteAction) error {
	if action == nil {
		return fault("invalid_request", "缺少原代理路由步骤")
	}
	n.proxyMu.Lock()
	defer n.proxyMu.Unlock()
	var state protocol.OperationState
	if op, err := n.operation(ctx, operation); err == nil {
		state = op.State
	} else {
		reply, err := n.executeRead(ctx, &protocol.OperationQuery{OperationID: operation})
		if err != nil {
			return err
		}
		result, ok := reply.(*protocol.OperationQueryResult)
		if !ok || result.Record == nil {
			return fault("conflict", "原代理操作结果待核对")
		}
		state = result.Record.State
	}
	if state != protocol.Succeeded {
		return fault("conflict", "原代理操作尚未完成")
	}
	observation, err := n.readProxyRouting(ctx, id, &action.Hostgroups)
	if err != nil {
		return err
	}
	if !databaseengine.ProxyRouteActionSatisfied(observation, *action) {
		return fault("conflict", "代理配置尚未完整读回")
	}
	local, err := n.proxyRoutingState(ctx, cluster, id)
	if err != nil {
		return err
	}
	setProxyApplied(&local, observation, *action, operation, n.Now().UTC())
	return n.saveProxyRoutingState(ctx, cluster, local)
}

func setProxyApplied(state *ProxyRoutingState, observation protocol.ProxyRoutingObservation, action protocol.ProxyRouteAction, operation string, now time.Time) {
	servers := slices.Clone(action.Servers)
	sort.Slice(servers, func(i, j int) bool { return servers[i].Weight > servers[j].Weight })
	order := []string{}
	for _, server := range servers {
		order = append(order, server.Key())
	}
	if !slices.Equal(state.Order, order) {
		state.ChangedAt = &now
	}
	state.Order, state.PolicyVersion, state.ConfigFingerprint = order, action.PolicyVersion, observation.Fingerprint
	state.PendingAction, state.Candidate, state.CandidateRounds = nil, nil, 0
	state.OperationID, state.State, state.Error = operation, "ready", nil
	state.CheckedAt = &observation.CheckedAt
	state.CurrentWriter = currentProxyWriter(observation)
}

func currentProxyWriter(observation protocol.ProxyRoutingObservation) *protocol.ProxyEndpoint {
	for _, server := range observation.RuntimeServers {
		if server.Hostgroup == observation.Hostgroups.Writer && server.State == "ONLINE" {
			endpoint := server.ProxyEndpoint
			return &endpoint
		}
	}
	return nil
}

func (n *Node) proxyRoutingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		clusters, err := n.DatabaseClusters(n.ctx)
		if err == nil {
			for _, c := range clusters {
				if c.Deleted || c.ProxyRouting == nil || n.Revoked(n.ctx, c.Coordinator) {
					continue
				}
				for _, ref := range c.Proxies {
					if ref.NodeID != n.ID {
						continue
					}
					ctx, cancel := context.WithTimeout(n.ctx, 12*time.Second)
					n.reconcileProxyRouting(ctx, c, ref)
					cancel()
				}
			}
		}
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *Node) reconcileProxyRouting(ctx context.Context, c ManagedDatabaseCluster, ref DatabaseRef) {
	n.proxyMu.Lock()
	defer n.proxyMu.Unlock()
	state, err := n.proxyRoutingState(ctx, c.ID, ref.InstanceID)
	if err != nil {
		return
	}
	save := func() { _ = n.saveProxyRoutingState(ctx, c.ID, state) }
	fail := func(code, message string) {
		state.State, state.Error = code, &protocol.Error{Code: code, Message: message}
		save()
	}
	if !c.ProxyRouting.Enabled {
		state.State = "paused"
		state.Error = nil
		save()
		return
	}
	if !routingPolicyMatches(c, *c.ProxyRouting) {
		fail("conflict", "数据库成员已变化，请重新预览代理路由")
		return
	}
	if state.PolicyVersion != routingVersion(*c.ProxyRouting) {
		state.State = "pending"
		save()
		return
	}
	if state.PendingAction != nil {
		op, err := n.operation(ctx, state.OperationID)
		if err != nil {
			fail("conflict", "原代理操作记录待核对")
			return
		}
		if !operations.Terminal(op.State) {
			state.State = "applying"
			if op.State == protocol.AttentionRequired {
				state.State, state.Error = "conflict", op.Error
			}
			save()
			return
		}
		if op.State != protocol.Succeeded {
			fail("conflict", "原代理操作未完成，请核对后重新预览")
			return
		}
		observation, err := n.readProxyRouting(ctx, ref.InstanceID, &c.ProxyRouting.Hostgroups)
		if err != nil {
			fail("error", "代理路由读取失败")
			return
		}
		if !databaseengine.ProxyRouteActionSatisfied(observation, *state.PendingAction) {
			fail("conflict", "原代理操作结果待核对")
			return
		}
		setProxyApplied(&state, observation, *state.PendingAction, op.ID, n.Now().UTC())
		save()
		return
	}
	if state.State == "conflict" {
		return
	}
	instance, err := n.databaseInstance(ctx, ref.InstanceID)
	if err != nil || instance.Revision != ref.InstanceRevision {
		fail("conflict", "代理连接配置已变化，请重新预览")
		return
	}
	observation, err := n.readProxyRouting(ctx, ref.InstanceID, &c.ProxyRouting.Hostgroups)
	if err != nil {
		fail("error", "代理路由读取失败")
		return
	}
	state.CheckedAt, state.CurrentWriter = &observation.CheckedAt, currentProxyWriter(observation)
	if observation.ClusterSync || observation.SplitRouting || observation.PendingChanges || observation.Fingerprint != state.ConfigFingerprint {
		fail("conflict", "代理配置出现外部差异，请重新预览")
		return
	}
	state.State, state.Error = "ready", nil
	order := nextProxyOrder(n.ID, *c.ProxyRouting, observation, &state, n.Now())
	if !c.ProxyRouting.Automatic || slices.Equal(order, state.Order) {
		save()
		return
	}
	var busy int
	if err = n.DB.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE terminal=0").Scan(&busy); err != nil {
		return
	}
	if busy > 0 {
		save()
		return
	}
	action := proxyRouteAction(*c.ProxyRouting, observation, order)
	connection, err := n.databaseConnection(ctx, ref.InstanceID)
	if err != nil {
		fail("error", "代理连接配置不可用")
		return
	}
	payload, _ := json.Marshal(protocol.DatabaseAction{Connection: connection, Kind: "proxy_route", Routing: &action})
	sum := sha256.Sum256([]byte(c.ID + "/" + ref.InstanceID + "/" + state.OperationID + "/" + action.PolicyVersion + "/" + fmt.Sprint(order) + "/" + observation.Fingerprint))
	key := "proxy-routing-" + hex.EncodeToString(sum[:])
	op, err := n.enqueue(ctx, n.ID, Request{Key: key, Kind: "database_step", ResourceID: c.ID, Payload: payload})
	if err != nil {
		fail("error", "自动排序请求未受理")
		return
	}
	state.OperationID, state.PendingAction, state.State = op.ID, &action, "applying"
	save()
}
