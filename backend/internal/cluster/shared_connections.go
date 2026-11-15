package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

func saveSharedCapacity(ctx context.Context, store *sharedsql.Store, node, kind, id string, value any) error {
	body, _ := json.Marshal(value)
	old, err := store.Object(ctx, kind, id)
	if err == nil && string(old.Body) == string(body) {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	return store.Transact(ctx, node, false, func(tx *sharedsql.Tx) error {
		old, err := tx.Object(ctx, kind, id)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		return tx.Put(ctx, sharedsql.Object{Kind: kind, ID: id, Revision: old.Revision + 1, Body: body}, old.Revision)
	})
}

func (n *Node) publishSharedCapacity(ctx context.Context, store *sharedsql.Store, state *SharedLocal) error {
	id := n.ID + "/" + state.ProxyID
	if _, err := store.Object(ctx, "proxy_capacity", id); err == sql.ErrNoRows {
		observed, e := n.readProxyRouting(ctx, state.ProxyID, nil)
		if e != nil {
			return e
		}
		if e = saveSharedCapacity(ctx, store, n.ID, "proxy_capacity", id, sharedProxyConnections(observed)); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	for _, ref := range state.Cluster.Members {
		if ref.NodeID != n.ID {
			continue
		}
		id := ref.NodeID + "/" + ref.InstanceID
		if _, err := store.Object(ctx, "database_capacity", id); err == nil {
			continue
		} else if err != sql.ErrNoRows {
			return err
		}
		var observed protocol.DatabaseReadResult
		if err := n.Call(ctx, n.ID, "database.read", DatabaseRPC{InstanceID: ref.InstanceID, ExpectedClusterID: state.Binding.GaleraID, Scope: "health"}, &observed); err != nil {
			return err
		}
		if err := saveSharedCapacity(ctx, store, n.ID, "database_capacity", id, observed.State.MaxConnections); err != nil {
			return err
		}
	}
	return nil
}

func sharedProxyConnections(observation protocol.ProxyRoutingObservation) map[string]int {
	limits := map[string]int{}
	for _, server := range observation.Servers {
		limits[server.Key()] = max(limits[server.Key()], server.MaxConnections)
	}
	for key := range limits {
		limits[key] += sharedsql.MonitorConnections
	}
	return limits
}

func (n *Node) checkSharedConnections(ctx context.Context, c ManagedDatabaseCluster) error {
	directory, err := n.Directory(ctx)
	if err != nil {
		return err
	}
	var store *sharedsql.Store
	if n.sharedActive() {
		store, err = n.sharedStore(ctx)
		if err != nil {
			return err
		}
	}
	withProxy := map[string]bool{}
	demand := map[string]int{}
	for _, ref := range c.Proxies {
		var observed protocol.ProxyRoutingObservation
		limits := map[string]int{}
		if store != nil {
			if cached, e := store.Object(ctx, "proxy_capacity", ref.NodeID+"/"+ref.InstanceID); e == nil {
				if e = json.Unmarshal(cached.Body, &limits); e != nil {
					return e
				}
				withProxy[ref.NodeID] = true
				for endpoint, count := range limits {
					demand[endpoint] += count
				}
				continue
			} else if e != sql.ErrNoRows {
				return e
			}
		}
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = n.Call(readCtx, ref.NodeID, "database.proxy_routing.read", DatabaseRPC{InstanceID: ref.InstanceID}, &observed)
		cancel()
		if err != nil {
			if store == nil {
				return err
			}
			cached, e := store.Object(ctx, "proxy_capacity", ref.NodeID+"/"+ref.InstanceID)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(cached.Body, &limits); e != nil {
				return e
			}
		} else {
			limits = sharedProxyConnections(observed)
			if store != nil {
				if err = saveSharedCapacity(ctx, store, n.ID, "proxy_capacity", ref.NodeID+"/"+ref.InstanceID, limits); err != nil {
					return err
				}
			}
		}
		withProxy[ref.NodeID] = true
		for endpoint, count := range limits {
			demand[endpoint] += count
		}
	}
	newProxies := 0
	for _, member := range directory.Members {
		if n.Active(ctx, member.ID) && !withProxy[member.ID] {
			newProxies++
		}
	}
	for _, ref := range c.Members {
		endpoint, e := sharedBackendEndpoint(c, ref)
		if e != nil {
			return e
		}
		var observed protocol.DatabaseReadResult
		if store != nil {
			if cached, e := store.Object(ctx, "database_capacity", ref.NodeID+"/"+ref.InstanceID); e == nil {
				if e = json.Unmarshal(cached.Body, &observed.State.MaxConnections); e != nil {
					return e
				}
			} else if e != sql.ErrNoRows {
				return e
			}
		}
		if observed.State.MaxConnections == 0 {
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = n.Call(readCtx, ref.NodeID, "database.read", DatabaseRPC{InstanceID: ref.InstanceID, ExpectedClusterID: c.ClusterID, Scope: "health"}, &observed)
			cancel()
		} else {
			err = nil
		}
		if err != nil {
			if store == nil {
				return err
			}
			cached, e := store.Object(ctx, "database_capacity", ref.NodeID+"/"+ref.InstanceID)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(cached.Body, &observed.State.MaxConnections); e != nil {
				return e
			}
		} else if store != nil {
			if err = saveSharedCapacity(ctx, store, n.ID, "database_capacity", ref.NodeID+"/"+ref.InstanceID, observed.State.MaxConnections); err != nil {
				return err
			}
		}
		required := demand[endpoint.Key()] + newProxies*(sharedsql.MaxConnections+sharedsql.MonitorConnections) + 2
		if observed.State.MaxConnections == 0 || int64(required) > observed.State.MaxConnections {
			return fault("conflict", fmt.Sprintf("数据库 %s 的代理、监控与管理连接需求为 %d，上限为 %d", ref.Name, required, observed.State.MaxConnections))
		}
	}
	return nil
}
