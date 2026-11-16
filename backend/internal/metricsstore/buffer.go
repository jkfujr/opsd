package metricsstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/storage"
)

type Buffer struct{ DB *sql.DB }

func Open(ctx context.Context, filename string) (*Buffer, error) {
	db, err := storage.Open(ctx, filename, storage.Metrics)
	if err != nil {
		return nil, err
	}
	return &Buffer{DB: db}, nil
}
func Digest(b Batch) string {
	data, _ := json.Marshal(b)
	v := sha256.Sum256(data)
	return hex.EncodeToString(v[:])
}

func (s *Buffer) Append(ctx context.Context, b Batch, node string, now time.Time, limit int64) (Batch, error) {
	if limit <= 0 {
		limit = BufferBytes
	}
	b.ID = identity.Random()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	var last int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(CAST(value AS INTEGER)),0) FROM state WHERE key='last_sample'").Scan(&last); err != nil {
		return b, err
	}
	if b.At <= last || b.At > now.Add(time.Minute).UnixMilli() {
		if err = addGap(ctx, tx, Gap{NodeID: node, From: min(b.At, now.UnixMilli()), To: min(last, now.UnixMilli()), Reason: "clock_changed", BatchID: b.ID}); err != nil {
			return b, err
		}
		return b, tx.Commit()
	}
	data, err := json.Marshal(b)
	if err != nil {
		return b, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO batches(id,at,body,size) VALUES(?,?,?,?)", b.ID, b.At, data, len(data)+256)
	if err != nil {
		return b, err
	}
	b.Sequence, err = result.LastInsertId()
	if err != nil {
		return b, err
	}
	if err = b.Validate(now); err != nil {
		return b, err
	}
	data, err = json.Marshal(b)
	if err != nil {
		return b, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE batches SET body=?,size=? WHERE sequence=?", data, len(data)+256, b.Sequence); err != nil {
		return b, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO state(key,value) VALUES('last_sample',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", b.At); err != nil {
		return b, err
	}
	if last > 0 && b.At-last > 2000 {
		if err = addGap(ctx, tx, Gap{NodeID: node, From: last + 1000, To: b.At - 1, Reason: "sampling_delay"}); err != nil {
			return b, err
		}
	}
	for _, reason := range b.Errors {
		if err = addGap(ctx, tx, Gap{NodeID: node, From: b.At, To: b.At + 999, Reason: reason, BatchID: b.ID}); err != nil {
			return b, err
		}
	}
	for _, sample := range b.Samples {
		if sample.ResourceID != "" {
			key := sample.Metric + "/" + sample.ResourceID + "/" + sample.Backend
			if _, err = tx.ExecContext(ctx, "INSERT INTO resources(sample_key,seen_at) VALUES(?,?) ON CONFLICT(sample_key) DO UPDATE SET seen_at=excluded.seen_at", key, b.At); err != nil {
				return b, err
			}
		}
	}
	if err = trim(ctx, tx, node, now, limit); err != nil {
		return b, err
	}
	return b, tx.Commit()
}

func trim(ctx context.Context, tx *sql.Tx, node string, now time.Time, limit int64) error {
	var total int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(size),0) FROM batches").Scan(&total); err != nil {
		return err
	}
	for {
		var seq, at, size int64
		var id string
		err := tx.QueryRowContext(ctx, "SELECT sequence,id,at,size FROM batches ORDER BY sequence LIMIT 1").Scan(&seq, &id, &at, &size)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if at >= now.Add(-BufferAge).UnixMilli() && total <= limit {
			return nil
		}
		reason := "buffer_expired"
		if total > limit {
			reason = "buffer_capacity"
		}
		if err = addGap(ctx, tx, Gap{NodeID: node, From: at, To: at + 999, Reason: reason, BatchID: id}); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM batches WHERE sequence=?", seq); err != nil {
			return err
		}
		total -= size
	}
}

func addGap(ctx context.Context, tx *sql.Tx, g Gap) error {
	if g.To < g.From {
		g.To = g.From
	}
	var id, start, end int64
	err := tx.QueryRowContext(ctx, "SELECT id,from_ms,to_ms FROM gaps WHERE node_id=? AND reason=? AND to_ms>=? AND from_ms<=? ORDER BY id DESC LIMIT 1", g.NodeID, g.Reason, g.From-2000, g.To+2000).Scan(&id, &start, &end)
	if err == nil && g.From >= start && g.To <= end {
		return nil
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE gaps SET from_ms=?,to_ms=?,batch_id=? WHERE id=?", min(start, g.From), max(end, g.To), g.BatchID, id)
		return err
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO gaps(node_id,from_ms,to_ms,reason,batch_id) VALUES(?,?,?,?,?)", g.NodeID, g.From, g.To, g.Reason, g.BatchID)
	return err
}

func (s *Buffer) Gap(ctx context.Context, g Gap) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = addGap(ctx, tx, g); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Buffer) Pending(ctx context.Context, node string, now time.Time) ([]Batch, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = trim(ctx, tx, node, now, BufferBytes); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT body FROM batches ORDER BY sequence LIMIT 5")
	if err != nil {
		return nil, err
	}
	out := []Batch{}
	for rows.Next() {
		var data []byte
		var b Batch
		if err = rows.Scan(&data); err != nil {
			break
		}
		if err = json.Unmarshal(data, &b); err != nil {
			break
		}
		out = append(out, b)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
func (s *Buffer) Acknowledge(ctx context.Context, b Batch) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM batches WHERE id=? AND sequence=?", b.ID, b.Sequence)
	return err
}
func (s *Buffer) Reject(ctx context.Context, node string, b Batch, reason string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = addGap(ctx, tx, Gap{NodeID: node, From: b.At, To: b.At + 999, Reason: reason, BatchID: b.ID}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM batches WHERE id=? AND sequence=?", b.ID, b.Sequence); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Buffer) Receipt(ctx context.Context, node, id string) (Receipt, error) {
	out := Receipt{ID: id}
	err := s.DB.QueryRowContext(ctx, "SELECT digest,error FROM receipts WHERE node_id=? AND batch_id=?", node, id).Scan(&out.Digest, &out.Error)
	return out, err
}
func (s *Buffer) SequenceReceipt(ctx context.Context, node string, sequence int64) (Receipt, error) {
	var out Receipt
	err := s.DB.QueryRowContext(ctx, "SELECT batch_id,digest,error FROM receipts WHERE node_id=? AND sequence=?", node, sequence).Scan(&out.ID, &out.Digest, &out.Error)
	return out, err
}
func (s *Buffer) Received(ctx context.Context, node string, b Batch, rejected string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	digest := Digest(b)
	_, err = tx.ExecContext(ctx, "INSERT INTO receipts(node_id,batch_id,sequence,digest,error,at) VALUES(?,?,?,?,?,?) ON CONFLICT(node_id,batch_id) DO NOTHING", node, b.ID, b.Sequence, digest, rejected, b.At)
	if err != nil {
		return err
	}
	var actual string
	if err = tx.QueryRowContext(ctx, "SELECT digest FROM receipts WHERE node_id=? AND batch_id=?", node, b.ID).Scan(&actual); err != nil {
		return err
	}
	if actual != digest {
		return fmt.Errorf("批次标识已用于其他内容")
	}
	if rejected != "" {
		if err = addGap(ctx, tx, Gap{NodeID: node, From: b.At, To: b.At + 999, Reason: rejected, BatchID: b.ID}); err != nil {
			return err
		}
	}
	for _, reason := range b.Errors {
		if err = addGap(ctx, tx, Gap{NodeID: node, From: b.At, To: b.At + 999, Reason: reason, BatchID: b.ID}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Buffer) Status(ctx context.Context, node string) (BufferStatus, error) {
	out := BufferStatus{Gaps: []Gap{}}
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(size),0),COALESCE(MIN(at),0),COALESCE(MAX(at),0) FROM batches").Scan(&out.Batches, &out.Bytes, &out.Oldest, &out.Latest)
	if err != nil {
		return out, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT node_id,from_ms,to_ms,reason,batch_id FROM gaps WHERE node_id=? ORDER BY id DESC LIMIT 100", node)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var g Gap
		if err = rows.Scan(&g.NodeID, &g.From, &g.To, &g.Reason, &g.BatchID); err != nil {
			return out, err
		}
		out.Gaps = append(out.Gaps, g)
	}
	return out, rows.Err()
}
func (s *Buffer) Cleanup(ctx context.Context, now time.Time) error {
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM receipts WHERE at<?", now.Add(-2*time.Hour-5*time.Minute).UnixMilli()); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM resources WHERE seen_at<?", now.Add(-2*time.Hour).UnixMilli()); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, "DELETE FROM gaps WHERE to_ms<?", now.Add(-365*24*time.Hour).UnixMilli())
	return err
}

func (s *Buffer) Inventory(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT sample_key FROM resources WHERE seen_at>=? ORDER BY sample_key LIMIT 10001", now.Add(-2*time.Hour).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	if len(out) > 10000 {
		return nil, fmt.Errorf("监控资源数量超过上限")
	}
	return out, rows.Err()
}
func (s *Buffer) DiskBytes(ctx context.Context) (int64, error) {
	var count, size int64
	if err := s.DB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&count); err != nil {
		return 0, err
	}
	if err := s.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&size); err != nil {
		return 0, err
	}
	return count * size, nil
}
