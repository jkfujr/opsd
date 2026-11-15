package databaseengine

import (
	"context"
	"fmt"
	"github.com/jkfujr/opsd/internal/protocol"
	"net"
	"strconv"
	"strings"
	"time"
)

func (m Manager) ProxySQLAddress(ctx context.Context, c protocol.DatabaseConnection) (string, error) {
	s, err := connect(ctx, c)
	if err != nil {
		return "", err
	}
	defer s.Close()
	var monitor string
	if err = s.QueryRowContext(ctx, "SELECT variable_value FROM runtime_global_variables WHERE variable_name='mysql-monitor_enabled'").Scan(&monitor); err != nil {
		return "", clean(err)
	}
	if monitor != "true" && monitor != "1" {
		return "", fmt.Errorf("已有代理尚未启用 Galera 健康检查")
	}
	var interfaces string
	if err = s.QueryRowContext(ctx, "SELECT variable_value FROM runtime_global_variables WHERE variable_name='mysql-interfaces'").Scan(&interfaces); err != nil {
		return "", clean(err)
	}
	for _, address := range strings.Split(interfaces, ";") {
		host, port, err := net.SplitHostPort(strings.TrimSpace(address))
		ip := net.ParseIP(host)
		number, e := strconv.Atoi(port)
		if err == nil && e == nil && number > 0 && number <= 65535 && ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
			return net.JoinHostPort("127.0.0.1", port), nil
		}
	}
	return "", fmt.Errorf("已有代理没有本机 SQL 监听地址")
}

func prepareSharedProxy(ctx context.Context, a protocol.DatabaseAction) ([]string, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	s, err := connect(ctx, a.Connection)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	layers, err := proxyLayers(ctx, s)
	if err != nil {
		return nil, err
	}
	if proxyRuntimeFingerprint(layers[0]) != proxyRuntimeFingerprint(layers[1]) || fingerprint(layers[0]) != fingerprint(layers[2]) {
		return nil, fmt.Errorf("代理存在其他待应用的账号配置")
	}
	return []string{"memory", "runtime", "disk"}, nil
}
func applySharedProxy(ctx context.Context, a protocol.DatabaseAction, stage string) error {
	if err := a.Validate(); err != nil {
		return err
	}
	s, err := connect(ctx, a.Connection)
	if err != nil {
		return err
	}
	defer s.Close()
	switch stage {
	case "memory":
		user := protocol.SharedUsername(a.Shared.NodeID)
		if a.Shared.Mode == "revoke" {
			_, err = s.ExecContext(ctx, "DELETE FROM mysql_users WHERE username=?", user)
		} else {
			existing, e := rows(ctx, s, "SELECT username FROM mysql_users WHERE username=?", user)
			if e != nil {
				return e
			}
			if len(existing) > 0 {
				_, err = s.ExecContext(ctx, "UPDATE mysql_users SET password=?,active=1,default_hostgroup=?,transaction_persistent=1,max_connections=2 WHERE username=?", a.Shared.PasswordHash, a.Hostgroup, user)
			} else {
				_, err = s.ExecContext(ctx, "INSERT INTO mysql_users(username,password,active,default_hostgroup,transaction_persistent,max_connections) VALUES(?,?,1,?,1,2)", user, a.Shared.PasswordHash, a.Hostgroup)
			}
		}
	case "runtime":
		_, err = s.ExecContext(ctx, "LOAD MYSQL USERS TO RUNTIME")
		if err == nil && a.Shared.Mode == "revoke" {
			var sessions []map[string]string
			sessions, err = rows(ctx, s, "SELECT SessionID FROM stats_mysql_processlist WHERE user=?", protocol.SharedUsername(a.Shared.NodeID))
			for _, session := range sessions {
				if err != nil {
					break
				}
				id, e := strconv.ParseUint(session["sessionid"], 10, 64)
				if e != nil {
					return fmt.Errorf("代理连接标识无效")
				}
				_, err = s.ExecContext(ctx, "KILL CONNECTION "+strconv.FormatUint(id, 10))
			}
			if err == nil {
				waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				for {
					var remaining int
					if err = s.QueryRowContext(waitCtx, "SELECT COUNT(*) FROM stats_mysql_processlist WHERE user=?", protocol.SharedUsername(a.Shared.NodeID)).Scan(&remaining); err != nil || remaining == 0 {
						break
					}
					select {
					case <-waitCtx.Done():
						return waitCtx.Err()
					case <-time.After(100 * time.Millisecond):
					}
				}
			}
		}
	case "disk":
		_, err = s.ExecContext(ctx, "SAVE MYSQL USERS TO DISK")
	default:
		return fmt.Errorf("代理账号维护阶段无效")
	}
	return clean(err)
}
