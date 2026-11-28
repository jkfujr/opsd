package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
)

type NetworkOptions struct {
	Listen, AddressMode, Address, AddressScheme, PeerTransport string
}

func networkConfigurationChangeError(before, next Config) error {
	endpointsChanged := next.Address != before.Address || !slices.Equal(next.Endpoints, before.Endpoints)
	if endpointsChanged && len(next.distinctPeerEndpoints()) > maxPeerEndpoints {
		return fault("invalid_request", "主地址和备用端点合计最多 8 个")
	}
	if endpointsChanged || next.AddressMode != before.AddressMode {
		return networkConfigurationError(next)
	}
	return nil
}

// networkConfigurationError 只拒绝能够确认无法接收公布地址的监听组合。
// 公网地址不在本机时仍允许 NAT 转发，不假定所有公网地址直接绑定接口。
func networkConfigurationError(c Config) error {
	host, port, err := net.SplitHostPort(c.Listen)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 {
		return fault("invalid_request", "节点通信监听地址无效")
	}
	if host == "" {
		return nil
	}
	bind, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	for _, endpoint := range c.PeerEndpoints() {
		if err := networkEndpointError(bind, endpoint); err != nil {
			return err
		}
	}
	return nil
}

func networkEndpointError(bind netip.Addr, endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fault("invalid_request", "节点通信地址无效")
	}
	advertised, err := netip.ParseAddr(u.Hostname())
	if err != nil || bind.Unmap() == advertised.Unmap() {
		return nil
	}
	if bind.IsUnspecified() {
		if bind.Is4() && advertised.Is6() {
			return fault("restart_required", "IPv6 通信地址需要对应监听，请通过 opsd network 修改后重启节点")
		}
		return nil
	}
	mismatch := bind.IsLoopback() || bind.Is6() != advertised.Is6()
	if netip.MustParsePrefix("100.64.0.0/10").Contains(bind) && !netip.MustParsePrefix("100.64.0.0/10").Contains(advertised) {
		mismatch = true
	}
	addresses, _ := net.InterfaceAddrs()
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Unmap() == advertised.Unmap() {
			mismatch = true
		}
	}
	if mismatch {
		return fault("restart_required", "通信地址与监听地址不匹配，请通过 opsd network 修改后重启节点")
	}
	return nil
}

// ConfigureNetwork 由持有 node.lock 的本机命令调用，节点服务须先停止。
func (n *Node) ConfigureNetwork(ctx context.Context, options NetworkOptions) error {
	c, err := n.Config(ctx)
	if err != nil {
		return err
	}
	var pending int
	if err = n.DB.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE terminal=0").Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return fault("node_busy", "节点存在未结束操作")
	}
	if mode := options.AddressMode; mode != "" && mode != "auto" && mode != "manual" {
		return fault("invalid_request", "地址模式须为 auto 或 manual")
	}
	if options.Address != "" && options.AddressScheme != "" {
		return fault("invalid_request", "通信 URL 与地址协议只能指定一项")
	}
	if options.Listen != "" {
		c.Listen = options.Listen
	}
	if options.PeerTransport != "" {
		c.PeerTransport = options.PeerTransport
	}
	bind, err := net.ResolveTCPAddr("tcp", c.Listen)
	if err != nil || bind.Port == 0 {
		return fault("invalid_request", "节点通信监听地址无效")
	}
	if options.AddressMode != "" {
		c.AddressMode = options.AddressMode
	}
	if options.Address != "" {
		if c.AddressMode == "auto" {
			return fault("invalid_request", "自动公网模式请省略手工地址")
		}
		c.Address = options.Address
		c.AddressMode = "manual"
	}
	if c.AddressMode == "auto" && (options.Listen != "" || options.AddressMode != "") {
		public, err := n.RefreshPublic(ctx)
		if err != nil {
			return err
		}
		ip := ""
		if len(public.IPv4.Addresses) > 0 {
			ip = public.IPv4.Addresses[0]
		} else if len(public.IPv6.Addresses) > 0 {
			ip = public.IPv6.Addresses[0]
		}
		if ip == "" {
			return fault("conflict", "公网地址尚未确认")
		}
		fresh, err := n.Config(ctx)
		if err != nil {
			return err
		}
		c.Revision = fresh.Revision
		u, err := url.Parse(c.Address)
		if err != nil {
			return err
		}
		u.Host = net.JoinHostPort(ip, fmt.Sprint(bind.Port))
		c.Address = u.String()
	}
	if scheme := options.AddressScheme; scheme != "" {
		switch scheme {
		case "http", "https", "tcp":
		default:
			return fault("invalid_request", "通信地址协议须为 http、https 或 tcp")
		}
		c.Address, err = peerAddressScheme(c.Address, scheme, bind.Port)
		if err != nil {
			return err
		}
		for i, endpoint := range c.Endpoints {
			c.Endpoints[i], err = peerAddressScheme(endpoint, scheme, bind.Port)
			if err != nil {
				return err
			}
		}
	}
	if err := ValidateConfig(c); err != nil {
		return err
	}
	if err := networkConfigurationError(c); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return fault("conflict", "节点通信监听地址不可用")
	}
	listener.Close()
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM queue WHERE terminal=0").Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return fault("node_busy", "节点存在未结束操作")
	}
	c.Revision++
	b, _ := json.Marshal(c)
	if _, err = tx.ExecContext(ctx, "UPDATE node_identity SET config=? WHERE id=1", string(b)); err != nil {
		return err
	}
	if err = n.publishMemberTx(ctx, tx, c); err != nil {
		return err
	}
	if err = n.appendTx(ctx, tx, "config", n.ID, c, n.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func peerAddressScheme(address, scheme string, listenPort int) (string, error) {
	u, err := url.Parse(address)
	if err != nil {
		return "", fault("invalid_request", "节点通信地址无效")
	}
	u.Scheme = scheme
	if scheme == "tcp" {
		u.Path, u.RawPath = "", ""
		if u.Port() == "" {
			u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(listenPort))
		}
	}
	return u.String(), nil
}
