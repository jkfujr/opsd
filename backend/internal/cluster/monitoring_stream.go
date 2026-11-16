package cluster

import (
	"context"
	"encoding/json"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/telemetry"
	"github.com/jkfujr/opsd/internal/transport"
)

type ArchiveStatus = telemetry.Status
type archiveUpload = telemetry.Upload
type archiveAck = metricsstore.Receipt

func (n *Node) metricsBuffer(ctx context.Context) (*metricsstore.Buffer, error) {
	return n.telemetry.Buffer(ctx)
}
func (n *Node) ArchiveStatus(ctx context.Context) (ArchiveStatus, error) {
	return n.telemetry.Status(ctx)
}
func (n *Node) archiveDispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	return n.telemetry.Dispatch(ctx, origin, method, body)
}
func archiveCall(ctx context.Context, channel *transport.Channel, method string, input, out any) error {
	return telemetry.CallStream(ctx, channel, method, input, out, fault)
}
func (n *Node) serveArchive(ctx context.Context, origin string, channel *transport.Channel) {
	n.mu.Lock()
	closed := n.closed
	n.mu.Unlock()
	if closed {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(n.ctx, cancel)
	defer stop()
	n.telemetry.Serve(ctx, origin, channel)
}
func (n *Node) openArchiveChannel(ctx context.Context, target string) (*transport.Channel, error) {
	m, err := n.Member(ctx, target)
	if err != nil {
		return nil, err
	}
	key, err := identity.Decode(m.Public)
	if err != nil {
		return nil, err
	}
	channel, err := n.openPeerChannel(ctx, m, key)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*transport.Channel, error) { channel.Close(); return nil, e }
	self, err := n.Self(ctx)
	if err != nil {
		return fail(err)
	}
	body, _ := json.Marshal(self)
	id := identity.Random()
	if err = channel.Send(ctx, protocol.Message{RequestID: id, Payload: &protocol.PeerHello{Member: body, Stream: "monitoring"}}); err != nil {
		return fail(err)
	}
	reply, err := channel.Receive(ctx)
	if err != nil {
		return fail(err)
	}
	hello, ok := reply.Payload.(*protocol.PeerHello)
	var remote Member
	if !ok || reply.ReplyTo != id || json.Unmarshal(hello.Member, &remote) != nil || remote.ID != target || remote.Public != m.Public || remote.Cluster != n.cluster || validateMember(remote) != nil {
		return fail(fault("forbidden", "监控节点身份不匹配"))
	}
	return channel, nil
}
