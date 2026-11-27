package storagepool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type ECPlan struct {
	Worker     string                     `json:"worker"`
	Input      protocol.PoolECWorkerInput `json:"input"`
	Bytes      int64                      `json:"bytes"`
	ShardBytes int64                      `json:"shard_bytes"`
	Limit      int64                      `json:"limit"`
	LastWrite  int64                      `json:"last_write"`
	Attempt    int                        `json:"attempt"`
	RetryAt    int64                      `json:"retry_at,omitempty"`
	Repairing  bool                       `json:"repairing,omitempty"`
}

type ECWork struct {
	PoolID, Operation, Direction, State, Error string
	Volume                                     uint32
	Generation                                 int64
	Cutover, Cleanup                           bool
	Plan                                       ECPlan
}

func (s Store) ReadECPolicy(ctx context.Context, p *protocol.StoragePool) error {
	if err := s.DB.QueryRowContext(ctx, `SELECT ec_restore FROM storage_pools WHERE pool_id=?`, p.ID).Scan(&p.ECRestore); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT node_id FROM storage_pool_nodes
		WHERE pool_id=? AND parity_preferred=1 ORDER BY node_id`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		p.ParityNodes = append(p.ParityNodes, id)
	}
	return rows.Err()
}

func (s Store) SetECPolicy(ctx context.Context, p protocol.PoolECSettings, operation string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision uint64
	var last string
	err = tx.QueryRowContext(ctx, `SELECT revision,ec_operation_id FROM storage_pools WHERE pool_id=?`, p.ID).Scan(&revision, &last)
	if err != nil || last == operation {
		return err
	}
	if revision != p.Revision {
		return Fault("precondition_failed", "池版本已变化")
	}
	var busy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM storage_pool_ec_conversions WHERE pool_id=?
		AND (state IN ('pending','encoding','verifying') OR cleanup_pending=1))`, p.ID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return Fault("conflict", "池仍有 EC 转换或清理任务")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE storage_pools SET ec_enabled=?,ec_restore=?,ec_restore_error='',ec_operation_id=?,revision=revision+1
		WHERE pool_id=?`, p.Enabled, p.RestoreReplicas, operation, p.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE storage_pool_nodes SET parity_preferred=0 WHERE pool_id=?`, p.ID); err != nil {
		return err
	}
	for _, id := range p.ParityPreferred {
		result, err := tx.ExecContext(ctx, `UPDATE storage_pool_nodes SET parity_preferred=1 WHERE pool_id=? AND node_id=?`, p.ID, id)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return Fault("invalid_request", "校验片优先节点不属于此池")
		}
	}
	return tx.Commit()
}

func (s Store) ClearECRestore(ctx context.Context, pool string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE storage_pools SET ec_restore=0,revision=revision+1
		WHERE pool_id=? AND ec_restore=1 AND NOT EXISTS
		(SELECT 1 FROM storage_pool_volumes WHERE pool_id=? AND layout='ec')`, pool, pool)
	return err
}

func (s Store) ECRestoreResult(ctx context.Context, pool string) (pending bool, message string, err error) {
	err = s.DB.QueryRowContext(ctx, `SELECT ec_restore,ec_restore_error FROM storage_pools WHERE pool_id=?`, pool).Scan(&pending, &message)
	return
}

func (s Store) FailECRestore(ctx context.Context, pool string, w *ECWork, message string, keepTargets bool, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if w != nil {
		result, err := tx.ExecContext(ctx, `UPDATE storage_pool_ec_conversions SET state='failed',error_code=?,updated_at=?
			WHERE pool_id=? AND volume_id=? AND generation=? AND direction='restore' AND state='verifying' AND cutover_committed=0`,
			message, now.Unix(), pool, w.Volume, w.Generation)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return Fault("conflict", "原 EC 反向转换已变化")
		}
		if keepTargets {
			targets, err := json.Marshal(w.Plan.Input.Restore)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE storage_pool_ec_conversions SET target_json=json_set(target_json,'$.input.restore',json(?))
				WHERE pool_id=? AND volume_id=? AND generation=? AND direction='encode' AND cutover_committed=1`,
				string(targets), pool, w.Volume, w.Plan.Input.Generation); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE storage_pool_volumes SET state='ready',checked_at=?
			WHERE pool_id=? AND volume_id=? AND layout='ec' AND generation=?`, now.Unix(), pool, w.Volume, w.Plan.Input.Generation); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE storage_pools SET ec_restore=0,ec_restore_error=?,revision=revision+1
		WHERE pool_id=? AND ec_restore=1`, message, pool); err != nil {
		return err
	}
	return tx.Commit()
}

const ecWorkQuery = `SELECT pool_id,volume_id,generation,operation_id,direction,state,target_json,
	cutover_committed,cleanup_pending,COALESCE(error_code,'') FROM storage_pool_ec_conversions`

func scanECWork(row interface{ Scan(...any) error }) (ECWork, error) {
	var w ECWork
	var raw string
	err := row.Scan(&w.PoolID, &w.Volume, &w.Generation, &w.Operation, &w.Direction, &w.State,
		&raw, &w.Cutover, &w.Cleanup, &w.Error)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &w.Plan)
	}
	return w, err
}

func (s Store) ActiveEC(ctx context.Context, pool string) (ECWork, error) {
	w, err := scanECWork(s.DB.QueryRowContext(ctx, ecWorkQuery+` WHERE pool_id=? AND
		(state IN ('pending','encoding','verifying') OR cleanup_pending=1) ORDER BY generation LIMIT 1`, pool))
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return w, err
}

func (s Store) EncodedEC(ctx context.Context, pool string) ([]ECWork, error) {
	rows, err := s.DB.QueryContext(ctx, ecWorkQuery+` AS c WHERE c.pool_id=? AND c.direction='encode' AND c.cutover_committed=1
		AND EXISTS(SELECT 1 FROM storage_pool_volumes v WHERE v.pool_id=c.pool_id AND v.volume_id=c.volume_id
		AND v.layout='ec' AND v.generation=c.generation) ORDER BY c.volume_id`, pool)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ECWork
	for rows.Next() {
		w, err := scanECWork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s Store) BeginEC(ctx context.Context, w ECWork, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled, restore, busy bool
	if err := tx.QueryRowContext(ctx, `SELECT ec_enabled, ec_restore, EXISTS(
		SELECT 1 FROM storage_pool_ec_conversions WHERE pool_id=?
		AND (state IN ('pending','encoding','verifying') OR cleanup_pending=1))
		FROM storage_pools WHERE pool_id=?`, w.PoolID, w.PoolID).Scan(&enabled, &restore, &busy); err != nil {
		return err
	}
	if busy || w.Direction == "encode" && !enabled || w.Direction == "restore" && !restore {
		return Fault("conflict", "EC 策略或在途转换已变化")
	}
	plan, err := json.Marshal(w.Plan)
	if err != nil {
		return err
	}
	sources, err := json.Marshal(w.Plan.Input.Sources)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO storage_pool_volumes(pool_id,volume_id,collection,layout,state,
		data_bytes,max_bytes,last_write_at,locations_json,checked_at)
		VALUES (?,?,?,'replicated','converting',?,?,?,?,?)
		ON CONFLICT(pool_id,volume_id) DO UPDATE SET state='converting',checked_at=excluded.checked_at`,
		w.PoolID, w.Volume, w.Plan.Input.Collection, w.Plan.Bytes, w.Plan.Limit, w.Plan.LastWrite, string(sources), now.Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO storage_pool_ec_conversions(pool_id,volume_id,generation,operation_id,
		direction,state,source_json,target_json,updated_at) VALUES (?,?,?,?,?,'pending',?,?,?)`,
		w.PoolID, w.Volume, w.Generation, w.Operation, w.Direction, string(sources), string(plan), now.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) SaveEC(ctx context.Context, w ECWork, now time.Time) error {
	b, err := json.Marshal(w.Plan)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE storage_pool_ec_conversions SET state=?,error_code=?,target_json=?,cleanup_pending=?,updated_at=?
		WHERE pool_id=? AND volume_id=? AND generation=?`,
		w.State, w.Error, string(b), w.Cleanup, now.Unix(), w.PoolID, w.Volume, w.Generation)
	return err
}

func (s Store) CommitEC(ctx context.Context, w ECWork, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE storage_pool_ec_conversions SET state='done',error_code=NULL,cutover_committed=1,cleanup_pending=1,updated_at=?
		WHERE pool_id=? AND volume_id=? AND generation=? AND state='verifying' AND cutover_committed=0`,
		now.Unix(), w.PoolID, w.Volume, w.Generation)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return Fault("conflict", "EC 持久切换缺少原验证记录")
	}
	layout := "ec"
	var locations any = w.Plan.Input.Members
	if w.Direction == "restore" {
		layout, locations = "replicated", w.Plan.Input.Restore
	}
	raw, err := json.Marshal(locations)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE storage_pool_volumes SET layout=?,state='ready',generation=?,locations_json=?,checked_at=?
		WHERE pool_id=? AND volume_id=?`, layout, w.Generation, string(raw), now.Unix(), w.PoolID, w.Volume); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) ECStatus(ctx context.Context, pool string) ([]protocol.PoolECStatus, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT v.volume_id,v.collection,v.layout,v.state,c.direction,c.state,c.generation,
		c.cleanup_pending,c.operation_id,COALESCE(c.error_code,'') FROM storage_pool_volumes v
		JOIN storage_pool_ec_conversions c ON c.pool_id=v.pool_id AND c.volume_id=v.volume_id
		AND c.generation=(SELECT max(generation) FROM storage_pool_ec_conversions
			WHERE pool_id=v.pool_id AND volume_id=v.volume_id)
		WHERE v.pool_id=? ORDER BY c.updated_at DESC,v.volume_id LIMIT 100`, pool)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.PoolECStatus
	for rows.Next() {
		var row protocol.PoolECStatus
		var generation int64
		if err := rows.Scan(&row.Volume, &row.Collection, &row.Layout, &row.State, &row.Direction, &row.Conversion,
			&generation, &row.CleanupPending, &row.Operation, &row.Error); err != nil {
			return nil, err
		}
		row.Generation = strconv.FormatInt(generation, 10)
		out = append(out, row)
	}
	return out, rows.Err()
}
