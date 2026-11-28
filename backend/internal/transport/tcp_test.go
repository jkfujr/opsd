package transport

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestTCPNoiseRecords(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	key, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	response := make(chan error, 1)
	go func() {
		server, err := HandshakeTCP(ctx, b, key, nil, false)
		if err == nil {
			defer server.Close()
			var message protocol.Message
			message, err = server.Receive(ctx)
			if err == nil {
				err = server.Send(ctx, message)
			}
		}
		response <- err
	}()
	client, err := HandshakeTCP(ctx, a, clientKey, key.Public, true)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	body := strings.Repeat("记录", RecordBytes)
	if err = client.Send(ctx, protocol.Message{RequestID: "large", Payload: &protocol.RequestFailed{Error: protocol.Error{Code: "test", Message: body}}}); err != nil {
		t.Fatal(err)
	}
	reply, err := client.Receive(ctx)
	result, ok := reply.Payload.(*protocol.RequestFailed)
	if err != nil || !ok || reply.RequestID != "large" || result.Error.Message != body {
		t.Fatal("跨记录消息或请求 ID 不符", err)
	}
	if err := <-response; err != nil {
		t.Fatal(err)
	}
}

func TestTCPRejectsInvalidPrefaceAndLength(t *testing.T) {
	for name, input := range map[string]string{
		"版本": "\x00OPSD\x7f", "空记录": TCPPreface + "\x00\x00", "超长": TCPPreface + "\xff\xff",
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			serverKey, err := identity.New()
			if err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			result := make(chan error, 1)
			go func() {
				_, err := HandshakeTCP(ctx, b, serverKey, nil, false)
				result <- err
			}()
			if _, err = io.WriteString(a, input); err != nil {
				t.Fatal(err)
			}
			if err = <-result; err == nil || ctx.Err() != nil {
				t.Fatal("异常记录未被立即拒绝", err)
			}
			if _, err := a.Read(make([]byte, 1)); err != io.EOF {
				t.Fatal("握手拒绝后连接未关闭", err)
			}
		})
	}
}
