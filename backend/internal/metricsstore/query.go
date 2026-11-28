package metricsstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type QueryTarget struct {
	NodeID      string   `json:"node_id"`
	ResourceIDs []string `json:"resource_ids"`
}
type Query struct {
	Metric      string        `json:"metric"`
	From        time.Time     `json:"from"`
	To          time.Time     `json:"to"`
	StepSeconds int           `json:"step_seconds"`
	Targets     []QueryTarget `json:"targets"`
	Aggregate   string        `json:"aggregate,omitempty"`
}
type Point struct {
	At    time.Time `json:"at"`
	Value *float64  `json:"value"`
}
type Series struct {
	NodeID     string  `json:"node_id"`
	ResourceID string  `json:"resource_id,omitempty"`
	Backend    string  `json:"backend,omitempty"`
	Points     []Point `json:"points"`
}
type QueryResult struct {
	Metric                  string    `json:"metric"`
	From                    time.Time `json:"from"`
	To                      time.Time `json:"to"`
	StepSeconds             int       `json:"step_seconds"`
	SourceResolutionSeconds int       `json:"source_resolution_seconds"`
	Items                   []Series  `json:"items"`
	Warnings                []string  `json:"warnings"`
	Partial                 bool      `json:"partial"`
	Missing                 []Gap     `json:"missing"`
}

func (q *Query) Normalize(now time.Time) error {
	if MetricModules[q.Metric] == "" {
		return fmt.Errorf("请选择受支持的指标")
	}
	if q.To.IsZero() {
		q.To = now.UTC()
	}
	if q.From.IsZero() {
		q.From = q.To.Add(-30 * 24 * time.Hour)
	}
	q.From = q.From.UTC().Truncate(time.Second)
	q.To = q.To.UTC().Truncate(time.Second)
	if !q.From.Before(q.To) || q.From.Before(now.Add(-365*24*time.Hour-time.Second)) || q.To.After(now.Add(time.Minute)) {
		return fmt.Errorf("查询范围须在最近 365 天内")
	}
	seconds := int(q.To.Sub(q.From) / time.Second)
	if q.StepSeconds == 0 {
		q.StepSeconds = max(1, (seconds+1198)/1199)
	}
	if q.StepSeconds < 1 || q.StepSeconds > 86400 || seconds/q.StepSeconds+1 > MaxQueryPoints {
		return fmt.Errorf("每条序列最多 10000 个点，请缩小范围或增加步长")
	}
	if q.Aggregate != "" && q.Aggregate != "sum" && q.Aggregate != "avg" {
		return fmt.Errorf("汇总方式无效")
	}
	if len(q.Targets) > 100 {
		return fmt.Errorf("查询节点超过 100 台")
	}
	for _, target := range q.Targets {
		if len(target.NodeID) != 32 || strings.Trim(target.NodeID, "0123456789abcdef") != "" || len(target.ResourceIDs) > 1000 {
			return fmt.Errorf("查询资源无效")
		}
		for _, id := range target.ResourceIDs {
			if id == "" || len(id) > 128 || strings.ContainsAny(id, "\x00\r\n") {
				return fmt.Errorf("查询资源标识无效")
			}
		}
		if MetricModules[q.Metric] == "databases" && len(target.ResourceIDs) == 0 {
			return fmt.Errorf("数据库查询缺少授权资源")
		}
	}
	return nil
}

func (q Query) Resolution() int {
	if q.StepSeconds >= 3600 {
		return 3600
	}
	if q.StepSeconds >= 300 {
		return 300
	}
	return 0
}
func (q Query) Expression(cluster string) string {
	selectors := []string{}
	for _, target := range q.Targets {
		labels := fmt.Sprintf("cluster_id=%q,node_id=%q", cluster, target.NodeID)
		if len(target.ResourceIDs) > 0 {
			ids := make([]string, len(target.ResourceIDs))
			for i, id := range target.ResourceIDs {
				ids[i] = regexp.QuoteMeta(id)
			}
			labels += ",resource_id=~" + strconv.Quote("("+strings.Join(ids, "|")+")")
		}
		selector := q.Metric + "{" + labels + "}"
		if q.Resolution() == 0 {
			selector = "last_over_time(" + selector + "[1s])"
		} else {
			selector = "avg_over_time(" + selector + "[" + strconv.Itoa(q.StepSeconds) + "s])"
		}
		selectors = append(selectors, selector)
	}
	return strings.Join(selectors, " or ")
}

func QueryRange(ctx context.Context, client *http.Client, endpoint, cluster string, q Query, now time.Time) (QueryResult, error) {
	out := QueryResult{Items: []Series{}, Warnings: []string{}, Missing: []Gap{}}
	if err := q.Normalize(now); err != nil {
		return out, err
	}
	out.Metric = q.Metric
	out.From = q.From
	out.To = q.To
	out.StepSeconds = q.StepSeconds
	out.SourceResolutionSeconds = q.Resolution()
	if len(q.Targets) == 0 {
		return out, nil
	}
	params := url.Values{"query": {q.Expression(cluster)}, "start": {strconv.FormatInt(q.From.Unix(), 10)}, "end": {strconv.FormatInt(q.To.Unix(), 10)}, "step": {strconv.Itoa(q.StepSeconds)}, "max_source_resolution": {strconv.Itoa(q.Resolution()) + "s"}, "partial_response": {"true"}, "dedup": {"true"}, "timeout": {"25s"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v1/query_range?"+params.Encode(), nil)
	if err != nil {
		return out, err
	}
	r, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, (64<<20)+1))
	if err != nil {
		return out, err
	}
	if len(data) > 64<<20 {
		return out, fmt.Errorf("查询结果过大，请减少节点或时间范围")
	}
	var result struct {
		Status   string   `json:"status"`
		Error    string   `json:"error"`
		Warnings []string `json:"warnings"`
		Data     struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string   `json:"metric"`
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return out, fmt.Errorf("时序查询应答无效")
	}
	if r.StatusCode != 200 || result.Status != "success" {
		return out, fmt.Errorf("时序查询失败：%s", result.Error)
	}
	if result.Data.ResultType != "matrix" {
		return out, fmt.Errorf("时序查询结果类型无效")
	}
	out.Warnings = append(out.Warnings, result.Warnings...)
	out.Partial = len(out.Warnings) > 0
	count := int(q.To.Sub(q.From)/time.Second)/q.StepSeconds + 1
	if count < 1 || count > MaxQueryPoints || len(result.Data.Result) > 250000/count {
		return out, fmt.Errorf("查询总点数超过 250000，请减少资源或增加步长")
	}
	for _, raw := range result.Data.Result {
		item := Series{NodeID: raw.Metric["node_id"], ResourceID: raw.Metric["resource_id"], Backend: raw.Metric["backend"]}
		allowed := false
		for _, target := range q.Targets {
			if target.NodeID == item.NodeID && (len(target.ResourceIDs) == 0 || slices.Contains(target.ResourceIDs, item.ResourceID)) {
				allowed = true
				break
			}
		}
		if raw.Metric["cluster_id"] != cluster || !allowed {
			return out, fmt.Errorf("查询结果包含未授权资源")
		}
		item.Points = make([]Point, count)
		for i := range item.Points {
			item.Points[i].At = q.From.Add(time.Duration(i*q.StepSeconds) * time.Second)
		}
		for _, value := range raw.Values {
			if len(value) != 2 {
				return out, fmt.Errorf("时序点格式无效")
			}
			var seconds float64
			var text string
			if json.Unmarshal(value[0], &seconds) != nil || json.Unmarshal(value[1], &text) != nil {
				return out, fmt.Errorf("时序点格式无效")
			}
			v, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			index := (int64(seconds) - q.From.Unix()) / int64(q.StepSeconds)
			if index >= 0 && index < int64(count) {
				item.Points[index].Value = &v
			}
		}
		for start := 0; start < count; {
			if item.Points[start].Value != nil {
				start++
				continue
			}
			end := start
			for end+1 < count && item.Points[end+1].Value == nil {
				end++
			}
			if len(out.Missing) < 1000 {
				out.Missing = append(out.Missing, Gap{NodeID: item.NodeID, From: item.Points[start].At.UnixMilli(), To: item.Points[end].At.UnixMilli(), Reason: "no_data"})
			}
			start = end + 1
		}
		out.Items = append(out.Items, item)
	}
	for _, target := range q.Targets {
		if !slices.ContainsFunc(out.Items, func(s Series) bool { return s.NodeID == target.NodeID }) {
			out.Missing = append(out.Missing, Gap{NodeID: target.NodeID, From: q.From.UnixMilli(), To: q.To.UnixMilli(), Reason: "no_data"})
		}
	}
	if q.Aggregate != "" && len(out.Items) > 0 {
		combined := Series{Points: make([]Point, count)}
		for i := range combined.Points {
			combined.Points[i].At = q.From.Add(time.Duration(i*q.StepSeconds) * time.Second)
			sum, n := 0.0, 0
			for _, item := range out.Items {
				if item.Points[i].Value != nil {
					sum += *item.Points[i].Value
					n++
				}
			}
			if n > 0 {
				if q.Aggregate == "avg" {
					sum /= float64(n)
				}
				combined.Points[i].Value = &sum
			}
		}
		out.Items = []Series{combined}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return out, fmt.Errorf("查询结果数值无效：%w", err)
	}
	if len(encoded) > 7<<20 {
		return out, fmt.Errorf("查询结果超过 7MiB，请减少资源或增加步长")
	}
	return out, nil
}
