package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	HistoryRetention = 7 * 24 * time.Hour
	HistoryBatch     = 128
	HistoryPage      = 256
	HistoryDBBytes   = 32 << 20
	HistoryWALBytes  = 4 << 20
	// 负值区分待补写的空水位和已按升级条件退出的补写任务。
	HistoryMirrorFromStart = -1
	HistoryMirrorRetired   = -2
)

var ErrHistoryFull = errors.New("本机历史库达到容量或 WAL 上限")

type HistoryRow struct {
	At   int64
	Body []byte
}

type LocalHistory struct {
	path   string
	db     *sql.DB
	reader *sql.DB
	mu     sync.Mutex
}

func OpenLocalHistory(ctx context.Context, path, nodeID string) (*LocalHistory, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = errors.Join(file.Chmod(0600), file.Close()); err != nil {
		return nil, err
	}
	db, err := Open(ctx, path, history)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*LocalHistory, error) { db.Close(); return nil, err }
	var pageSize, vacuum, maxPages int
	if err = db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err == nil {
		err = db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&vacuum)
	}
	if err == nil {
		err = db.QueryRowContext(ctx, "PRAGMA max_page_count").Scan(&maxPages)
	}
	if err != nil {
		return fail(err)
	}
	if pageSize != 4096 || vacuum != 2 || maxPages != HistoryDBBytes/4096 {
		return fail(fmt.Errorf("本机历史库页大小、回收模式或容量设置不符"))
	}
	var owner string
	err = db.QueryRowContext(ctx, "SELECT node_id FROM history_meta WHERE id=1").Scan(&owner)
	if err == sql.ErrNoRows {
		_, err = db.ExecContext(ctx, "INSERT INTO history_meta(id,node_id) VALUES(1,?)", nodeID)
		owner = nodeID
	}
	if err != nil {
		return fail(err)
	}
	if owner != nodeID {
		return fail(fmt.Errorf("本机历史库属于另一节点"))
	}
	reader, err := readOnly(ctx, path, "&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)")
	if err != nil {
		return fail(err)
	}
	return &LocalHistory{path: path, db: db, reader: reader}, nil
}

func (h *LocalHistory) Close() error { return errors.Join(h.reader.Close(), h.db.Close()) }

func (h *LocalHistory) Meta(ctx context.Context) (importThrough, mirrorThrough sql.NullInt64, err error) {
	err = h.reader.QueryRowContext(ctx, "SELECT import_through,mirror_through FROM history_meta WHERE id=1").Scan(&importThrough, &mirrorThrough)
	return
}

func (h *LocalHistory) FinishImport(ctx context.Context, through int64) error {
	return h.write(ctx, func() error {
		_, err := h.db.ExecContext(ctx, "UPDATE history_meta SET import_through=?,mirror_through=? WHERE id=1 AND import_through IS NULL", through, HistoryMirrorFromStart)
		return err
	})
}

func (h *LocalHistory) AdvanceMirror(ctx context.Context, from, through int64) error {
	return h.write(ctx, func() error {
		result, err := h.db.ExecContext(ctx, "UPDATE history_meta SET mirror_through=? WHERE id=1 AND mirror_through=?", through, from)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err == nil && count != 1 {
			err = fmt.Errorf("历史补写水位已变化")
		}
		return err
	})
}

func (h *LocalHistory) Integrity(ctx context.Context) error {
	var result string
	if err := h.reader.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("本机历史库完整性检查失败：%s", result)
	}
	return nil
}

func (h *LocalHistory) Bounds(ctx context.Context) (first, last sql.NullInt64, err error) {
	err = h.reader.QueryRowContext(ctx, "SELECT min(collected_at),max(collected_at) FROM history_samples").Scan(&first, &last)
	return
}

func (h *LocalHistory) Sizes() (int64, int64, error) {
	main, err := historySize(h.path)
	if err != nil {
		return 0, 0, err
	}
	wal, err := historySize(h.path + "-wal")
	return main, wal, err
}

func historySize(path string) (int64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (h *LocalHistory) Page(ctx context.Context, from, to int64, limit int) ([]HistoryRow, error) {
	return historyRows(ctx, h.reader, "history_samples", from, to, limit)
}

func LegacyHistoryPage(ctx context.Context, db *sql.DB, from, to int64, limit int) ([]HistoryRow, error) {
	return historyRows(ctx, db, "monitoring_samples", from, to, limit)
}

func LegacyHistoryLatest(ctx context.Context, db *sql.DB, before int64) (latest sql.NullInt64, err error) {
	err = db.QueryRowContext(ctx, "SELECT max(collected_at) FROM monitoring_samples WHERE collected_at<?", before).Scan(&latest)
	return
}

func historyRows(ctx context.Context, db *sql.DB, table string, from, to int64, limit int) ([]HistoryRow, error) {
	rows, err := db.QueryContext(ctx, "SELECT collected_at,body FROM "+table+" WHERE collected_at>=? AND collected_at<? ORDER BY collected_at LIMIT ?", from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryRow
	for rows.Next() {
		var row HistoryRow
		if err := rows.Scan(&row.At, &row.Body); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (h *LocalHistory) Save(ctx context.Context, rows []HistoryRow) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > HistoryBatch {
		return fmt.Errorf("本机历史写入超过每批上限")
	}
	return h.write(ctx, func() error {
		tx, err := h.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, row := range rows {
			result, err := tx.ExecContext(ctx, "INSERT INTO history_samples(collected_at,body) VALUES(?,?) ON CONFLICT(collected_at) DO NOTHING", row.At, row.Body)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if count == 0 {
				var old []byte
				if err := tx.QueryRowContext(ctx, "SELECT body FROM history_samples WHERE collected_at=?", row.At).Scan(&old); err != nil {
					return err
				}
				if !bytes.Equal(old, row.Body) {
					return fmt.Errorf("时间戳 %d 的本机历史样本内容冲突", row.At)
				}
			}
		}
		return tx.Commit()
	})
}

func (h *LocalHistory) Prune(ctx context.Context, before int64) error {
	for {
		var removed int64
		err := h.write(ctx, func() error {
			result, err := h.db.ExecContext(ctx, "DELETE FROM history_samples WHERE collected_at IN (SELECT collected_at FROM history_samples WHERE collected_at<? ORDER BY collected_at LIMIT ?)", before, HistoryBatch)
			if err == nil {
				removed, err = result.RowsAffected()
			}
			return err
		})
		if err != nil {
			return err
		}
		if removed > 0 {
			if err := h.write(ctx, func() error {
				_, err := h.db.ExecContext(ctx, "PRAGMA incremental_vacuum(128)")
				return err
			}); err != nil {
				return err
			}
		}
		if removed < HistoryBatch {
			return nil
		}
	}
}

func (h *LocalHistory) write(ctx context.Context, write func() error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.checkpoint(ctx); err != nil {
		return err
	}
	if err := write(); err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code()&255 == sqlite3.SQLITE_FULL {
			return ErrHistoryFull
		}
		return err
	}
	return h.checkpoint(ctx)
}

func (h *LocalHistory) checkpoint(ctx context.Context) error {
	size, err := historySize(h.path + "-wal")
	if err != nil || size < HistoryWALBytes {
		return err
	}
	var busy, pages, checkpointed int
	if err := h.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed); err != nil {
		return errors.Join(ErrHistoryFull, err)
	}
	if busy != 0 {
		return ErrHistoryFull
	}
	return nil
}

// HistoryImportSource 用独立只读连接取得旧库快照，采样继续使用原连接。
func HistoryImportSource(ctx context.Context, source, dir string) (*sql.DB, func(), error) {
	path := filepath.Join(dir, ".history-import.db")
	cleanup := func() error {
		var failure error
		for _, name := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
			if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
				failure = errors.Join(failure, err)
			}
		}
		return failure
	}
	// 节点实例锁只允许一个导入者。重启时丢弃上次中断留下的源库副本。
	if err := cleanup(); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, nil, err
	}
	_ = file.Close()
	copyDB, err := BackupSQLite(ctx, source, path)
	if err != nil {
		_ = cleanup()
		return nil, nil, err
	}
	return copyDB, func() { _ = copyDB.Close(); _ = cleanup() }, nil
}

func BackupSQLite(ctx context.Context, source, destination string) (*sql.DB, error) {
	db, err := ReadOnly(ctx, source)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	uri, err := databaseURL(destination)
	if err != nil {
		return nil, err
	}
	err = conn.Raw(func(raw any) (err error) {
		backup, err := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		}).NewBackup(uri.String())
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, backup.Finish()) }()
		for ctx.Err() == nil {
			more, err := backup.Step(128)
			if err != nil || !more {
				return err
			}
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return ReadOnly(ctx, destination)
}
