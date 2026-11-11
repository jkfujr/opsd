package firewall

import (
	"context"
	"slices"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

func stripTrustPolicy(text string) string {
	lines := []string{}
	skip := false
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" && line[0] != ' ' && line[0] != '\t' {
			skip = strings.Fields(line)[0] == trustMarker
		}
		if !skip {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
func firewalldTrustCommand(ctx context.Context, h Host, active, permanent bool, args ...string) ([]byte, error) {
	command := "firewall-cmd"
	options := []string{}
	if !active {
		command = "firewall-offline-cmd"
	} else if permanent {
		options = append(options, "--permanent")
	}
	return h.Run(ctx, command, append(options, args...)...)
}
func ensureFirewalldTrust(ctx context.Context, h Host, active bool, create bool) error {
	query := func(args ...string) ([]byte, error) { return firewalldTrustCommand(ctx, h, active, true, args...) }
	b, e := query("--get-policies")
	if e != nil {
		return e
	}
	exists := slices.Contains(strings.Fields(string(b)), trustMarker)
	if !exists {
		if !create {
			return nil
		}
		if active {
			if e := (&Firewalld{Host: h}).reloadSafe(ctx, "", false); e != nil {
				return e
			}
		}
		if _, e = query("--new-policy=" + trustMarker); e != nil {
			return e
		}
		if _, e = query("--policy="+trustMarker, "--set-description="+trustMarker); e != nil {
			return e
		}
		for _, option := range []string{"--set-target=CONTINUE", "--set-priority=-32768", "--add-ingress-zone=ANY", "--add-egress-zone=HOST"} {
			if _, e = query("--policy="+trustMarker, option); e != nil {
				return e
			}
		}
	} else {
		b, e := query("--policy="+trustMarker, "--get-description")
		if e != nil {
			return e
		}
		if strings.TrimSpace(string(b)) != trustMarker {
			return Conflict("互信策略名称已被外部配置使用")
		}
		for _, check := range []struct{ option, want string }{{"--get-target", "CONTINUE"}, {"--get-priority", "-32768"}, {"--list-ingress-zones", "ANY"}, {"--list-egress-zones", "HOST"}} {
			b, e := query("--policy="+trustMarker, check.option)
			if e != nil {
				return e
			}
			if strings.TrimSpace(string(b)) != check.want {
				return Conflict("互信策略作用范围发生变化")
			}
		}
	}
	if active && (exists || create) {
		b, e = h.Run(ctx, "firewall-cmd", "--get-policies")
		if e != nil {
			return e
		}
		if !slices.Contains(strings.Fields(string(b)), trustMarker) {
			f := &Firewalld{Host: h}
			if e = f.reloadSafeWithPolicy(ctx, "", false, true); e != nil {
				return e
			}
			if _, e = h.Run(ctx, "firewall-cmd", "--reload"); e != nil {
				return e
			}
		}
	}
	return nil
}
func readFirewalldTrust(ctx context.Context, h Host, active, permanent bool) (map[protocol.AddressFamily][]string, []string, error) {
	out := map[protocol.AddressFamily][]string{protocol.IPv4: {}, protocol.IPv6: {}}
	b, e := firewalldTrustCommand(ctx, h, active, permanent, "--get-policies")
	if e != nil {
		return out, nil, e
	}
	if !slices.Contains(strings.Fields(string(b)), trustMarker) {
		return out, []string{}, nil
	}
	// 只维护来源放行，策略内出现其他作用时交给管理员处理。
	b, e = firewalldTrustCommand(ctx, h, active, permanent, "--policy="+trustMarker, "--list-all")
	if e != nil {
		return out, nil, e
	}
	inRichRules := false
	for _, line := range strings.Split(string(b), "\n") {
		if inRichRules && strings.HasPrefix(strings.TrimSpace(line), "rule ") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "priority":
			if value != "-32768" {
				return out, nil, Conflict("互信策略优先级发生变化")
			}
		case "target":
			if value != "CONTINUE" {
				return out, nil, Conflict("互信策略目标发生变化")
			}
		case "ingress-zones":
			if value != "ANY" {
				return out, nil, Conflict("互信来源区域发生变化")
			}
		case "egress-zones":
			if value != "HOST" {
				return out, nil, Conflict("互信目标区域发生变化")
			}
		case "rich rules":
			inRichRules = true
		case "masquerade", "forward", "icmp-block-inversion":
			if value != "no" && value != "" {
				return out, nil, Conflict("互信策略包含额外转发配置")
			}
		default:
			if value != "" {
				return out, nil, Conflict("互信策略包含外部配置")
			}
		}
	}
	b, e = firewalldTrustCommand(ctx, h, active, permanent, "--policy="+trustMarker, "--list-rich-rules")
	if e != nil {
		return out, nil, e
	}
	expressions := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		spec, priority := parseRichRule(line)
		if spec == nil || spec.Action != "allow" || spec.Protocol != "any" || spec.Port != "" || canonicalAddress(spec.Destination, spec.Family) != canonicalAddress("", spec.Family) || priority != nil && *priority != 0 {
			return out, nil, Conflict("互信策略包含外部规则")
		}
		ip, e := exactSource(spec.Source, spec.Family)
		if e != nil {
			return out, nil, e
		}
		out[spec.Family] = append(out[spec.Family], ip)
		expressions = append(expressions, line)
	}
	return out, expressions, nil
}
func syncFirewalldTrust(ctx context.Context, h Host, target protocol.PeerTrustTarget) []protocol.ScopeResult {
	_, e := h.Run(ctx, "firewall-cmd", "--state")
	active := e == nil
	create := target.Enabled && (len(target.IPv4) > 0 || len(target.IPv6) > 0)
	if e = ensureFirewalldTrust(ctx, h, active, create); e != nil {
		return failedTrust(e)
	}
	out := []protocol.ScopeResult{}
	for _, scope := range []protocol.ConfigScope{protocol.Permanent, protocol.Runtime} {
		if scope == protocol.Runtime && !active {
			for _, f := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
				r := trustResult(f, scope, nil, nil)
				r.State = "inactive"
				out = append(out, r)
			}
			continue
		}
		permanent := scope == protocol.Permanent
		actual, old, e := readFirewalldTrust(ctx, h, active, permanent)
		if e == nil {
			for _, f := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
				for _, ip := range trustList(target, f) {
					if slices.Contains(actual[f], ip) {
						continue
					}
					expression, err := richExpression(&protocol.RuleSpec{Family: f, Source: ip, Protocol: "any", Action: "allow"}, nil)
					if err != nil {
						e = err
						break
					}
					if _, e = firewalldTrustCommand(ctx, h, active, permanent, "--policy="+trustMarker, "--add-rich-rule="+expression); e != nil {
						break
					}
				}
				if e != nil {
					break
				}
			}
			if e == nil {
				for _, expression := range old {
					spec, _ := parseRichRule(expression)
					ip, _ := exactSource(spec.Source, spec.Family)
					if !slices.Contains(trustList(target, spec.Family), ip) {
						if _, e = firewalldTrustCommand(ctx, h, active, permanent, "--policy="+trustMarker, "--remove-rich-rule="+expression); e != nil {
							break
						}
					}
				}
			}
			var readErr error
			actual, _, readErr = readFirewalldTrust(ctx, h, active, permanent)
			if e == nil {
				e = readErr
			}
		}
		for _, f := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
			err := e
			got := actual[f]
			want := trustList(target, f)
			if err == nil && (!sameIPSet(got, want)) {
				err = Conflict("互信策略读回不一致")
			}
			out = append(out, trustResult(f, scope, got, err))
		}
	}
	return out
}
func sameIPSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, ip := range a {
		if !slices.Contains(b, ip) {
			return false
		}
	}
	return true
}
