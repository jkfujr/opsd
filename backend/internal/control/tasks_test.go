package control

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestAPIV2TaskRunCursorBindsQueryAndAuthorization(t *testing.T) {
	a := openAPINode(t, "入口", "东京", 1, 1)
	b := openAPINode(t, "任务节点", "东京", 1, 1)
	joinAPINodes(t, a, b)
	token, raw, err := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "任务读取", Enabled: true, Grants: []apiauth.Grant{{Module: "tasks", Level: "read", Scope: "all"}}})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for _, entry := range []struct {
		id     string
		offset time.Duration
	}{
		{"old-z", 0}, {"new-a", time.Second}, {"same-z", 0}, {"fraction", time.Nanosecond},
	} {
		op := cluster.Operation{OperationRecord: protocol.OperationRecord{ID: b.node.ID + "_" + entry.id, State: protocol.Succeeded, Revision: 1, Decision: "undecided", Steps: []protocol.OperationStep{}}, NodeID: b.node.ID, Subject: "task_run", ResourceID: "recording", CreatedAt: base.Add(entry.offset), UpdatedAt: base.Add(entry.offset), Executor: "node"}
		body, e := json.Marshal(op)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.node.DB.ExecContext(t.Context(), "INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,phase,body,terminal) VALUES(?,?,?,'{}','',?,'finished',?,1)", op.ID, b.node.ID, op.Subject, []byte("test"), string(body)); e != nil {
			t.Fatal(e)
		}
	}
	path := "/api/v2/nodes/" + b.node.ID + "/tasks/recording/executions"
	var first, next httpapi.ScheduledTaskRuns
	if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", path+"?limit=2", raw, "", "", nil, &first); code != 200 || len(first.Items) != 2 || first.Items[0].Id != b.node.ID+"_new-a" || first.Items[1].Id != b.node.ID+"_fraction" || first.NextCursor == nil {
		t.Fatalf("HTTP 覆盖了任务时间顺序或下一页：%d %+v", code, first)
	}
	cursor := url.QueryEscape(*first.NextCursor)
	if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", path+"?limit=2&cursor="+cursor, raw, "", "", nil, &next); code != 200 || len(next.Items) != 2 || next.Items[0].Id != b.node.ID+"_same-z" || next.Items[1].Id != b.node.ID+"_old-z" || next.NextCursor != nil {
		t.Fatalf("HTTP 翻页遗漏或重复：%d %+v", code, next)
	}
	for _, path := range []string{
		path + "?limit=1&cursor=" + cursor,
		"/api/v2/nodes/" + b.node.ID + "/tasks/other/executions?limit=2&cursor=" + cursor,
		"/api/v2/nodes/" + a.node.ID + "/tasks/recording/executions?limit=2&cursor=" + cursor,
		path + "?limit=2&cursor=invalid",
	} {
		if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", path, raw, "", "", nil, nil); code != 400 {
			t.Fatal("条件变化或非法游标被接受", path, code)
		}
	}
	var outer collectionCursor
	data, err := base64.RawURLEncoding.DecodeString(*first.NextCursor)
	if err != nil || json.Unmarshal(data, &outer) != nil {
		t.Fatal("返回游标无效", err)
	}
	for _, position := range []string{"invalid", "null", `{}`, `{"created_at":"2026-11-10T12:00:00Z","id":"other-node_run"}`} {
		broken := outer
		broken.Position = base64.RawURLEncoding.EncodeToString([]byte(position))
		data, err := json.Marshal(broken)
		if err != nil {
			t.Fatal(err)
		}
		encoded := url.QueryEscape(base64.RawURLEncoding.EncodeToString(data))
		if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", path+"?limit=2&cursor="+encoded, raw, "", "", nil, nil); code != 400 {
			t.Fatal("非法时间位置被接受", position, code)
		}
	}
	for _, test := range []struct {
		limit  string
		status int
	}{{"0", 422}, {"101", 422}, {"-1", 422}, {"invalid", 400}} {
		if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", path+"?limit="+test.limit, raw, "", "", nil, nil); code != test.status {
			t.Fatal("非法页大小的状态错误", test.limit, code)
		}
	}
	input := tokenInput(token)
	input.Grants = []apiauth.Grant{{Module: "tasks", Level: "read", Scope: "nodes", Resources: []string{b.node.ID}}}
	if _, err = tokenStore(a).Update(t.Context(), token.ID, input); err != nil {
		t.Fatal(err)
	}
	if code := apiCall(t, http.DefaultClient, a.web.URL, "GET", path+"?limit=2&cursor="+cursor, raw, "", "", nil, nil); code != 400 {
		t.Fatal("授权变化后仍接受旧游标", code)
	}
}
