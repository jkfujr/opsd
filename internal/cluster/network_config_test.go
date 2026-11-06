package cluster

import (
	"encoding/json"
	"net"
	"testing"
)

func TestLocalNetworkConfigPreservesIdentityAndAccount(t *testing.T) {
	n, _ := openTestNode(t)
	manualAddress(t, n, "8.8.8.8")
	account, err := n.Account(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(account)
	self, err := n.Self(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := listener.Addr().String()
	listener.Close()
	_, port, _ := net.SplitHostPort(listen)
	if err = n.ConfigureNetwork(t.Context(), listen, "auto", ""); err != nil {
		t.Fatal(err)
	}
	c, err := n.Config(t.Context())
	if err != nil || c.Listen != listen || c.Address != "http://8.8.8.8:"+port || c.AddressMode != "auto" {
		t.Fatal(c, err)
	}
	after, _ := n.Account(t.Context())
	raw, _ := json.Marshal(after)
	member, _ := n.Self(t.Context())
	if string(before) != string(raw) || member.Public != self.Public || member.ID != self.ID || member.Address != c.Address {
		t.Fatal("网络配置改变了身份或账号", member)
	}
	if err = n.ConfigureNetwork(t.Context(), "", "manual", "http://100.100.1.2:8444"); err != nil {
		t.Fatal(err)
	}
	c, _ = n.Config(t.Context())
	if c.Address != "http://100.100.1.2:8444" || c.AddressMode != "manual" {
		t.Fatal(c)
	}
}
func TestLocalNetworkRejectsPendingAndOccupiedListener(t *testing.T) {
	n, _ := openTestNode(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	before, _ := n.Config(t.Context())
	if err = n.ConfigureNetwork(t.Context(), listener.Addr().String(), "manual", ""); err == nil {
		t.Fatal("占用中的监听地址被接受")
	}
	after, _ := n.Config(t.Context())
	if before.Revision != after.Revision || before.Listen != after.Listen {
		t.Fatal("失败后保存了新配置")
	}
	if _, err = n.Submit(t.Context(), n.ID, Request{Key: "pending", Kind: "name", Payload: payload("排队中的名称")}); err != nil {
		t.Fatal(err)
	}
	if err = n.ConfigureNetwork(t.Context(), "", "manual", "http://100.100.1.2:8444"); err == nil {
		t.Fatal("存在未结束操作仍修改网络")
	}
}
