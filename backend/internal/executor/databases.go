package executor

import (
	"context"
	"fmt"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (s *Services) discoverDatabases(ctx context.Context) (protocol.Payload, error) {
	if s.Docker == nil {
		return nil, fmt.Errorf("数据库发现入口不可用")
	}
	items, e := s.Docker.DatabaseInstances(ctx)
	if e != nil {
		return nil, e
	}
	return &protocol.DatabaseDiscovered{Instances: databaseengine.CompleteDiscovered(items)}, nil
}
func (s *Services) readDatabase(ctx context.Context, r protocol.DatabaseRead) (protocol.Payload, error) {
	manager := databaseengine.Manager{}
	out := &protocol.DatabaseReadResult{Backups: []protocol.DatabaseBackup{}}
	if e := r.Connection.Validate(); e != nil {
		return nil, e
	}
	if r.Backups {
		out.Backups = databaseengine.BackupStatus(ctx, r.Connection.BackupRoot)
		return out, nil
	}
	if r.User != "" {
		hash, e := manager.PasswordHash(ctx, r.Connection, r.User, r.Host, r.ClusterID)
		out.PasswordHash = hash
		return out, e
	}
	var e error
	out.State, e = manager.Read(ctx, r.Connection, r.Scope)
	return out, e
}
