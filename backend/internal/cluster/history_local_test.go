package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

func testHistoryMetric(at time.Time, cores int) protocol.Metrics {
	return protocol.Metrics{CollectedAt: at, CPUCores: cores, Errors: []string{}}
}

func seedOldHistory(t *testing.T, n *Node, m protocol.Metrics) {
	t.Helper()
	body, _ := json.Marshal(m)
	if _, err := n.DB.Exec("INSERT INTO monitoring_samples(collected_at,body) VALUES(?,?)", m.CollectedAt.Unix(), string(body)); err != nil {
		t.Fatal(err)
	}
}

func TestLocalHistoryImportAndEightDayWindow(t *testing.T) {
	n, _ := openTestNode(t)
	start := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	now := start
	n.Now = func() time.Time { return now }
	first := testHistoryMetric(start.Add(-5*time.Minute), 4)
	seedOldHistory(t, n, first)
	seedOldHistory(t, n, testHistoryMetric(start.Add(-storage.HistoryRetention-time.Second), 9))
	h, err := storage.OpenLocalHistory(t.Context(), n.history.path, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := compactHistorySample(first)
	if err := h.Save(t.Context(), []storage.HistoryRow{{At: first.CollectedAt.Unix(), Body: body}}); err != nil {
		t.Fatal(err)
	}
	_ = h.Close()
	if err := n.initializeLocalHistory(t.Context()); err != nil {
		t.Fatal("中断后的重复导入失败", err)
	}
	h, _, _, _ = n.history.snapshot()
	imported, _ := h.Page(t.Context(), 0, start.Unix()+1, storage.HistoryPage)
	if len(imported) != 1 || imported[0].At != first.CollectedAt.Unix() {
		t.Fatal("导入窗口或去重错误", len(imported))
	}
	step := 5 * time.Minute
	for now = start; !now.After(start.Add(8 * 24 * time.Hour)); now = now.Add(step) {
		if err := n.saveHistorySample(t.Context(), testHistoryMetric(now, 8), step); err != nil {
			t.Fatal(err)
		}
		if err := h.Prune(t.Context(), now.Add(-storage.HistoryRetention).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(-step)
	from := now.Add(-storage.HistoryRetention)
	window := n.ReadHistoryWindow(t.Context(), []string{n.ID}, HistoryQuery{From: from, To: now})[0]
	if window.State != "ready" || len(window.Samples) != 2016 || !window.Samples[0].CollectedAt.Equal(from) || !window.Samples[2015].CollectedAt.Equal(now.Add(-step)) {
		t.Fatal("八天后窗口错误", window.State, len(window.Samples), window.LastSampleAt)
	}
	if err := n.saveHistorySample(t.Context(), testHistoryMetric(now.Add(time.Second), 16), step); err != nil {
		t.Fatal(err)
	}
	rows, err := h.Page(t.Context(), now.Unix(), now.Unix()+1, storage.HistoryPage)
	if err != nil || len(rows) != 1 {
		t.Fatal("同一采样档被重复写入", len(rows), err)
	}
	t.Logf("滚动八天后保留 %d 个五分钟样本，左闭右开 [%s,%s)", len(window.Samples), from, now)
}

func TestLocalHistoryImportConflictKeepsLegacy(t *testing.T) {
	n, _ := openTestNode(t)
	at := n.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	seedOldHistory(t, n, testHistoryMetric(at, 8))
	h, err := storage.OpenLocalHistory(t.Context(), n.history.path, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := compactHistorySample(testHistoryMetric(at, 16))
	if err := h.Save(t.Context(), []storage.HistoryRow{{At: at.Unix(), Body: body}}); err != nil {
		t.Fatal(err)
	}
	_ = h.Close()
	if err := n.initializeLocalHistory(t.Context()); err == nil {
		t.Fatal("同键异体未终止导入")
	}
	items, err := n.localHistory(t.Context(), HistoryQuery{From: at, To: at})
	if err != nil || len(items) != 1 || items[0].CPUCores != 8 {
		t.Fatal("异体冲突后旧读取路径失效", items, err)
	}
}

func TestLocalHistorySamplesIndependentlyOfArchiveCache(t *testing.T) {
	n, _ := openTestNode(t)
	collector := &archiveCollector{}
	n.Executor = collector
	archiveTestConfig(t, n, n)
	if err := n.initializeLocalHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	buffer, err := n.metricsBuffer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := n.telemetry.Sample(t.Context(), buffer); err != nil {
		t.Fatal(err)
	}
	if err := n.sample(t.Context(), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	now := n.Now().UTC()
	rows, err := n.localHistory(t.Context(), HistoryQuery{From: now.Add(-5 * time.Minute), To: now})
	if err != nil || len(rows) != 1 || rows[0].CPUUsage == nil || *rows[0].CPUUsage != 2 || collector.reads.Load() != 2 {
		t.Fatal("五分钟历史读到了秒级缓存", rows, err)
	}
}

func TestLocalHistoryFullKeepsEarlierSamples(t *testing.T) {
	n, _ := openTestNode(t)
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	n.Now = func() time.Time { return now }
	older := now.Add(-24 * time.Hour)
	seedOldHistory(t, n, testHistoryMetric(older, 4))
	if err := n.initializeLocalHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	h, _, _, _ := n.history.snapshot()
	reader, err := storage.ReadOnly(t.Context(), n.history.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	pin, err := reader.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Rollback()
	var count int
	if err := pin.QueryRow("SELECT count(*) FROM history_samples").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	for start := 1; start < 10000; start += storage.HistoryBatch {
		rows := make([]storage.HistoryRow, storage.HistoryBatch)
		for i := range rows {
			at := older.Add(time.Duration(start+i) * time.Second)
			m := testHistoryMetric(at, 2)
			m.Errors = []string{strings.Repeat("x", 800)}
			body, _ := compactHistorySample(m)
			rows[i] = storage.HistoryRow{At: at.Unix(), Body: body}
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		err = h.Save(ctx, rows)
		cancel()
		if err != nil {
			break
		}
	}
	if !errors.Is(err, storage.ErrHistoryFull) {
		t.Fatal("未触发 WAL 止写", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	err = n.saveHistorySample(ctx, testHistoryMetric(now, 8), 5*time.Minute)
	cancel()
	if !errors.Is(err, storage.ErrHistoryFull) {
		t.Fatal("采样降级失败", err)
	}
	status, err := n.localHistoryStatus(t.Context())
	if err != nil || status.State != "history_full" {
		t.Fatal("状态未报告 history_full", status, err)
	}
	items, err := n.localHistory(t.Context(), HistoryQuery{From: older, To: older})
	if err != nil || len(items) != 1 || items[0].CPUCores != 4 {
		t.Fatal("已提交历史不可读", items, err)
	}
	if err := pin.Rollback(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Minute)
	if err := n.saveHistorySample(t.Context(), testHistoryMetric(now, 8), 5*time.Minute); err != nil {
		t.Fatal("解除阻塞后下一档未恢复", err)
	}
	status, err = n.localHistoryStatus(t.Context())
	if err != nil || status.State != "ready" || status.LastSampleAt == nil || !status.LastSampleAt.Equal(now) {
		t.Fatal("采样恢复状态无效", status, err)
	}
}

func TestHistoryCompatMirrorReentry(t *testing.T) {
	n, _ := openTestNode(t)
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	n.Now = func() time.Time { return now }
	if err := n.initializeLocalHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	h, _, _, _ := n.history.snapshot()
	for _, at := range []time.Time{now.Add(-5 * time.Minute), now} {
		if err := n.saveHistorySample(t.Context(), testHistoryMetric(at, 2), 5*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.legacyMirror(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	if err := h.AdvanceMirror(t.Context(), now.Unix(), storage.HistoryMirrorFromStart); err != nil {
		t.Fatal(err)
	}
	if err := n.legacyMirror(t.Context(), h); err != nil {
		t.Fatal("旧库事务已提交而水位未推进时重入失败", err)
	}
	for _, query := range []string{"SELECT count(*) FROM monitoring_samples", "SELECT count(*) FROM records WHERE kind='metrics'"} {
		var count int
		if err := n.DB.QueryRow(query).Scan(&count); err != nil || count != 2 {
			t.Fatal(query, count, err)
		}
	}
	var old []protocol.Metrics
	if err := n.Call(t.Context(), n.ID, "history", HistoryQuery{From: now, To: now}, &old); err != nil || len(old) != 1 {
		t.Fatal("旧 RPC 的包含式上界失效", old, err)
	}
	now = now.Add(storage.HistoryRetention + 5*time.Minute)
	if err := n.saveHistorySample(t.Context(), testHistoryMetric(now, 2), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := n.legacyMirror(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	if _, watermark, err := h.Meta(t.Context()); err != nil || watermark.Int64 != now.Unix() {
		t.Fatal("原上传未追平时退出了补写", watermark, err)
	}
	if _, err := n.DB.Exec("UPDATE records SET uploaded=1 WHERE kind='metrics'"); err != nil {
		t.Fatal(err)
	}
	if err := n.legacyMirror(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	if _, watermark, err := h.Meta(t.Context()); err != nil || watermark.Int64 != storage.HistoryMirrorRetired {
		t.Fatal("条件已满足但补写没有退出", watermark, err)
	}
}

func TestHistoryWindowWithoutSharedStorage(t *testing.T) {
	a, _ := openTestNode(t)
	b, peerB := openTestNode(t)
	c, peerC := openTestNode(t)
	joinTest(t, b, a)
	joinTest(t, c, a)
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	for _, node := range []*Node{a, b, c} {
		node.Now = func() time.Time { return now }
		if err := node.initializeLocalHistory(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := node.saveHistorySample(t.Context(), testHistoryMetric(now.Add(-5*time.Minute), 2), 5*time.Minute); err != nil {
			t.Fatal(err)
		}
		node.sharedLocal = &SharedLocal{Phase: "active"}
	}
	q := HistoryQuery{From: now.Add(-48 * time.Hour), To: now}
	assert := func(ctx context.Context, ids []string, ready int) {
		t.Helper()
		count := 0
		for _, node := range a.ReadHistoryWindow(ctx, ids, q) {
			if node.State == "ready" && len(node.Samples) == 1 {
				count++
			}
		}
		if count != ready {
			t.Fatal("断开共享库及归档后的节点覆盖不符", count, ready)
		}
	}
	assert(t.Context(), []string{a.ID, b.ID, c.ID}, 3)
	_ = c.Close()
	peerC.Close()
	assert(t.Context(), []string{a.ID, b.ID, c.ID}, 2)
	_ = b.Close()
	peerB.Close()
	assert(t.Context(), []string{b.ID, c.ID}, 0)
}
