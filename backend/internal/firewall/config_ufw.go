package firewall

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

type inputHost interface {
	RunInput(context.Context, []byte, string, ...string) ([]byte, error)
}

func ufwExpression(r protocol.RuleSpec, chain string) string {
	parts := []string{"-A", chain}
	if s := canonicalAddress(r.Source, r.Family); s != canonicalAddress("", r.Family) {
		parts = append(parts, "-s", s)
	}
	if s := canonicalAddress(r.Destination, r.Family); s != canonicalAddress("", r.Family) {
		parts = append(parts, "-d", s)
	}
	if r.Protocol != "any" {
		parts = append(parts, "-p", r.Protocol, "-m", r.Protocol)
	}
	if r.Port != "" {
		parts = append(parts, "--dport", strings.ReplaceAll(r.Port, "-", ":"))
	}
	return strings.Join(append(parts, "-j", map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT"}[r.Action]), " ")
}

func ufwTuple(r protocol.RuleSpec, chain string) []string {
	port := strings.ReplaceAll(r.Port, "-", ":")
	if port == "" {
		port = "any"
	}
	line := fmt.Sprintf("### tuple ### %s %s %s %s any %s in", r.Action, r.Protocol, port, canonicalAddress(r.Destination, r.Family), canonicalAddress(r.Source, r.Family))
	if r.Comment != "" {
		line += " comment=" + hex.EncodeToString([]byte(r.Comment))
	}
	return []string{line, ufwExpression(r, chain)}
}

func ufwConfiguredFile(text string, family protocol.AddressFamily, target []protocol.FirewallRule) (string, error) {
	lines := strings.Split(text, "\n")
	out := []string{}
	next := 0
	inserted := false
	for i := 0; i < len(lines); {
		if strings.HasPrefix(lines[i], "### tuple ### ") {
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(lines[end], "###") {
				end++
			}
			block := strings.Join(lines[i:end], "\n")
			parsed := parseUFWFile(block, family, "")
			if len(parsed) == 1 && parsed[0].Spec != nil && parsed[0].Native.Chain == ufwPrefix(family)+"-user-input" {
				if next < len(target) {
					out = append(out, ufwTuple(*target[next].Spec, ufwPrefix(family)+"-user-input")...)
					next++
				}
			} else {
				out = append(out, lines[i:end]...)
			}
			i = end
			continue
		}
		if lines[i] == "### END RULES ###" {
			for next < len(target) {
				out = append(out, ufwTuple(*target[next].Spec, ufwPrefix(family)+"-user-input")...)
				next++
			}
			inserted = true
		}
		out = append(out, lines[i])
		i++
	}
	if !inserted && next < len(target) {
		return "", Unsupported("用户配置缺少规则结束位置")
	}
	return strings.Join(out, "\n"), nil
}

func prepareUFWConfiguration(ctx context.Context, u *UFW, s protocol.FirewallState, d protocol.FirewallDefinition) ([]Step, error) {
	steps := []Step{}
	for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		present := false
		for _, p := range d.DefaultPolicies {
			if p.Family == family {
				present = true
			}
		}
		if !present {
			continue
		}
		chain := ufwPrefix(family) + "-user-input"
		for _, scope := range []protocol.ConfigScope{protocol.Permanent, protocol.Runtime} {
			if scope == protocol.Runtime && (s.ServiceActive == nil || !*s.ServiceActive) {
				continue
			}
			sel := Selector{Kind: "managed_rules", Scope: scope, Family: family, Chain: chain}
			before := Capture(s, sel)
			target := configurationGroup(d, family, chain, "", scope)
			path := "/etc/ufw/user.rules"
			if family == protocol.IPv6 {
				path = "/etc/ufw/user6.rules"
			}
			for i := range target {
				if scope == protocol.Permanent {
					target[i].Native.SourceFile = path
				} else {
					spec := *target[i].Spec
					spec.Comment = ""
					target[i].Spec = &spec
				}
			}
			after := View{Rules: mergeManagedRules(before.Rules, target)}
			if Equivalent(before, after) {
				continue
			}
			step := Step{Name: "同步用户入站规则", Selector: sel, Before: before, After: after, Forward: protocol.FirewallChange{Kind: "managed_rules"}}
			if scope == protocol.Permanent {
				data, e := u.Host.Read(path)
				if e != nil {
					return nil, e
				}
				next, e := ufwConfiguredFile(string(data), family, target)
				if e != nil {
					return nil, e
				}
				step.Source = &SourceEdit{Path: path, Before: string(data), After: next, WholeFile: true}
			}
			steps = append(steps, step)
		}
	}
	if len(d.DefaultPolicies) > 0 {
		action := d.DefaultPolicies[0].Action
		for _, p := range d.DefaultPolicies {
			if p.Action != action {
				return nil, Unsupported("UFW 两地址族须使用相同默认策略")
			}
		}
		sel := Selector{Kind: "policy", Scope: protocol.Permanent}
		before := Capture(s, sel)
		after := cloneView(before)
		changed := false
		for i := range after.Policies {
			if after.Policies[i].Action != action {
				changed = true
			}
			after.Policies[i].Action = action
		}
		if changed {
			data, e := u.Host.Read("/etc/default/ufw")
			if e != nil {
				return nil, e
			}
			next, e := ufwDefaultFile(string(data), action)
			if e != nil {
				return nil, e
			}
			steps = append(steps, Step{Name: "保存默认入站策略", Selector: sel, Before: before, After: after, Forward: protocol.FirewallChange{Kind: "managed_policy"}, Source: &SourceEdit{Path: "/etc/default/ufw", Before: string(data), After: next, WholeFile: true}})
		}
		if s.ServiceActive != nil && *s.ServiceActive {
			for _, p := range d.DefaultPolicies {
				sel := Selector{Kind: "ufw_policy", Scope: protocol.Runtime, Family: p.Family}
				before := Capture(s, sel)
				after := cloneView(before)
				after.Rules = ufwPolicyRules(p.Family, action)
				for i := range after.Policies {
					after.Policies[i].Action = action
				}
				if Equivalent(before, after) {
					continue
				}
				for _, r := range before.Rules {
					known := false
					for _, kind := range []string{"allow", "deny", "reject"} {
						for _, v := range ufwPolicyRules(p.Family, kind) {
							if sameRule(r, v) {
								known = true
							}
						}
					}
					if !known {
						return nil, Unsupported("默认入站策略相关链含自定义规则，无法完整恢复")
					}
				}
				steps = append(steps, Step{Name: "应用默认入站策略", Selector: sel, Before: before, After: after, Forward: protocol.FirewallChange{Kind: "managed_policy"}})
			}
		}
	}
	return steps, nil
}

func (u *UFW) writeManagedRules(ctx context.Context, step Step, reverse bool) error {
	if step.Source != nil {
		before, after := step.Source.Before, step.Source.After
		if reverse {
			before, after = after, before
		}
		return u.Host.Replace(step.Source.Path, []byte(before), []byte(after))
	}
	v := step.After
	if reverse {
		v = step.Before
	}
	chain := step.Selector.Chain
	if chain != ufwPrefix(step.Selector.Family)+"-user-input" {
		return Unsupported("用户链无效")
	}
	var b strings.Builder
	b.WriteString("*filter\n-F " + chain + "\n")
	for _, r := range v.Rules {
		line := r.NativeExpression
		if r.Spec != nil {
			line = ufwExpression(*r.Spec, chain)
		}
		if !strings.HasPrefix(line, "-A "+chain+" ") || strings.ContainsAny(line, "\r\n") {
			return Unsupported("用户规则无法完整还原")
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("COMMIT\n")
	h, ok := u.Host.(inputHost)
	if !ok {
		return Unsupported("主机不支持原生规则输入")
	}
	command := "iptables-restore"
	if step.Selector.Family == protocol.IPv6 {
		command = "ip6tables-restore"
	}
	_, e := h.RunInput(ctx, []byte(b.String()), command, "--noflush", "--wait", "5")
	return e
}

func (u *UFW) writeManagedPolicy(ctx context.Context, step Step, reverse bool) error {
	if step.Source != nil {
		return u.writeManagedRules(ctx, step, reverse)
	}
	target := step.After
	if reverse {
		target = step.Before
	}
	if len(target.Policies) != 1 {
		return Unsupported("运行默认策略无法定位")
	}
	policy := map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "DROP"}[target.Policies[0].Action]
	if policy == "" {
		return Unsupported("默认策略无效")
	}
	prefix := ufwPrefix(step.Selector.Family)
	var b strings.Builder
	b.WriteString("*filter\n:INPUT " + policy + " [0:0]\n")
	for _, suffix := range []string{"reject-input", "track-input", "skip-to-policy-input"} {
		b.WriteString("-F " + prefix + "-" + suffix + "\n")
	}
	for _, r := range target.Rules {
		if !strings.HasPrefix(r.NativeExpression, "-A "+r.Native.Chain+" ") || strings.ContainsAny(r.NativeExpression, "\r\n") {
			return Unsupported("默认策略规则无法还原")
		}
		b.WriteString(r.NativeExpression + "\n")
	}
	b.WriteString("COMMIT\n")
	h, ok := u.Host.(inputHost)
	if !ok {
		return Unsupported("主机不支持原生规则输入")
	}
	name := "iptables-restore"
	if step.Selector.Family == protocol.IPv6 {
		name = "ip6tables-restore"
	}
	_, e := h.RunInput(ctx, []byte(b.String()), name, "--noflush", "--wait", "5")
	return e
}

func ufwDefaultFile(text, action string) (string, error) {
	value := map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT"}[action]
	if value == "" {
		return "", Unsupported("默认策略无效")
	}
	lines := strings.Split(text, "\n")
	found := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "DEFAULT_INPUT_POLICY=") {
			lines[i] = "DEFAULT_INPUT_POLICY=\"" + value + "\""
			found++
		}
	}
	if found != 1 {
		return "", Unsupported("永久默认策略无法唯一定位")
	}
	return strings.Join(lines, "\n"), nil
}

func ufwPolicyRules(family protocol.AddressFamily, action string) []protocol.FirewallRule {
	prefix := ufwPrefix(family)
	rules := []protocol.FirewallRule{}
	add := func(suffix, tail string) {
		chain := prefix + "-" + suffix
		rules = append(rules, protocol.FirewallRule{Scope: protocol.Runtime, Native: protocol.NativePosition{Chain: chain}, NativeExpression: "-A " + chain + " " + tail})
	}
	add("skip-to-policy-input", "-j "+map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT"}[action])
	if action == "reject" {
		add("reject-input", "-j REJECT")
	}
	if action == "allow" {
		for _, proto := range []string{"tcp", "udp"} {
			add("track-input", "-p "+proto+" -m conntrack --ctstate NEW -j ACCEPT")
		}
	}
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Native.Chain+rules[i].NativeExpression < rules[j].Native.Chain+rules[j].NativeExpression
	})
	return rules
}
