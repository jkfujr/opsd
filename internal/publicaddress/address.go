// Package publicaddress 负责分地址族的公网观测，不携带节点凭据。
package publicaddress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"
)

var excluded = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func Parse(text string) (netip.Addr, error) {
	a, err := netip.ParseAddr(strings.TrimSpace(text))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("请输入精确公网 IP")
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("地址须为公网 IP")
	}
	for _, p := range excluded {
		if p.Contains(a) {
			return netip.Addr{}, fmt.Errorf("地址须为公网 IP")
		}
	}
	if a.Is6() && !netip.MustParsePrefix("2000::/3").Contains(a) {
		return netip.Addr{}, fmt.Errorf("地址须为公网 IPv6")
	}
	return a, nil
}

func Client(family string) *http.Client {
	network := "tcp4"
	if family == "ipv6" {
		network = "tcp6"
	}
	d := &net.Dialer{Timeout: 4 * time.Second}
	return &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		return d.DialContext(ctx, network, address)
	}}, Timeout: 6 * time.Second}
}

func Echo(ctx context.Context, family string) (string, error) {
	endpoint := "https://api.ipify.org"
	if family == "ipv6" {
		endpoint = "https://api6.ipify.org"
	}
	request, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	client := Client(family)
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("公共回显不可达")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("公共回显返回异常")
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, 128))
	if err != nil {
		return "", fmt.Errorf("公共回显读取失败")
	}
	a, err := Parse(string(b))
	if err != nil || a.Is6() != (family == "ipv6") {
		return "", fmt.Errorf("公共回显地址无效")
	}
	return a.String(), nil
}

func Discover(ctx context.Context) (string, error) {
	// IPv4 优先；IPv6 单栈仍可安装。
	for _, family := range []string{"ipv4", "ipv6"} {
		if address, err := Echo(ctx, family); err == nil {
			return address, nil
		}
	}
	return "", fmt.Errorf("公网地址发现失败，请使用 --address 指定可达地址")
}

func Fingerprint() string {
	addresses, _ := net.InterfaceAddrs()
	values := make([]string, 0, len(addresses))
	for _, a := range addresses {
		values = append(values, a.String())
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
