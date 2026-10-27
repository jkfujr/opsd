package transport

import (
	"context"
	"fmt"
	"sync"

	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/protocol"
)

// Calls 由连接唯一的读取循环分发应答，调用者不能另行读取密码状态。
type Calls struct {
	mu      sync.Mutex
	pending map[string]chan protocol.Message
	done    chan struct{}
}

func NewCalls() *Calls {
	return &Calls{pending: map[string]chan protocol.Message{}, done: make(chan struct{})}
}
func (c *Calls) Call(ctx context.Context, channel *Channel, payload protocol.Payload) (protocol.Message, error) {
	id := identity.Random()
	reply := make(chan protocol.Message, 1)
	c.mu.Lock()
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := channel.Send(ctx, protocol.Message{RequestID: id, Payload: payload}); err != nil {
		return protocol.Message{}, err
	}
	select {
	case message := <-reply:
		return message, nil
	case <-ctx.Done():
		return protocol.Message{}, ctx.Err()
	case <-c.done:
		return protocol.Message{}, fmt.Errorf("节点连接已断开")
	}
}
func (c *Calls) Deliver(message protocol.Message) bool {
	if message.ReplyTo == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	reply := c.pending[message.ReplyTo]
	if reply == nil {
		return false
	}
	select {
	case reply <- message:
	default:
	}
	return true
}
func (c *Calls) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
	default:
		close(c.done)
	}
}
