package cluster

import (
	"context"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/protocol"
)

type managedDatabaseReadEntry struct {
	Done  chan struct{}
	At    time.Time
	Value protocol.DatabaseReadResult
	TTL   time.Duration
	Err   error
}

func (n *Node) cachedManagedDatabaseRead(ctx context.Context, id, scope string) (protocol.DatabaseReadResult, error) {
	key := id + "/" + scope
	ttl := 30 * time.Second
	if scope == "" || scope == "health" {
		ttl = 5 * time.Second
	}
	n.databaseMu.Lock()
	if n.databaseReads == nil {
		n.databaseReads = map[string]*managedDatabaseReadEntry{}
	}
	if cached := n.databaseReads[key]; cached != nil && (cached.Done != nil || n.Now().Sub(cached.At) < ttl) {
		done := cached.Done
		n.databaseMu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return protocol.DatabaseReadResult{}, ctx.Err()
			}
		}
		return cached.Value, cached.Err
	}
	for key, old := range n.databaseReads {
		if old.Done == nil && n.Now().Sub(old.At) >= old.TTL {
			delete(n.databaseReads, key)
		}
	}
	if len(n.databaseReads) >= 32 {
		n.databaseMu.Unlock()
		return protocol.DatabaseReadResult{}, fault("node_busy", "数据库读取繁忙")
	}
	entry := &managedDatabaseReadEntry{Done: make(chan struct{}), At: n.Now(), TTL: ttl}
	n.databaseReads[key] = entry
	done := entry.Done
	n.databaseMu.Unlock()
	go func() {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		value, e := n.managedDatabaseRead(readCtx, id, false, "", "", DatabaseRPC{Scope: scope})
		cancel()
		n.databaseMu.Lock()
		entry.Value = value
		entry.Err = e
		entry.At = n.Now()
		entry.Done = nil
		close(done)
		n.databaseMu.Unlock()
	}()
	select {
	case <-done:
		return entry.Value, entry.Err
	case <-ctx.Done():
		return protocol.DatabaseReadResult{}, ctx.Err()
	}
}
func operationTargetAllowed(t apiauth.Token, m Member, databases []ManagedDatabaseCluster) bool {
	for _, module := range []string{"nodes", "containers", "firewall", "tasks", "compose"} {
		if t.Allows(module, "read", m.ID, m.Region) {
			return true
		}
	}
	for _, c := range databases {
		if !c.Deleted && c.Coordinator == m.ID && t.Allows("databases", "read", c.ID, "") {
			return true
		}
	}
	return false
}
func (n *Node) readManagedDatabaseGroup(ctx context.Context, c ManagedDatabaseCluster, scopes ...string) ([]ManagedDatabaseMember, []ManagedDatabaseMember) {
	scope := "health"
	if len(scopes) > 0 && scopes[0] != "" {
		scope = scopes[0]
	}
	all := append(append([]DatabaseRef{}, c.Members...), c.Proxies...)
	out := make([]ManagedDatabaseMember, len(all))
	for i, ref := range all {
		selectedScope := scope
		if scope == "proxies" && i < len(c.Members) || scope != "proxies" && i >= len(c.Members) {
			selectedScope = "health"
		}
		var result protocol.DatabaseReadResult
		interval := 30 * time.Second
		if selectedScope == "health" {
			interval = 5 * time.Second
		}
		read := n.CurrentRead(ctx, ref.NodeID, "database.read", DatabaseRPC{InstanceID: ref.InstanceID, Scope: selectedScope}, &result, interval)
		result.State.Read = read
		if read.DataAt == nil {
			result.State.State = "unknown"
			result.State.Error = nil
		} else if i < len(c.Members) && result.State.ClusterID != "" && result.State.ClusterID != c.ClusterID {
			result.State.State = "mismatch"
			result.State.Error = &protocol.Error{Code: "conflict", Message: "配置关联与实际 Galera 集群身份不同"}
		}
		out[i] = ManagedDatabaseMember{DatabaseRef: ref, State: result.State}
	}
	return out[:len(c.Members)], out[len(c.Members):]
}
