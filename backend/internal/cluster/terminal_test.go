package cluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type echoTerminal struct {
	metricExecutor
	frames chan protocol.TerminalFrame
	closed chan struct{}
	once   sync.Once
}

func (e *echoTerminal) Terminal(context.Context) (protocol.TerminalStream, error) { return e, nil }
func (e *echoTerminal) Send(ctx context.Context, f protocol.TerminalFrame) error {
	f.Type = "output"
	select {
	case e.frames <- f:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return context.Canceled
	}
}
func (e *echoTerminal) Receive(ctx context.Context) (protocol.TerminalFrame, error) {
	select {
	case f := <-e.frames:
		return f, nil
	case <-ctx.Done():
		return protocol.TerminalFrame{}, ctx.Err()
	case <-e.closed:
		return protocol.TerminalFrame{}, context.Canceled
	}
}
func (e *echoTerminal) Close() { e.once.Do(func() { close(e.closed) }) }
func TestTerminalUsesIndependentAuthenticatedStreamAndStopsWithNode(t *testing.T) {
	a, _ := openTestNode(t)
	b, _ := openTestNode(t)
	joinTest(t, b, a)
	x := &echoTerminal{frames: make(chan protocol.TerminalFrame, 1), closed: make(chan struct{})}
	b.Executor = x
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream, err := a.Terminal(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err = stream.Send(ctx, protocol.TerminalFrame{Type: "input", Data: []byte("独立数据")}); err != nil {
		t.Fatal(err)
	}
	f, err := stream.Receive(ctx)
	if err != nil || f.Type != "output" || string(f.Data) != "独立数据" {
		t.Fatal(f, err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-x.closed:
	case <-ctx.Done():
		t.Fatal("节点退出未释放终端")
	}
	if _, err = stream.Receive(ctx); err == nil {
		t.Fatal("节点退出后终端仍可读")
	}
}
