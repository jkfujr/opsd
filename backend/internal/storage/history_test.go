package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func historyTestRow(at int64) HistoryRow {
	return HistoryRow{At: at, Body: []byte(`{"value":"` + strings.Repeat("A", 980) + `"}`)}
}

func TestLocalHistoryCapacityAndWAL(t *testing.T) {
	for _, limit := range []string{"page_count", "wal"} {
		t.Run(limit, func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "monitoring", "history.db")
			h, err := OpenLocalHistory(ctx, path, "node-a")
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			for query, want := range map[string]int{"page_size": 4096, "auto_vacuum": 2, "max_page_count": 8192, "foreign_keys": 1, "synchronous": 2, "busy_timeout": 5000} {
				var got int
				if err := h.db.QueryRowContext(ctx, "PRAGMA "+query).Scan(&got); err != nil || got != want {
					t.Fatal(query, got, err)
				}
			}
			if err := h.Save(ctx, []HistoryRow{historyTestRow(1)}); err != nil {
				t.Fatal(err)
			}
			if limit == "page_count" {
				if _, err := h.db.ExecContext(ctx, "PRAGMA max_page_count=12"); err != nil {
					t.Fatal(err)
				}
			} else {
				reader, err := ReadOnly(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				pin, err := reader.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer pin.Rollback()
				var count int
				if err := pin.QueryRow("SELECT count(*) FROM history_samples").Scan(&count); err != nil || count != 1 {
					t.Fatal(count, err)
				}
				for next := int64(2); next < 10000; next += HistoryBatch {
					rows := make([]HistoryRow, HistoryBatch)
					for i := range rows {
						rows[i] = historyTestRow(next + int64(i))
					}
					short, cancel := context.WithTimeout(ctx, time.Second)
					err = h.Save(short, rows)
					cancel()
					if err != nil {
						break
					}
				}
				if !errors.Is(err, ErrHistoryFull) {
					t.Fatal("WAL 上限未止写", err)
				}
				snapshot, e := h.Page(ctx, 1, 2, 1)
				if e != nil || len(snapshot) != 1 {
					t.Fatal("WAL 受阻时旧样本不可读", snapshot, e)
				}
				fenced, cancel := context.WithTimeout(ctx, time.Second)
				err = h.Save(fenced, []HistoryRow{historyTestRow(20000)})
				cancel()
				if !errors.Is(err, ErrHistoryFull) {
					t.Fatal("WAL 受阻期间允许新写入", err)
				}
				if err := pin.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			if limit == "page_count" {
				rows := make([]HistoryRow, HistoryBatch)
				for i := range rows {
					rows[i] = historyTestRow(int64(2 + i))
				}
				if err := h.Save(ctx, rows); !errors.Is(err, ErrHistoryFull) {
					t.Fatal("主库页上限未止写", err)
				}
				visible, err := h.Page(ctx, 1, 200, HistoryPage)
				if err != nil || len(visible) != 1 || visible[0].At != 1 {
					t.Fatal("写满后事务回滚或旧数据保留失败", len(visible), err)
				}
			}
			if err := h.Save(ctx, []HistoryRow{historyTestRow(20001)}); err != nil {
				t.Fatal("恢复写入失败", err)
			}
			if err := h.Integrity(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
