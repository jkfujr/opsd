package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
)

// ConfigureNetwork 由持有 node.lock 的本机命令调用，节点服务须先停止。
func (n *Node) ConfigureNetwork(ctx context.Context, listen, mode, address string) error {
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
	if mode != "" && mode != "auto" && mode != "manual" {
		return fault("invalid_request", "地址模式须为 auto 或 manual")
	}
	if listen != "" {
		c.Listen = listen
	}
	bind, err := net.ResolveTCPAddr("tcp", c.Listen)
	if err != nil || bind.Port == 0 {
		return fault("invalid_request", "节点通信监听地址无效")
	}
	if mode != "" {
		c.AddressMode = mode
	}
	if address != "" {
		if c.AddressMode == "auto" {
			return fault("invalid_request", "自动公网模式请省略手工地址")
		}
		c.Address = address
		c.AddressMode = "manual"
	}
	if c.AddressMode == "auto" {
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
	if err := ValidateConfig(c); err != nil {
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
