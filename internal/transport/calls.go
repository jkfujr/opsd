package transport

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

// Calls 由连接唯一的读取循环分发应答，调用者不能另行读取密码状态。
type Calls struct {
	mu           sync.Mutex
	pending      map[string]chan protocol.Message
	done         chan struct{}
	ordinary     chan struct{}
	control      chan struct{}
	bytes        atomic.Int64
	controlBytes atomic.Int64
}

func NewCalls() *Calls {
	return &Calls{pending: map[string]chan protocol.Message{}, done: make(chan struct{}), ordinary: make(chan struct{}, 24), control: make(chan struct{}, 8)}
}
func (c *Calls) Call(ctx context.Context, channel *Channel, payload protocol.Payload) (protocol.Message, error) {
	slots, size := c.ordinary, int64(0)
	budget, limit := &c.bytes, int64(16*1024*1024)
	if call, ok := payload.(*protocol.PeerCall); ok {
		size = int64(len(call.Body))
		if protocol.ControlMethod(call.Method) {
			slots = c.control
			budget, limit = &c.controlBytes, 1024*1024
		}
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return protocol.Message{}, ctx.Err()
	case <-c.done:
		return protocol.Message{}, fmt.Errorf("节点连接已断开")
	}
	defer func() { <-slots }()
	if budget.Add(size) > limit {
		budget.Add(-size)
		return protocol.Message{}, fmt.Errorf("节点请求数据已达上限")
	}
	defer budget.Add(-size)
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
