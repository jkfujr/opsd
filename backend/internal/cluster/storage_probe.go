package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/jkfujr/opsd/internal/objectstore"
	"github.com/jkfujr/opsd/internal/requeststate"
)

func (n *Node) storageConnectionCheck(ctx context.Context, t objectstore.Target) (*objectstore.Probe, error) {
	var raw string
	err := n.DB.QueryRowContext(ctx, "SELECT result_json FROM api_requests WHERE kind='storage_probe' AND resource_id=? ORDER BY rowid DESC LIMIT 1", t.ID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var check objectstore.Probe
	if err := json.Unmarshal([]byte(raw), &check); err != nil {
		return nil, err
	}
	if check.TargetRevision != t.Revision {
		return nil, nil
	}
	return &check, nil
}

func (n *Node) probeStorageTarget(ctx context.Context, id, prefix string, revision uint64, write *requeststate.Write) (objectstore.Probe, error) {
	n.telemetry.LockConfig()
	defer n.telemetry.UnlockConfig()
	out := objectstore.Probe{Prefix: prefix, TargetRevision: revision}
	if write == nil || write.Key == "" {
		return out, fault("invalid_request", "连接探测缺少请求标识")
	}
	prior, err := requeststate.Lookup(ctx, n.DB, *write)
	if errors.Is(err, requeststate.ErrConflict) || err == nil && (prior.Kind != "storage_probe" || prior.ResourceID != id) {
		return out, fault("conflict", requeststate.ErrConflict.Error())
	}
	if err == nil {
		err = json.Unmarshal(prior.Result, &out)
		return out, err
	}
	if err != sql.ErrNoRows {
		return out, err
	}
	if len(prefix) > 1024 || strings.IndexFunc(prefix, unicode.IsControl) >= 0 {
		return out, fault("invalid_request", "S3 列举前缀无效")
	}
	t, err := n.StorageTarget(ctx, id)
	if err != nil {
		return out, err
	}
	if t.NodeID != n.ID || !t.Enabled {
		return out, fault("conflict", "S3 目标未在当前节点启用")
	}
	if t.Revision != revision {
		return out, fault("precondition_failed", "存储目标已经改变")
	}
	keys, err := n.storageCredentials(ctx, t)
	if err != nil {
		return out, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out.State = "ready"
	if err := objectstore.ProbeConnection(checkCtx, t, keys.Store, prefix); err != nil {
		out.State, out.Error = "failed", err.Error()
	}
	out.CheckedAt = n.Now().UTC()
	raw, _ := json.Marshal(out)
	finish, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	err = requeststate.Save(finish, n.DB, requeststate.Receipt{Write: *write, Kind: "storage_probe", ResourceID: id, Result: raw, CreatedAt: out.CheckedAt})
	return out, err
}
