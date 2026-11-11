package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

type containerRead struct {
	done  chan struct{}
	at    time.Time
	value protocol.Payload
	err   error
}

func (n *Node) readContainer(ctx context.Context, payload protocol.Payload, ttl time.Duration) (protocol.Payload, error) {
	b, _ := json.Marshal(payload)
	key := string(payload.Kind()) + string(b)
	n.dockerMu.Lock()
	if n.dockerReads == nil {
		n.dockerReads = map[string]*containerRead{}
	}
	entry := n.dockerReads[key]
	if entry != nil && (entry.done != nil || n.Now().Sub(entry.at) < ttl) {
		done := entry.done
		n.dockerMu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return entry.value, entry.err
	}
	for k, old := range n.dockerReads {
		if old.done == nil && n.Now().Sub(old.at) >= 5*time.Second {
			delete(n.dockerReads, k)
		}
	}
	if len(n.dockerReads) >= 16 {
		n.dockerMu.Unlock()
		return nil, fault("node_busy", "Docker 读取繁忙，请稍后刷新")
	}
	entry = &containerRead{done: make(chan struct{}), at: n.Now()}
	n.dockerReads[key] = entry
	done := entry.done
	n.dockerMu.Unlock()
	go func() {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		value, err := n.executeRead(readCtx, payload)
		cancel()
		n.dockerMu.Lock()
		entry.value, entry.err = value, err
		entry.at = n.Now()
		entry.done = nil
		close(done)
		n.dockerMu.Unlock()
	}()
	select {
	case <-done:
		return entry.value, entry.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (n *Node) containerDispatch(ctx context.Context, method string, body json.RawMessage) (any, error) {
	var payload protocol.Payload
	ttl := time.Second
	switch method {
	case "containers.read":
		payload = &protocol.DockerRead{}
		ttl = 5 * time.Second
	case "container.inspect":
		payload = &protocol.ContainerInspect{}
	case "container.logs":
		payload = &protocol.ContainerLogsRead{}
		ttl = 2 * time.Second
	}
	if len(body) > 0 && string(body) != "null" {
		if err := json.Unmarshal(body, payload); err != nil {
			return nil, err
		}
	}
	result, err := n.readContainer(ctx, payload, ttl)
	if err != nil {
		return nil, err
	}
	switch v := result.(type) {
	case *protocol.DockerReadResult:
		if method == "containers.read" {
			return v.State, nil
		}
	case *protocol.ContainerInspectResult:
		if method == "container.inspect" {
			return v.Container, nil
		}
	case *protocol.ContainerLogsResult:
		if method == "container.logs" {
			return v.Logs, nil
		}
	}
	return nil, fmt.Errorf("Docker 应答类型无效")
}
func (n *Node) invalidateContainers() {
	n.dockerMu.Lock()
	n.dockerReads = nil
	n.dockerMu.Unlock()
	n.notifyChange(Change{Resource: "containers", NodeID: n.ID})
}
