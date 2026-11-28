package cluster

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

func openPeerTestNode(t *testing.T, tcp bool) *Node {
	t.Helper()
	if tcp {
		n, _ := openTCPTestNode(t, "127.0.0.1:0", true, "")
		return n
	}
	n, _ := openTestNode(t)
	return n
}

func openTCPTestNode(t *testing.T, listen string, tcpOnly bool, primary string) (*Node, string) {
	t.Helper()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "tcp://" + listener.Addr().String()
	config := Config{Address: endpoint, Listen: listener.Addr().String()}
	if primary != "" {
		config.Address, config.Endpoints = primary, []string{endpoint}
	}
	n, err := Open(t.Context(), filepath.Join(t.TempDir(), "node.db"), Options{Password: "test-password", Config: config})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	n.Log = t.Logf
	done := make(chan error, 1)
	go func() { done <- n.ServePeers(listener, tcpOnly) }()
	t.Cleanup(func() {
		n.Close()
		listener.Close()
		<-done
	})
	return n, endpoint
}

func TestPrivateTCPJoinDualListenerAndRevoke(t *testing.T) {
	seed, endpoint := openTCPTestNode(t, "127.0.0.1:0", false, "")
	source := openPeerTestNode(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	legacy := openPeerTestNode(t, false)
	token, _, _, err := seed.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = legacy.Join(ctx, "http"+endpoint[len("tcp"):], identity.Encode(seed.Key.Public), token); err != nil {
		t.Fatal("保留旧 WebSocket 接入失败", err)
	}
	joinTest(t, source, seed)
	if err := source.Call(ctx, seed.ID, "ping", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := seed.Revoke(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Terminal(ctx, seed.ID); faultCode(err) != "forbidden" {
		t.Fatal("撤销后仍可通过私有 TCP 准入", err)
	}
}

func TestPrivateTCPFallbackVerifiesIdentityAndIPv6(t *testing.T) {
	check, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("测试宿主不支持 IPv6")
	}
	check.Close()
	_, wrong := openTCPTestNode(t, "127.0.0.1:0", true, "")
	seed, actual := openTCPTestNode(t, "[::1]:0", true, wrong)
	source := openPeerTestNode(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if ws, _, err := websocket.Dial(ctx, "http"+actual[len("tcp"):]+"/peer", nil); err == nil {
		ws.CloseNow()
		t.Fatal("仅 TCP 监听仍允许旧 WebSocket")
	}
	token, _, _, err := seed.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	member, err := seed.Self(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = source.Join(ctx, actual, member.Public, token); err != nil {
		t.Fatal("IPv6 私有 TCP 接入失败", err)
	}
	grantAll(t, seed, source.ID)
	var result Config
	if err := source.Call(ctx, seed.ID, "config.read", nil, &result); err != nil || result.Address != wrong {
		t.Fatal("错误公钥的主地址阻断了正确的 IPv6 备用端点", err)
	}
}

func TestPrivateTCPReadOriginalOperationAfterDisconnect(t *testing.T) {
	target := openPeerTestNode(t, true)
	source := openPeerTestNode(t, true)
	joinTest(t, source, target)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	member, err := target.Self(ctx)
	if err != nil {
		t.Fatal(err)
	}
	self, err := source.Self(ctx)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := source.openPeerChannel(ctx, member, target.Key.Public)
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	if err = channel.Send(ctx, protocol.Message{RequestID: "hello", Payload: &protocol.PeerHello{Member: payload(self)}}); err != nil {
		t.Fatal(err)
	}
	if reply, err := channel.Receive(ctx); err != nil || reply.ReplyTo != "hello" {
		t.Fatal("私有通道接入失败", err)
	}
	id := NewOperationID(target.ID)
	request := Request{ID: id, Kind: "name", Key: "original-key", Payload: payload("断线后查询原操作")}
	if err = channel.Send(ctx, protocol.Message{RequestID: "submit-once", Payload: &protocol.PeerCall{Method: "submit", Body: payload(request)}}); err != nil {
		t.Fatal(err)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := target.operation(ctx, id); err == nil {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("目标未持久保存原写入")
		}
	}
	channel.Close()
	got, err := source.GetOperation(ctx, id)
	if err != nil || got.ID != id || got.State != protocol.Accepted {
		t.Fatal("重连后未取得原操作", err, got.ID, got.State)
	}
	var count int
	if err := target.DB.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE origin=? AND request_key=?", source.ID, request.Key).Scan(&count); err != nil || count != 1 {
		t.Fatal("重连重复提交了原写入", err, count)
	}
}
