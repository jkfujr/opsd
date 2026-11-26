package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jkfujr/opsd/internal/storage"
)

type historyCapability struct {
	LocalHistoryVersion int        `json:"local_history_version"`
	LocalHistorySince   *time.Time `json:"local_history_since,omitempty"`
}

func (n *Node) localHistoryCapability() historyCapability {
	_, since, _, _ := n.history.snapshot()
	out := historyCapability{LocalHistoryVersion: localHistoryVersion}
	if !since.IsZero() {
		out.LocalHistorySince = &since
	}
	return out
}

func (n *Node) writeLegacyHistory(ctx context.Context, rows []storage.HistoryRow) error {
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, row := range rows {
		insert, err := tx.ExecContext(ctx, "INSERT INTO monitoring_samples(collected_at,body) VALUES(?,?) ON CONFLICT(collected_at) DO NOTHING", row.At, string(row.Body))
		if err != nil {
			return err
		}
		count, err := insert.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			var raw []byte
			if err := tx.QueryRowContext(ctx, "SELECT body FROM monitoring_samples WHERE collected_at=?", row.At).Scan(&raw); err != nil {
				return err
			}
			if err := matchHistoryRow(row, raw); err != nil {
				return err
			}
		}
		object := strconv.FormatInt(row.At, 10)
		var raw []byte
		err = tx.QueryRowContext(ctx, "SELECT body FROM records WHERE kind='metrics' AND object=? ORDER BY sequence DESC LIMIT 1", object).Scan(&raw)
		if err == sql.ErrNoRows {
			err = n.appendTx(ctx, tx, "metrics", object, json.RawMessage(row.Body), time.Unix(row.At, 0))
		} else if err == nil {
			var signed Record
			if err = json.Unmarshal(raw, &signed); err == nil {
				err = matchHistoryRow(row, signed.Data)
			}
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func matchHistoryRow(want storage.HistoryRow, body []byte) error {
	got, err := makeHistoryRow(want.At, body)
	if err != nil {
		return err
	}
	if !bytes.Equal(want.Body, got.Body) {
		return fmt.Errorf("时间戳 %d 的旧监控样本内容冲突", want.At)
	}
	return nil
}

func (n *Node) pruneLegacyHistory(ctx context.Context, before int64) error {
	for _, statement := range []string{
		"DELETE FROM monitoring_samples WHERE collected_at IN (SELECT collected_at FROM monitoring_samples WHERE collected_at<? ORDER BY collected_at LIMIT ?)",
		"DELETE FROM records WHERE id IN (SELECT id FROM records WHERE kind='metrics' AND uploaded<>0 AND CAST(object AS INTEGER)<? LIMIT ?)",
	} {
		for {
			result, err := n.DB.ExecContext(ctx, statement, before, storage.HistoryBatch)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if count < storage.HistoryBatch {
				break
			}
		}
	}
	return nil
}

func (n *Node) legacyMirror(ctx context.Context, h *storage.LocalHistory) error {
	imported, mirrored, err := h.Meta(ctx)
	if err != nil || mirrored.Int64 == storage.HistoryMirrorRetired {
		return err
	}
	now := n.Now().UTC()
	from, to := now.Add(-storage.HistoryRetention).Unix(), now.Unix()+1
	mirror := mirrored.Int64
	if err := walkHistory(ctx, h.Page, max(from, mirror+1), to, storage.HistoryBatch, func(rows []storage.HistoryRow) error {
		if err := n.writeLegacyHistory(ctx, rows); err != nil {
			return err
		}
		last := rows[len(rows)-1].At
		if err := h.AdvanceMirror(ctx, mirror, last); err != nil {
			return err
		}
		mirror = last
		return nil
	}); err != nil {
		return err
	}
	if err := n.pruneLegacyHistory(ctx, from); err != nil {
		return err
	}
	if !imported.Valid || imported.Int64 > from {
		return nil
	}
	return n.retireLegacyMirror(ctx, h, mirror, from, to)
}

func (n *Node) retireLegacyMirror(ctx context.Context, h *storage.LocalHistory, mirror, from, to int64) error {
	directory, err := n.Directory(ctx)
	if err != nil {
		return err
	}
	for _, member := range directory.Members {
		if member.ID == n.ID || n.Revoked(ctx, member.ID) {
			continue
		}
		var capability historyCapability
		check, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := n.Call(check, member.ID, "api.capabilities", nil, &capability)
		cancel()
		if err != nil || capability.LocalHistoryVersion < localHistoryVersion || capability.LocalHistorySince == nil || capability.LocalHistorySince.Unix() > from {
			return nil
		}
	}
	n.history.sampleMu.Lock()
	defer n.history.sampleMu.Unlock()
	local, err := digestHistory(ctx, h.Page, from, to)
	if err != nil || local.count == 0 || local.last != mirror {
		return err
	}
	old, err := digestHistory(ctx, legacyPager(n.DB), from, to)
	if err != nil || local != old {
		return err
	}
	var pending int
	if err := n.DB.QueryRowContext(ctx, "SELECT count(*) FROM records WHERE kind='metrics' AND uploaded=0").Scan(&pending); err != nil || pending != 0 {
		return err
	}
	return h.AdvanceMirror(ctx, mirror, storage.HistoryMirrorRetired)
}
