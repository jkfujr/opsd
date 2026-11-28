package metricsstore

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang/snappy"
	"google.golang.org/protobuf/encoding/protowire"
)

func testBuffer(t *testing.T) (*Buffer, string) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "metrics.sqlite")
	b, err := Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.DB.Close() })
	return b, filename
}
func testBatch(at time.Time, value float64) Batch {
	return Batch{At: at.UnixMilli(), Samples: []Sample{{Metric: "opsd_node_cpu_usage_percent", At: at.UnixMilli(), Value: value}}}
}

func TestBufferOriginalDeliveryAndAtomicRejection(t *testing.T) {
	b, filename := testBuffer(t)
	now := time.Now().UTC().Truncate(time.Second)
	first, err := b.Append(t.Context(), testBatch(now, 41.25), "source", now, BufferBytes)
	if err != nil {
		t.Fatal(err)
	}
	digest := Digest(first)
	if err = b.DB.Close(); err != nil {
		t.Fatal(err)
	}
	b, err = Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	defer b.DB.Close()
	pending, err := b.Pending(t.Context(), "source", now)
	if err != nil || len(pending) != 1 || Digest(pending[0]) != digest || pending[0].Samples[0].Value != 41.25 {
		t.Fatalf("重启改变了原始批次：%+v %v", pending, err)
	}
	if err = b.Received(t.Context(), "source", first, ""); err != nil {
		t.Fatal(err)
	}
	if err = b.Received(t.Context(), "source", first, ""); err != nil {
		t.Fatal("同一回执重复提交失败", err)
	}
	if _, err = b.Receipt(t.Context(), "other-source", first.ID); err != sql.ErrNoRows {
		t.Fatal("回执泄露给了其他来源", err)
	}
	changed := first
	changed.Samples = append([]Sample{}, first.Samples...)
	changed.Samples[0].Value++
	if err = b.Received(t.Context(), "source", changed, ""); err == nil {
		t.Fatal("同一标识覆盖了原始内容")
	}
	if receipt, e := b.Receipt(t.Context(), "source", first.ID); e != nil || receipt.Digest != digest || receipt.Error != "" {
		t.Fatal("应答丢失后的查询改变了回执", receipt, e)
	}
	second, err := b.Append(t.Context(), testBatch(now.Add(time.Second), 50), "source", now.Add(time.Second), BufferBytes)
	if err != nil || second.Sequence <= first.Sequence {
		t.Fatal("批次序号未跨重启递增", second, err)
	}
	if _, err = b.DB.Exec("CREATE TRIGGER reject_gap BEFORE INSERT ON gaps BEGIN SELECT RAISE(ABORT,'拒绝缺口写入'); END"); err != nil {
		t.Fatal(err)
	}
	if err = b.Reject(t.Context(), "source", second, "permanent_error"); err == nil {
		t.Fatal("缺口保存失败仍删除了待传批次")
	}
	if _, err = b.DB.Exec("DROP TRIGGER reject_gap"); err != nil {
		t.Fatal(err)
	}
	pending, err = b.Pending(t.Context(), "source", now.Add(time.Second))
	if err != nil || len(pending) != 2 {
		t.Fatal("失败事务丢失原批次", pending, err)
	}
	if err = b.Reject(t.Context(), "source", second, "permanent_error"); err != nil {
		t.Fatal(err)
	}
	if err = b.Acknowledge(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	status, err := b.Status(t.Context(), "source")
	if err != nil || status.Batches != 0 || len(status.Gaps) != 1 || status.Gaps[0].BatchID != second.ID {
		t.Fatal(status, err)
	}
}

func TestBufferCapacityExpiryAndClock(t *testing.T) {
	b, _ := testBuffer(t)
	now := time.Now().UTC().Truncate(time.Second)
	for i := range 10 {
		at := now.Add(time.Duration(i) * time.Second)
		if _, err := b.Append(t.Context(), testBatch(at, float64(i)), "source", at, 1500); err != nil {
			t.Fatal(err)
		}
	}
	status, err := b.Status(t.Context(), "source")
	if err != nil || status.Bytes > 1500 || len(status.Gaps) == 0 || status.Gaps[0].Reason != "buffer_capacity" {
		t.Fatal(status, err)
	}
	for _, at := range []time.Time{now.Add(-time.Hour), now.Add(2 * time.Hour)} {
		got, err := b.Append(t.Context(), testBatch(at, 1), "source", now.Add(10*time.Second), BufferBytes)
		if err != nil || got.Sequence != 0 {
			t.Fatal("异常时钟写入了样本", got, err)
		}
	}
	status, _ = b.Status(t.Context(), "source")
	for _, gap := range status.Gaps {
		if gap.To > now.Add(10*time.Second).UnixMilli() {
			t.Fatal("时钟异常留下不可补传的未来缺口", gap)
		}
	}
	pending, err := b.Pending(t.Context(), "source", now.Add(61*time.Minute))
	if err != nil || len(pending) != 0 {
		t.Fatal("过期样本未淘汰", pending, err)
	}
	status, _ = b.Status(t.Context(), "source")
	if len(status.Gaps) == 0 || status.Gaps[0].Reason != "buffer_expired" {
		t.Fatal("过期淘汰缺少时间范围", status)
	}
	invalid := testBatch(now.Add(2*time.Hour), math.NaN())
	if _, err = b.Append(t.Context(), invalid, "source", now.Add(2*time.Hour), BufferBytes); err == nil {
		t.Fatal("非有限样本进入数据库")
	}
}

func TestBatchAndCapacityBoundaries(t *testing.T) {
	now := time.Now().UTC()
	b := testBatch(now, 20)
	b.ID, b.Sequence = strings.Repeat("a", 43), 1
	if err := b.Validate(now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Batch){
		func(b *Batch) { b.Samples[0].Metric = "arbitrary_metric" },
		func(b *Batch) { b.Samples[0].ResourceID = "foreign" },
		func(b *Batch) { b.Samples[0].Backend = "foreign" },
		func(b *Batch) { b.Samples[0].At++ },
		func(b *Batch) { b.Samples = append(b.Samples, b.Samples[0]) },
		func(b *Batch) { b.At = now.Add(2 * time.Minute).UnixMilli() },
	} {
		copy := b
		copy.Samples = append([]Sample{}, b.Samples...)
		mutate(&copy)
		if copy.Validate(now) == nil {
			t.Fatal("接受了非法监控样本", copy)
		}
	}
	for _, v := range []struct {
		paused bool
		used   int64
		want   bool
	}{{false, 899, false}, {false, 900, true}, {true, 800, true}, {true, 799, false}} {
		if got := DiskPaused(v.paused, v.used, 1000, 1000); got != v.want {
			t.Fatal("磁盘暂停阈值错误", v, got)
		}
	}
	if !DiskPaused(false, 10, 1000, 99) || !DiskPaused(true, 10, 1000, 199) {
		t.Fatal("未保留文件系统空间")
	}
	for _, prefix := range []string{"", "/", "opsd/../files/", "opsd//metrics/", "opsd\\metrics/"} {
		if ValidPrefix(prefix) {
			t.Fatal("接受了无效前缀", prefix)
		}
	}
}

func TestBufferRepeatedHistoricalGaps(t *testing.T) {
	b, _ := testBuffer(t)
	first := Gap{NodeID: "source", From: 1000, To: 1999, Reason: "clock_changed"}
	second := Gap{NodeID: "source", From: 10000, To: 10999, Reason: first.Reason}
	for range 3 {
		for _, gap := range []Gap{first, second} {
			if err := b.Gap(t.Context(), gap); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := b.Gap(t.Context(), Gap{NodeID: first.NodeID, From: 500, To: 1500, Reason: first.Reason}); err != nil {
		t.Fatal(err)
	}
	if err := b.Gap(t.Context(), Gap{NodeID: "another-source", From: 1000, To: 2000, Reason: first.Reason}); err != nil {
		t.Fatal(err)
	}
	status, err := b.Status(t.Context(), "source")
	if err != nil || len(status.Gaps) != 2 || status.Gaps[1].From != 500 || status.Gaps[1].To != first.To {
		t.Fatal("重复补传增加了缺口或缩短了缺失时间范围", status, err)
	}
}

func TestSamplingErrorGapSurvivesDelivery(t *testing.T) {
	b, _ := testBuffer(t)
	now := time.Now().UTC()
	batch := testBatch(now, 20)
	batch.Errors = []string{"资源采集超时"}
	batch, err := b.Append(t.Context(), batch, "source", now, BufferBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Acknowledge(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	status, err := b.Status(t.Context(), "source")
	if err != nil || status.Batches != 0 || len(status.Gaps) != 1 || status.Gaps[0].BatchID != batch.ID || status.Gaps[0].Reason != batch.Errors[0] {
		t.Fatal("上传后丢失了来源节点的采集错误", status, err)
	}
}

func TestRemoteWriteAndHTTPClassification(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	b := testBatch(now, 12.5)
	raw, err := snappy.Decode(nil, RemoteWrite("cluster", "node", []Batch{b}))
	if err != nil {
		t.Fatal(err)
	}
	number, kind, n := protowire.ConsumeTag(raw)
	if number != 1 || kind != protowire.BytesType {
		t.Fatal("WriteRequest 缺少 TimeSeries")
	}
	series, used := protowire.ConsumeBytes(raw[n:])
	if used < 0 || n+used != len(raw) {
		t.Fatal("TimeSeries 编码无效")
	}
	labels, samples := []string{}, 0
	for len(series) > 0 {
		field, kind, n := protowire.ConsumeTag(series)
		if kind != protowire.BytesType {
			t.Fatal("TimeSeries 字段编码无效")
		}
		value, used := protowire.ConsumeBytes(series[n:])
		if used < 0 {
			t.Fatal("TimeSeries 字段截断")
		}
		if field == 1 {
			_, _, start := protowire.ConsumeTag(value)
			name, _ := protowire.ConsumeBytes(value[start:])
			labels = append(labels, string(name))
		} else if field == 2 {
			samples++
		}
		series = series[n+used:]
	}
	if strings.Join(labels, ",") != "__name__,cluster_id,node_id" || samples != 1 {
		t.Fatal(labels, samples)
	}
	for _, status := range []int{204, 400, 408, 429, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("THANOS-TENANT") != "opsd" || r.Header.Get("Content-Encoding") != "snappy" {
				t.Error("缺少 Remote Write 请求头")
			}
			w.WriteHeader(status)
		}))
		err := Write(t.Context(), server.Client(), server.URL, "cluster", "node", []Batch{b})
		server.Close()
		if status == 204 && err != nil || status != 204 && (err == nil || err.(*WriteError).Permanent() != (status == 400)) {
			t.Fatal(status, err)
		}
	}
}

func TestQueryLimitsPermissionsAndGaps(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	node := strings.Repeat("a", 32)
	q := Query{Metric: "opsd_node_cpu_usage_percent", From: now.Add(-2 * time.Second), To: now, StepSeconds: 1, Targets: []QueryTarget{{NodeID: node}}}
	foreign, empty := false, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("query"), "node_id=\""+node+"\"") || r.URL.Query().Get("max_source_resolution") != "0s" {
			t.Error("查询未限制授权来源或原始精度", r.URL.Query())
		}
		selected := node
		if foreign {
			selected = strings.Repeat("b", 32)
		}
		items := []any{}
		if !empty {
			items = append(items, map[string]any{"metric": map[string]string{"cluster_id": "cluster", "node_id": selected}, "values": [][]any{{q.From.Unix(), "10"}, {q.To.Unix(), "20"}}})
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": items}, "warnings": []string{"S3 暂时不可用"}})
	}))
	defer server.Close()
	result, err := QueryRange(t.Context(), server.Client(), server.URL, "cluster", q, now)
	if err != nil || !result.Partial || len(result.Items) != 1 || len(result.Items[0].Points) != 3 || result.Items[0].Points[1].Value != nil || len(result.Missing) != 1 {
		t.Fatal(result, err)
	}
	foreign = true
	if _, err = QueryRange(t.Context(), server.Client(), server.URL, "cluster", q, now); err == nil {
		t.Fatal("查询回读接受了未授权资源")
	}
	foreign, empty = false, true
	result, err = QueryRange(t.Context(), server.Client(), server.URL, "cluster", q, now)
	if err != nil || len(result.Items) != 0 || len(result.Missing) != 1 {
		t.Fatal("查询为空时缺失范围不完整", result, err)
	}
	defaultQuery := Query{Metric: q.Metric}
	if err = defaultQuery.Normalize(now); err != nil || defaultQuery.To.Sub(defaultQuery.From) != 30*24*time.Hour || int(defaultQuery.To.Sub(defaultQuery.From)/time.Second)/defaultQuery.StepSeconds+1 > 1200 {
		t.Fatal("默认 30 天查询未限制点数", defaultQuery, err)
	}
	for _, invalid := range []Query{
		{Metric: q.Metric, From: now.Add(-366 * 24 * time.Hour)},
		{Metric: q.Metric, From: now.Add(-24 * time.Hour), StepSeconds: 1},
		{Metric: "arbitrary_metric"},
		{Metric: "opsd_database_up", Targets: []QueryTarget{{NodeID: node}}},
	} {
		if invalid.Normalize(now) == nil {
			t.Fatal("接受了越界查询", invalid)
		}
	}
}

func TestQueryPointBudget(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	node := strings.Repeat("a", 32)
	q := Query{Metric: "opsd_node_cpu_usage_percent", From: now.Add(-(MaxQueryPoints - 1) * time.Second), To: now, StepSeconds: 1, Targets: []QueryTarget{{NodeID: node}}, Aggregate: "sum"}
	const maxSeries = 250000 / MaxQueryPoints
	series := map[string]any{"metric": map[string]string{"cluster_id": "cluster", "node_id": node}, "values": [][]any{}}
	for _, count := range []int{maxSeries, maxSeries + 1} {
		items := make([]map[string]any, count)
		for i := range items {
			items[i] = series
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": items}})
		}))
		result, err := QueryRange(t.Context(), server.Client(), server.URL, "cluster", q, now)
		server.Close()
		if count == maxSeries && (err != nil || len(result.Items) != 1 || len(result.Items[0].Points) != MaxQueryPoints) {
			t.Fatal("总点数刚好达到上限应成功", err)
		}
		if count > maxSeries && (err == nil || len(result.Items) != 0) {
			t.Fatal("超量结果应在分配前拒绝", err)
		}
	}
}
