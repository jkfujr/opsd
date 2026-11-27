package cluster

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/transport"
)

const maxPeerEndpoints = 8

func (c Config) distinctPeerEndpoints() []string {
	endpoints := []string{c.Address}
	seen := map[string]bool{strings.TrimRight(c.Address, "/"): true}
	for _, endpoint := range c.Endpoints {
		key := strings.TrimRight(endpoint, "/")
		if !seen[key] {
			seen[key] = true
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

func (c Config) PeerEndpoints() []string {
	endpoints := c.distinctPeerEndpoints()
	// 旧配置可能含八个备用端点，按原优先顺序公布协议可接收的八个端点。
	return endpoints[:min(len(endpoints), maxPeerEndpoints)]
}

func (n *Node) openPeerChannel(ctx context.Context, m Member, key []byte) (*transport.Channel, error) {
	endpoints := append([]string{}, m.Endpoints...)
	if len(endpoints) == 0 {
		endpoints = append(endpoints, m.Address)
	}
	seen := map[string]bool{}
	var failures []error
	for _, endpoint := range endpoints {
		if seen[endpoint] {
			continue
		}
		seen[endpoint] = true
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		socket, _, err := websocket.Dial(attempt, strings.TrimRight(endpoint, "/")+"/peer", nil)
		if err == nil {
			var channel *transport.Channel
			channel, err = transport.Handshake(attempt, socket, n.Key, key, true)
			if err == nil {
				cancel()
				return channel, nil
			}
			socket.CloseNow()
		}
		cancel()
		failures = append(failures, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.Join(failures...)
}
