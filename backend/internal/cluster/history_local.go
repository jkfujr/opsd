package cluster

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

const localHistoryVersion = 1

type localHistoryState struct {
	path, source string
	sampleMu     sync.Mutex
	sampleReady  chan struct{}
	probeOnce    sync.Once
	mu           sync.Mutex
	store        *storage.LocalHistory
	since        time.Time
	state, issue string
}

func (h *localHistoryState) allowSampling() {
	h.probeOnce.Do(func() { close(h.sampleReady) })
}

func (h *localHistoryState) snapshot() (*storage.LocalHistory, time.Time, string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.store, h.since, h.state, h.issue
}

func (h *localHistoryState) writeResult(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case err == nil:
		h.state, h.issue = "ready", ""
	case errors.Is(err, storage.ErrHistoryFull):
		h.state, h.issue = "history_full", storage.ErrHistoryFull.Error()
	default:
		h.state, h.issue = "error", err.Error()
	}
}

type LocalHistoryStatus struct {
	NodeID          string     `json:"node_id"`
	Revision        uint64     `json:"revision"`
	Directory       string     `json:"directory"`
	IntervalMinutes int        `json:"interval_minutes"`
	RetentionDays   int        `json:"retention_days"`
	DBBytes         int64      `json:"db_bytes"`
	WALBytes        int64      `json:"wal_bytes"`
	MaxDBBytes      int64      `json:"max_db_bytes"`
	WALBudgetBytes  int64      `json:"wal_budget_bytes"`
	FirstSampleAt   *time.Time `json:"first_sample_at,omitempty"`
	LastSampleAt    *time.Time `json:"last_sample_at,omitempty"`
	State           string     `json:"state"`
	Error           string     `json:"error,omitempty"`
}

func (n *Node) localHistoryStatus(ctx context.Context) (LocalHistoryStatus, error) {
	config, err := n.Config(ctx)
	h, _, state, issue := n.history.snapshot()
	out := LocalHistoryStatus{
		NodeID: n.ID, Revision: config.Revision, Directory: filepath.Dir(n.history.path), IntervalMinutes: config.IntervalMinutes,
		RetentionDays: config.RetentionDays, MaxDBBytes: storage.HistoryDBBytes, WALBudgetBytes: storage.HistoryWALBytes, State: state, Error: issue,
	}
	if err == nil && h != nil {
		out.DBBytes, out.WALBytes, err = h.Sizes()
		if err == nil {
			var first, last sql.NullInt64
			first, last, err = h.Bounds(ctx)
			if first.Valid {
				at := time.Unix(first.Int64, 0).UTC()
				out.FirstSampleAt = &at
			}
			if last.Valid {
				at := time.Unix(last.Int64, 0).UTC()
				out.LastSampleAt = &at
			}
		}
	}
	if err != nil {
		return out, fault("dependency_unavailable", "本机历史状态读取失败")
	}
	return out, nil
}

func compactHistorySample(m protocol.Metrics) ([]byte, error) {
	if m.Errors == nil {
		m.Errors = []string{}
	}
	body, err := json.Marshal(m)
	if err != nil || len(body) <= 1024 {
		return body, err
	}
	// 裁剪错误文本时保留摘要，同一时间戳的异体样本仍可识别。
	m.Errors = []string{fmt.Sprintf("采集错误文本过长，摘要 %x", sha256.Sum256(body))}
	return json.Marshal(m)
}

func makeHistoryRow(at int64, body []byte) (storage.HistoryRow, error) {
	var m protocol.Metrics
	if err := json.Unmarshal(body, &m); err != nil || m.CollectedAt.IsZero() || m.CollectedAt.Unix() != at || at < 0 {
		return storage.HistoryRow{}, fmt.Errorf("时间戳 %d 的本机历史样本无效", at)
	}
	body, err := compactHistorySample(m)
	return storage.HistoryRow{At: at, Body: body}, err
}

func (n *Node) saveHistorySample(ctx context.Context, m protocol.Metrics, interval time.Duration) error {
	n.history.sampleMu.Lock()
	defer n.history.sampleMu.Unlock()
	h, _, _, _ := n.history.snapshot()
	if h == nil {
		body, err := compactHistorySample(m)
		if err != nil {
			return err
		}
		if err := n.writeLegacyHistory(ctx, []storage.HistoryRow{{At: m.CollectedAt.Unix(), Body: body}}); err != nil {
			return err
		}
		return n.pruneLegacyHistory(ctx, n.Now().Add(-storage.HistoryRetention).Unix())
	}
	m.CollectedAt = m.CollectedAt.UTC().Truncate(interval)
	cutoff := n.Now().Add(-storage.HistoryRetention).Unix()
	if m.CollectedAt.Unix() < cutoff {
		return nil
	}
	_, last, err := h.Bounds(ctx)
	if err == nil && last.Valid && last.Int64 >= m.CollectedAt.Unix() {
		return nil
	}
	if err == nil {
		var body []byte
		body, err = compactHistorySample(m)
		if err == nil {
			rows := []storage.HistoryRow{{At: m.CollectedAt.Unix(), Body: body}}
			err = h.Save(ctx, rows)
			if errors.Is(err, storage.ErrHistoryFull) {
				if e := h.Prune(ctx, cutoff); e == nil {
					err = h.Save(ctx, rows)
				}
			}
		}
	}
	n.history.writeResult(err)
	return err
}

func (n *Node) localHistoryLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var nextPrune time.Time
	for {
		h, _, _, _ := n.history.snapshot()
		if h == nil {
			ctx, cancel := context.WithTimeout(n.ctx, time.Minute)
			if err := n.initializeLocalHistory(ctx); err != nil && ctx.Err() != context.Canceled {
				n.history.mu.Lock()
				n.history.state, n.history.issue = "fallback", err.Error()
				n.history.mu.Unlock()
			}
			cancel()
			h, _, _, _ = n.history.snapshot()
		}
		if h != nil {
			ctx, cancel := context.WithTimeout(n.ctx, 45*time.Second)
			now := n.Now()
			if !now.Before(nextPrune) {
				if err := h.Prune(ctx, now.Add(-storage.HistoryRetention).Unix()); err != nil {
					n.history.writeResult(err)
				}
				nextPrune = now.UTC().Truncate(5 * time.Minute).Add(5 * time.Minute)
			}
			if err := n.legacyMirror(ctx, h); err != nil && n.Log != nil && ctx.Err() != context.Canceled {
				n.Log("历史过渡补写未完成：%v", err)
			}
			cancel()
		}
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func historyBounds(q HistoryQuery) (int64, int64) {
	from, to := q.From.Unix(), q.To.Unix()
	if q.To.Nanosecond() > 0 {
		to++
	}
	return from, to
}

type historyPager func(context.Context, int64, int64, int) ([]storage.HistoryRow, error)

func walkHistory(ctx context.Context, page historyPager, from, to int64, limit int, visit func([]storage.HistoryRow) error) error {
	for from < to {
		rows, err := page(ctx, from, to, limit)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		if err := visit(rows); err != nil {
			return err
		}
		from = rows[len(rows)-1].At + 1
	}
	return nil
}

func legacyPager(db *sql.DB) historyPager {
	return func(ctx context.Context, from, to int64, limit int) ([]storage.HistoryRow, error) {
		rows, err := storage.LegacyHistoryPage(ctx, db, from, to, limit)
		if err != nil {
			return nil, err
		}
		for i, row := range rows {
			rows[i], err = makeHistoryRow(row.At, row.Body)
			if err != nil {
				return nil, err
			}
		}
		return rows, nil
	}
}

type historyDigest struct {
	count       int
	first, last int64
	hash        [32]byte
}

func digestHistory(ctx context.Context, page historyPager, from, to int64) (historyDigest, error) {
	var out historyDigest
	h := sha256.New()
	err := walkHistory(ctx, page, from, to, storage.HistoryPage, func(rows []storage.HistoryRow) error {
		for _, row := range rows {
			if out.count == 0 {
				out.first = row.At
			}
			out.last = row.At
			out.count++
			_, _ = h.Write(binary.BigEndian.AppendUint64(nil, uint64(row.At)))
			_, _ = h.Write(binary.BigEndian.AppendUint32(nil, uint32(len(row.Body))))
			_, _ = h.Write(row.Body)
		}
		return nil
	})
	copy(out.hash[:], h.Sum(nil))
	return out, err
}

func (n *Node) initializeLocalHistory(ctx context.Context) error {
	defer n.history.allowSampling()
	h, err := storage.OpenLocalHistory(ctx, n.history.path, n.ID)
	if err != nil {
		return err
	}
	active := false
	defer func() {
		if !active {
			_ = h.Close()
		}
	}()
	through, mirror, err := h.Meta(ctx)
	if err != nil {
		return err
	}
	if !through.Valid {
		n.history.allowSampling()
		err = n.importLocalHistory(ctx, h)
		active = err == nil
		return err
	}
	if !mirror.Valid {
		return fmt.Errorf("本机历史库缺少补写水位")
	}
	if err := h.Integrity(ctx); err != nil {
		return err
	}
	n.publishLocalHistory(h, through.Int64)
	active = true
	return nil
}

func (n *Node) publishLocalHistory(h *storage.LocalHistory, through int64) {
	n.history.mu.Lock()
	defer n.history.mu.Unlock()
	n.history.store, n.history.since = h, time.Unix(through, 0).UTC()
	n.history.state, n.history.issue = "ready", ""
}

func (n *Node) importLocalHistory(ctx context.Context, h *storage.LocalHistory) error {
	now := n.Now()
	from, to := now.Add(-storage.HistoryRetention).Unix(), now.Unix()+1
	source, release, err := storage.HistoryImportSource(ctx, n.history.source, filepath.Dir(n.history.path))
	if err != nil {
		return err
	}
	defer release()
	save := func(rows []storage.HistoryRow) error { return h.Save(ctx, rows) }
	if err := walkHistory(ctx, legacyPager(source), from, to, storage.HistoryBatch, save); err != nil {
		return err
	}
	if err := h.Integrity(ctx); err != nil {
		return err
	}
	// 旧库只在切换前复核和补齐增量时暂停五分钟写入。
	n.history.sampleMu.Lock()
	defer n.history.sampleMu.Unlock()
	now = n.Now()
	from, to = now.Add(-storage.HistoryRetention).Unix(), now.Unix()+1
	legacy := legacyPager(n.DB)
	want, err := digestHistory(ctx, legacy, from, to)
	if err != nil {
		return err
	}
	got, err := digestHistory(ctx, h.Page, from, to)
	if err != nil {
		return err
	}
	if got != want {
		if err := walkHistory(ctx, legacy, from, to, storage.HistoryBatch, save); err != nil {
			return err
		}
		got, err = digestHistory(ctx, h.Page, from, to)
		if err != nil {
			return err
		}
	}
	if got != want {
		return fmt.Errorf("本机历史导入行数、时间范围或内容摘要不一致")
	}
	if err := h.FinishImport(ctx, to); err != nil {
		return err
	}
	n.publishLocalHistory(h, to)
	return nil
}

func (n *Node) localHistory(ctx context.Context, q HistoryQuery) ([]protocol.Metrics, error) {
	h, _, _, _ := n.history.snapshot()
	if h == nil {
		return n.legacyLocalHistory(ctx, q)
	}
	return n.historySlice(ctx, h, HistoryQuery{From: q.From, To: time.Unix(q.To.Unix()+1, 0).UTC()})
}

func (n *Node) historySlice(ctx context.Context, h *storage.LocalHistory, q HistoryQuery) ([]protocol.Metrics, error) {
	if cutoff := n.Now().Add(-storage.HistoryRetention); q.From.Before(cutoff) {
		q.From = cutoff
	}
	from, to := historyBounds(q)
	out := []protocol.Metrics{}
	err := walkHistory(ctx, h.Page, from, to, storage.HistoryPage, func(rows []storage.HistoryRow) error {
		for _, row := range rows {
			var m protocol.Metrics
			if err := json.Unmarshal(row.Body, &m); err != nil {
				return err
			}
			if !m.CollectedAt.Before(q.From) && m.CollectedAt.Before(q.To) {
				out = append(out, m)
			}
		}
		return nil
	})
	return out, err
}
