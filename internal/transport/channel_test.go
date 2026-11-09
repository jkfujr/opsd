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
		{"篡改", frame(1, 2, 0, []byte("{}")), true},
		{"截断", frame(1, 20, 0, []byte("{}")), false},
		{"逻辑超限", frame(1, protocol.MaxMessageBytes+1, 0, []byte("a")), false},
		{"记录超限", make([]byte, RecordBytes+1), false},
		{"长度不匹配", frame(1, 1, 0, []byte("{}")), false},
		{"分片次序", frame(1, 20, 4, []byte("{}")), false},
		{"版本不兼容", nil, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			channel, received := pair(t)
			plain := item.data
			if item.name == "版本不兼容" {
				body := []byte(`{"version":99,"kind":"heartbeat","request_id":"x","body":{"sent_at":"2026-10-27T00:00:00Z"}}`)
				plain = frame(1, len(body), 0, body)
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

func frame(id uint64, total, offset int, body []byte) []byte {
	out := make([]byte, 16+len(body))
	binary.BigEndian.PutUint64(out, id)
	binary.BigEndian.PutUint32(out[8:], uint32(total))
	binary.BigEndian.PutUint32(out[12:], uint32(offset))
	copy(out[16:], body)
	return out
}

func TestSmallReplyPassesIncompleteLargeMessage(t *testing.T) {
	c, received := pair(t)
	large, e := protocol.Encode(protocol.Message{RequestID: "large", Payload: &protocol.RequestFailed{Error: protocol.Error{Code: "test", Message: strings.Repeat("x", RecordBytes*4)}}})
	if e != nil {
		t.Fatal(e)
	}
	small, e := protocol.Encode(protocol.Message{RequestID: "control", ReplyTo: "request", Payload: &protocol.RequestFailed{Error: protocol.Error{Code: "test", Message: "确认应答"}}})
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range [][]byte{frame(1, len(large), 0, large[:RecordBytes-16]), frame(2, len(small), 0, small)} {
		record, e := c.send.Encrypt(nil, nil, part)
		if e != nil {
			t.Fatal(e)
		}
		if e = c.socket.Write(t.Context(), websocket.MessageBinary, record); e != nil {
			t.Fatal(e)
		}
	}
	reply, e := c.Receive(t.Context())
	if e != nil || reply.RequestID != "control" {
		t.Fatal("小应答被大消息阻塞", reply.RequestID, e)
	}
	if e = <-received; e != nil {
		t.Fatal(e)
	}
}
