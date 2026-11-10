package cluster

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

type peerRequest struct {
	message protocol.Message
	call    *protocol.PeerCall
}

// 接收方只入队，处理中的业务不会阻塞双向应答分发。
type peerDispatch struct {
	peer              *peer
	ordinary          chan peerRequest
	control           chan peerRequest
	bytes             atomic.Int64
	controlBytes      atomic.Int64
	replyBytes        atomic.Int64
	controlReplyBytes atomic.Int64
	wg                sync.WaitGroup
}

func (n *Node) startPeerDispatch(ctx context.Context, p *peer) *peerDispatch {
	d := &peerDispatch{peer: p, ordinary: make(chan peerRequest, 24), control: make(chan peerRequest, 16)}
	for _, group := range []struct {
		queue   chan peerRequest
		workers int
	}{{d.ordinary, 6}, {d.control, 2}} {
		for range group.workers {
			d.wg.Add(1)
			go func(queue chan peerRequest) {
				defer d.wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case request := <-queue:
						budget := &d.bytes
						if protocol.ControlMethod(request.call.Method) {
							budget = &d.controlBytes
						}
						budget.Add(-int64(len(request.call.Body)))
						callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
						value, err := n.dispatch(callCtx, p.id, request.call.Method, request.call.Body)
						reply := &protocol.PeerReply{}
						if err != nil {
							reply.Error = wireError(err)
						} else {
							reply.Body, err = json.Marshal(value)
							if err != nil {
								reply.Error = wireError(err)
							}
						}
						size := int64(len(reply.Body))
						replyBudget, replyLimit := &d.replyBytes, int64(16*1024*1024)
						if protocol.ControlMethod(request.call.Method) {
							replyBudget = &d.controlReplyBytes
							replyLimit = 1024 * 1024
						}
						if replyBudget.Add(size) > replyLimit {
							replyBudget.Add(-size)
							size = 0
							reply.Body = nil
							reply.Error = &protocol.Error{Code: "node_busy", Message: "节点应答繁忙"}
						}
						if err = p.channel.Send(callCtx, protocol.Message{RequestID: identity.Random(), ReplyTo: request.message.RequestID, Payload: reply}); err != nil {
							p.channel.Close()
						}
						replyBudget.Add(-size)
						cancel()
						p.incoming.Add(-1)
					}
				}
			}(group.queue)
		}
	}
	return d
}

func (d *peerDispatch) enqueue(m protocol.Message, call *protocol.PeerCall) bool {
	size := int64(len(call.Body))
	queue, budget, limit := d.ordinary, &d.bytes, int64(16*1024*1024)
	if protocol.ControlMethod(call.Method) {
		queue = d.control
		budget = &d.controlBytes
		limit = 1024 * 1024
	}
	if budget.Add(size) > limit {
		budget.Add(-size)
		return false
	}
	d.peer.incoming.Add(1)
	select {
	case queue <- peerRequest{m, call}:
		return true
	default:
		d.peer.incoming.Add(-1)
		budget.Add(-size)
		return false
	}
}
