package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/objectstore"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

type StorageCommand struct {
	Revision uint64              `json:"revision,omitempty"`
	ID       string              `json:"id,omitempty"`
	Prefix   string              `json:"prefix,omitempty"`
	Input    *objectstore.Input  `json:"input,omitempty"`
	Write    *requeststate.Write `json:"write,omitempty"`
}

func (n *Node) storageDispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	var c StorageCommand
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	// 回执按已认证来源归属；Write.Origin 跨 RPC 后为空，在这里回填。
	if c.Write != nil {
		c.Write.Origin = origin
	}
	switch method {
	case "storage.save", "storage.save_connection":
		if c.Input == nil {
			return nil, fault("invalid_request", "缺少存储目标")
		}
		return n.saveStorageTarget(ctx, *c.Input, c.Write)
	case "storage.get":
		t, err := n.StorageTarget(ctx, c.ID)
		if err == nil && t.NodeID == n.ID {
			var raw []byte
			if e := n.DB.QueryRowContext(ctx, "SELECT body FROM storage_checks WHERE target_id=?", t.ID).Scan(&raw); e == nil {
				var check objectstore.Check
				if json.Unmarshal(raw, &check) == nil && check.TargetRevision == t.Revision {
					t.Check = &check
				}
			} else if e != sql.ErrNoRows {
				return t, e
			}
			if check, e := n.storageConnectionCheck(ctx, t); e != nil {
				return t, e
			} else {
				t.ConnectionCheck = check
			}
		}
		return t, err
	case "storage.check":
		return n.checkStorageTarget(ctx, c.ID, c.Prefix, c.Revision, c.Write)
	case "storage.probe":
		return n.probeStorageTarget(ctx, c.ID, c.Prefix, c.Revision, c.Write)
	}
	return nil, fault("invalid_request", "未知存储操作")
}

func (n *Node) StorageTargets(ctx context.Context) ([]objectstore.Target, error) {
	if s, err := n.sharedStore(ctx); err == nil {
		objects, err := s.Objects(ctx, "storage_target")
		if err != nil {
			return nil, err
		}
		out := []objectstore.Target{}
		for _, o := range objects {
			var t objectstore.Target
			if err = json.Unmarshal(o.Body, &t); err != nil {
				return nil, err
			}
			out = append(out, t)
			if err = n.cacheStorageTarget(ctx, t); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	rows, err := n.DB.QueryContext(ctx, "SELECT body FROM storage_targets ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []objectstore.Target{}
	for rows.Next() {
		var data []byte
		var t objectstore.Target
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (n *Node) cacheStorageTarget(ctx context.Context, t objectstore.Target) error {
	data, _ := json.Marshal(t)
	_, err := n.DB.ExecContext(ctx, "INSERT INTO storage_targets(id,revision,body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,body=excluded.body WHERE storage_targets.revision<=excluded.revision", t.ID, t.Revision, string(data))
	return err
}
func (n *Node) StorageTarget(ctx context.Context, id string) (objectstore.Target, error) {
	if s, err := n.sharedStore(ctx); err == nil {
		o, err := s.Object(ctx, "storage_target", id)
		if err == nil {
			var t objectstore.Target
			err = json.Unmarshal(o.Body, &t)
			if err == nil {
				err = n.cacheStorageTarget(ctx, t)
			}
			return t, err
		}
		if err == sql.ErrNoRows {
			return objectstore.Target{}, err
		}
	}
	return n.localStorageTarget(ctx, id)
}
func (n *Node) localStorageTarget(ctx context.Context, id string) (objectstore.Target, error) {
	var t objectstore.Target
	var data []byte
	err := n.DB.QueryRowContext(ctx, "SELECT body FROM storage_targets WHERE id=?", id).Scan(&data)
	if err == nil {
		err = json.Unmarshal(data, &t)
	}
	return t, err
}
func (n *Node) storageCredentials(ctx context.Context, t objectstore.Target) (protocol.S3Credentials, error) {
	var out protocol.S3Credentials
	if t.NodeID != n.ID {
		return out, fault("forbidden", "S3 凭据由所属节点保管")
	}
	var data []byte
	var encrypted Secret
	err := n.DB.QueryRowContext(ctx, "SELECT secret FROM object_storage_credentials WHERE id=? AND target_id=?", t.CredentialRef, t.ID).Scan(&data)
	if err == nil {
		err = json.Unmarshal(data, &encrypted)
	}
	if err == nil {
		data, err = unseal(encrypted, n.age)
	}
	if err == nil {
		err = json.Unmarshal(data, &out)
	}
	return out, err
}

func (n *Node) saveStorageTarget(ctx context.Context, input objectstore.Input, write *requeststate.Write) (objectstore.Target, error) {
	n.telemetry.LockConfig()
	defer n.telemetry.UnlockConfig()
	t := input.Target
	if t.NodeID != n.ID {
		return t, fault("forbidden", "存储目标只能在所属节点保存")
	}
	if write == nil || write.Key == "" {
		return t, fault("invalid_request", "存储修改缺少请求标识")
	}
	s, err := n.sharedStore(ctx)
	if err != nil {
		return t, err
	}
	receipt := sharedsql.Receipt{Origin: write.Origin, Actor: write.Actor, Key: write.Key, Digest: write.Digest}
	if prior, e := s.Receipt(ctx, receipt); e == nil {
		e = json.Unmarshal(prior.Result, &t)
		return t, e
	} else if e != sql.ErrNoRows {
		return t, sharedFault(e)
	}
	var previous objectstore.Target
	if old, e := s.Object(ctx, "storage_target", t.ID); e == nil {
		if err = json.Unmarshal(old.Body, &previous); err != nil {
			return t, err
		}
		if previous.NodeID != n.ID {
			return t, fault("forbidden", "存储目标归属不可修改")
		}
	} else if e != sql.ErrNoRows {
		return t, e
	}
	if previous.Revision != t.Revision {
		return t, fault("precondition_failed", "存储目标已经改变")
	}
	t.CAPEM = previous.CAPEM
	if input.CAPEM != nil {
		t.CAPEM = *input.CAPEM
	}
	if err := t.Validate(); err != nil {
		return t, fault("invalid_request", err.Error())
	}
	var pendingKey string
	if e := n.DB.QueryRowContext(ctx, "SELECT object_key FROM storage_checks WHERE target_id=?", t.ID).Scan(&pendingKey); e != nil && e != sql.ErrNoRows {
		return t, e
	}
	// 待清理的对象仍在原桶。允许修复信任链以恢复该桶的访问。
	pendingConnection := t.S3Connection
	pendingConnection.CAPEM = previous.CAPEM
	if pendingKey != "" && previous.S3Connection != pendingConnection {
		return t, fault("conflict", "请先完成原连接检查并清理临时对象")
	}
	config, err := n.ArchiveConfig(ctx)
	if err != nil {
		return t, err
	}
	checkReference := func(c metricsstore.Config) error {
		if c.StorageTargetID != t.ID {
			return nil
		}
		if previous.Endpoint != t.Endpoint || previous.Bucket != t.Bucket {
			return fault("conflict", "归档位置变化需要先迁移已有数据")
		}
		if (c.Enabled || c.State == "applying") && (!t.Enabled || t.S3Connection != previous.S3Connection || input.Credentials != nil) {
			return fault("conflict", "请先停用集中监控，再修改存储连接或凭据")
		}
		return nil
	}
	if err = checkReference(config); err != nil {
		return t, err
	}
	t.Check, t.ConnectionCheck = nil, nil
	t.CredentialRef = previous.CredentialRef
	t.CredentialsAvailable = previous.CredentialsAvailable
	if input.Credentials != nil {
		if err = input.Credentials.Validate(); err != nil {
			return t, fault("invalid_request", err.Error())
		}
		plain, _ := json.Marshal(input.Credentials)
		secret, e := seal(plain, []string{n.age.Recipient().String()})
		if e != nil {
			return t, e
		}
		data, _ := json.Marshal(secret)
		t.CredentialRef = identity.Random()
		t.CredentialsAvailable = true
		if _, err = n.DB.ExecContext(ctx, "INSERT INTO object_storage_credentials(id,target_id,secret) VALUES(?,?,?)", t.CredentialRef, t.ID, string(data)); err != nil {
			return t, err
		}
	}
	if t.Enabled && !t.CredentialsAvailable {
		return t, fault("invalid_request", "请提供 S3 凭据")
	}
	t.Revision++
	err = s.Transact(ctx, n.ID, false, func(tx *sharedsql.Tx) error {
		if prior, e := tx.Receipt(ctx, receipt); e == nil {
			return json.Unmarshal(prior.Result, &t)
		} else if e != sql.ErrNoRows {
			return e
		}
		// 与监控绑定在同一共享事务内检查引用。
		if o, e := tx.Object(ctx, "monitoring", "config"); e == nil {
			var current metricsstore.Config
			if e = json.Unmarshal(o.Body, &current); e != nil {
				return e
			}
			if e = checkReference(current); e != nil {
				return e
			}
		} else if e != sql.ErrNoRows {
			return e
		}
		if err := tx.Put(ctx, sharedObject("storage_target", t.ID, t.Revision, t), previous.Revision); err != nil {
			return err
		}
		receipt.Kind = "storage_target"
		receipt.ResourceID = t.ID
		receipt.Result, _ = json.Marshal(t)
		return tx.SaveReceipt(ctx, receipt)
	})
	if err != nil {
		return t, sharedFault(err)
	}
	if err = n.cacheStorageTarget(ctx, t); err != nil {
		return t, err
	}
	n.notifyChange(Change{Resource: "storage"})
	return t, nil
}

func (n *Node) checkStorageTarget(ctx context.Context, id, prefix string, revision uint64, write *requeststate.Write) (objectstore.Check, error) {
	n.telemetry.LockConfig()
	defer n.telemetry.UnlockConfig()
	out := objectstore.Check{Prefix: prefix, TargetRevision: revision}
	if write == nil || write.Key == "" {
		return out, fault("invalid_request", "连接检查缺少请求标识")
	}
	prior, lookupErr := requeststate.Lookup(ctx, n.DB, *write)
	if errors.Is(lookupErr, requeststate.ErrConflict) || lookupErr == nil && (prior.Kind != "storage_check" || prior.ResourceID != id) {
		return out, fault("conflict", requeststate.ErrConflict.Error())
	}
	if lookupErr == nil {
		if err := json.Unmarshal(prior.Result, &out); err != nil {
			return out, err
		}
		if out.State != "checking" {
			return out, nil
		}
	} else if lookupErr != sql.ErrNoRows {
		return out, lookupErr
	}
	if !metricsstore.ValidPrefix(prefix) {
		return out, fault("invalid_request", "检查前缀无效")
	}
	t, err := n.StorageTarget(ctx, id)
	if err != nil {
		return out, err
	}
	if t.NodeID != n.ID || !t.Enabled {
		return out, fault("conflict", "存储目标未在当前节点启用")
	}
	if t.Revision != revision {
		return out, fault("precondition_failed", "存储目标已经改变")
	}
	keys, err := n.storageCredentials(ctx, t)
	if err != nil {
		return out, err
	}
	var raw []byte
	var key string
	err = n.DB.QueryRowContext(ctx, "SELECT object_key,body FROM storage_checks WHERE target_id=?", id).Scan(&key, &raw)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}
	if key != "" && !strings.HasPrefix(key, prefix+".opsd-check-") {
		return out, fault("conflict", "原连接检查尚有未清理对象，请先完成原目录检查")
	}
	if key == "" {
		key = prefix + ".opsd-check-" + identity.Random()
	}
	out.ObjectKey = key
	out.State = "checking"
	out.CheckedAt = n.Now().UTC()
	raw, _ = json.Marshal(out)
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO storage_checks(target_id,object_key,body) VALUES(?,?,?) ON CONFLICT(target_id) DO UPDATE SET object_key=excluded.object_key,body=excluded.body", id, key, string(raw))
	if err == nil && lookupErr == sql.ErrNoRows {
		err = requeststate.Save(ctx, tx, requeststate.Receipt{Write: *write, Kind: "storage_check", ResourceID: id, Result: raw, CreatedAt: n.Now().UTC()})
	}
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, _ = objectstore.CheckConnection(checkCtx, t, keys, prefix, key)
	out.TargetRevision = t.Revision
	raw, _ = json.Marshal(out)
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	tx, err = n.DB.BeginTx(finishCtx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(finishCtx, "UPDATE storage_checks SET object_key=?,body=? WHERE target_id=?", out.ObjectKey, string(raw), id); err != nil {
		return out, err
	}
	if err = requeststate.Finish(finishCtx, tx, requeststate.Receipt{Write: *write, ResourceID: id, Result: raw}); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (n *Node) checkedStorage(ctx context.Context, id, prefix string) (objectstore.Target, protocol.S3Credentials, error) {
	t, err := n.StorageTarget(ctx, id)
	if err != nil {
		return t, protocol.S3Credentials{}, err
	}
	if !t.Enabled || t.NodeID != n.ID {
		return t, protocol.S3Credentials{}, fault("conflict", "S3 目标须属于监控节点且已启用")
	}
	var raw []byte
	var check objectstore.Check
	err = n.DB.QueryRowContext(ctx, "SELECT body FROM storage_checks WHERE target_id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &check)
	}
	if err != nil || check.State != "ready" || check.Prefix != prefix || check.TargetRevision != t.Revision {
		return t, protocol.S3Credentials{}, fault("conflict", "请先完成监控目录的 S3 连接检查")
	}
	keys, err := n.storageCredentials(ctx, t)
	if err != nil {
		return t, keys, err
	}
	client, err := objectstore.New(t.S3Connection, keys.Compact)
	if err != nil {
		return t, keys, err
	}
	err = objectstore.VerifyOwner(ctx, client, t.Bucket, prefix, n.cluster)
	return t, keys, err
}
