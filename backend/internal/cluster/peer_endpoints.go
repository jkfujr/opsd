package cluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/publicaddress"
	"github.com/jkfujr/opsd/internal/transport"
)

const maxPeerEndpoints = 8

func ValidPeerAddress(address string) bool {
	if validAddress(address) {
		return true
	}
	u, err := url.Parse(address)
	if err != nil || !strings.HasPrefix(address, "tcp://") || u.User != nil || u.Path != "" ||
		u.RawQuery != "" || u.ForceQuery || strings.Contains(address, "#") {
		return false
	}
	host, port, err := net.SplitHostPort(u.Host)
	number, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || number < 1 || number > 65535 {
		return false
	}
	ip, ipErr := netip.ParseAddr(host)
	if strings.HasPrefix(u.Host, "[") {
		return ipErr == nil && ip.Is6() && ip.Zone() == "" && !ip.IsUnspecified() && !ip.IsMulticast()
	}
	if ipErr == nil {
		return ip.Is4() && !ip.IsUnspecified() && !ip.IsMulticast()
	}
	numeric := strings.IndexFunc(host, func(c rune) bool { return c != '.' && (c < '0' || c > '9') }) == -1
	return !strings.Contains(host, ":") && !(numeric && strings.Contains(host, "."))
}

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

func (m Member) peerEndpoints() []string {
	if len(m.Endpoints) == 0 {
		return []string{m.Address}
	}
	return m.Endpoints
}

func (n *Node) openPeerChannel(ctx context.Context, m Member, key []byte) (*transport.Channel, error) {
	seen := map[string]bool{}
	var failures []error
	for _, endpoint := range m.peerEndpoints() {
		if seen[endpoint] {
			continue
		}
		seen[endpoint] = true
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		channel, err := n.dialPeerEndpoint(attempt, endpoint, key, "")
		cancel()
		if err == nil {
			return channel, nil
		}
		failures = append(failures, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.Join(failures...)
}

func (n *Node) dialPeerEndpoint(ctx context.Context, endpoint string, key []byte, family protocol.AddressFamily) (*transport.Channel, error) {
	if !ValidPeerAddress(endpoint) {
		return nil, fmt.Errorf("节点通信端点无效")
	}
	u, _ := url.Parse(endpoint)
	if u.Scheme == "tcp" {
		network := "tcp"
		switch family {
		case protocol.IPv4:
			network = "tcp4"
		case protocol.IPv6:
			network = "tcp6"
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, network, u.Host)
		if err != nil {
			return nil, err
		}
		return transport.HandshakeTCP(ctx, conn, n.Key, key, true)
	}
	var options websocket.DialOptions
	if family != "" {
		options.HTTPClient = publicaddress.Client(string(family))
		defer options.HTTPClient.CloseIdleConnections()
	}
	socket, _, err := websocket.Dial(ctx, strings.TrimRight(endpoint, "/")+"/peer", &options)
	if err != nil {
		return nil, err
	}
	channel, err := transport.Handshake(ctx, socket, n.Key, key, true)
	if err != nil {
		socket.CloseNow()
	}
	return channel, err
}
