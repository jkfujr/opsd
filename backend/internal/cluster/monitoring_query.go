package cluster

import (
	"context"

	"github.com/jkfujr/opsd/internal/metricsstore"
)

func (n *Node) archiveSeries(ctx context.Context, q metricsstore.Query) (metricsstore.QueryResult, error) {
	return n.telemetry.Query(ctx, q)
}
