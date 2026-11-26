package control

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestHistoryHTTPWindowAndOffline(t *testing.T) {
	a, b, c := openAPINode(t, "节点 A", "本地", 1, 20), openAPINode(t, "节点 B", "本地", 1, 20), openAPINode(t, "节点 C", "本地", 1, 20)
	joinAPINodes(t, a, b, c)
	client, _ := apiAdmin(t, a)
	from := time.Now().UTC().Truncate(5 * time.Minute).Add(-20 * time.Minute)
	to := from.Add(10 * time.Minute)
	for _, at := range []time.Time{from, to} {
		data, _ := json.Marshal(protocol.Metrics{CollectedAt: at, CPUCores: 2, Errors: []string{}})
		if _, err := a.node.DB.Exec("INSERT INTO monitoring_samples(collected_at,body) VALUES(?,?)", at.Unix(), string(data)); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string, out any) int {
		return apiCall(t, client, a.web.URL, http.MethodGet, "/api/v2"+path, "", "", "", nil, out)
	}
	var original httpapi.MonitoringHistory
	if status := read("/monitoring/history?group_by=node", &original); status != 200 || original.From == nil || original.To == nil || original.To.Sub(*original.From) != 24*time.Hour || original.Nodes == nil {
		t.Fatal("旧调用方式或默认 24 小时失效", status)
	}
	a.node.Start()
	deadline := time.Now().Add(4 * time.Second)
	for {
		var local httpapi.MonitoringLocal
		if status := read("/nodes/"+a.node.ID+"/monitoring/local", &local); status != 200 {
			t.Fatal("本机历史状态读取失败", status)
		}
		if local.State == "ready" {
			if local.DbBytes == 0 || local.MaxDbBytes != 32<<20 || local.WalBudgetBytes != 4<<20 {
				t.Fatal(local)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("未切换到本机库", local)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = c.node.Close()
	c.peer.Close()
	var window httpapi.MonitoringHistory
	query := "/monitoring/history?from=" + url.QueryEscape(from.Format(time.RFC3339)) + "&to=" + url.QueryEscape(to.Format(time.RFC3339)) + "&group_by=node"
	status := read(query, &window)
	if status != 200 || window.From == nil || !window.From.Equal(from) || window.To == nil || !window.To.Equal(to) || window.Nodes == nil || len(*window.Nodes) != 2 || window.NodeStatus == nil || len(*window.NodeStatus) != 3 || window.Unavailable == nil || len(*window.Unavailable) != 1 || (*window.Unavailable)[0] != c.node.ID {
		t.Fatal("部分失败结果或节点状态不符", status, window)
	}
	if len(window.Items) != 2 || window.Items[0].SampleCount == nil || *window.Items[0].SampleCount != 1 || window.Items[1].SampleCount == nil || *window.Items[1].SampleCount != 0 {
		t.Fatal("左闭右开或缺口桶错误", window.Items)
	}
	_ = b.node.Close()
	b.peer.Close()
	if status := read(query+"&node_ids="+b.node.ID+","+c.node.ID, nil); status != 503 {
		t.Fatal("全部目标故障未返回 503", status)
	}
	if status := read(query+"&node_id="+a.node.ID+"&node_ids="+b.node.ID, nil); status != 400 {
		t.Fatal("组合筛选未返回 400", status)
	}
}
