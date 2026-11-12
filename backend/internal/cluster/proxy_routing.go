package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type ProxyRouteDiff struct {
	Kind string `json:"kind"`
	protocol.ProxyEndpoint
	BeforeWeight int `json:"before_weight"`
	AfterWeight  int `json:"after_weight"`
}

type ProxyRoutePreview struct {
	NodeID           string                    `json:"node_id"`
	InstanceID       string                    `json:"instance_id"`
	InstanceRevision uint64                    `json:"instance_revision"`
	Action           protocol.ProxyRouteAction `json:"action"`
	Changes          []ProxyRouteDiff          `json:"changes"`
}

type ProxyRoutingPreview struct {
	ExpectedRevision uint64                      `json:"expected_revision"`
	Policy           protocol.ProxyRoutingPolicy `json:"policy"`
	Targets          []ProxyRoutePreview         `json:"targets"`
}

type ProxyRoutingPreviewInput struct {
	ExpectedRevision uint64                  `json:"expected_revision"`
	Enabled          bool                    `json:"enabled"`
	Automatic        bool                    `json:"automatic"`
	Backends         []protocol.ProxyBackend `json:"backends,omitempty"`
}

type ProxyRoutingStatus struct {
	Policy  *protocol.ProxyRoutingPolicy `json:"policy,omitempty"`
	Proxies []ProxyRoutingState          `json:"proxies"`
}

func routingVersion(policy protocol.ProxyRoutingPolicy) string {
	b, _ := json.Marshal(policy)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (n *Node) readProxyRouting(ctx context.Context, id string, groups *protocol.ProxyHostgroups) (protocol.ProxyRoutingObservation, error) {
	c, err := n.databaseConnection(ctx, id)
	if err != nil {
		return protocol.ProxyRoutingObservation{}, err
	}
	reply, err := n.executeRead(ctx, &protocol.ProxyRoutingRead{Connection: c, Hostgroups: groups})
	if err != nil {
		return protocol.ProxyRoutingObservation{}, err
	}
	out, ok := reply.(*protocol.ProxyRoutingReadResult)
	if !ok {
		return protocol.ProxyRoutingObservation{}, fault("dependency_unavailable", "代理路由读取应答无效")
	}
	return out.Routing, nil
}

func (n *Node) GetProxyRouting(ctx context.Context, c ManagedDatabaseCluster) (ProxyRoutingStatus, error) {
	out := ProxyRoutingStatus{Policy: c.ProxyRouting, Proxies: make([]ProxyRoutingState, len(c.Proxies))}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i, ref := range c.Proxies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			err := n.Call(readCtx, ref.NodeID, "database.proxy_routing.state", DatabaseRPC{ResourceID: c.ID, InstanceID: ref.InstanceID}, &out.Proxies[i])
			if err != nil {
				out.Proxies[i] = ProxyRoutingState{NodeID: ref.NodeID, InstanceID: ref.InstanceID, State: "offline", Error: wireError(err), Entries: []ProxyRankEntry{}, Order: []string{}}
			}
		}()
	}
	wg.Wait()
	return out, ctx.Err()
}

func (n *Node) PreviewProxyRouting(ctx context.Context, c ManagedDatabaseCluster, input ProxyRoutingPreviewInput) (ProxyRoutingPreview, error) {
	out := ProxyRoutingPreview{ExpectedRevision: c.Revision, Targets: []ProxyRoutePreview{}}
	if c.Revision != input.ExpectedRevision {
		return out, fault("precondition_failed", "数据库集群配置已经变化")
	}
	if len(c.Proxies) == 0 {
		return out, fault("invalid_request", "请先登记 ProxySQL 实例")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if c.ProxyRouting == nil {
		if err := n.requireProxyRoutingSupport(ctx); err != nil {
			return out, err
		}
	}
	observations := make([]protocol.ProxyRoutingObservation, len(c.Proxies))
	instances := make([]DatabaseInstance, len(c.Proxies))
	var groups *protocol.ProxyHostgroups
	if c.ProxyRouting != nil {
		value := c.ProxyRouting.Hostgroups
		groups = &value
	}
	for i, ref := range c.Proxies {
		if err := n.Call(ctx, ref.NodeID, "database.instance.get", DatabaseRPC{InstanceID: ref.InstanceID}, &instances[i]); err != nil {
			return out, err
		}
		if err := n.Call(ctx, ref.NodeID, "database.proxy_routing.read", DatabaseRPC{InstanceID: ref.InstanceID, Hostgroups: groups}, &observations[i]); err != nil {
			return out, err
		}
		if groups == nil {
			value := observations[i].Hostgroups
			groups = &value
		}
		if observations[i].PendingChanges {
			return out, fault("conflict", ref.Name+" 存在待应用的后端配置")
		}
		if observations[i].ClusterSync {
			return out, fault("conflict", ref.Name+" 已启用 ProxySQL 后端配置全量同步，请先停用")
		}
		if observations[i].SplitRouting {
			return out, fault("conflict", ref.Name+" 的查询规则包含其他读写主机组，请先调整路由")
		}
	}
	policy := protocol.ProxyRoutingPolicy{Enabled: input.Enabled, Automatic: input.Automatic, Hostgroups: *groups, Backends: []protocol.ProxyBackend{}, Retired: []protocol.ProxyEndpoint{}, Accounts: []protocol.ProxyAccountBinding{}}
	if len(input.Backends) > 0 {
		policy.Backends = input.Backends
	} else {
		for _, ref := range c.Members {
			if c.ProxyRouting != nil {
				index := slices.IndexFunc(c.ProxyRouting.Backends, func(backend protocol.ProxyBackend) bool {
					return backend.NodeID == ref.NodeID && backend.InstanceID == ref.InstanceID
				})
				if index >= 0 {
					policy.Backends = append(policy.Backends, c.ProxyRouting.Backends[index])
					continue
				}
			}
			var result protocol.DatabaseReadResult
			if err := n.Call(ctx, ref.NodeID, "database.read", DatabaseRPC{InstanceID: ref.InstanceID, Scope: "health"}, &result); err != nil {
				return out, err
			}
			if result.State.ClusterID != c.ClusterID {
				return out, fault("conflict", "数据库成员与实际 Galera 集群关联不一致")
			}
			host, port, err := net.SplitHostPort(result.State.MemberAddress)
			if err != nil {
				host, port = result.State.MemberAddress, "3306"
			}
			number, err := strconv.Atoi(port)
			if err != nil || host == "" {
				return out, fault("invalid_request", "请为 "+ref.Name+" 设置代理可达 SQL 地址")
			}
			policy.Backends = append(policy.Backends, protocol.ProxyBackend{ProxyEndpoint: protocol.ProxyEndpoint{Host: host, Port: number}, NodeID: ref.NodeID, InstanceID: ref.InstanceID})
		}
	}
	sort.Slice(policy.Backends, func(i, j int) bool {
		return policy.Backends[i].NodeID+policy.Backends[i].InstanceID < policy.Backends[j].NodeID+policy.Backends[j].InstanceID
	})
	retired := map[string]protocol.ProxyEndpoint{}
	if c.ProxyRouting != nil {
		for _, endpoint := range c.ProxyRouting.Retired {
			retired[endpoint.Key()] = endpoint
		}
	}
	for i, observation := range observations {
		for _, server := range observation.Servers {
			if policy.Hostgroups.Contains(server.Hostgroup) && !slices.ContainsFunc(policy.Backends, func(backend protocol.ProxyBackend) bool { return backend.ProxyEndpoint == server.ProxyEndpoint }) {
				retired[server.Key()] = server.ProxyEndpoint
			}
		}
		for _, name := range observation.Accounts {
			policy.Accounts = append(policy.Accounts, protocol.ProxyAccountBinding{Name: name, NodeID: c.Proxies[i].NodeID, InstanceID: c.Proxies[i].InstanceID})
		}
	}
	for _, endpoint := range retired {
		if !slices.ContainsFunc(policy.Backends, func(backend protocol.ProxyBackend) bool { return backend.ProxyEndpoint == endpoint }) {
			policy.Retired = append(policy.Retired, endpoint)
		}
	}
	sort.Slice(policy.Retired, func(i, j int) bool { return policy.Retired[i].Key() < policy.Retired[j].Key() })
	if err := policy.Validate(); err != nil {
		return out, fault("invalid_request", err.Error())
	}
	if !routingPolicyMatches(c, policy) {
		return out, fault("conflict", "代理后端须与已登记数据库成员对应")
	}
	out.Policy = policy
	for i, ref := range c.Proxies {
		state, _ := n.readProxyRoutingState(ctx, c.ID, ref)
		order := initialProxyOrder(ref.NodeID, policy, observations[i], state.Order)
		if !policy.Automatic && len(state.Order) == len(policy.Backends) {
			order = slices.Clone(state.Order)
		}
		action := proxyRouteAction(policy, observations[i], order)
		changes := proxyRouteChanges(observations[i], action)
		out.Targets = append(out.Targets, ProxyRoutePreview{NodeID: ref.NodeID, InstanceID: ref.InstanceID, InstanceRevision: instances[i].Revision, Action: action, Changes: changes})
	}
	return out, nil
}

func routingPolicyMatches(c ManagedDatabaseCluster, policy protocol.ProxyRoutingPolicy) bool {
	if len(c.Members) != len(policy.Backends) {
		return false
	}
	for _, ref := range c.Members {
		if !slices.ContainsFunc(policy.Backends, func(backend protocol.ProxyBackend) bool {
			return backend.NodeID == ref.NodeID && backend.InstanceID == ref.InstanceID
		}) {
			return false
		}
	}
	return true
}

func proxyRouteAction(policy protocol.ProxyRoutingPolicy, observation protocol.ProxyRoutingObservation, order []string) protocol.ProxyRouteAction {
	action := protocol.ProxyRouteAction{Hostgroups: policy.Hostgroups, Servers: []protocol.ProxyRouteServer{}, Remove: slices.Clone(policy.Retired), MaxTransactionsBehind: 10, ExpectedFingerprint: observation.Fingerprint, UnmanagedFingerprint: observation.UnmanagedFingerprint, PolicyVersion: routingVersion(policy)}
	for _, mapping := range observation.Mappings {
		if mapping.Hostgroups.Writer == policy.Hostgroups.Writer {
			action.MaxTransactionsBehind = mapping.MaxTransactionsBehind
		}
	}
	for i, key := range order {
		for _, backend := range policy.Backends {
			if backend.Key() != key {
				continue
			}
			server := protocol.ProxyRouteServer{ProxyEndpoint: backend.ProxyEndpoint, Hostgroup: policy.Hostgroups.Writer, State: "ONLINE", Weight: 1000 - i*10, MaxConnections: 100}
			for _, actual := range observation.Servers {
				if policy.Hostgroups.Contains(actual.Hostgroup) && actual.ProxyEndpoint == backend.ProxyEndpoint {
					server.MaxConnections, server.UseSSL = actual.MaxConnections, actual.UseSSL
					break
				}
			}
			action.Servers = append(action.Servers, server)
		}
	}
	return action
}

func proxyRouteChanges(observation protocol.ProxyRoutingObservation, action protocol.ProxyRouteAction) []ProxyRouteDiff {
	changes := []ProxyRouteDiff{}
	for _, desired := range action.Servers {
		index := slices.IndexFunc(observation.Servers, func(actual protocol.ProxyRouteServer) bool {
			return actual.Hostgroup == action.Hostgroups.Writer && actual.ProxyEndpoint == desired.ProxyEndpoint
		})
		kind, before := "add", 0
		if index >= 0 {
			kind, before = "weight", observation.Servers[index].Weight
		}
		if kind == "add" || before != desired.Weight {
			changes = append(changes, ProxyRouteDiff{Kind: kind, ProxyEndpoint: desired.ProxyEndpoint, BeforeWeight: before, AfterWeight: desired.Weight})
		}
	}
	for _, removed := range action.Remove {
		if slices.ContainsFunc(observation.Servers, func(actual protocol.ProxyRouteServer) bool {
			return action.Hostgroups.Contains(actual.Hostgroup) && actual.ProxyEndpoint == removed
		}) {
			changes = append(changes, ProxyRouteDiff{Kind: "remove", ProxyEndpoint: removed})
		}
	}
	for _, mapping := range observation.Mappings {
		if mapping.Hostgroups == action.Hostgroups && mapping.MaxWriters == 1 && mapping.WriterIsAlsoReader == 0 {
			return changes
		}
	}
	return append(changes, ProxyRouteDiff{Kind: "hostgroups"})
}

func validateRoutingSubmission(input DatabaseSubmission) error {
	c, d := input.Cluster, input.Change
	if c.ID == "" || c.Deleted || d.Cluster != c.ID || c.Revision != d.ExpectedRevision || d.Routing == nil || len(d.RoutingTargets) != len(c.Proxies) {
		return fault("invalid_request", "代理路由请求无效")
	}
	if err := d.Routing.Validate(); err != nil {
		return fault("invalid_request", err.Error())
	}
	if !routingPolicyMatches(c, *d.Routing) {
		return fault("conflict", "代理后端与当前数据库成员不一致")
	}
	seen := map[string]bool{}
	for _, target := range d.RoutingTargets {
		key := target.NodeID + "/" + target.InstanceID
		if seen[key] || target.InstanceRevision == 0 || !slices.ContainsFunc(c.Proxies, func(ref DatabaseRef) bool { return ref.NodeID == target.NodeID && ref.InstanceID == target.InstanceID }) {
			return fault("invalid_request", "代理目标无效或重复")
		}
		if !slices.ContainsFunc(c.Proxies, func(ref DatabaseRef) bool {
			return ref.NodeID == target.NodeID && ref.InstanceID == target.InstanceID && ref.InstanceRevision == target.InstanceRevision
		}) {
			return fault("precondition_failed", "代理连接配置已变化，请重新预览")
		}
		seen[key] = true
		if err := target.Action.Validate(); err != nil {
			return fault("invalid_request", err.Error())
		}
		if target.Action.PolicyVersion != routingVersion(*d.Routing) || target.Action.Hostgroups != d.Routing.Hostgroups || len(target.Action.Servers) != len(d.Routing.Backends) || !reflect.DeepEqual(target.Action.Remove, d.Routing.Retired) {
			return fault("conflict", "代理策略与预览内容不一致")
		}
		for _, server := range target.Action.Servers {
			if !slices.ContainsFunc(d.Routing.Backends, func(backend protocol.ProxyBackend) bool { return backend.ProxyEndpoint == server.ProxyEndpoint }) {
				return fault("invalid_request", "代理后端超出集群范围")
			}
		}
	}
	return nil
}

func (n *Node) ApplyProxyRouting(ctx context.Context, c ManagedDatabaseCluster, preview ProxyRoutingPreview, actor, key, digest string) (Operation, error) {
	change := DatabaseChange{Cluster: c.ID, ExpectedRevision: preview.ExpectedRevision, Kind: "configure_proxy_routes", Routing: &preview.Policy, RoutingTargets: preview.Targets, Proxies: []DatabaseProxySelection{}}
	for _, target := range preview.Targets {
		change.Proxies = append(change.Proxies, DatabaseProxySelection{NodeID: target.NodeID, InstanceID: target.InstanceID, Hostgroup: preview.Policy.Hostgroups.Writer})
	}
	return n.SubmitDatabaseChange(ctx, change, actor, key, digest)
}

// 新策略会进入签名目录，首次接管前确认所有有效成员都能解析同一字段。
func (n *Node) requireProxyRoutingSupport(ctx context.Context) error {
	directory, err := n.Directory(ctx)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failure error
	slots := make(chan struct{}, 8)
	for _, member := range directory.Members {
		if n.Revoked(ctx, member.ID) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			check, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			var capability struct {
				Version int `json:"proxy_routing_version"`
			}
			err := n.Call(check, member.ID, "api.capabilities", nil, &capability)
			if err != nil || capability.Version < 1 {
				mu.Lock()
				if failure == nil {
					failure = fault("conflict", member.Name+" 尚未确认代理路由支持，请先更新并连接该成员")
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return failure
}

func (n *Node) finishProxyRoutingPolicy(ctx context.Context, tx *sql.Tx, id string) error {
	var encrypted string
	if err := tx.QueryRowContext(ctx, "SELECT payload FROM queue WHERE id=?", id).Scan(&encrypted); err != nil {
		return err
	}
	var secret Secret
	if err := json.Unmarshal([]byte(encrypted), &secret); err != nil {
		return err
	}
	plain, err := unseal(secret, n.age)
	if err != nil {
		return err
	}
	var input DatabaseSubmission
	if err = json.Unmarshal(plain, &input); err != nil {
		return err
	}
	var body string
	if err = tx.QueryRowContext(ctx, "SELECT body FROM managed_database_clusters WHERE id=?", input.Cluster.ID).Scan(&body); err != nil {
		return err
	}
	var current ManagedDatabaseCluster
	if err = json.Unmarshal([]byte(body), &current); err != nil {
		return err
	}
	if current.Revision != input.Change.ExpectedRevision {
		return fault("conflict", "集群配置已变化，请核对代理配置后重新预览")
	}
	current.ProxyRouting = input.Change.Routing
	for i, ref := range current.Proxies {
		for _, applied := range input.Cluster.Proxies {
			if ref.NodeID == applied.NodeID && ref.InstanceID == applied.InstanceID {
				current.Proxies[i].InstanceRevision = applied.InstanceRevision
			}
		}
	}
	current.Revision++
	current.UpdatedAt = n.Now().UTC()
	current.Signature = ed25519.Sign(n.signing, databaseClusterBytes(current))
	b, _ := json.Marshal(current)
	if _, err = tx.ExecContext(ctx, "UPDATE managed_database_clusters SET body=?,revision=? WHERE id=?", string(b), current.Revision, current.ID); err != nil {
		return err
	}
	return n.appendTx(ctx, tx, "database_cluster", current.ID, current, n.Now())
}

func (n *Node) readProxyRoutingState(ctx context.Context, cluster string, ref DatabaseRef) (ProxyRoutingState, error) {
	var state ProxyRoutingState
	err := n.Call(ctx, ref.NodeID, "database.proxy_routing.state", DatabaseRPC{ResourceID: cluster, InstanceID: ref.InstanceID}, &state)
	return state, err
}
