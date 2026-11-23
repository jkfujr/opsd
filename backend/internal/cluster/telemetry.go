package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/telemetry"
)

func (n *Node) newTelemetry(directory string) *telemetry.Service {
	return telemetry.New(directory, telemetry.Dependencies{
		DB: n.DB, NodeID: n.ID, ClusterID: func() string { return n.cluster }, Now: func() time.Time { return n.Now() },
		Peer: telemetry.Peer{
			Call: n.Call, Active: n.Active, Revoked: n.Revoked, Allowed: n.authorize,
			OpenStream: func(ctx context.Context, target string) (telemetry.Stream, error) {
				return n.openArchiveChannel(ctx, target)
			},
		},
		Queue: telemetry.Queue{
			Submit: func(ctx context.Context, target, id, actor string, payload json.RawMessage) (protocol.OperationRecord, error) {
				op, err := n.Submit(ctx, target, Request{ID: id, Kind: "monitoring_deploy", Payload: payload, Actor: actor})
				return op.OperationRecord, err
			},
			Get: func(ctx context.Context, id string) (protocol.OperationRecord, error) {
				op, err := n.GetOperation(ctx, id)
				return op.OperationRecord, err
			},
			Decide: func(ctx context.Context, method, id string, revision uint64) error {
				_, err := n.Decide(ctx, method, OperationDecision{ID: id, ExpectedRevision: revision})
				return err
			},
			StepID: stepID,
		},
		Storage: telemetry.Storage{Target: n.StorageTarget, Checked: n.checkedStorage, LocalTarget: n.localStorageTarget, Credentials: n.storageCredentials},
		Shared:  telemetry.Shared{Store: n.sharedStore, WriteAllowed: n.sharedWriteAllowed},
		Errors: telemetry.Errors{New: fault, Code: func(err error) string {
			var f *Fault
			if errors.As(err, &f) {
				return f.Code
			}
			return ""
		}, Shared: sharedFault, Wire: wireError},
		Executor: n.executeRead,
		Source:   telemetry.Source{Host: n.collect, Resources: n.collectTelemetryResources},
		Changed:  func() { n.notifyChange(Change{Resource: "monitoring"}) },
	})
}

func (n *Node) collectTelemetryResources(ctx context.Context) (protocol.ResourceMetrics, error) {
	var resources protocol.ResourceMetrics
	connections := map[string]protocol.DatabaseConnection{}
	clusters, err := n.DatabaseClusters(ctx)
	if err != nil {
		return resources, err
	}
	for _, c := range clusters {
		if c.Deleted {
			continue
		}
		for _, ref := range append(append([]DatabaseRef{}, c.Members...), c.Proxies...) {
			if ref.NodeID != n.ID {
				continue
			}
			if _, ok := connections[ref.InstanceID]; ok {
				continue
			}
			connection, err := n.databaseConnection(ctx, ref.InstanceID)
			if err != nil {
				resources.Errors = append(resources.Errors, "database: "+ref.InstanceID+": "+err.Error())
				continue
			}
			connections[ref.InstanceID] = connection
		}
	}
	if n.Executor == nil {
		return resources, nil
	}
	reply, err := n.Executor.Call(ctx, &protocol.ResourceMetricsRead{Connections: connections})
	if err != nil {
		return resources, err
	}
	value, ok := reply.(*protocol.ResourceMetrics)
	if !ok {
		return resources, fmt.Errorf("资源指标应答无效")
	}
	connectionErrors := resources.Errors
	resources = *value
	resources.Errors = append(connectionErrors, resources.Errors...)
	return resources, nil
}

type telemetryParent struct {
	node *Node
	op   *Operation
}

func (p telemetryParent) Record() *protocol.OperationRecord { return &p.op.OperationRecord }
func (p telemetryParent) Actor() string                     { return p.op.Actor }
func (p telemetryParent) Save(ctx context.Context, phase string) error {
	return p.node.saveOperation(ctx, p.op, phase)
}
