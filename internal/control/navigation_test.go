package control

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
)

func TestNavigationAccountScopeSessionsAndConflicts(t *testing.T) {
	a := openAPINode(t, "甲", "东京", 1, 0)
	b := openAPINode(t, "乙", "东京", 1, 0)
	joinAPINodes(t, a, b)
	client, csrf := apiAdmin(t, a)
	otherClient, otherCSRF := apiAdmin(t, a)
	otherNode, _ := apiAdmin(t, b)
	const path = "/api/v1/settings/navigation"
	call := func(client *http.Client, method, csrf string, input, out any) int {
		t.Helper()
		return apiCall(t, client, a.web.URL, method, path, "", "", csrf, input, out)
	}
	if code := call(http.DefaultClient, "GET", "", nil, nil); code != 401 {
		t.Fatal("未登录请求状态错误", code)
	}
	var defaults cluster.NavigationPreferences
	if code := call(client, "GET", "", nil, &defaults); code != 200 || defaults.Revision != 0 || !slices.Equal(defaults.Order, []string{"overview", "nodes", "containers", "databases", "storage", "firewall", "settings"}) {
		t.Fatal("默认菜单错误", code, defaults)
	}
	input := defaults
	input.Order = slices.Clone(defaults.Order)
	input.Order[0], input.Order[4] = input.Order[4], input.Order[0]
	if code := call(client, "PATCH", "", input, nil); code != 403 {
		t.Fatal("缺少 CSRF 的写入状态错误", code)
	}
	changes, cancel := a.node.Subscribe()
	defer cancel()
	var saved cluster.NavigationPreferences
	if code := call(client, "PATCH", csrf, input, &saved); code != 200 || saved.Revision != 1 {
		t.Fatal("菜单保存失败", code, saved)
	}
	select {
	case change := <-changes:
		if change.Resource != "navigation" || change.NodeID != a.node.ID {
			t.Fatal("菜单事件错误", change)
		}
	case <-time.After(time.Second):
		t.Fatal("菜单事件未发送")
	}
	var current cluster.NavigationPreferences
	if code := call(otherClient, "GET", "", nil, &current); code != 200 || current.Revision != saved.Revision || !slices.Equal(current.Order, input.Order) {
		t.Fatal("另一会话未读取相同菜单", code, current)
	}
	if code := call(otherClient, "PATCH", otherCSRF, input, nil); code != 409 {
		t.Fatal("旧版本未返回冲突", code)
	}
	if code := call(client, "GET", "", nil, nil); code != 200 {
		t.Fatal("保存菜单撤销了原会话", code)
	}
	for _, order := range [][]string{defaults.Order[:6], {"overview", "nodes", "containers", "databases", "storage", "firewall", "firewall"}, {"overview", "nodes", "containers", "databases", "storage", "firewall", "unknown"}} {
		if code := call(client, "PATCH", csrf, cluster.NavigationPreferences{Order: order, Revision: saved.Revision}, nil); code != 422 {
			t.Fatal("无效菜单未拒绝", code, order)
		}
	}
	if code := call(client, "PATCH", csrf, map[string]any{"order": saved.Order, "revision": -1}, nil); code != 422 {
		t.Fatal("负版本未拒绝", code)
	}
	if code := apiCall(t, otherNode, b.web.URL, "GET", path, "", "", "", nil, &current); code != 200 || current.Revision != 0 || !slices.Equal(current.Order, defaults.Order) {
		t.Fatal("其他节点账号的菜单被改变", code, current)
	}
	_, raw, e := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "只读", Enabled: true, Grants: []apiauth.Grant{{Module: "nodes", Level: "read", Scope: "all"}}})
	if e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"GET", "PATCH"} {
		if code := apiCall(t, client, a.web.URL, method, path, raw, "", csrf, saved, nil); code != 403 {
			t.Fatal("Bearer 访问了账号偏好", method, code)
		}
	}
}
