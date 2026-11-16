package control

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/metricsstore"
)

func TestMonitoringAPISchemasAndPermissionBeforeAggregation(t *testing.T) {
	a := openAPINode(t, "监控入口", "日本", 4, 10)
	b := openAPINode(t, "其他节点", "美国", 1, 90)
	joinAPINodes(t, a, b)
	client, csrf := apiAdmin(t, a)
	var targets httpapi.StorageTargetList
	if code := apiCall(t, client, a.web.URL, "GET", "/api/v2/storage/targets", "", "", csrf, nil, &targets); code != 200 || len(targets.Items) != 0 {
		t.Fatal("存储初始状态无效", code, targets)
	}
	var config httpapi.MonitoringArchiveConfig
	if code := apiCall(t, client, a.web.URL, "GET", "/api/v2/monitoring/config", "", "", csrf, nil, &config); code != 200 || config.Enabled || config.LocalDays != 30 || config.ArchiveDays != 365 {
		t.Fatal("监控默认配置无效", code, config)
	}
	var status httpapi.MonitoringArchiveStatusList
	if code := apiCall(t, client, a.web.URL, "GET", "/api/v2/monitoring/status", "", "", csrf, nil, &status); code != 200 || len(status.Items) != 2 {
		t.Fatal("监控初始状态无效", code, status)
	}
	config.Enabled, config.NodeId, config.StorageTargetId, config.State, config.Revision = true, a.node.ID, "s3", "ready", 1
	data, _ := json.Marshal(config)
	if _, err := a.node.DB.Exec("INSERT INTO monitoring_config(id,revision,body) VALUES(1,1,?)", string(data)); err != nil {
		t.Fatal(err)
	}
	a.node.Start()
	listener, err := net.Listen("tcp", "127.0.0.1:19292")
	if err != nil {
		t.Fatal(err)
	}
	var expression string
	query := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expression = r.URL.Query().Get("query")
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": map[string]string{"cluster_id": strings.TrimSuffix(strings.TrimPrefix(config.Prefix, "opsd/metrics/"), "/"), "node_id": a.node.ID}, "values": [][]any{{time.Now().Unix(), "10"}}}}}})
	}))
	query.Listener.Close()
	query.Listener = listener
	query.Start()
	defer query.Close()
	_, raw, err := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "本机监控", Enabled: true, Grants: []apiauth.Grant{{Module: "monitoring", Level: "read", Scope: "nodes", Resources: []string{a.node.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var result metricsstore.QueryResult
	for {
		code := apiCall(t, http.DefaultClient, a.web.URL, "GET", "/api/v2/monitoring/series?metric=opsd_node_cpu_usage_percent&aggregate=sum", raw, "", "", nil, &result)
		if code == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("已确认配置未进入监控查询", code)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(result.Items) != 1 || !strings.Contains(expression, a.node.ID) || strings.Contains(expression, b.node.ID) {
		t.Fatal("汇总查询先包含了未授权节点", expression, result)
	}
	for _, path := range []string{"/storage/targets", "/monitoring/config", "/monitoring/series?metric=opsd_container_running"} {
		if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", "/api/v2"+path, raw, "", "", nil, nil); code != 403 {
			t.Fatal("监控令牌读取了其他模块", path, code)
		}
	}
	if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", "/api/v2/monitoring/status", raw, "", "", nil, &status); code != 200 || len(status.Items) != 1 || status.Items[0].NodeId != a.node.ID {
		t.Fatal("采集状态泄露到授权范围外", code, status)
	}
}
