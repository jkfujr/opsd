package storagepool

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type Store struct{ DB *sql.DB }

func (s Store) CheckName(ctx context.Context, name, owner string) error {
	var exists bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM storage_pools WHERE owner_node_id=? AND normalized_name=?)`,
		owner, strings.ToLower(name)).Scan(&exists)
	if err == nil && exists {
		return Fault("conflict", "已存在同名存储池")
	}
	return err
}

func (s Store) Begin(ctx context.Context, plan protocol.PoolPlan, operation, ca string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	spec, err := json.Marshal(plan.Nodes)
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(spec))
	_, err = tx.ExecContext(ctx, `INSERT INTO storage_pools
		(pool_id,owner_node_id,name,normalized_name,revision,state,ec_enabled,port_offset,operation_id,spec_hash,ca_pem,created_at,updated_at)
		VALUES (?,?,?,?,1,'creating',?,?,?,?,?,?,?) ON CONFLICT(pool_id) DO NOTHING`,
		plan.PoolID, plan.OwnerNodeID, plan.Normalized.Name, strings.ToLower(plan.Normalized.Name),
		plan.Normalized.ECEnabled, plan.Normalized.PortOffset, operation, digest, ca, now.Unix(), now.Unix())
	if err != nil {
		return err
	}
	var existing, existingHash string
	if err = tx.QueryRowContext(ctx, `SELECT operation_id,spec_hash FROM storage_pools WHERE pool_id=?`,
		plan.PoolID).Scan(&existing, &existingHash); err != nil {
		return err
	}
	if existing != operation || existingHash != digest {
		return Fault("conflict", "池已由其它操作登记")
	}
	for _, n := range plan.Nodes {
		roles, _ := json.Marshal(n.Roles)
		ports, _ := json.Marshal(n.Ports)
		if _, err = tx.ExecContext(ctx, `INSERT INTO storage_pool_nodes
		(pool_id,node_id,failure_domain,private_ip,data_root_id,data_fs_uuid,control_fs_uuid,capacity_bytes,control_bytes,roles_json,ports_json,state)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,'pending') ON CONFLICT(pool_id,node_id) DO NOTHING`,
			plan.PoolID, n.NodeID, n.FailureDomain, n.PrivateIP, n.StorageRootID, n.DataDisk.UUID, n.ControlDisk.UUID,
			n.DataBytes+n.ControlBytes, n.ControlBytes, string(roles), string(ports)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s Store) Update(ctx context.Context, id, state, ca string, nodes []protocol.PoolMemberStatus, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE storage_pools SET state=?,ca_pem=?,updated_at=?,
		revision=revision+CASE WHEN state!=? OR ca_pem!=? THEN 1 ELSE 0 END WHERE pool_id=?`,
		state, ca, now.Unix(), state, ca, id); err != nil {
		return err
	}
	for _, n := range nodes {
		if _, err = tx.ExecContext(ctx, `UPDATE storage_pool_nodes SET state=?,checked_at=?,last_error=?,
			applied_revision=CASE WHEN ?='ready' THEN max(applied_revision,1) ELSE applied_revision END,
			data_fs_uuid=?,data_root_id=? WHERE pool_id=? AND node_id=?`,
			n.State, now.Unix(), n.Error, n.State, n.DataDisk.UUID, n.StorageRootID, id, n.NodeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s Store) Get(ctx context.Context, id string) (protocol.StoragePool, error) {
	if err := ctx.Err(); err != nil {
		return protocol.StoragePool{}, err
	}
	p, err := readPool(s.DB.QueryRowContext(ctx, poolQuery+" WHERE pool_id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, Fault("not_found", "存储池不存在")
	}
	return p, err
}

func (s Store) List(ctx context.Context, after string, limit int) ([]protocol.StoragePool, error) {
	rows, err := s.DB.QueryContext(ctx, poolQuery+" WHERE pool_id>? ORDER BY pool_id LIMIT ?", after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.StoragePool{}
	for rows.Next() {
		p, err := readPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const poolQuery = `SELECT pool_id,owner_node_id,name,revision,state,ec_enabled,replication,operation_id,ca_pem,updated_at FROM storage_pools`

func readPool(row interface{ Scan(...any) error }) (protocol.StoragePool, error) {
	p := protocol.StoragePool{Nodes: []protocol.PoolMemberStatus{}}
	var checked int64
	err := row.Scan(&p.ID, &p.OwnerNodeID, &p.Name, &p.Revision, &p.State, &p.ECEnabled, &p.Replication, &p.OperationID, &p.CAPEM, &checked)
	p.CheckedAt = time.Unix(checked, 0).UTC()
	return p, err
}
