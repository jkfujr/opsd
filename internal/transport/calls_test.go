package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/protocol"
)

func TestCallsDispatchReversedRepliesAndHeartbeat(t *testing.T) {
	serverKey, _ := identity.New()
	clientKey, _ := identity.New()
	serverDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer socket.CloseNow()
		ch, err := Handshake(r.Context(), socket, serverKey, nil, false)
		if err != nil {
			serverDone <- err
			return
		}
		messages := []protocol.Message{}
		for range 2 {
			m, e := ch.Receive(r.Context())
			if e != nil {
				serverDone <- e
				return
			}
			messages = append(messages, m)
		}
		if err = ch.Send(r.Context(), protocol.Message{RequestID: "heartbeat", Payload: &protocol.Heartbeat{SentAt: time.Now().UTC()}}); err != nil {
			serverDone <- err
			return
		}
		for i := len(messages) - 1; i >= 0; i-- {
			request := messages[i]
			query := request.Payload.(*protocol.OperationQuery)
			err = ch.Send(r.Context(), protocol.Message{RequestID: "reply-" + query.OperationID, ReplyTo: request.RequestID, Payload: &protocol.OperationQueryResult{OperationID: query.OperationID, Found: false}})
			if err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}))
	defer server.Close()
	socket, _, err := websocket.Dial(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.CloseNow()
	ch, err := Handshake(t.Context(), socket, clientKey, serverKey.Public, true)
	if err != nil {
		t.Fatal(err)
	}
	calls := NewCalls()
	defer calls.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	readDone := make(chan error, 1)
	heartbeats := make(chan struct{}, 1)
	go func() {
		for range 3 {
			m, e := ch.Receive(ctx)
			if e != nil {
				readDone <- e
				return
			}
			if !calls.Deliver(m) {
				if _, ok := m.Payload.(*protocol.Heartbeat); !ok {
					readDone <- context.Canceled
					return
				}
				heartbeats <- struct{}{}
			}
		}
		readDone <- nil
	}()
	results := make(chan error, 2)
	for _, id := range []string{"first", "second"} {
		go func() {
			m, e := calls.Call(ctx, ch, &protocol.OperationQuery{OperationID: id})
			if e == nil {
				if result, ok := m.Payload.(*protocol.OperationQueryResult); !ok || result.OperationID != id {
					e = context.Canceled
				}
			}
			results <- e
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("逆序应答发生串包：%v", err)
		}
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	if len(heartbeats) != 1 {
		t.Fatal("业务应答吞掉了心跳")
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if calls.Deliver(protocol.Message{ReplyTo: "已结束请求"}) {
		t.Fatal("迟到应答不能进入其他请求")
	}
}
