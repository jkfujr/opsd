package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/opsd-labs/opsd/internal/httpapi"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/protocol"
	"github.com/opsd-labs/opsd/internal/transport"
)

func (f *fixture) token(csrf string) httpapi.EnrollmentTokenCreated {
	f.t.Helper()
	response, data := f.request(f.client, "POST", "/api/v1/enrollment-tokens", "", csrf, true)
	expect(f.t, response, data, 201)
	var token httpapi.EnrollmentTokenCreated
	if err := json.Unmarshal(data, &token); err != nil {
		f.t.Fatal(err)
	}
	if token.Record.ExpiresAt.Sub(token.Record.CreatedAt) != 24*time.Hour || token.Token == "" {
		f.t.Fatal("接入码期限或明文错误")
	}
	return token
}

func (f *fixture) dial(ctx context.Context, key identity.Key, token string, pending []string) (*transport.Channel, protocol.Message, error) {
	socket, _, err := websocket.Dial(ctx, f.server.URL+"/api/v1/agent/connect", nil)
	if err != nil {
		return nil, protocol.Message{}, err
	}
	channel, err := transport.Handshake(ctx, socket, key, f.service.key.Public, true)
	if err != nil {
		socket.CloseNow()
		return nil, protocol.Message{}, err
	}
	err = channel.Send(ctx, protocol.Message{RequestID: "registration", Payload: &protocol.SessionOpen{Hostname: "原始主机名", Purpose: "business", EnrollmentToken: token, AgentVersion: "0.1.0-dev", OS: "linux", Arch: "amd64", PendingOperationIDs: pending}})
	if err != nil {
		channel.Close()
		return nil, protocol.Message{}, err
	}
	response, err := channel.Receive(ctx)
	if err != nil {
		channel.Close()
		return nil, protocol.Message{}, err
	}
	return channel, response, nil
}

func TestEncryptedNodeEnrollmentAndLifecycle(t *testing.T) {
	f := setup(t)
	session := f.login(f.client, "/secret-test", "admin", "test-password")
	token := f.token(session.CsrfToken)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type result struct {
		key     identity.Key
		channel *transport.Channel
		message protocol.Message
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		key, err := identity.New()
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			<-start
			channel, message, err := f.dial(ctx, key, token.Token, []string{})
			results <- result{key, channel, message, err}
		}()
	}
	close(start)
	var winner result
	acceptedCount := 0
	for range 2 {
		value := <-results
		if value.err != nil {
			t.Fatal(value.err)
		}
		t.Cleanup(value.channel.Close)
		switch payload := value.message.Payload.(type) {
		case *protocol.SessionAccepted:
			acceptedCount++
			winner = value
		case *protocol.RequestFailed:
			if payload.Error.Code != "enrollment_rejected" {
				t.Fatalf("错误接入结果：%+v", payload)
			}
		default:
			t.Fatalf("意外应答：%T", payload)
		}
	}
	if acceptedCount != 1 {
		t.Fatalf("同一接入码建立了 %d 个节点", acceptedCount)
	}
	first := winner.message.Payload.(*protocol.SessionAccepted)
	if !first.Writable || winner.message.ReplyTo != "registration" {
		t.Fatal("登记应答或恢复状态错误")
	}
	response, data := f.request(f.client, "PATCH", "/api/v1/nodes/"+first.NodeID, `{"name":"用户设置的名称"}`, session.CsrfToken, true)
	expect(t, response, data, 200)
	// 第一份登记应答未落到 Agent 本机状态时，同一密钥仍应恢复原节点。
	next, message, err := f.dial(ctx, winner.key, token.Token, []string{"未知未决操作"})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	second, ok := message.Payload.(*protocol.SessionAccepted)
	if !ok || second.NodeID != first.NodeID || second.ConnectionID == first.ConnectionID || second.Writable {
		t.Fatalf("重连身份、接替或写入限制错误：%+v", message)
	}
	if _, err := winner.channel.Receive(ctx); err == nil {
		t.Fatal("旧业务连接仍可接收")
	}
	nodes, err := f.service.nodes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Name != "用户设置的名称" || !nodes[0].Connected || nodes[0].Writable {
		t.Fatalf("节点名称或连接状态错误：%+v", nodes)
	}
	response, data = f.request(f.client, "GET", "/api/v1/enrollment-tokens", "", "", false)
	expect(t, response, data, 200)
	var tokens httpapi.EnrollmentTokenList
	if err := json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	if len(tokens.Items) != 1 || tokens.Items[0].ConsumedAt == nil {
		t.Fatal("消费状态未保存")
	}
	if bytes.Contains(data, []byte(token.Token)) {
		t.Fatal("接入码列表泄露了明文")
	}
	next.Close()
	f.service.Close()
	f.server.Close()
	f.start()
	nodes, err = f.service.nodes(t.Context())
	if err != nil || len(nodes) != 1 || nodes[0].Connected {
		t.Fatalf("重启沿用了历史在线状态：%+v %v", nodes, err)
	}
	restored, restoredMessage, err := f.dial(ctx, winner.key, "", []string{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if value, ok := restoredMessage.Payload.(*protocol.SessionAccepted); !ok || value.NodeID != first.NodeID {
		t.Fatal("重启后无法恢复原身份")
	}
	response, data = f.request(f.client, "POST", "/api/v1/nodes/"+first.NodeID+"/revoke", "", session.CsrfToken, true)
	expect(t, response, data, 200)
	replacementToken := f.token(session.CsrfToken)
	refused, refusedMessage, err := f.dial(ctx, winner.key, replacementToken.Token, []string{})
	if err != nil {
		t.Fatal(err)
	}
	defer refused.Close()
	if value, ok := refusedMessage.Payload.(*protocol.RequestFailed); !ok || value.Error.Code != "identity_revoked" {
		t.Fatalf("新接入码重新授权了撤销身份：%+v", refusedMessage)
	}
	nodes, err = f.service.nodes(t.Context())
	if err != nil || len(nodes) != 1 || nodes[0].RevokedAt == nil || nodes[0].Connected {
		t.Fatalf("撤销未保留历史或仍在线：%+v %v", nodes, err)
	}
}

func TestExpiredRevokedTokensAndWrongControlKey(t *testing.T) {
	f := setup(t)
	session := f.login(f.client, "/secret-test", "admin", "test-password")
	expired := f.token(session.CsrfToken)
	f.clock.Add(int64(25 * time.Hour))
	revoked := f.token(session.CsrfToken)
	response, data := f.request(f.client, "DELETE", "/api/v1/enrollment-tokens/"+revoked.Record.Id, "", session.CsrfToken, true)
	expect(t, response, data, 204)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for _, token := range []string{expired.Token, revoked.Token} {
		key, err := identity.New()
		if err != nil {
			t.Fatal(err)
		}
		channel, message, err := f.dial(ctx, key, token, []string{})
		if err != nil {
			t.Fatal(err)
		}
		channel.Close()
		if value, ok := message.Payload.(*protocol.RequestFailed); !ok || value.Error.Code != "enrollment_rejected" {
			t.Fatalf("无效接入码未被拒绝：%+v", message)
		}
	}
	socket, _, err := websocket.Dial(ctx, f.server.URL+"/api/v1/agent/connect", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.CloseNow()
	key, _ := identity.New()
	wrong, _ := identity.New()
	if _, err := transport.Handshake(ctx, socket, key, wrong.Public, true); !errors.Is(err, transport.ErrAuthentication) {
		t.Fatalf("错误控制端公钥未明确失败：%v", err)
	}
	var count int
	if err := f.db.QueryRow("SELECT count(*) FROM nodes").Scan(&count); err != nil || count != 0 {
		t.Fatalf("拒绝接入时仍创建了节点：%d %v", count, err)
	}
}
