package control

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/operations"
	"github.com/opsd-labs/opsd/internal/protocol"
	"github.com/opsd-labs/opsd/internal/transport"
)

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	socket, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer socket.CloseNow()
	channel, err := transport.Handshake(r.Context(), socket, s.key, nil, false)
	if err != nil {
		if errors.Is(err, transport.ErrAuthentication) {
			socket.Close(websocket.StatusPolicyViolation, "身份或协议不匹配")
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	first, err := channel.Receive(ctx)
	cancel()
	if err != nil {
		return
	}
	open, ok := first.Payload.(*protocol.SessionOpen)
	if !ok {
		return
	}
	if open.Purpose != "business" {
		s.channelError(r.Context(), channel, first.RequestID, "unsupported_purpose", "当前版本未实现此连接目的")
		return
	}
	s.lifecycle.Lock()
	nodeID, err := s.register(r.Context(), identity.Encode(channel.PeerPublic), open)
	if err != nil {
		s.lifecycle.Unlock()
		code := "internal_error"
		if errors.Is(err, ErrRevoked) {
			code = "identity_revoked"
		}
		if errors.Is(err, ErrToken) {
			code = "enrollment_rejected"
		}
		s.channelError(r.Context(), channel, first.RequestID, code, "节点登记被拒绝")
		return
	}
	known, err := s.pendingIDs(r.Context(), nodeID)
	if err != nil {
		s.lifecycle.Unlock()
		return
	}
	ready := len(open.PendingOperationIDs) == 0 && len(known) == 0
	conn := &connection{channel: channel, id: identity.Random(), writable: ready, ready: ready, calls: transport.NewCalls()}
	if !ready {
		conn.blocked = "正在核对未决操作"
	}
	s.mu.Lock()
	old := s.connections[nodeID]
	s.connections[nodeID] = conn
	s.mu.Unlock()
	if old != nil {
		old.calls.Close()
		old.channel.Close()
	}
	s.lifecycle.Unlock()
	defer func() {
		conn.calls.Close()
		s.mu.Lock()
		if s.connections[nodeID] == conn {
			delete(s.connections, nodeID)
		}
		s.mu.Unlock()
		s.notify("nodes")
	}()
	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
	err = channel.Send(ctx, protocol.Message{RequestID: identity.Random(), ReplyTo: first.RequestID, Payload: &protocol.SessionAccepted{NodeID: nodeID, ConnectionID: conn.id, PendingOperationIDs: known, Writable: ready}})
	cancel()
	s.notify("nodes")
	if err != nil {
		return
	}
	if !ready {
		s.spawn(func(ctx context.Context) { s.reconcileConnection(ctx, nodeID, conn, known, open.PendingOperationIDs) })
	}
	for {
		ctx, cancel = context.WithTimeout(r.Context(), 45*time.Second)
		message, err := channel.Receive(ctx)
		cancel()
		if err != nil {
			return
		}
		s.mu.Lock()
		active := s.connections[nodeID] == conn
		s.mu.Unlock()
		if !active {
			return
		}
		if conn.calls.Deliver(message) {
			continue
		}
		if message.ReplyTo != "" {
			continue
		}
		switch body := message.Payload.(type) {
		case *protocol.Heartbeat:
			// 生命周期锁覆盖当前连接核对与落盘，旧连接的迟到心跳不能覆盖新状态。
			s.lifecycle.Lock()
			s.mu.Lock()
			active = s.connections[nodeID] == conn
			s.mu.Unlock()
			if active {
				_, err = s.db.ExecContext(r.Context(), "UPDATE nodes SET last_seen_at=? WHERE id=? AND revoked_at IS NULL", s.now().Unix(), nodeID)
			}
			s.lifecycle.Unlock()
			if !active || err != nil {
				return
			}
			ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
			err = channel.Send(ctx, protocol.Message{RequestID: identity.Random(), ReplyTo: message.RequestID, Payload: &protocol.Heartbeat{SentAt: body.SentAt}})
			cancel()
			if err != nil {
				return
			}
		case *protocol.OperationUpdated:
			s.lifecycle.Lock()
			s.mu.Lock()
			active = s.connections[nodeID] == conn
			s.mu.Unlock()
			if active {
				err = s.saveRecord(r.Context(), nodeID, body.Record)
			}
			s.lifecycle.Unlock()
			if !active || err != nil {
				return
			}
			s.updateWritable(r.Context(), nodeID, conn)
			s.notify("operations")
			if operations.Terminal(body.Record.State) {
				s.notify("firewall")
			}
		default:
			s.channelError(r.Context(), channel, message.RequestID, "unsupported_message", "当前版本未实现此消息")
			return
		}
	}
}

func (s *Server) channelError(ctx context.Context, channel *transport.Channel, replyTo, code, message string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	channel.Send(ctx, protocol.Message{RequestID: identity.Random(), ReplyTo: replyTo, Payload: &protocol.RequestFailed{Error: protocol.Error{Code: code, Message: message}}})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	session := current(r)
	// 注册前后各自的撤销竞态由锁与数据库复核衔接，不能漏掉一次退出。
	sub := &subscriber{session: session.session.Id, events: make(chan string, 1), done: make(chan struct{})}
	s.mu.Lock()
	s.subscribers[sub] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subscribers, sub); s.mu.Unlock() }()
	if _, err := s.authenticate(r); err != nil {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	send := func(event string) bool {
		controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: invalidate\ndata: {\"resource\":\"%s\"}\n\n", event); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	if !send("nodes") {
		return
	}
	expiration := time.NewTimer(session.session.ExpiresAt.Sub(s.now()))
	defer expiration.Stop()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.done:
			return
		case <-expiration.C:
			return
		case event := <-sub.events:
			if !send(event) {
				return
			}
		case <-keepalive.C:
			controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprint(w, ": 心跳\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}
