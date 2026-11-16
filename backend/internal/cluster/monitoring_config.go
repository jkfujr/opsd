package cluster

import (
	"context"

	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) ArchiveConfig(ctx context.Context) (metricsstore.Config, error) {
	return n.telemetry.Config(ctx)
}
func (n *Node) cacheArchiveConfig(ctx context.Context, c metricsstore.Config) error {
	return n.telemetry.CacheConfig(ctx, c)
}
func (n *Node) validateArchiveRequest(ctx context.Context, body []byte) error {
	return n.telemetry.ValidateRequest(ctx, body)
}
func (n *Node) advanceArchive(ctx context.Context, op *Operation, body []byte) error {
	return n.telemetry.Advance(ctx, telemetryParent{n, op}, body)
}
func (n *Node) decideArchive(ctx context.Context, method string, d OperationDecision, op Operation) (Operation, error) {
	err := n.telemetry.Decide(ctx, method, d.ExpectedRevision, telemetryParent{n, &op})
	return op, err
}
func (n *Node) archiveDeployment(ctx context.Context, c metricsstore.Config) (protocol.MonitoringDeployment, error) {
	return n.telemetry.Deployment(ctx, c)
}
