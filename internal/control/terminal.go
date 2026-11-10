package control

import (
	"context"
	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/transport"
	"net/http"
	"time"
)

type terminalTicket struct {
	session, target string
	expires         time.Time
}

func (s *Server) CreateNodeTerminal(w http.ResponseWriter, r *http.Request, id string, _ httpapi.CreateNodeTerminalParams) {
	session := current(r).session.Id
	if session == "" || access(r.Context()) != nil {
		s.fail(w, r, 403, "forbidden", "终端需要管理员登录")
		return
	}
	if !s.node.Active(r.Context(), id) {
		s.fail(w, r, 404, "not_found", "节点不存在或已撤销")
		return
	}
	s.mu.Lock()
	if s.terminalTickets == nil {
		s.terminalTickets = map[string]terminalTicket{}
	}
	count := 0
	for key, t := range s.terminalTickets {
		if !t.expires.After(s.now()) {
			delete(s.terminalTickets, key)
		} else if t.session == session {
			count++
		}
	}
	if count >= 4 || len(s.terminalTickets) >= 32 {
		s.mu.Unlock()
		s.fail(w, r, 429, "node_busy", "终端连接过多，请稍后重试")
		return
	}
	key := identity.Random()
	expires := s.now().Add(30 * time.Second)
	s.terminalTickets[key] = terminalTicket{session: session, target: id, expires: expires}
	s.mu.Unlock()
	write(w, 201, httpapi.TerminalTicket{Url: "/api/v1/terminals/" + key, ExpiresAt: expires})
}

func (s *Server) AttachTerminal(w http.ResponseWriter, r *http.Request, ticket string) {
	if !sameOrigin(r) || current(r).session.Id == "" || access(r.Context()) != nil {
		s.fail(w, r, 403, "forbidden", "终端连接校验失败")
		return
	}
	s.mu.Lock()
	t, ok := s.terminalTickets[ticket]
	if ok && t.session == current(r).session.Id && t.expires.After(s.now()) {
		delete(s.terminalTickets, ticket)
	} else {
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		s.fail(w, r, 403, "forbidden", "终端连接已失效，请重新打开")
		return
	}
	socket, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	socket.SetReadLimit(65536)
	stream := transport.TerminalSocket{Socket: socket}
	defer stream.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	sub := &subscriber{session: t.session, events: make(chan string, 1), done: make(chan struct{})}
	s.mu.Lock()
	s.subscribers[sub] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subscribers, sub); s.mu.Unlock() }()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sub.done:
				cancel()
				return
			case <-ticker.C:
				if _, e := s.authenticate(r); e != nil || !s.node.Active(ctx, t.target) {
					cancel()
					return
				}
			}
		}
	}()
	local, err := s.node.Terminal(ctx, t.target)
	if err != nil {
		_ = stream.Send(ctx, protocol.TerminalFrame{Type: "exit", Message: err.Error()})
		return
	}
	transport.Relay(ctx, stream, local)
}
