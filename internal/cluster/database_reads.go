package cluster

import (
	"context"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/protocol"
)

type managedDatabaseReadEntry struct {
	Done  chan struct{}
	At    time.Time
	Value protocol.DatabaseReadResult
	Err   error
}

func (n *Node) cachedManagedDatabaseRead(ctx context.Context, id string) (protocol.DatabaseReadResult, error) {
	n.databaseMu.Lock()
	if n.databaseReads == nil {
		n.databaseReads = map[string]*managedDatabaseReadEntry{}
	}
	if cached := n.databaseReads[id]; cached != nil && (cached.Done != nil || n.Now().Sub(cached.At) < 5*time.Second) {
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
		if old.Done == nil && n.Now().Sub(old.At) >= 5*time.Second {
			delete(n.databaseReads, key)
		}
	}
	if len(n.databaseReads) >= 32 {
		n.databaseMu.Unlock()
		return protocol.DatabaseReadResult{}, fault("node_busy", "数据库读取繁忙")
	}
	entry := &managedDatabaseReadEntry{Done: make(chan struct{}), At: n.Now()}
	n.databaseReads[id] = entry
	done := entry.Done
	n.databaseMu.Unlock()
	go func() {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		value, e := n.managedDatabaseRead(readCtx, id, false, "", "")
		cancel()
		n.databaseMu.Lock()
		entry.Value = value
		entry.Err = e
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
	for _, module := range []string{"nodes", "containers", "firewall"} {
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
func (n *Node) readManagedDatabaseGroup(ctx context.Context, c ManagedDatabaseCluster) ([]ManagedDatabaseMember, []ManagedDatabaseMember) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	all := append(append([]DatabaseRef{}, c.Members...), c.Proxies...)
	out := make([]ManagedDatabaseMember, len(all))
	slots := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, ref := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result protocol.DatabaseReadResult
			var e error
			select {
			case slots <- struct{}{}:
				e = n.Call(ctx, ref.NodeID, "database.read", DatabaseRPC{InstanceID: ref.InstanceID}, &result)
				<-slots
			case <-ctx.Done():
				e = ctx.Err()
			}
			if e != nil {
				result.State = protocol.DatabaseState{State: "unavailable", CheckedAt: n.Now().UTC(), Error: &protocol.Error{Code: "dependency_unavailable", Message: "数据库实例不可达"}}
			} else if i < len(c.Members) && result.State.ClusterID != c.ClusterID {
				result.State.State = "mismatch"
				result.State.Error = &protocol.Error{Code: "conflict", Message: "配置关联与实际 Galera 集群身份不同"}
			}
			out[i] = ManagedDatabaseMember{DatabaseRef: ref, State: result.State}
		}()
	}
	wg.Wait()
	return out[:len(c.Members)], out[len(c.Members):]
}
