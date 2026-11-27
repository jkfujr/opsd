package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) storageRoots(ctx context.Context) (protocol.StorageRootView, error) {
	out, err := n.executeRead(ctx, &protocol.StorageRootRead{})
	if err != nil {
		return protocol.StorageRootView{}, err
	}
	v, ok := out.(*protocol.StorageRootView)
	if !ok {
		return protocol.StorageRootView{}, fault("dependency_unavailable", "存储根读回无效")
	}
	v.NodeID = n.ID
	return *v, nil
}

func (n *Node) checkStorageRoot(ctx context.Context, r Request) error {
	id := r.ID
	if id == "" {
		id = n.ID + "_" + identity.Random()
	}
	_, err := n.storageRootWrite(ctx, id, r.Payload, true)
	return err
}

func (n *Node) storageRootWrite(ctx context.Context, id string, raw []byte, check bool) (*protocol.StorageRootResult, error) {
	in := protocol.StorageRootWrite{ID: id, CheckOnly: check}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in.Input); err != nil || in.Input.ExpectedRevision == 0 {
		return nil, fault("invalid_request", "存储根参数或修订无效")
	}
	if err := in.Input.Validate(); err != nil {
		return nil, fault("invalid_request", err.Error())
	}
	p, err := n.executeRead(ctx, &in)
	if err != nil {
		var f *firewall.Fault
		var c *Fault
		if errors.As(err, &f) || errors.As(err, &c) {
			return nil, err
		}
		return nil, fault("dependency_unavailable", "本机存储根入口不可用")
	}
	out, ok := p.(*protocol.StorageRootResult)
	if !ok {
		return nil, fault("dependency_unavailable", "存储根操作读回无效")
	}
	return out, nil
}

func (n *Node) applyStorageRoot(ctx context.Context, raw []byte, op *Operation) error {
	out, err := n.storageRootWrite(ctx, op.ID, raw, false)
	if err != nil {
		return err
	}
	op.State = protocol.Succeeded
	op.Revision++
	op.Steps = []protocol.OperationStep{{Name: "存储根", State: out.State}}
	return n.saveOperation(ctx, op, "finished")
}
