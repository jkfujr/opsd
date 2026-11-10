package cluster

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/transport"
	"time"
)

type terminalConnector interface {
	Terminal(context.Context) (protocol.TerminalStream, error)
}

func (n *Node) Terminal(ctx context.Context, target string) (protocol.TerminalStream, error) {
	if !n.Active(ctx, target) {
		return nil, fault("forbidden", "节点未加入或已撤销")
	}
	if target == n.ID {
		connector, ok := n.Executor.(terminalConnector)
		if !ok {
			return nil, fault("unsupported", "终端入口不可用")
		}
		return connector.Terminal(ctx)
	}
	m, err := n.Member(ctx, target)
	if err != nil {
		return nil, err
	}
	key, err := identity.Decode(m.Public)
	if err != nil {
		return nil, err
	}
	openCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	socket, _, err := websocket.Dial(openCtx, m.Address+"/peer", nil)
	if err != nil {
		return nil, err
	}
	channel, err := transport.Handshake(openCtx, socket, n.Key, key, true)
	if err != nil {
		socket.CloseNow()
		return nil, err
	}
	fail := func(e error) (protocol.TerminalStream, error) { channel.Close(); return nil, e }
	self, err := n.Self(openCtx)
	if err != nil {
		return fail(err)
	}
	body, _ := json.Marshal(self)
	id := identity.Random()
	err = channel.Send(openCtx, protocol.Message{RequestID: id, Payload: &protocol.PeerHello{Member: body, Stream: "terminal"}})
	if err != nil {
		return fail(err)
	}
	reply, err := channel.Receive(openCtx)
	if err != nil {
		return fail(err)
	}
	hello, ok := reply.Payload.(*protocol.PeerHello)
	var remote Member
	if !ok || reply.ReplyTo != id || json.Unmarshal(hello.Member, &remote) != nil || remote.ID != target || remote.Public != m.Public || remote.Cluster != n.cluster || validateMember(remote) != nil {
		return fail(fault("forbidden", "节点身份不匹配"))
	}
	return transport.TerminalChannel{Channel: channel}, nil
}

func (n *Node) serveTerminal(ctx context.Context, origin string, stream protocol.TerminalStream) {
	defer stream.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(n.ctx, cancel)
	defer stop()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !n.Active(ctx, origin) || !n.Active(ctx, n.ID) {
					cancel()
					return
				}
			}
		}
	}()
	local, err := n.Terminal(ctx, n.ID)
	if err != nil {
		_ = stream.Send(ctx, protocol.TerminalFrame{Type: "exit", Message: err.Error()})
		return
	}
	transport.Relay(ctx, stream, local)
}
