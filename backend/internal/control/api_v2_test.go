package control

import (
	"encoding/json"
	"fmt"
	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAPIV2ValidationConditionsAndReceipts(t *testing.T) {
	n := openAPINode(t, "契约节点", "东京", 1, 1)
	client, csrf := apiAdmin(t, n)
	call := func(method, path, media, body string, headers map[string]string) (int, http.Header, []byte) {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, n.web.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", n.web.URL)
		r.Header.Set("X-CSRF-Token", csrf)
		if media != "" {
			r.Header.Set("Content-Type", media)
		}
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		validateTestResponse(t, r, response)
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, response.Header, data
	}
	const navigation = "/api/v2/settings/navigation"
	_, headers, _ := call("GET", navigation, "", "", nil)
	etag := headers.Get("ETag")
	if etag == "" || strings.HasPrefix(etag, "W/") {
		t.Fatal("配置缺少强 ETag")
	}
	for _, test := range []struct {
		name, media, body string
		status            int
	}{
		{"媒体类型", "text/plain", `{}`, 415},
		{"错误 JSON", "application/json", `{`, 400},
		{"缺失字段", "application/json", `{}`, 422},
		{"未知字段", "application/json", `{"order":[],"unknown":true}`, 422},
		{"空值", "application/json", `{"order":null}`, 422},
		{"超限正文", "application/json", strings.Repeat(" ", 262145), 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, _, data := call("PUT", navigation, test.media, test.body, map[string]string{"If-Match": etag})
			if code != test.status {
				t.Fatalf("%d %s", code, data)
			}
		})
	}
	if code, _, _ := call("PUT", navigation, "application/json", `{"order":[]}`, nil); code != 428 {
		t.Fatal("缺少并发条件", code)
	}
	path := "/api/v2/nodes/" + n.node.ID + "/api-tokens"
	body := `{"name":"并发创建","enabled":true,"grants":[{"module":"tasks","level":"read","scope":"all","resources":[]}],"source_ips":[]}`
	var statuses [2]int
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			statuses[i], _, _ = call("POST", path, "application/json", body, map[string]string{"Idempotency-Key": "concurrent-create"})
		})
	}
	wg.Wait()
	if !(statuses[0] == 201 && statuses[1] == 409 || statuses[1] == 201 && statuses[0] == 409) {
		t.Fatal("并发同键未复用创建", statuses)
	}
	var count int
	if err := n.node.DB.QueryRow("SELECT count(*) FROM api_tokens").Scan(&count); err != nil || count != 1 {
		t.Fatal("创建产生重复记录", count, err)
	}
	code, _, data := call("GET", "/api/v2/requests/concurrent-create?node_id="+n.node.ID, "", "", nil)
	var receipt httpapi.RequestReceipt
	if code != 200 || json.Unmarshal(data, &receipt) != nil || receipt.ResourceUrl == nil || receipt.SecretUnavailable == nil || !*receipt.SecretUnavailable {
		t.Fatalf("丢应答后查询 %d %s", code, data)
	}
	if code, _, _ := call("POST", path, "application/json", strings.Replace(body, "并发创建", "其他参数", 1), map[string]string{"Idempotency-Key": "concurrent-create"}); code != 409 {
		t.Fatal("同键异参", code)
	}
	resource := *receipt.ResourceUrl
	_, tokenHeaders, tokenBody := call("GET", resource, "", "", nil)
	var token httpapi.APIToken
	if json.Unmarshal(tokenBody, &token) != nil {
		t.Fatal("令牌记录不可读")
	}
	for i := range 2 {
		wg.Go(func() {
			statuses[i], _, _ = call("PATCH", resource, "application/merge-patch+json", `{"name":"并发修改"}`, map[string]string{"If-Match": tokenHeaders.Get("ETag")})
		})
	}
	wg.Wait()
	if !(statuses[0] == 200 && statuses[1] == 412 || statuses[1] == 200 && statuses[0] == 412) {
		t.Fatal("两个入口修改未拒绝旧版本", statuses)
	}
	_, _, tokenBody = call("GET", resource, "", "", nil)
	if json.Unmarshal(tokenBody, &token) != nil {
		t.Fatal("修改后的令牌不可读")
	}
	regenerate := fmt.Sprintf(`{"action":"regenerate","expected_revision":%d}`, token.Revision)
	if code, _, _ = call("POST", resource+"/actions", "application/json", regenerate, map[string]string{"Idempotency-Key": "regenerate-once"}); code != 200 {
		t.Fatal("重新生成动作失败", code)
	}
	code, _, data = call("POST", resource+"/actions", "application/json", regenerate, map[string]string{"Idempotency-Key": "regenerate-once"})
	var secret httpapi.Problem
	if code != 409 || json.Unmarshal(data, &secret) != nil || secret.Code != "secret_unavailable" || secret.ResourceUrl == nil || *secret.ResourceUrl != resource {
		t.Fatalf("重复秘密请求未返回原资源：%d %s", code, data)
	}
	// 重新登录后仍使用相同账号身份查询原回执。
	jar, _ := cookiejar.New(nil)
	client = &http.Client{Jar: jar}
	code, _, data = call("POST", "/api/v2/auth/sessions", "application/json", `{"username":"admin","password":"test-password","entrance":"/signin"}`, nil)
	var session httpapi.CurrentSession
	if code != 201 || json.Unmarshal(data, &session) != nil {
		t.Fatal("再次登录失败", code)
	}
	csrf = session.CsrfToken
	if code, _, _ := call("GET", "/api/v2/requests/concurrent-create?node_id="+n.node.ID, "", "", nil); code != 200 {
		t.Fatal("再次登录改变了请求身份", code)
	}
}

func TestAPIV2OperationDomainFault(t *testing.T) {
	n := openAPINode(t, "领域错误", "东京", 1, 1)
	client, _ := apiAdmin(t, n)
	op, err := n.node.Submit(t.Context(), n.node.ID, cluster.Request{Kind: "name", Key: "domain-fault", Payload: json.RawMessage(`"等待执行"`)})
	if err != nil {
		t.Fatal(err)
	}
	op.State = protocol.AttentionRequired
	op.Steps = []protocol.OperationStep{{Name: "发送到执行器", State: "intent"}}
	op.Error = &protocol.Error{Code: "executor_reply_lost", Message: "执行结果未知，请查询原操作"}
	body, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = n.node.DB.ExecContext(t.Context(), "UPDATE queue SET body=? WHERE id=?", string(body), op.ID); err != nil {
		t.Fatal(err)
	}
	var result httpapi.Operation
	if code := apiCall(t, client, n.web.URL, "GET", "/api/v2/operations/"+op.ID, "", "", "", nil, &result); code != 200 || result.Error == nil || result.Error.Code != "executor_reply_lost" {
		t.Fatal("领域故障不能要求 HTTP 请求 ID 或替换错误码", code, result.Error)
	}
}

func TestAPIV2ComposeRegistrationChecksTargetRevision(t *testing.T) {
	a := openAPINode(t, "入口", "东京", 1, 1)
	b := openAPINode(t, "目标", "东京", 1, 1)
	joinAPINodes(t, a, b)
	_, raw, err := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "Compose", Enabled: true, Grants: []apiauth.Grant{{Module: "compose", Level: "manage", Scope: "nodes", Resources: []string{b.node.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	ref := protocol.ComposeRef{ID: strings.Repeat("b", 64), EngineID: "engine", Name: "example", Directory: "/example", Files: []string{"/example/compose.yml"}, EnvironmentFiles: []string{}}
	save := func() {
		t.Helper()
		body, _ := json.Marshal(ref)
		if _, err := b.node.DB.ExecContext(t.Context(), "INSERT INTO compose_projects(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", ref.ID, string(body)); err != nil {
			t.Fatal(err)
		}
	}
	save()
	path := "/api/v2/nodes/" + b.node.ID + "/engines/engine/compose-projects/" + ref.ID + "/registration"
	call := func(method, tag, key string) (int, http.Header, []byte) {
		t.Helper()
		request, _ := http.NewRequestWithContext(t.Context(), method, a.web.URL+path, nil)
		request.Header.Set("Authorization", "Bearer "+raw)
		if tag != "" {
			request.Header.Set("If-Match", tag)
		}
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		validateTestResponse(t, request, response)
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, response.Header, body
	}
	code, headers, _ := call("GET", "", "")
	if code != 200 || headers.Get("ETag") == "" {
		t.Fatal("登记配置缺少 GET 或 ETag", code)
	}
	if code, _, _ := call("PUT", "", "missing-condition"); code != 428 {
		t.Fatal("登记缺少条件仍受理", code)
	}
	code, _, body := call("PUT", headers.Get("ETag"), "replace-registration")
	var op httpapi.Operation
	if code != 202 || json.Unmarshal(body, &op) != nil {
		t.Fatalf("登记未受理：%d %s", code, body)
	}
	ref.Directory = "/changed"
	save()
	if code, _, _ := call("PUT", headers.Get("ETag"), "stale-registration"); code != 412 {
		t.Fatal("目标未核验登记版本", code)
	}
	if code, _, _ := call("PUT", headers.Get("ETag"), "replace-registration"); code != 202 {
		t.Fatal("重复原键先检查了新版本", code)
	}
	b.node.Start()
	for deadline := time.Now().Add(3 * time.Second); ; {
		current, err := b.node.GetOperation(t.Context(), op.Id)
		if err == nil && current.State == protocol.Rejected && current.Error != nil && strings.Contains(current.Error.Message, "登记已经改变") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("受理后冲突未进入原操作：%+v %v", current, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAPIV2OfflineMemberCanBeRevoked(t *testing.T) {
	a := openAPINode(t, "入口", "东京", 1, 1)
	b := openAPINode(t, "离线目标", "东京", 1, 1)
	joinAPINodes(t, a, b)
	member, err := a.node.Member(t.Context(), b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.node.Close(); err != nil {
		t.Fatal(err)
	}
	client, csrf := apiAdmin(t, a)
	code := apiCall(t, client, a.web.URL, "POST", "/api/v2/nodes/"+b.node.ID+"/actions", "", "revoke-offline", csrf, map[string]any{"action": "revoke", "expected_revision": member.ConfigurationRevision}, nil)
	if code != 200 || !a.node.Revoked(t.Context(), b.node.ID) {
		t.Fatal("撤销本机成员目录被错误地路由到离线目标", code)
	}
}

func TestAPIV2CursorBindsQueryAndAuthorization(t *testing.T) {
	n := openAPINode(t, "分页", "东京", 1, 1)
	token, raw, err := tokenStore(n).Create(t.Context(), apiauth.Input{Name: "节点读取", Enabled: true, Grants: []apiauth.Grant{{Module: "nodes", Level: "read", Scope: "all"}}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		body, _ := json.Marshal(fmt.Sprint(i))
		if _, err := n.node.Submit(t.Context(), n.node.ID, cluster.Request{Kind: "name", Key: fmt.Sprint(i), Payload: body}); err != nil {
			t.Fatal(err)
		}
	}
	var first, next httpapi.OperationList
	if code := apiCall(t, http.DefaultClient, n.web.URL, "GET", "/api/v2/operations?limit=1", raw, "", "", nil, &first); code != 200 || first.NextCursor == nil {
		t.Fatal("操作未提供下一页", code)
	}
	cursor := url.QueryEscape(*first.NextCursor)
	if code := apiCall(t, http.DefaultClient, n.web.URL, "GET", "/api/v2/operations?limit=1&cursor="+cursor, raw, "", "", nil, &next); code != 200 || len(next.Items) != 1 || next.Items[0].Id == first.Items[0].Id {
		t.Fatal("翻页未前进", code)
	}
	if code := apiCall(t, http.DefaultClient, n.web.URL, "GET", "/api/v2/operations?limit=2&cursor="+cursor, raw, "", "", nil, nil); code != 400 {
		t.Fatal("游标未绑定查询", code)
	}
	input := tokenInput(token)
	input.Grants = []apiauth.Grant{{Module: "nodes", Level: "read", Scope: "nodes", Resources: []string{n.node.ID}}}
	if _, err = tokenStore(n).Update(t.Context(), token.ID, input); err != nil {
		t.Fatal(err)
	}
	if code := apiCall(t, http.DefaultClient, n.web.URL, "GET", "/api/v2/operations?limit=1&cursor="+cursor, raw, "", "", nil, nil); code != 400 {
		t.Fatal("权限改变后仍接受旧游标", code)
	}
}

func TestAPIV2QueuedConflictAndTaskPermission(t *testing.T) {
	a := openAPINode(t, "入口", "东京", 1, 1)
	b := openAPINode(t, "目标", "东京", 1, 1)
	joinAPINodes(t, a, b)
	_, raw, err := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "运维", Enabled: true, Grants: []apiauth.Grant{{Module: "nodes", Level: "manage", Scope: "nodes", Resources: []string{b.node.ID}}, {Module: "tasks", Level: "manage", Scope: "nodes", Resources: []string{b.node.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v2/nodes/" + b.node.ID
	var first, second httpapi.Operation
	if code := apiCall(t, http.DefaultClient, a.web.URL, "PATCH", path, raw, "first", "", map[string]string{"name": "第一"}, &first); code != 202 {
		t.Fatal(code)
	}
	if code := apiCall(t, http.DefaultClient, a.web.URL, "PATCH", path, raw, "second", "", map[string]string{"name": "第二"}, &second); code != 202 {
		t.Fatal(code)
	}
	b.node.Start()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		x, e1 := b.node.GetOperation(t.Context(), first.Id)
		y, e2 := b.node.GetOperation(t.Context(), second.Id)
		if e1 == nil && e2 == nil && x.State == "succeeded" && y.State == "rejected" {
			break
		}
		if time.Now().After(deadline.Add(-30 * time.Millisecond)) {
			t.Fatalf("异步版本冲突未保留在原操作：%+v %+v", x, y)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var definition httpapi.Operation
	if code := apiCall(t, http.DefaultClient, a.web.URL, "PUT", "/api/v2/nodes/"+b.node.ID+"/tasks/permission/definition", raw, "save-task", "", map[string]any{"name": "任务", "schedule": "30 2 * * *", "timezone": "Asia/Shanghai", "script": "true", "directory": "/tmp", "enabled": false, "revision": 0}, &definition); code != 202 {
		t.Fatal("任务管理令牌保存定义失败", code)
	}
	waitAPIOperation(t, b.node, definition.Id)
	_, readOnly, e := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "任务只读", Enabled: true, Grants: []apiauth.Grant{{Module: "tasks", Level: "read", Scope: "nodes", Resources: []string{b.node.ID}}}})
	if e != nil {
		t.Fatal(e)
	}
	var tasks httpapi.ScheduledTaskList
	for deadline := time.Now().Add(3 * time.Second); ; {
		code := apiCall(t, http.DefaultClient, a.web.URL, "GET", "/api/v2/tasks", readOnly, "", "", nil, &tasks)
		if code != 200 {
			t.Fatal("任务元数据读取错误", code)
		}
		if len(tasks.Items) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("任务元数据未完成读取")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", "/api/v2/nodes/"+b.node.ID+"/tasks/permission/definition", readOnly, "", "", nil, nil); code != 403 {
		t.Fatal("读取权限泄露脚本", code)
	}
	if code := apiCall(t, http.DefaultClient, a.web.URL, "POST", "/api/v2/nodes/"+b.node.ID+"/tasks/permission/executions", readOnly, "forbidden-run", "", map[string]int{"expected_revision": 1}, nil); code != 403 {
		t.Fatal("读取令牌执行了 Shell", code)
	}
}

func TestAPIV2Contract(t *testing.T) {
	n := openAPINode(t, "v2", "test", 1, 1)
	_, raw, e := tokenStore(n).Create(t.Context(), apiauth.Input{Name: "契约", Enabled: true, Grants: []apiauth.Grant{{Module: "nodes", Level: "read", Scope: "all"}}})
	if e != nil {
		t.Fatal(e)
	}
	request := func(method, path, media, body, token string) (int, http.Header, []byte) {
		t.Helper()
		r, e := http.NewRequest(method, n.web.URL+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if media != "" {
			r.Header.Set("Content-Type", media)
		}
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		data, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		return response.StatusCode, response.Header, data
	}
	for _, path := range []string{"/api/v1/nodes", "/api/v2/missing"} {
		code, h, b := request("GET", path, "", "", raw)
		var p httpapi.Problem
		if code != 404 || json.Unmarshal(b, &p) != nil || p.RequestId != h.Get("X-Request-ID") || h.Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s: %d %s", path, code, b)
		}
	}
	code, _, b := request("GET", "/api/v2/", "", "", raw)
	var identity httpapi.APIIdentity
	if code != 200 || json.Unmarshal(b, &identity) != nil || identity.ApiVersion != "2.0.0" || identity.Principal.Id == "" {
		t.Fatalf("身份 %d %s", code, b)
	}
	code, h, b := request("GET", "/api/v2/openapi.json", "", "", raw)
	if code != 200 || !json.Valid(b) || !strings.Contains(string(b), `"version":"2.0.0"`) {
		t.Fatalf("规范 %d %s", code, b)
	}
	code, h, b = request("POST", "/api/v2/nodes", "", "", raw)
	if code != 405 || h.Get("Allow") == "" {
		t.Fatalf("方法 %d %s", code, b)
	}
	code, h, b = request("GET", "/metrics", "", "", "")
	if code != 401 || h.Get("WWW-Authenticate") == "" {
		t.Fatalf("认证 %d %s", code, b)
	}
	code, _, b = request("GET", "/api/v2/tasks", "", "", raw)
	if code != 403 {
		t.Fatalf("权限 %d %s", code, b)
	}
	code, _, b = request("HEAD", "/api/v2/nodes", "", "", raw)
	if code != 200 || len(b) != 0 {
		t.Fatalf("HEAD %d %s", code, b)
	}
}
