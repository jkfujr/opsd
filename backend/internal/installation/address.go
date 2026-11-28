package installation

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/publicaddress"
)

var discoverAddress = publicaddress.Discover

// NodeAddress 在新安装写入任何状态前确定对外地址，不发送接入码。
func NodeAddress(ctx context.Context, config cluster.Config, peer string) (string, error) {
	if config.Address != "" {
		return config.Address, nil
	}
	listen := config.Listen
	if listen == "" {
		listen = ":8444"
	}
	bind, err := net.ResolveTCPAddr("tcp", listen)
	if err != nil || bind.Port == 0 {
		return "", fmt.Errorf("节点监听地址无效")
	}
	scheme := "http"
	if peer != "" {
		if !cluster.ValidPeerAddress(peer) {
			return "", fmt.Errorf("接入地址无效")
		}
		u, _ := url.Parse(peer)
		if u.Scheme == "tcp" {
			scheme = u.Scheme
		}
	}
	address, err := discoverAddress(ctx)
	if err != nil {
		return "", err
	}
	return scheme + "://" + net.JoinHostPort(address, fmt.Sprint(bind.Port)), nil
}
