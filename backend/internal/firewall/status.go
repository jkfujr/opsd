package firewall

import (
	"context"
	"strings"
)

// Status 只查询管理器启用状态，概览不必反复读取全部规则。
func (m *Manager) Status(ctx context.Context) (string, error) {
	backend, err := m.Detect(ctx)
	if err != nil {
		return "unknown", err
	}
	if backend == "none" {
		return "none", nil
	}
	if backend == "ufw" {
		data, e := m.Host.Run(ctx, "ufw", "status")
		if e != nil {
			return "unknown", e
		}
		for _, line := range strings.Split(string(data), "\n") {
			switch strings.TrimSpace(line) {
			case "Status: active":
				return "enabled", nil
			case "Status: inactive":
				return "disabled", nil
			}
		}
		return "unknown", Unsupported("防火墙状态读取失败")
	}
	var data []byte
	if backend == "firewalld" {
		data, err = m.Host.Run(ctx, "firewall-cmd", "--state")
	} else {
		data, err = m.Host.Run(ctx, "systemctl", "is-active", "nftables.service")
	}
	text := strings.TrimSpace(string(data))
	if text == "running" || text == "active" {
		return "enabled", nil
	}
	if text == "not running" || text == "inactive" || text == "failed" {
		return "disabled", nil
	}
	if err != nil {
		return "unknown", err
	}
	return "unknown", Unsupported("防火墙状态读取失败")
}

// 只识别已知 UFW 默认表达式，不按只读属性或链名前缀隐藏自定义条目。
func ufwSystemDefault(line, prefix string) bool {
	line = strings.Join(strings.Fields(line), " ")
	if strings.HasPrefix(line, "-A INPUT -j ") {
		target := strings.TrimPrefix(line, "-A INPUT -j ")
		for _, suffix := range []string{"before-logging-input", "before-input", "after-input", "after-logging-input", "reject-input", "track-input", "user-input"} {
			if target == prefix+"-"+suffix {
				return true
			}
		}
	}
	chain, tail, ok := strings.Cut(strings.TrimPrefix(line, "-A "), " ")
	if !ok {
		return false
	}
	if chain == prefix+"-after-input" {
		for _, value := range []string{"-p udp -m udp --dport 137", "-p udp -m udp --dport 138", "-p tcp -m tcp --dport 139", "-p tcp -m tcp --dport 445", "-p udp -m udp --dport 67", "-p udp -m udp --dport 68", "-m addrtype --dst-type BROADCAST"} {
			if tail == value+" -j "+prefix+"-skip-to-policy-input" {
				return true
			}
		}
	}
	if chain != prefix+"-before-input" {
		return false
	}
	for _, value := range []string{
		"-i lo -j ACCEPT",
		"-m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT",
		"-m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
		"-m conntrack --ctstate INVALID -j DROP",
		"-m conntrack --ctstate INVALID -j " + prefix + "-logging-deny",
		"-j " + prefix + "-not-local",
		"-p udp -m udp --sport 67 --dport 68 -j ACCEPT",
		"-d 224.0.0.251/32 -p udp -m udp --dport 5353 -j ACCEPT",
		"-d 239.255.255.250/32 -p udp -m udp --dport 1900 -j ACCEPT",
	} {
		if tail == value {
			return true
		}
	}
	for _, kind := range []string{"3", "11", "12", "8", "destination-unreachable", "time-exceeded", "parameter-problem", "echo-request"} {
		if prefix == "ufw" && tail == "-p icmp -m icmp --icmp-type "+kind+" -j ACCEPT" {
			return true
		}
	}
	if prefix == "ufw6" {
		for _, kind := range []string{"1", "2", "3", "4", "128", "129", "133", "134", "135", "136", "137"} {
			if tail == "-p ipv6-icmp -m icmp6 --icmpv6-type "+kind+" -j ACCEPT" {
				return true
			}
		}
	}
	return false
}
