package apiauth_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
)

func TestTokenLifecycleScopeAndPrivateValue(t *testing.T) {
	now := time.Now().UTC()
	n, e := cluster.Open(t.Context(), filepath.Join(t.TempDir(), "node.db"), cluster.Options{Password: "test-password"})
	if e != nil {
		t.Fatal(e)
	}
	defer n.Close()
	store := apiauth.Store{DB: n.DB, Owner: n.ID, Now: func() time.Time { return now }}
	expires := now.Add(time.Hour)
	input := apiauth.Input{Name: "日本数据库", Enabled: true, Grants: []apiauth.Grant{{Module: "monitoring", Level: "read", Scope: "regions", Resources: []string{"日本"}}, {Module: "databases", Level: "manage", Scope: "clusters", Resources: []string{"cluster-a"}}}, ExpiresAt: &expires, SourceIPs: []string{"192.0.2.0/24", "2001:db8::/64"}}
	token, raw, e := store.Create(t.Context(), input)
	if e != nil {
		t.Fatal(e)
	}
	if apiauth.Owner(raw) != n.ID {
		t.Fatal("令牌所属节点错误")
	}
	var summary string
	var digest []byte
	if e = n.DB.QueryRow("SELECT body,token_hash FROM api_tokens WHERE id=?", token.ID).Scan(&summary, &digest); e != nil {
		t.Fatal(e)
	}
	if len(digest) != 32 || strings.Contains(summary, raw) || strings.Contains(summary, strings.Split(raw, ".")[3]) {
		t.Fatal("保存了令牌完整值")
	}
	for _, ip := range []string{"192.0.2.12", "2001:db8::1"} {
		verified, e := store.Verify(t.Context(), raw, ip)
		if e != nil || verified.ID != token.ID {
			t.Fatal("允许来源校验失败", e)
		}
	}
	if _, e = store.Verify(t.Context(), raw, "203.0.113.1"); !errors.Is(e, apiauth.ErrUnauthorized) {
		t.Fatal("来源范围失效", e)
	}
	if !token.Allows("monitoring", "read", "node-a", "日本") || token.Allows("monitoring", "manage", "node-a", "日本") || token.Allows("monitoring", "read", "node-a", "美国") || !token.Allows("databases", "manage", "cluster-a", "") || token.Allows("databases", "read", "cluster-b", "") || token.Has("containers", "read") {
		t.Fatal("模块或资源授权错误")
	}
	input.Revision = token.Revision
	input.Enabled = false
	token, e = store.Update(t.Context(), token.ID, input)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Verify(t.Context(), raw, "192.0.2.12"); !errors.Is(e, apiauth.ErrUnauthorized) {
		t.Fatal("停用未生效", e)
	}
	if _, e = store.Update(t.Context(), token.ID, input); !errors.Is(e, apiauth.ErrConflict) {
		t.Fatal("旧版本覆盖了令牌", e)
	}
	input.Enabled = true
	input.Revision = token.Revision
	token, e = store.Update(t.Context(), token.ID, input)
	if e != nil {
		t.Fatal(e)
	}
	token, replacement, e := store.Regenerate(t.Context(), token.ID, token.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Verify(t.Context(), raw, "192.0.2.12"); !errors.Is(e, apiauth.ErrUnauthorized) {
		t.Fatal("重新生成后旧值仍可用", e)
	}
	if _, e = store.Verify(t.Context(), replacement, "192.0.2.12"); e != nil {
		t.Fatal(e)
	}
	now = expires
	if _, e = store.Verify(t.Context(), replacement, "192.0.2.12"); !errors.Is(e, apiauth.ErrUnauthorized) {
		t.Fatal("过期未生效", e)
	}
	if e = store.Delete(t.Context(), token.ID, token.Revision); e != nil {
		t.Fatal(e)
	}
	items, e := store.List(t.Context())
	if e != nil || len(items) != 0 {
		t.Fatal("删除未保存", e)
	}
}
func TestGrantValidation(t *testing.T) {
	valid := apiauth.Input{Name: "容器", Enabled: true, Grants: []apiauth.Grant{{Module: "containers", Level: "manage", Scope: "nodes", Resources: []string{"node-a"}}}}
	for _, change := range []func(*apiauth.Input){func(i *apiauth.Input) { i.Grants[0].Module = "future_module" }, func(i *apiauth.Input) { i.Grants[0].Scope = "clusters" }, func(i *apiauth.Input) { i.Grants[0].Resources = nil }, func(i *apiauth.Input) { i.Grants[0].Module = "monitoring" }, func(i *apiauth.Input) { i.SourceIPs = []string{"invalid"} }} {
		input := valid
		input.Grants = append([]apiauth.Grant{}, valid.Grants...)
		change(&input)
		if e := apiauth.Validate(input); !errors.Is(e, apiauth.ErrInvalid) {
			t.Fatal("接受了无效授权", e)
		}
	}
}
