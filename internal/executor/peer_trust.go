package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/publicaddress"
)

func (s *Services) lock() (func(), error) {
	if s.OperationLock != nil {
		return s.OperationLock()
	}
	s.localMu.Lock()
	return s.localMu.Unlock, nil
}
func (s *Services) trustState(ctx context.Context) (protocol.PeerTrustTarget, protocol.PeerTrustUpdated, error) {
	var target protocol.PeerTrustTarget
	status := protocol.PeerTrustUpdated{Results: []protocol.ScopeResult{}}
	var a, b string
	e := s.Engine.Store.DB.QueryRowContext(ctx, "SELECT target,status FROM peer_trust WHERE id=1").Scan(&a, &b)
	if errors.Is(e, sql.ErrNoRows) {
		return target, status, nil
	}
	if e == nil {
		e = json.Unmarshal([]byte(a), &target)
	}
	if e == nil {
		e = json.Unmarshal([]byte(b), &status)
	}
	return target, status, e
}
func (s *Services) receiveTrust(ctx context.Context, target protocol.PeerTrustTarget) (*protocol.PeerTrustUpdated, error) {
	if target.Revision == 0 || len(target.IPv4) > 16384 || len(target.IPv6) > 16384 || !target.Enabled && (len(target.IPv4) > 0 || len(target.IPv6) > 0) {
		return nil, firewall.Unsupported("互信目标无效")
	}
	for i, list := range [][]string{target.IPv4, target.IPv6} {
		for _, text := range list {
			a, e := publicaddress.Parse(text)
			if e != nil || a.Is6() != (i == 1) {
				return nil, firewall.Unsupported("互信目标包含无效公网地址")
			}
		}
		sort.Strings(list)
		if len(slices.Compact(append([]string(nil), list...))) != len(list) {
			return nil, firewall.Unsupported("互信地址重复")
		}
	}
	s.trustMu.Lock()
	defer s.trustMu.Unlock()
	old, status, e := s.trustState(ctx)
	if e != nil {
		return nil, e
	}
	if target.Revision < old.Revision {
		return &status, nil
	}
	if target.Revision == old.Revision {
		if !reflect.DeepEqual(target, old) {
			return nil, firewall.Conflict("相同互信版本包含不同规则")
		}
		return &status, nil
	}
	status.TargetRevision = target.Revision
	status.ReceivedRevision = target.Revision
	for i := range status.Results {
		status.Results[i].State = "pending"
		status.Results[i].Error = nil
	}
	a, _ := json.Marshal(target)
	b, _ := json.Marshal(status)
	_, e = s.Engine.Store.DB.ExecContext(ctx, "INSERT INTO peer_trust(id,target,status) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET target=excluded.target,status=excluded.status", string(a), string(b))
	return &status, e
}

// SyncTrust 与普通操作共用跨进程锁，按最新持久目标核对原生规则。
func (s *Services) SyncTrust(ctx context.Context) error {
	s.trustMu.Lock()
	defer s.trustMu.Unlock()
	target, status, e := s.trustState(ctx)
	if e != nil || target.Revision == 0 {
		return e
	}
	unlock, e := s.lock()
	if e != nil {
		return e
	}
	defer unlock()
	ids, e := s.Engine.Store.Pending(ctx)
	if e != nil {
		return e
	}
	if len(ids) > 0 {
		return operations.ErrBusy
	}
	status.Results = s.Engine.Manager.SyncPeerTrust(ctx, target)
	status.TargetRevision = target.Revision
	status.ReceivedRevision = target.Revision
	status.AppliedRevision = 0
	complete := true
	for _, r := range status.Results {
		if r.State != "in_sync" && r.State != "inactive" {
			complete = false
		}
	}
	if complete {
		status.AppliedRevision = target.Revision
	}
	now := time.Now().UTC()
	status.CheckedAt = &now
	b, _ := json.Marshal(status)
	_, e = s.Engine.Store.DB.ExecContext(ctx, "UPDATE peer_trust SET status=? WHERE id=1", string(b))
	return e
}
func (s *Services) MaintainTrust(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		_ = s.SyncTrust(c)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
