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
	listen = ":" + port
	if err = n.ConfigureNetwork(t.Context(), NetworkOptions{Listen: listen, AddressMode: "auto"}); err != nil {
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
	if err = n.ConfigureNetwork(t.Context(), NetworkOptions{AddressScheme: "tcp"}); err != nil {
		t.Fatal(err)
	}
	c, _ = n.Config(t.Context())
	if c.Address != "tcp://8.8.8.8:"+port || c.AddressMode != "auto" || c.PeerTransport != "" {
		t.Fatal("改变协议时未保留地址模式或兼容监听", c)
	}
	if err = n.ConfigureNetwork(t.Context(), NetworkOptions{PeerTransport: "tcp"}); err != nil {
		t.Fatal(err)
	}
	if err = n.ConfigureNetwork(t.Context(), NetworkOptions{AddressScheme: "http"}); err == nil {
		t.Fatal("仅 TCP 接收时仍能公布 WebSocket 地址")
	}
	if err = n.ConfigureNetwork(t.Context(), NetworkOptions{PeerTransport: "dual", AddressScheme: "http"}); err != nil {
		t.Fatal("兼容监听和原地址恢复失败", err)
	}
	c, _ = n.Config(t.Context())
	if c.Address != "http://8.8.8.8:"+port || c.AddressMode != "auto" || c.PeerTransport != "dual" {
		t.Fatal("回退改变了自动地址配置", c)
	}
	if err = n.ConfigureNetwork(t.Context(), NetworkOptions{AddressMode: "manual", Address: "http://100.100.1.2:8444"}); err != nil {
		t.Fatal(err)
	}
	c, _ = n.Config(t.Context())
	if c.Address != "http://100.100.1.2:8444" || c.AddressMode != "manual" {
		t.Fatal(c)
	}
}

func TestTCPPeerAddressValidation(t *testing.T) {
	for _, address := range []string{"tcp://100.100.201.91:8444", "tcp://[::1]:8444", "tcp://opsd.example:8444", "http://127.0.0.1:8444/"} {
		if !ValidPeerAddress(address) {
			t.Errorf("有效端点被拒绝：%s", address)
		}
	}
	for _, address := range []string{
		"tcp://host", "tcp://host:65536", "tcp://host:0", "tcp://127.0.0.999:8444",
		"tcp://[host]:8444", "tcp://::1:8444", "tcp://[::]:8444", "tcp://[fe80::1%25eth0]:8444",
		"tcp://user@host:8444", "tcp://host:8444/peer", "tcp://host:8444?", "tcp://host:8444#",
	} {
		if ValidPeerAddress(address) {
			t.Errorf("接受了无效端点：%s", address)
		}
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
	if err = n.ConfigureNetwork(t.Context(), NetworkOptions{Listen: listener.Addr().String(), AddressMode: "manual"}); err == nil {
		t.Fatal("占用中的监听地址被接受")
	}
	after, _ := n.Config(t.Context())
	if before.Revision != after.Revision || before.Listen != after.Listen {
		t.Fatal("失败后保存了新配置")
	}
	if _, err = n.Submit(t.Context(), n.ID, Request{Key: "pending", Kind: "name", Payload: payload("排队中的名称")}); err != nil {
		t.Fatal(err)
	}
	if err = n.ConfigureNetwork(t.Context(), NetworkOptions{AddressMode: "manual", Address: "http://100.100.1.2:8444"}); err == nil {
		t.Fatal("存在未结束操作仍修改网络")
	}
}
