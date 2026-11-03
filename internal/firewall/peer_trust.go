package firewall

import (
	"context"
	"net/netip"
	"slices"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

const trustMarker = "opsd-peer-trust"
const trustChain = "OPSD_PEERS"

func trustResult(family protocol.AddressFamily, scope protocol.ConfigScope, addresses []string, e error) protocol.ScopeResult {
	r := protocol.ScopeResult{Family: family, Component: "host", Scope: scope, State: "in_sync", Addresses: addresses}
	if r.Addresses == nil {
		r.Addresses = []string{}
	}
	if e != nil {
		r.State = "failed"
		r.Error = &protocol.Error{Code: Code(e), Message: e.Error()}
	}
	return r
}
func failedTrust(e error) []protocol.ScopeResult {
	out := []protocol.ScopeResult{}
	for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		for _, scope := range []protocol.ConfigScope{protocol.Permanent, protocol.Runtime} {
			out = append(out, trustResult(family, scope, nil, e))
		}
	}
	return out
}
func (m *Manager) SyncPeerTrust(ctx context.Context, target protocol.PeerTrustTarget) []protocol.ScopeResult {
	backend, e := m.Detect(ctx)
	if e != nil {
		return failedTrust(e)
	}
	switch backend {
	case "ufw":
		return syncUFWTrust(ctx, m.Host, target)
	case "nftables":
		return syncNFTTrust(ctx, m.Host, target)
	case "firewalld":
		return syncFirewalldTrust(ctx, m.Host, target)
	default:
		return failedTrust(Unsupported("未安装可用的主机防火墙"))
	}
}
func exactSource(text string, family protocol.AddressFamily) (string, error) {
	if p, e := netip.ParsePrefix(text); e == nil {
		if p.Bits() != p.Addr().BitLen() {
			return "", Conflict("互信规则来源范围发生变化")
		}
		text = p.Addr().String()
	}
	a, e := netip.ParseAddr(text)
	if e != nil || a.Is6() != (family == protocol.IPv6) {
		return "", Conflict("互信规则地址族无效")
	}
	return a.Unmap().String(), nil
}
func trustList(target protocol.PeerTrustTarget, family protocol.AddressFamily) []string {
	if !target.Enabled {
		return []string{}
	}
	if family == protocol.IPv6 {
		return target.IPv6
	}
	return target.IPv4
}
func inputRules(h Host, ctx context.Context, input, name string, args ...string) error {
	v, ok := h.(inputHost)
	if !ok {
		return Unsupported("主机不支持原生规则事务")
	}
	_, e := v.RunInput(ctx, []byte(input), name, args...)
	return e
}

// UFW 的互信规则在独立 before 链中，用户配置与自动修复不改写此链。
func ufwTrustFile(text string, family protocol.AddressFamily, addresses []string) (string, error) {
	begin, end := "# OPSD_PEER_TRUST_BEGIN", "# OPSD_PEER_TRUST_END"
	lines := strings.Split(text, "\n")
	out := []string{}
	inside := false
	seen := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == begin {
			if inside || seen {
				return "", Conflict("互信配置标记重复")
			}
			inside = true
			seen = true
			continue
		}
		if t == end {
			if !inside {
				return "", Conflict("互信配置标记不完整")
			}
			inside = false
			continue
		}
		if inside {
			if t != "" {
				fields, e := splitWords(t)
				if e != nil {
					return "", Conflict("互信配置含外部内容")
				}
				if len(fields) == 6 && fields[0] == "-A" && fields[1] == trustChain && fields[2] == "-s" && fields[4] == "-j" && fields[5] == "ACCEPT" {
					if _, e = exactSource(fields[3], family); e != nil {
						return "", e
					}
				} else if t != "-A "+ufwPrefix(family)+"-before-input -m comment --comment "+trustMarker+" -j "+trustChain {
					return "", Conflict("互信配置含外部内容")
				}
			}
			continue
		}
		if t == ":"+trustChain+" - [0:0]" && strings.Contains(text, begin) {
			continue
		}
		if strings.Contains(t, trustChain) {
			return "", Conflict("互信链名称已被外部配置使用")
		}
		out = append(out, line)
	}
	if inside {
		return "", Conflict("互信配置标记不完整")
	}
	if len(addresses) == 0 {
		return strings.Join(out, "\n"), nil
	}
	filter := -1
	insert := -1
	for i, line := range out {
		if strings.TrimSpace(line) == "*filter" {
			filter = i
		}
		if insert < 0 && strings.HasPrefix(strings.TrimSpace(line), "-A "+ufwPrefix(family)+"-before-input ") {
			insert = i
		}
	}
	if filter < 0 || insert < 0 {
		return "", Unsupported("UFW 入站持久链无法定位")
	}
	block := []string{begin, "-A " + ufwPrefix(family) + "-before-input -m comment --comment " + trustMarker + " -j " + trustChain}
	for _, ip := range addresses {
		block = append(block, "-A "+trustChain+" -s "+ip+" -j ACCEPT")
	}
	block = append(block, end)
	final := []string{}
	for i, line := range out {
		if i == insert {
			final = append(final, block...)
		}
		final = append(final, line)
		if i == filter {
			final = append(final, ":"+trustChain+" - [0:0]")
		}
	}
	return strings.Join(final, "\n"), nil
}
func readUFWTrustRuntime(text string, family protocol.AddressFamily) ([]string, []string, bool, error) {
	addresses := []string{}
	hooks := []string{}
	chain := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, ":"+trustChain+" ") {
			chain = true
			continue
		}
		if !strings.Contains(line, trustChain) {
			continue
		}
		fields, e := splitWords(line)
		if e != nil {
			return addresses, hooks, chain, e
		}
		if len(fields) == 6 && fields[0] == "-A" && fields[1] == trustChain && fields[2] == "-s" && fields[4] == "-j" && fields[5] == "ACCEPT" {
			ip, e := exactSource(fields[3], family)
			if e != nil {
				return addresses, hooks, chain, e
			}
			addresses = append(addresses, ip)
			continue
		}
		if len(fields) == 8 && fields[0] == "-A" && fields[1] == ufwPrefix(family)+"-before-input" && strings.Join(fields[2:], " ") == "-m comment --comment "+trustMarker+" -j "+trustChain {
			hooks = append(hooks, line)
			continue
		}
		return addresses, hooks, chain, Conflict("互信链包含外部规则或引用")
	}
	if chain && len(hooks) == 0 {
		return addresses, hooks, chain, Conflict("互信链缺少所属入站引用")
	}
	return addresses, hooks, chain, nil
}

func ufwTrustInputPath(text string, family protocol.AddressFamily) error {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "-A INPUT ") {
			continue
		}
		if line == "-A INPUT -j "+ufwPrefix(family)+"-before-input" {
			return nil
		}
		if line != "-A INPUT -j "+ufwPrefix(family)+"-before-logging-input" {
			return Conflict("UFW 入站前置路径包含外部规则")
		}
	}
	return Conflict("UFW 入站调用路径缺失")
}
func syncUFWTrust(ctx context.Context, h Host, target protocol.PeerTrustTarget) []protocol.ScopeResult {
	out := []protocol.ScopeResult{}
	conf, e := h.Read("/etc/ufw/ufw.conf")
	if e != nil {
		return failedTrust(e)
	}
	active := configValue(string(conf), "ENABLED") == "yes"
	defaults, e := h.Read("/etc/default/ufw")
	if e != nil {
		return failedTrust(e)
	}
	for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		wanted := trustList(target, family)
		path, save, restore := "/etc/ufw/before.rules", "iptables-save", "iptables-restore"
		if family == protocol.IPv6 {
			path = "/etc/ufw/before6.rules"
			save = "ip6tables-save"
			restore = "ip6tables-restore"
		}
		if family == protocol.IPv6 && configValue(string(defaults), "IPV6") != "yes" && len(wanted) > 0 {
			for _, scope := range []protocol.ConfigScope{protocol.Permanent, protocol.Runtime} {
				r := trustResult(family, scope, nil, Unsupported("UFW 未启用 IPv6"))
				r.State = "unsupported"
				out = append(out, r)
			}
			continue
		}
		before, e := h.Read(path)
		permanent := []string{}
		if e == nil {
			var after string
			after, e = ufwTrustFile(string(before), family, wanted)
			if e == nil && after != string(before) {
				e = h.Replace(path, before, []byte(after))
			}
			if e == nil {
				var b []byte
				b, e = h.Read(path)
				if e == nil && string(b) != after {
					e = Conflict("互信持久配置读回不一致")
				}
				if e == nil {
					permanent = append(permanent, wanted...)
				}
			}
		}
		out = append(out, trustResult(family, protocol.Permanent, permanent, e))
		if !active {
			r := trustResult(family, protocol.Runtime, nil, nil)
			r.State = "inactive"
			out = append(out, r)
			continue
		}
		raw, readErr := h.Run(ctx, save, "-t", "filter")
		if readErr != nil {
			out = append(out, trustResult(family, protocol.Runtime, nil, readErr))
			continue
		}
		actual, hooks, exists, readErr := readUFWTrustRuntime(string(raw), family)
		if readErr == nil && len(wanted) > 0 {
			readErr = ufwTrustInputPath(string(raw), family)
		}
		if readErr == nil && e != nil {
			readErr = e
		}
		if readErr == nil && (!slices.Equal(actual, wanted) || len(hooks) != func() int {
			if len(wanted) > 0 {
				return 1
			}
			return 0
		}()) {
			commands := []string{"*filter"}
			if len(wanted) > 0 {
				commands = append(commands, ":"+trustChain+" - [0:0]")
			}
			if exists {
				commands = append(commands, "-F "+trustChain)
			}
			for _, hook := range hooks {
				commands = append(commands, strings.Replace(hook, "-A ", "-D ", 1))
			}
			if len(wanted) > 0 {
				commands = append(commands, "-I "+ufwPrefix(family)+"-before-input 1 -m comment --comment "+trustMarker+" -j "+trustChain)
				for _, ip := range wanted {
					commands = append(commands, "-A "+trustChain+" -s "+ip+" -j ACCEPT")
				}
			} else if exists {
				commands = append(commands, "-X "+trustChain)
			}
			commands = append(commands, "COMMIT")
			readErr = inputRules(h, ctx, strings.Join(commands, "\n")+"\n", restore, "--noflush", "--wait", "5")
			if readErr == nil {
				raw, readErr = h.Run(ctx, save, "-t", "filter")
				if readErr == nil {
					actual, hooks, _, readErr = readUFWTrustRuntime(string(raw), family)
					if readErr == nil && (!slices.Equal(actual, wanted) || len(wanted) > 0 && len(hooks) != 1) {
						readErr = Conflict("互信运行规则读回不一致")
					}
				}
			}
		}
		out = append(out, trustResult(family, protocol.Runtime, actual, readErr))
	}
	return out
}

func isTrustRule(r protocol.FirewallRule) bool { return r.Spec != nil && r.Spec.Comment == trustMarker }
func trustNFTSpec(ip string, family protocol.AddressFamily) *protocol.RuleSpec {
	return &protocol.RuleSpec{Family: family, Source: ip, Protocol: "any", Action: "allow", Comment: trustMarker}
}
func verifyNFTTrust(spec *protocol.RuleSpec) (string, error) {
	if spec == nil || spec.Comment != trustMarker || spec.Action != "allow" || spec.Protocol != "any" || spec.Port != "" || canonicalAddress(spec.Destination, spec.Family) != canonicalAddress("", spec.Family) {
		return "", Conflict("互信规则含外部条件")
	}
	return exactSource(spec.Source, spec.Family)
}
