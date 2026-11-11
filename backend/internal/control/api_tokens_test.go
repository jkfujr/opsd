package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
)

type apiExecutor struct {
	cores int
	cpu   float64
	live  atomic.Int32
}

func (e *apiExecutor) Call(_ context.Context, p protocol.Payload) (protocol.Payload, error) {
	switch p := p.(type) {
	case *protocol.MonitoringRead:
		if p.Stream == "live" {
			e.live.Add(1)
		}
		used, total := uint64(e.cores*20), uint64(e.cores*100)
		cpu := e.cpu
		return &protocol.MonitoringResult{Metrics: protocol.Metrics{CollectedAt: time.Now().UTC(), CPUCores: e.cores, CPUUsage: &cpu, MemoryUsed: &used, MemoryTotal: &total, Errors: []string{}}, Firewall: "enabled"}, nil
	case *protocol.DockerRead:
		return &protocol.DockerReadResult{State: protocol.DockerState{EngineID: "test-engine", State: "ready", Containers: []protocol.ContainerSummary{{ContainerRef: protocol.ContainerRef{EngineID: "test-engine", ContainerID: strings.Repeat("b", 64)}, Name: "测试容器", State: "running", Ports: []protocol.ContainerPort{}}}, CheckedAt: time.Now().UTC()}}, nil
	}
	return nil, fmt.Errorf("本用例不执行宿主机变更")
}
func (*apiExecutor) Storage(context.Context, cluster.StorageConfig) (cluster.Credentials, error) {
	return cluster.Credentials{}, nil
}

type apiNode struct {
	node     *cluster.Node
	peer     *httptest.Server
	web      *httptest.Server
	executor *apiExecutor
}

func openAPINode(t *testing.T, name, region string, cores int, cpu float64) *apiNode {
	t.Helper()
	peer := httptest.NewUnstartedServer(nil)
	x := &apiExecutor{cores: cores, cpu: cpu}
	n, e := cluster.Open(t.Context(), filepath.Join(t.TempDir(), "node.db"), cluster.Options{Username: "admin", Password: "test-password", Entrance: "/signin", Config: cluster.Config{Name: name, Region: region, Address: "http://" + peer.Listener.Addr().String(), API: true, Console: true}, Executor: x})
	if e != nil {
		t.Fatal(e)
	}
	peer.Config.Handler = n.PeerHandler()
	peer.Start()
	server, e := New(t.Context(), n.DB, Options{Node: n, EnforceListenerState: true})
	if e != nil {
		t.Fatal(e)
	}
	web := httptest.NewServer(server.Handler())
	out := &apiNode{n, peer, web, x}
	t.Cleanup(func() { web.Close(); server.Close(); n.Close(); peer.Close() })
	return out
}
func joinAPINodes(t *testing.T, nodes ...*apiNode) {
	t.Helper()
	for _, entry := range nodes[1:] {
		token, _, _, e := nodes[0].node.Token(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		self, e := nodes[0].node.Self(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		if e = entry.node.Join(t.Context(), self.Address, self.Public, token); e != nil {
			t.Fatal(e)
		}
	}
	d, e := nodes[0].node.Directory(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range nodes[1:] {
		if e = nodes[0].node.Call(t.Context(), entry.node.ID, "directory", cluster.DirectoryExchange{Delta: d}, nil); e != nil {
			t.Fatal(e)
		}
	}
}
func apiCall(t *testing.T, client *http.Client, base, method, path, raw, key, csrf string, body, out any) int {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		reader = bytes.NewReader(b)
	}
	request, e := http.NewRequest(method, base+path, reader)
	if e != nil {
		t.Fatal(e)
	}
	if raw != "" {
		request.Header.Set("Authorization", "Bearer "+raw)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	request.Header.Set("Origin", base)
	request.Header.Set("Content-Type", "application/json")
	prepareTestRequest(t, client, request)
	response, e := client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	validateTestResponse(t, request, response)
	defer response.Body.Close()
	if response.StatusCode < 400 && out != nil {
		if e = json.NewDecoder(response.Body).Decode(out); e != nil {
			t.Fatal(e)
		}
	}
	return response.StatusCode
}
func apiAdmin(t *testing.T, n *apiNode) (*http.Client, string) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	var session httpapi.CurrentSession
	if code := apiCall(t, client, n.web.URL, "POST", "/api/v2/auth/sessions", "", "", "", map[string]string{"username": "admin", "password": "test-password", "entrance": "/signin"}, &session); code != 201 {
		t.Fatal("管理员登录失败", code)
	}
	return client, session.CsrfToken
}
func tokenStore(n *apiNode) apiauth.Store { return apiauth.Store{DB: n.node.DB, Owner: n.node.ID} }
func tokenInput(t apiauth.Token) apiauth.Input {
	return apiauth.Input{Name: t.Name, Enabled: t.Enabled, Revision: t.Revision, Grants: t.Grants, ExpiresAt: t.ExpiresAt, SourceIPs: t.SourceIPs}
}
func waitAPIOperation(t *testing.T, n *cluster.Node, id string) cluster.Operation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		op, e := n.GetOperation(t.Context(), id)
		if e == nil && op.State == protocol.Succeeded {
			return op
		}
		if e == nil && op.State == protocol.Rejected {
			t.Fatal("操作被拒绝", op.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("操作未完成")
	return cluster.Operation{}
}
func configureAPI(t *testing.T, n *apiNode, change func(*cluster.Config)) {
	t.Helper()
	c, e := n.node.Config(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	change(&c)
	b, _ := json.Marshal(c)
	op, e := n.node.Submit(t.Context(), n.node.ID, cluster.Request{ID: cluster.NewOperationID(n.node.ID), Kind: "config", Payload: b})
	if e != nil {
		t.Fatal(e)
	}
	waitAPIOperation(t, n.node, op.ID)
}

func TestAPITokensAcrossEntriesScopesAndOwnerFailure(t *testing.T) {
	a := openAPINode(t, "日本 A", "日本", 1, 20)
	b := openAPINode(t, "美国 B", "美国", 2, 40)
	c := openAPINode(t, "日本 C", "日本", 3, 80)
	joinAPINodes(t, a, b, c)
	client, csrf := apiAdmin(t, a)
	var created httpapi.APITokenCreated
	input := map[string]any{"name": "地域看板", "enabled": true, "revision": 0, "grants": []apiauth.Grant{{Module: "monitoring", Level: "read", Scope: "regions", Resources: []string{"日本"}}}, "source_ips": []string{}}
	if code := apiCall(t, client, a.web.URL, "POST", "/api/v2/nodes/"+a.node.ID+"/api-tokens", "", "", csrf, input, &created); code != 201 {
		t.Fatal("创建令牌失败", code)
	}
	for _, entry := range []*apiNode{a, b, c} {
		var current httpapi.MonitoringCurrent
		deadline := time.Now().Add(3 * time.Second)
		for {
			code := apiCall(t, http.DefaultClient, entry.web.URL, "GET", "/api/v2/monitoring/current", created.Value, "", "", nil, &current)
			if code != 200 || len(current.Items) != 2 {
				t.Fatal("跨入口地域过滤错误", code, len(current.Items))
			}
			if current.Summary.Metrics.CpuUsage != nil && *current.Summary.Metrics.CpuUsage == 65 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("共享读取未取得实际数据", current.Items)
			}
			time.Sleep(10 * time.Millisecond)
		}
		for _, item := range current.Items {
			if item.NodeId == b.node.ID || item.Region != "日本" {
				t.Fatal("监控返回了越权节点")
			}
		}
	}
	if a.executor.live.Load() != 1 || b.executor.live.Load() != 0 || c.executor.live.Load() != 1 {
		t.Fatal("实时读取未复用采集或读取了越权节点")
	}
	for _, path := range []string{"/api/v2/nodes", "/api/v2/nodes/" + a.node.ID + "/account", "/api/v2/peer-trust", "/api/v2/nodes/" + a.node.ID + "/api-tokens", "/api/v2/monitoring/current?node_id=" + b.node.ID} {
		if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", path, created.Value, "", "", nil, nil); code != 403 {
			t.Fatal("权限边界错误", path, code)
		}
	}
	if code := apiCall(t, client, a.web.URL, "GET", "/api/v2/nodes", "invalid", "", csrf, nil, nil); code != 401 {
		t.Fatal("无效令牌回退到了浏览器会话", code)
	}
	historyCPU := 25.0
	metric := protocol.Metrics{CollectedAt: time.Now().Add(-5 * time.Minute).UTC(), CPUCores: 1, CPUUsage: &historyCPU, Errors: []string{}}
	body, _ := json.Marshal(metric)
	if _, e := a.node.DB.Exec("INSERT INTO monitoring_samples(collected_at,body) VALUES(?,?)", metric.CollectedAt.Unix(), string(body)); e != nil {
		t.Fatal(e)
	}
	var history httpapi.MonitoringHistory
	if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", "/api/v2/monitoring/history?node_ids="+a.node.ID+","+c.node.ID+"&group_by=node", created.Value, "", "", nil, &history); code != 200 || history.Nodes == nil || len(*history.Nodes) != 1 || history.Unavailable == nil || len(*history.Unavailable) != 1 {
		t.Fatal("部分历史或节点分组错误", code)
	}
	older := url.QueryEscape(time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339))
	if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", "/api/v2/monitoring/history?from="+older, created.Value, "", "", nil, nil); code != 422 {
		t.Fatal("七天历史边界错误", code)
	}
	c.node.Start()
	configureAPI(t, c, func(config *cluster.Config) { config.Region = "美国" })
	directory, err := c.node.Directory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []*apiNode{a, b} {
		if err = c.node.Call(t.Context(), entry.node.ID, "directory", cluster.DirectoryExchange{Delta: directory}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var regional httpapi.MonitoringCurrent
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/monitoring/current", created.Value, "", "", nil, &regional); code != 200 || len(regional.Items) != 1 || regional.Items[0].NodeId != a.node.ID {
		t.Fatal("地域变更后授权范围未更新", code)
	}
	a.node.Start()
	configureAPI(t, a, func(config *cluster.Config) { config.API = false })
	if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", "/api/v2/monitoring/current", created.Value, "", "", nil, nil); code != 503 {
		t.Fatal("API 开关未生效", code)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/monitoring/current", created.Value, "", "", nil, nil); code != 200 {
		t.Fatal("所属节点关闭 API 后其他入口无法核验令牌", code)
	}
	var metadata apiauth.Token
	if e := a.node.DB.QueryRow("SELECT body FROM api_tokens WHERE id=?", created.Token.Id).Scan(&body); e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal(body, &metadata); e != nil {
		t.Fatal(e)
	}
	updated := tokenInput(metadata)
	updated.Grants = []apiauth.Grant{{Module: "monitoring", Level: "read", Scope: "nodes", Resources: []string{b.node.ID}}}
	metadata, e := tokenStore(a).Update(t.Context(), metadata.ID, updated)
	if e != nil {
		t.Fatal(e)
	}
	var current httpapi.MonitoringCurrent
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/monitoring/current", created.Value, "", "", nil, &current); code != 200 || len(current.Items) != 1 || current.Items[0].NodeId != b.node.ID {
		t.Fatal("权限编辑未立即生效", code)
	}
	updated = tokenInput(metadata)
	updated.Enabled = false
	metadata, e = tokenStore(a).Update(t.Context(), metadata.ID, updated)
	if e != nil {
		t.Fatal(e)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/monitoring/current", created.Value, "", "", nil, nil); code != 401 {
		t.Fatal("停用未生效", code)
	}
	updated = tokenInput(metadata)
	updated.Enabled = true
	metadata, e = tokenStore(a).Update(t.Context(), metadata.ID, updated)
	if e != nil {
		t.Fatal(e)
	}
	_, replacement, e := tokenStore(a).Regenerate(t.Context(), metadata.ID, metadata.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/monitoring/current", created.Value, "", "", nil, nil); code != 401 {
		t.Fatal("旧令牌仍可用", code)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/monitoring/current", replacement, "", "", nil, nil); code != 200 {
		t.Fatal("替换令牌不可用", code)
	}
	a.node.Close()
	a.peer.Close()
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/monitoring/current", replacement, "", "", nil, nil); code != 503 {
		t.Fatal("所属节点失联未返回不可用", code)
	}
}

func TestAPIWriteIdentityPaginationAndConsoleSeparation(t *testing.T) {
	a := openAPINode(t, "入口 A", "日本", 1, 20)
	b := openAPINode(t, "目标 B", "日本", 1, 20)
	c := openAPINode(t, "入口 C", "美国", 1, 20)
	joinAPINodes(t, a, b, c)
	token, raw, e := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "管理 B", Enabled: true, Grants: []apiauth.Grant{{Module: "nodes", Level: "manage", Scope: "nodes", Resources: []string{b.node.ID}}}})
	if e != nil {
		t.Fatal(e)
	}
	var original, duplicate cluster.Operation
	path := "/api/v2/nodes/" + b.node.ID
	for i, entry := range []*apiNode{a, c} {
		out := &original
		if i == 1 {
			out = &duplicate
		}
		if code := apiCall(t, http.DefaultClient, entry.web.URL, "PATCH", path, raw, "same-key", "", map[string]string{"name": "固定参数"}, out); code != 202 {
			t.Fatal("令牌写入失败", code)
		}
	}
	if original.ID != duplicate.ID || original.Actor != token.Actor() {
		t.Fatal("跨入口产生了重复操作或丢失令牌身份")
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "PATCH", path, raw, "same-key", "", map[string]string{"name": "其他参数"}, nil); code != 409 {
		t.Fatal("同一键接受了不同参数", code)
	}
	privatePayload, _ := json.Marshal(cluster.AccountChange{Username: "updated-admin"})
	private, e := a.node.Submit(t.Context(), b.node.ID, cluster.Request{Kind: "account", Key: "private", Payload: privatePayload})
	if e != nil {
		t.Fatal(e)
	}
	var list httpapi.OperationList
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/operations?limit=1", raw, "", "", nil, &list); code != 200 || len(list.Items) != 1 || list.Items[0].Id != original.ID {
		t.Fatal("分页前未过滤权限", code)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/operations/"+private.ID, raw, "", "", nil, nil); code != 403 {
		t.Fatal("读取了管理员操作", code)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/operations?node_id="+a.node.ID, raw, "", "", nil, nil); code != 403 {
		t.Fatal("显式越权目标未拒绝", code)
	}
	input := tokenInput(token)
	input.Enabled = false
	if _, e = tokenStore(a).Update(t.Context(), token.ID, input); e != nil {
		t.Fatal(e)
	}
	b.node.Start()
	result := waitAPIOperation(t, b.node, original.ID)
	if result.Subject != "name" || result.Actor != token.Actor() {
		t.Fatal("已受理操作因令牌停用而改变")
	}
	c.node.Start()
	configureAPI(t, c, func(config *cluster.Config) { config.Console = false })
	_, monitor, e := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "容器 B", Enabled: true, Grants: []apiauth.Grant{{Module: "containers", Level: "read", Scope: "nodes", Resources: []string{b.node.ID}}}})
	if e != nil {
		t.Fatal(e)
	}
	var containers httpapi.ContainerList
	deadline := time.Now().Add(3 * time.Second)
	for {
		code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/containers", monitor, "", "", nil, &containers)
		if code != 200 || len(containers.Nodes) != 1 {
			t.Fatal("关闭网页后 API 不可用或容器范围错误", code)
		}
		if len(containers.Items) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("容器共享读取未完成", containers.Nodes)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "POST", "/api/v2/nodes/"+b.node.ID+"/engines/test-engine/containers/"+strings.Repeat("b", 64)+"/actions", monitor, "write", "", map[string]string{"action": "restart", "engine_id": "test-engine"}, nil); code != 403 {
		t.Fatal("读取令牌执行了容器变更", code)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "POST", "/api/v2/auth/sessions", "", "", "", map[string]string{"username": "admin", "password": "test-password", "entrance": "/signin"}, nil); code != 404 {
		t.Fatal("关闭网页后仍能登录", code)
	}
	configureAPI(t, c, func(config *cluster.Config) { config.API = false })
	if code := apiCall(t, http.DefaultClient, c.web.URL, "GET", "/api/v2/containers", monitor, "", "", nil, nil); code != 503 {
		t.Fatal("关闭 API 后仍能访问", code)
	}
}
