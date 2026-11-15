package cluster

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"net"
	"reflect"
	"slices"
	"strconv"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

func (n *Node) syncSharedFlowCoordinator(ctx context.Context, s *sharedsql.Store, f sharedsql.Flow) error {
	o, err := s.Object(ctx, "database", f.ResourceID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var c ManagedDatabaseCluster
	if err = json.Unmarshal(o.Body, &c); err != nil {
		return err
	}
	if c.Coordinator == n.ID {
		return nil
	}
	return s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		o, err := t.Object(ctx, "database", f.ResourceID)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(o.Body, &c); err != nil {
			return err
		}
		if c.Coordinator == n.ID {
			return nil
		}
		if err = t.CheckFlow(ctx, f.ID, n.ID, f.Epoch); err != nil {
			return err
		}
		c.Coordinator = n.ID
		c.Revision++
		c.UpdatedAt = n.Now().UTC()
		c.CoordinatorEpoch = max(c.CoordinatorEpoch+1, f.Epoch)
		c.Signature = ed25519.Sign(n.signing, databaseClusterBytes(c))
		next := sharedObject("database", c.ID, c.Revision, c)
		next.Identity = c.ClusterID
		return t.Put(ctx, next, o.Revision)
	})
}
func (n *Node) registerSharedProxy(ctx context.Context, s *sharedsql.Store, state *SharedLocal) error {
	if err := n.publishSharedCapacity(ctx, s, state); err != nil {
		return err
	}
	instance, err := n.databaseInstance(ctx, state.ProxyID)
	if err != nil {
		return err
	}
	o, err := s.Object(ctx, "database", state.Binding.ClusterID)
	if err != nil {
		return err
	}
	var c ManagedDatabaseCluster
	if err = json.Unmarshal(o.Body, &c); err != nil {
		return err
	}
	binding := protocol.ProxyAccountBinding{Name: state.Access.Username, NodeID: n.ID, InstanceID: state.ProxyID}
	if slices.ContainsFunc(c.Proxies, func(r DatabaseRef) bool { return r.NodeID == n.ID && r.InstanceID == state.ProxyID }) && c.ProxyRouting != nil && slices.Contains(c.ProxyRouting.Accounts, binding) {
		return nil
	}
	return s.Transact(ctx, n.ID, false, func(t *sharedsql.Tx) error {
		o, err := t.Object(ctx, "database", state.Binding.ClusterID)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(o.Body, &c); err != nil {
			return err
		}
		hasProxy := slices.ContainsFunc(c.Proxies, func(r DatabaseRef) bool { return r.NodeID == n.ID && r.InstanceID == state.ProxyID })
		if hasProxy && c.ProxyRouting != nil && slices.Contains(c.ProxyRouting.Accounts, binding) {
			return nil
		}
		if !hasProxy {
			c.Proxies = append(c.Proxies, DatabaseRef{NodeID: n.ID, InstanceID: state.ProxyID, InstanceRevision: instance.Revision, Name: instance.Name, Address: state.Admin.Address})
		}
		if c.ProxyRouting == nil {
			c.ProxyRouting = state.Cluster.ProxyRouting
			sharedMemberPolicy(&c)
		}
		if c.ProxyRouting != nil {
			c.ProxyRouting.Accounts = append(append([]protocol.ProxyAccountBinding{}, c.ProxyRouting.Accounts...), binding)
		}
		c.Revision++
		c.Signature = ed25519.Sign(n.signing, databaseClusterBytes(c))
		next := sharedObject("database", c.ID, c.Revision, c)
		next.Identity = c.ClusterID
		return t.Put(ctx, next, o.Revision)
	})
}
func sharedMemberPolicy(c *ManagedDatabaseCluster) {
	if c.ProxyRouting == nil {
		return
	}
	old := c.ProxyRouting
	policy := *old
	policy.Backends = nil
	policy.Retired = append([]protocol.ProxyEndpoint{}, old.Retired...)
	for _, ref := range c.Members {
		if i := slices.IndexFunc(old.Backends, func(b protocol.ProxyBackend) bool { return b.NodeID == ref.NodeID && b.InstanceID == ref.InstanceID }); i >= 0 {
			policy.Backends = append(policy.Backends, old.Backends[i])
			continue
		}
		host, port, err := net.SplitHostPort(ref.Address)
		if err != nil {
			continue
		}
		number, err := strconv.Atoi(port)
		if err != nil {
			continue
		}
		policy.Backends = append(policy.Backends, protocol.ProxyBackend{ProxyEndpoint: protocol.ProxyEndpoint{Host: host, Port: number}, NodeID: ref.NodeID, InstanceID: ref.InstanceID})
	}
	for _, b := range old.Backends {
		if !slices.ContainsFunc(policy.Backends, func(current protocol.ProxyBackend) bool { return current.ProxyEndpoint == b.ProxyEndpoint }) {
			policy.Retired = append(policy.Retired, b.ProxyEndpoint)
		}
	}
	c.ProxyRouting = &policy
}
func (n *Node) sharedRoutingFinished(ctx context.Context, t *sharedsql.Tx, p sharedFlowPayload) error {
	key, err := n.sharedIdentity()
	if err != nil {
		return err
	}
	plain, err := unseal(p.Input, key)
	if err != nil {
		return err
	}
	var input DatabaseSubmission
	if err = json.Unmarshal(plain, &input); err != nil {
		return err
	}
	o, err := t.Object(ctx, "database", input.Cluster.ID)
	if err != nil {
		return err
	}
	var c ManagedDatabaseCluster
	if err = json.Unmarshal(o.Body, &c); err != nil {
		return err
	}
	if o.Revision != input.Change.ExpectedRevision && !reflect.DeepEqual(c.Members, input.Cluster.Members) {
		return sharedsql.ErrConflict
	}
	policy := *input.Change.Routing
	policy.Accounts = append([]protocol.ProxyAccountBinding{}, policy.Accounts...)
	if c.ProxyRouting != nil {
		for _, account := range c.ProxyRouting.Accounts {
			if sharedProtected(account.Name) && !slices.Contains(policy.Accounts, account) {
				policy.Accounts = append(policy.Accounts, account)
			}
		}
	}
	c.ProxyRouting = &policy
	c.Revision++
	c.UpdatedAt = n.Now().UTC()
	c.Signature = ed25519.Sign(n.signing, databaseClusterBytes(c))
	next := sharedObject("database", c.ID, c.Revision, c)
	next.Identity = c.ClusterID
	return t.Put(ctx, next, o.Revision)
}
