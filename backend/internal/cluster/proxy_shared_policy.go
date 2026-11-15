package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) queueSharedProxyPolicy(ctx context.Context, c ManagedDatabaseCluster, ref DatabaseRef, state *ProxyRoutingState) {
	fail := func(err error) {
		state.State = "conflict"
		state.Error = &protocol.Error{Code: "conflict", Message: err.Error()}
		_ = n.saveProxyRoutingState(ctx, c.ID, *state)
	}
	observation, err := n.readProxyRouting(ctx, ref.InstanceID, &c.ProxyRouting.Hostgroups)
	if err != nil {
		fail(err)
		return
	}
	order := initialProxyOrder(n.ID, *c.ProxyRouting, observation, state.Order)
	action := proxyRouteAction(*c.ProxyRouting, observation, order)
	if local := n.sharedState(); local != nil && local.ProxyID == ref.InstanceID && local.Deployment != nil {
		for i := range action.Servers {
			action.Servers[i].MaxConnections = 2
		}
	}
	if databaseengine.ProxyRouteActionSatisfied(observation, action) {
		setProxyApplied(state, observation, action, state.OperationID, n.Now().UTC())
		_ = n.saveProxyRoutingState(ctx, c.ID, *state)
		return
	}
	if state.ConfigFingerprint != "" && observation.Fingerprint != state.ConfigFingerprint {
		fail(fmt.Errorf("代理配置存在外部修改，请核对原操作"))
		return
	}
	connection, err := n.databaseConnection(ctx, ref.InstanceID)
	if err != nil {
		fail(err)
		return
	}
	payload, _ := json.Marshal(protocol.DatabaseAction{Connection: connection, Kind: "proxy_route", Routing: &action})
	key := "proxy-policy-" + routingVersion(*c.ProxyRouting) + "-" + ref.InstanceID
	_, err = n.enqueueTx(ctx, n.ID, Request{Key: key, Kind: "database_step", ResourceID: c.ID, Payload: payload}, func(tx *sql.Tx, op Operation) error {
		state.OperationID = op.ID
		state.PendingAction = &action
		state.State = "applying"
		state.Error = nil
		body, _ := json.Marshal(state)
		_, e := tx.ExecContext(ctx, "INSERT INTO proxy_routing_state(cluster_id,instance_id,body) VALUES(?,?,?) ON CONFLICT(cluster_id,instance_id) DO UPDATE SET body=excluded.body", c.ID, ref.InstanceID, string(body))
		return e
	})
	if err != nil {
		fail(err)
	}
}
