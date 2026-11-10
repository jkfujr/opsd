package transport

import (
	"context"
	"fmt"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"time"
)

type TerminalChannel struct{ Channel *Channel }

func (t TerminalChannel) Send(ctx context.Context, f protocol.TerminalFrame) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return t.Channel.Send(ctx, protocol.Message{RequestID: identity.Random(), Payload: &f})
}
func (t TerminalChannel) Receive(ctx context.Context) (protocol.TerminalFrame, error) {
	m, e := t.Channel.Receive(ctx)
	if e != nil {
		return protocol.TerminalFrame{}, e
	}
	f, ok := m.Payload.(*protocol.TerminalFrame)
	if !ok {
		return protocol.TerminalFrame{}, fmt.Errorf("终端载荷无效")
	}
	return *f, nil
}
func (t TerminalChannel) Close() { t.Channel.Close() }

type TerminalSocket struct{ Socket *websocket.Conn }

func (t TerminalSocket) Send(ctx context.Context, f protocol.TerminalFrame) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if e := f.Validate(); e != nil {
		return e
	}
	return wsjson.Write(ctx, t.Socket, f)
}
func (t TerminalSocket) Receive(ctx context.Context) (protocol.TerminalFrame, error) {
	var f protocol.TerminalFrame
	if e := wsjson.Read(ctx, t.Socket, &f); e != nil {
		return f, e
	}
	return f, f.Validate()
}
func (t TerminalSocket) Close() { t.Socket.CloseNow() }

// Relay 在任一方向结束时释放两侧，写入期限限制慢接收端。
func Relay(ctx context.Context, a, b protocol.TerminalStream) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer a.Close()
	defer b.Close()
	copy := func(from, to protocol.TerminalStream) {
		defer cancel()
		for {
			f, e := from.Receive(ctx)
			if e != nil {
				return
			}
			if e = to.Send(ctx, f); e != nil {
				return
			}
			if f.Type == "exit" {
				return
			}
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); copy(a, b) }()
	copy(b, a)
	cancel()
	a.Close()
	b.Close()
	<-done
}
