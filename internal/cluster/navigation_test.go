package cluster

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestNavigationPersistsAcrossAccountChangeAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	n, e := Open(t.Context(), path, Options{Username: "admin", Password: "test-password", Entrance: "/signin"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { n.Close() })
	value, e := n.Navigation(t.Context())
	if e != nil || value.Revision != 0 || !slices.Equal(value.Order, defaultNavigation) {
		t.Fatal("默认菜单顺序错误", value, e)
	}
	value.Order[0], value.Order[4] = value.Order[4], value.Order[0]
	saved, e := n.SaveNavigation(t.Context(), value)
	if e != nil || saved.Revision != 1 {
		t.Fatal("保存菜单顺序失败", saved, e)
	}
	op, e := n.Submit(t.Context(), n.ID, Request{ID: NewOperationID(n.ID), Kind: "account", Payload: payload(AccountChange{Username: "changed", Password: "changed-password", Entrance: "/other-signin"})})
	if e != nil {
		t.Fatal(e)
	}
	n.advance()
	result, e := n.GetOperation(t.Context(), op.ID)
	if e != nil || result.State != protocol.Succeeded {
		t.Fatal("账号修改失败", result, e)
	}
	if e = n.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(t.Context(), path, Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	current, e := reopened.Navigation(t.Context())
	if e != nil || current.Revision != saved.Revision || !slices.Equal(current.Order, saved.Order) {
		t.Fatal("账号修改或重开丢失菜单顺序", current, e)
	}
	a, e := reopened.Account(t.Context())
	if e != nil || a.Username != "changed" {
		t.Fatal("账号修改未保留", a.Username, e)
	}
}
