package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/transport"
)

type peer struct {
	refs     int
	channel  *transport.Channel
	calls    *transport.Calls
	id       string
	resident bool
	done     chan struct{}
	last     time.Time
}

func (n *Node) PeerHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /peer", n.accept)
	return mux
}
func (n *Node) accept(w http.ResponseWriter, r *http.Request) {
	if n.Revoked(r.Context(), n.ID) {
		http.Error(w, "节点已退出集群", 403)
		return
	}
	socket, e := websocket.Accept(w, r, nil)
	if e != nil {
		return
	}
	defer socket.CloseNow()
	channel, e := transport.Handshake(r.Context(), socket, n.Key, nil, false)
	if e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	first, e := channel.Receive(ctx)
	cancel()
	if e != nil {
		return
	}
	hello, ok := first.Payload.(*protocol.PeerHello)
	if !ok {
		return
	}
	var m Member
	if json.Unmarshal(hello.Member, &m) != nil || validateMember(m) != nil || m.Public != identity.Encode(channel.PeerPublic) {
		return
	}
	joining := hello.Token != ""
	if hello.ProbeFamily != "" {
		if joining || !n.Active(r.Context(), m.ID) || m.Cluster != n.cluster {
			n.sendFailure(r.Context(), channel, first.RequestID, fault("forbidden", "成员身份无效"))
			return
		}
		n.probeEcho(r.Context(), w, r, channel, first, hello)
		return
	}
	if joining {
		e = n.acceptMember(r.Context(), m, hello.Token)
	} else {
		if !n.Active(r.Context(), m.ID) {
			e = fault("forbidden", "节点未加入或已撤销")
		} else if m.Cluster != n.cluster {
			e = fault("forbidden", "集群身份不匹配")
		}
	}
	var d Directory
	if e == nil && !joining && len(hello.Directory) > 0 {
		if e = json.Unmarshal(hello.Directory, &d); e == nil {
			e = n.merge(r.Context(), d, false)
		}
	}
	if e != nil {
		n.sendFailure(r.Context(), channel, first.RequestID, e)
		return
	}
	self, e := n.Self(r.Context())
	if e != nil {
		return
	}
	d, e = n.Directory(r.Context())
	if e != nil {
		return
	}
	memberBody, _ := json.Marshal(self)
	directoryBody, _ := json.Marshal(d)
	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
	e = channel.Send(ctx, protocol.Message{RequestID: identity.Random(), ReplyTo: first.RequestID, Payload: &protocol.PeerHello{Member: memberBody, Directory: directoryBody, Resident: hello.Resident}})
	cancel()
	if e != nil {
		return
	}
	if joining {
		return
	}
	p := &peer{channel: channel, calls: transport.NewCalls(), id: m.ID, resident: hello.Resident, done: make(chan struct{}), last: n.Now()}
	if !n.attach(p) {
		return
	}
	n.servePeer(n.ctx, p)
}
func (n *Node) attach(p *peer) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || n.ctx.Err() != nil {
		return false
	}
	if old := n.connections[p.id]; old != nil {
		return false
	}
	residents, total := 0, 0
	for _, c := range n.connections {
		total++
		if c.resident {
			residents++
		}
	}
	if total >= 32 || (p.resident && residents >= 8) {
		return false
	}
	n.connections[p.id] = p
	n.wg.Add(1)
	return true
}
func (n *Node) servePeer(ctx context.Context, p *peer) {
	defer n.wg.Done()
	defer func() {
		p.calls.Close()
		p.channel.Close()
		n.mu.Lock()
		if n.connections[p.id] == p {
			delete(n.connections, p.id)
		}
		close(p.done)
		n.mu.Unlock()
	}()
	slots := make(chan struct{}, 8)
	for {
		readCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		message, e := p.channel.Receive(readCtx)
		cancel()
		if e != nil {
			return
		}
		if !n.Active(ctx, p.id) {
			return
		}
		n.mu.Lock()
		p.last = n.Now()
		n.mu.Unlock()
		if p.calls.Deliver(message) {
			continue
		}
		if message.ReplyTo != "" {
			continue
		}
		body, ok := message.Payload.(*protocol.PeerCall)
		if !ok {
			return
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		go func(m protocol.Message, b *protocol.PeerCall) {
			defer func() { <-slots }()
			callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			value, e := n.dispatch(callCtx, p.id, b.Method, b.Body)
			reply := &protocol.PeerReply{}
			if e != nil {
				reply.Error = wireError(e)
			} else {
				reply.Body, _ = json.Marshal(value)
			}
			_ = p.channel.Send(callCtx, protocol.Message{RequestID: identity.Random(), ReplyTo: m.RequestID, Payload: reply})
		}(message, body)
	}
}
func wireError(e error) *protocol.Error {
	var f *Fault
	if errors.As(e, &f) {
		return &protocol.Error{Code: f.Code, Message: f.Message}
	}
	return &protocol.Error{Code: "dependency_unavailable", Message: "节点操作未完成，请查询原操作"}
}
func (n *Node) sendFailure(ctx context.Context, c *transport.Channel, id string, e error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = c.Send(ctx, protocol.Message{RequestID: identity.Random(), ReplyTo: id, Payload: &protocol.PeerReply{Error: wireError(e)}})
}
func (n *Node) dial(ctx context.Context, m Member, resident bool, token string) (*peer, error) {
	key, e := identity.Decode(m.Public)
	if e != nil {
		return nil, e
	}
	socket, _, e := websocket.Dial(ctx, m.Address+"/peer", nil)
	if e != nil {
		return nil, e
	}
	channel, e := transport.Handshake(ctx, socket, n.Key, key, true)
	if e != nil {
		socket.CloseNow()
		return nil, e
	}
	self, e := n.Self(ctx)
	if e != nil {
		channel.Close()
		return nil, e
	}
	selfBody, _ := json.Marshal(self)
	var dir json.RawMessage
	if token == "" {
		d, err := n.Directory(ctx)
		if err != nil {
			channel.Close()
			return nil, err
		}
		dir, _ = json.Marshal(d)
	}
	id := identity.Random()
	e = channel.Send(ctx, protocol.Message{RequestID: id, Payload: &protocol.PeerHello{Member: selfBody, Directory: dir, Resident: resident, Token: token}})
	if e != nil {
		channel.Close()
		return nil, e
	}
	reply, e := channel.Receive(ctx)
	if e != nil {
		channel.Close()
		return nil, e
	}
	if failed, ok := reply.Payload.(*protocol.PeerReply); ok && failed.Error != nil {
		channel.Close()
		return nil, &Fault{failed.Error.Code, failed.Error.Message}
	}
	hello, ok := reply.Payload.(*protocol.PeerHello)
	if !ok || reply.ReplyTo != id {
		channel.Close()
		return nil, fault("forbidden", "接入应答无效")
	}
	var remote Member
	var d Directory
	if json.Unmarshal(hello.Member, &remote) != nil || json.Unmarshal(hello.Directory, &d) != nil || remote.Public != m.Public {
		channel.Close()
		return nil, fault("forbidden", "节点身份不匹配")
	}
	if token != "" {
		e = n.joinDirectory(ctx, d)
		channel.Close()
		return nil, e
	}
	if e = n.merge(ctx, d, false); e != nil {
		channel.Close()
		return nil, e
	}
	p := &peer{channel: channel, calls: transport.NewCalls(), id: m.ID, resident: resident, done: make(chan struct{}), last: n.Now()}
	if !n.attach(p) {
		channel.Close()
		n.mu.Lock()
		old := n.connections[m.ID]
		n.mu.Unlock()
		if old != nil {
			return old, nil
		}
		return nil, fault("node_unavailable", "连接数量已达上限")
	}
	go n.servePeer(n.ctx, p)
	return p, nil
}
func (n *Node) Join(ctx context.Context, address, public, token string) error {
	if !validAddress(address) || token == "" {
		return fault("invalid_request", "接入地址或接入码无效")
	}
	_, e := n.dial(ctx, Member{Address: address, Public: public}, false, token)
	if e == nil {
		n.notify()
	}
	return e
}
func (n *Node) connection(ctx context.Context, id string, resident bool) (*peer, error) {
	if !n.Active(ctx, id) {
		return nil, fault("forbidden", "节点未加入或已撤销")
	}
	for {
		n.mu.Lock()
		p := n.connections[id]
		waiting := n.dialing[id]
		if p != nil {
			n.mu.Unlock()
			return p, nil
		}
		if waiting != nil {
			n.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-waiting:
				continue
			}
		}
		waiting = make(chan struct{})
		n.dialing[id] = waiting
		n.mu.Unlock()
		m, e := n.Member(ctx, id)
		if e == nil {
			p, e = n.dial(ctx, m, resident, "")
		}
		n.mu.Lock()
		delete(n.dialing, id)
		close(waiting)
		n.mu.Unlock()
		return p, e
	}
}
func (n *Node) Call(ctx context.Context, target, method string, body, out any) error {
	b, e := json.Marshal(body)
	if e != nil {
		return e
	}
	if target == n.ID {
		v, e := n.dispatch(ctx, n.ID, method, b)
		if e != nil {
			return e
		}
		if out != nil {
			b, e = json.Marshal(v)
			if e == nil {
				e = json.Unmarshal(b, out)
			}
		}
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p, e := n.connection(ctx, target, false)
	if e != nil {
		return fault("node_unavailable", "目标节点不可达")
	}
	n.mu.Lock()
	p.refs++
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		p.refs--
		closePeer := !p.resident && p.refs == 0
		if closePeer && n.connections[p.id] == p {
			delete(n.connections, p.id)
		}
		n.mu.Unlock()
		if closePeer {
			p.channel.Close()
		}
	}()
	message, e := p.calls.Call(ctx, p.channel, &protocol.PeerCall{Method: method, Body: b})
	if e != nil {
		return fault("node_unavailable", "未取得目标应答，请查询原操作")
	}
	reply, ok := message.Payload.(*protocol.PeerReply)
	if !ok {
		return fault("conflict", "节点应答无效")
	}
	if reply.Error != nil {
		return &Fault{reply.Error.Code, reply.Error.Message}
	}
	n.mu.Lock()
	if n.seen == nil {
		n.seen = map[string]time.Time{}
	}
	n.seen[target] = n.Now()
	n.mu.Unlock()
	if out != nil {
		return json.Unmarshal(reply.Body, out)
	}
	return nil
}
func (n *Node) Connected(id string) bool {
	if id == n.ID {
		return true
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.connections[id] != nil || (!n.seen[id].IsZero() && n.Now().Sub(n.seen[id]) < 15*time.Second)
}
func (n *Node) discovery() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		n.exchange()
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (n *Node) exchange() {
	d, e := n.Directory(n.ctx)
	if e != nil || n.Revoked(n.ctx, n.ID) {
		return
	}
	members := []Member{}
	for _, m := range d.Members {
		if !n.Revoked(n.ctx, m.ID) {
			members = append(members, m)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	index := -1
	for i, m := range members {
		if m.ID == n.ID {
			index = i
		}
	}
	if index < 0 {
		return
	}
	neighbors := map[string]Member{}
	for _, offset := range []int{-2, -1, 1, 2} {
		if len(members) > 1 {
			m := members[(index+offset+len(members)*2)%len(members)]
			if m.ID != n.ID {
				neighbors[m.ID] = m
			}
		}
	}
	for id := range neighbors {
		ctx, cancel := context.WithTimeout(n.ctx, 4*time.Second)
		p, err := n.connection(ctx, id, true)
		if err == nil {
			b, _ := json.Marshal(d)
			message, err := p.calls.Call(ctx, p.channel, &protocol.PeerCall{Method: "directory", Body: b})
			if err == nil {
				if reply, ok := message.Payload.(*protocol.PeerReply); ok && reply.Error == nil {
					var other Directory
					if json.Unmarshal(reply.Body, &other) == nil {
						_ = n.merge(ctx, other, false)
					}
				}
			}
		}
		cancel()
	}
	_ = n.Rewrap(n.ctx)
}
