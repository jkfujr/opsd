package transport

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

func pair(t *testing.T) (*Channel, <-chan error) {
	t.Helper()
	serverKey, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			received <- err
			return
		}
		defer socket.CloseNow()
		channel, err := Handshake(r.Context(), socket, serverKey, nil, false)
		if err != nil {
			received <- err
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		message, err := channel.Receive(ctx)
		if err == nil {
			err = channel.Send(ctx, message)
		}
		received <- err
	}))
	t.Cleanup(server.Close)
	socket, _, err := websocket.Dial(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { socket.CloseNow() })
	channel, err := Handshake(t.Context(), socket, clientKey, serverKey.Public, true)
	if err != nil {
		t.Fatal(err)
	}
	return channel, received
}

func TestEncryptedFragmentation(t *testing.T) {
	channel, received := pair(t)
	text := strings.Repeat("加密记录", 12000)
	message := protocol.Message{RequestID: "large-message", Payload: &protocol.RequestFailed{Error: protocol.Error{Code: "test", Message: text}}}
	if err := channel.Send(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	echo, err := channel.Receive(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := echo.Payload.(*protocol.RequestFailed)
	if !ok || echo.RequestID != message.RequestID || payload.Error.Message != text {
		t.Fatal("跨记录消息发生丢失或串包")
	}
	if err := <-received; err != nil {
		t.Fatal(err)
	}
}

func TestRejectDamagedTruncatedAndOversizedRecords(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		corrupt bool
	}{
		{"篡改", []byte{0, 0, 0, 2, '{', '}'}, true},
		{"截断", []byte{0, 0, 0, 20, '{', '}'}, false},
		{"逻辑超限", []byte{0, 128, 0, 1}, false},
		{"记录超限", make([]byte, RecordBytes+1), false},
		{"长度不匹配", []byte{0, 0, 0, 1, '{', '}'}, false},
		{"版本不兼容", nil, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			channel, received := pair(t)
			plain := item.data
			if item.name == "版本不兼容" {
				body := []byte(`{"version":2,"kind":"heartbeat","request_id":"x","body":{"sent_at":"2026-10-27T00:00:00Z"}}`)
				plain = make([]byte, 4+len(body))
				binary.BigEndian.PutUint32(plain, uint32(len(body)))
				copy(plain[4:], body)
			}
			encrypted, err := channel.send.Encrypt(nil, nil, plain)
			if err != nil {
				t.Fatal(err)
			}
			if item.corrupt {
				encrypted[len(encrypted)-1] ^= 1
			}
			if err := channel.socket.Write(t.Context(), websocket.MessageBinary, encrypted); err != nil {
				t.Fatal(err)
			}
			if item.name == "截断" {
				channel.Close()
			}
			select {
			case err := <-received:
				if err == nil {
					t.Fatal("非法消息进入了调用者")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("损坏消息未在期限内终止")
			}
		})
	}
}
