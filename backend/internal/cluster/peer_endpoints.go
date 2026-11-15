package cluster

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/transport"
	"strings"
	"time"
)

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
