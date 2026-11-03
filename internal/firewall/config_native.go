package firewall

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

// PrepareConfiguration 使用一次原生读数生成可持久恢复的步骤。
func (m *Manager) PrepareConfiguration(ctx context.Context, s protocol.FirewallState, d protocol.FirewallDefinition) ([]Step, error) {
	if e := ValidateConfiguration(d); e != nil {
		return nil, e
	}
	if !s.Complete || s.Backend != d.Backend {
		return nil, Conflict("防火墙后端或系统读取状态已经改变")
	}
	switch d.Backend {
	case "ufw":
		return prepareUFWConfiguration(ctx, &UFW{Host: m.Host}, s, d)
	case "nftables":
		return prepareNFTConfiguration(ctx, &NFT{Host: m.Host}, s, d)
	case "firewalld":
		return prepareFirewalldConfiguration(ctx, &Firewalld{Host: m.Host}, s, d)
	case "none":
		return []Step{}, nil
	}
	return nil, Unsupported("没有可用的防火墙后端")
}

func prepareNFTConfiguration(ctx context.Context, n *NFT, s protocol.FirewallState, d protocol.FirewallDefinition) ([]Step, error) {
	sources, e := n.sources(ctx)
	if e != nil {
		return nil, e
	}
	steps := []Step{}
	files := map[string]string{}
	seen := map[string]bool{}
	for _, p := range d.DefaultPolicies {
		if seen[p.Chain] {
			continue
		}
		seen[p.Chain] = true
		parts := strings.Split(p.Chain, "/")
		if len(parts) != 3 || len(nftFamilies(parts[0])) == 0 || !nftIdentifier.MatchString(parts[1]) || !nftIdentifier.MatchString(parts[2]) {
			return nil, Unsupported("入站链引用无效")
		}
		source, ok := sources.Chains[p.Chain]
		if !ok || source.Policy == "" {
			return nil, Unsupported("持久入站链无法唯一定位")
		}
		for _, other := range d.DefaultPolicies {
			if other.Chain == p.Chain && other.Action != p.Action {
				return nil, Unsupported("同一入站链须使用相同默认策略")
			}
		}
		for _, scope := range []protocol.ConfigScope{protocol.Permanent, protocol.Runtime} {
			if scope == protocol.Runtime && (s.ServiceActive == nil || !*s.ServiceActive) {
				continue
			}
			sel := Selector{Kind: "managed_chain", Scope: scope, Chain: p.Chain}
			before := Capture(s, sel)
			if len(before.Policies) == 0 {
				return nil, Conflict("系统入站链缺失")
			}
			target := configurationGroup(d, "", p.Chain, "", scope)
			for i := range target {
				if scope == protocol.Permanent {
					target[i].Native.SourceFile = source.Path
				}
			}
			after := View{Rules: mergeManagedRules(before.Rules, target), Policies: append([]protocol.DefaultPolicy(nil), before.Policies...)}
			for i := range after.Policies {
				after.Policies[i].Action = p.Action
			}
			if Equivalent(before, after) {
				continue
			}
			step := Step{Name: "同步入站链", Selector: sel, Before: before, After: after, Forward: protocol.FirewallChange{Kind: "managed_chain"}}
			if scope == protocol.Permanent {
				text, ok := files[source.Path]
				if !ok {
					b, e := n.Host.Read(source.Path)
					if e != nil {
						return nil, e
					}
					text = string(b)
				}
				next, e := nftConfiguredFile(text, p.Chain, target, p.Action)
				if e != nil {
					return nil, e
				}
				step.Source = &SourceEdit{Path: source.Path, Chain: p.Chain, Before: text, After: next, WholeFile: true}
				files[source.Path] = next
			}
			steps = append(steps, step)
		}
	}
	return steps, nil
}

func nftConfiguredFile(text, chain string, target []protocol.FirewallRule, action string) (string, error) {
	lines := strings.Split(text, "\n")
	start, end, e := nftChainRegion(lines, chain)
	if e != nil {
		return "", e
	}
	family := strings.Split(chain, "/")[0]
	out := append([]string(nil), lines[:start+1]...)
	next := 0
	for _, line := range lines[start+1 : end] {
		t := strings.TrimSpace(line)
		if nftPolicyLine.MatchString(t) {
			out = append(out, nftPolicyLine.ReplaceAllString(line, "policy "+map[string]string{"allow": "accept", "deny": "drop"}[action]+";"))
			continue
		}
		if spec := parseNFTText(t, family); spec != nil && spec.Comment != trustMarker {
			if next < len(target) {
				out = append(out, "\t\t"+nftExpression(target[next].Spec))
				next++
			}
		} else {
			out = append(out, line)
		}
	}
	for next < len(target) {
		out = append(out, "\t\t"+nftExpression(target[next].Spec))
		next++
	}
	return strings.Join(append(out, lines[end:]...), "\n"), nil
}

func (n *NFT) writeManagedChain(ctx context.Context, step Step, reverse bool) error {
	if step.Source != nil {
		before, after := step.Source.Before, step.Source.After
		if reverse {
			before, after = after, before
		}
		return n.Host.Replace(step.Source.Path, []byte(before), []byte(after))
	}
	s, e := n.Read(ctx)
	if e != nil {
		return e
	}
	current := Capture(s, step.Selector)
	expected, target := step.Before, step.After
	if reverse {
		expected, target = step.After, step.Before
	}
	if !Equivalent(current, expected) {
		return Conflict("入站链在执行前发生变化")
	}
	parts := strings.Split(step.Selector.Chain, "/")
	if len(parts) != 3 {
		return Unsupported("入站链引用无效")
	}
	ref := strings.Join(parts, " ")
	commands := []string{}
	opaque := []protocol.FirewallRule{}
	for _, r := range current.Rules {
		if r.Spec == nil || isTrustRule(r) {
			opaque = append(opaque, r)
			continue
		}
		if r.Native.Handle == "" {
			return Conflict("无法定位运行规则")
		}
		commands = append(commands, "delete rule "+ref+" handle "+r.Native.Handle)
	}
	segment := []protocol.FirewallRule{}
	rawIndex := 0
	for _, r := range target.Rules {
		if r.Spec != nil && !isTrustRule(r) {
			segment = append(segment, r)
			continue
		}
		if rawIndex >= len(opaque) || !sameRule(opaque[rawIndex], r) {
			return Conflict("保留的原生片段发生变化")
		}
		anchor := opaque[rawIndex].Native.Handle
		if anchor == "" {
			return Conflict("保留规则无法定位")
		}
		for _, v := range segment {
			commands = append(commands, "insert rule "+ref+" position "+anchor+" "+nftExpression(v.Spec))
		}
		segment = nil
		rawIndex++
	}
	if rawIndex != len(opaque) {
		return Conflict("保留的原生规则数量发生变化")
	}
	for _, r := range segment {
		commands = append(commands, "add rule "+ref+" "+nftExpression(r.Spec))
	}
	if len(target.Policies) > 0 {
		commands = append(commands, "add chain "+ref+" { policy "+map[string]string{"allow": "accept", "deny": "drop"}[target.Policies[0].Action]+"; }")
	}
	h, ok := n.Host.(inputHost)
	if !ok {
		return Unsupported("主机不支持原生规则输入")
	}
	_, e = h.RunInput(ctx, []byte(strings.Join(commands, "\n")+"\n"), "nft", "-f", "-")
	return e
}

func prepareFirewalldConfiguration(ctx context.Context, f *Firewalld, s protocol.FirewallState, d protocol.FirewallDefinition) ([]Step, error) {
	steps := []Step{}
	zones := map[string]bool{}
	order := []string{}
	for _, p := range d.DefaultPolicies {
		if !zones[p.Zone] {
			zones[p.Zone] = true
			order = append(order, p.Zone)
		}
	}
	for _, zone := range order {
		for _, scope := range []protocol.ConfigScope{protocol.Runtime, protocol.Permanent} {
			if scope == protocol.Runtime && (s.ServiceActive == nil || !*s.ServiceActive) {
				continue
			}
			sel := Selector{Kind: "rules", Scope: scope, Zone: zone}
			current := Capture(s, sel)
			target := configurationGroup(d, "", "", zone, scope)
			contains := func(r protocol.FirewallRule, list []protocol.FirewallRule) bool {
				for _, v := range list {
					if sameRule(r, v) {
						return true
					}
				}
				return false
			}
			// firewalld 的简单端口是一个跨地址族对象，按对象删除和恢复。
			for _, r := range append([]protocol.FirewallRule(nil), current.Rules...) {
				if r.Spec == nil || contains(r, target) || !contains(r, current.Rules) {
					continue
				}
				port := strings.HasPrefix(r.NativeExpression, "port:")
				after := cloneView(current)
				after.Rules = nil
				for _, v := range current.Rules {
					if port && v.NativeExpression == r.NativeExpression || !port && sameRule(v, r) {
						continue
					}
					after.Rules = append(after.Rules, v)
				}
				kind := "delete_rule"
				if port {
					kind = "delete_port"
				}
				forward := protocol.FirewallChange{Kind: kind, Scope: scope, Zone: zone, Rule: r.Spec, Priority: r.Native.Priority}
				reverse := forward
				reverse.Kind = strings.Replace(kind, "delete", "add", 1)
				steps = append(steps, Step{Name: "移除旧区域规则", Selector: sel, Before: current, After: after, Forward: forward, Reverse: reverse})
				current = after
			}
			for _, r := range target {
				if contains(r, current.Rules) {
					continue
				}
				after := cloneView(current)
				kind := "add_rule"
				additions := []protocol.FirewallRule{r}
				if strings.HasPrefix(r.NativeExpression, "port:") {
					kind = "add_port"
					additions = nil
					for _, v := range target {
						if v.NativeExpression == r.NativeExpression {
							additions = append(additions, v)
						}
					}
					if len(additions) != 2 {
						return nil, Unsupported("简单端口须同时包含两个地址族")
					}
				}
				after.Rules = append(after.Rules, additions...)
				forward := protocol.FirewallChange{Kind: kind, Scope: scope, Zone: zone, Rule: r.Spec, Priority: r.Native.Priority}
				reverse := forward
				reverse.Kind = strings.Replace(kind, "add", "delete", 1)
				steps = append(steps, Step{Name: "写入区域规则", Selector: sel, Before: current, After: after, Forward: forward, Reverse: reverse})
				current = after
			}
		}
	}
	// 先使规则一致，再一次重载所有已接管区域目标。
	runtimeSel := Selector{Kind: "policy", Scope: protocol.Runtime}
	runtimeBefore := Capture(s, runtimeSel)
	runtimeAfter := cloneView(runtimeBefore)
	reload := false
	for _, zone := range order {
		action := ""
		for _, p := range d.DefaultPolicies {
			if p.Zone == zone {
				if action != "" && action != p.Action {
					return nil, Unsupported("区域默认策略须一致")
				}
				action = p.Action
			}
		}
		sel := Selector{Kind: "policy", Scope: protocol.Permanent, Zone: zone}
		before := Capture(s, sel)
		after := cloneView(before)
		changed := false
		if len(before.Policies) == 0 {
			return nil, Conflict("永久区域缺失")
		}
		for i := range after.Policies {
			if after.Policies[i].Action != action {
				changed = true
			}
			after.Policies[i].Action = action
		}
		if changed {
			old := before.Policies[0].Action
			forward := protocol.FirewallChange{Kind: "set_default_policy", Scope: protocol.Permanent, Zone: zone, Action: action}
			reverse := forward
			reverse.Action = old
			steps = append(steps, Step{Name: "保存区域默认策略", Selector: sel, Before: before, After: after, Forward: forward, Reverse: reverse})
		}
		for i, p := range runtimeAfter.Policies {
			if p.Zone == zone {
				if p.Action != action {
					reload = true
				}
				runtimeAfter.Policies[i].Action = action
			}
		}
	}
	if reload && s.ServiceActive != nil && *s.ServiceActive {
		steps = append(steps, Step{Name: "应用区域默认策略", Selector: runtimeSel, Before: runtimeBefore, After: runtimeAfter, Forward: protocol.FirewallChange{Kind: "managed_reload"}})
	}
	return steps, nil
}

func (f *Firewalld) writeManagedReload(ctx context.Context, step Step, reverse bool) (result error) {
	if e := f.reloadSafe(ctx, "*", true); e != nil {
		return e
	}
	if !reverse {
		_, e := f.Host.Run(ctx, "firewall-cmd", "--reload")
		return e
	}
	s, e := f.Read(ctx)
	if e != nil {
		return e
	}
	permanent := map[string]string{}
	before := map[string]string{}
	for _, p := range s.DefaultPolicies {
		if p.Scope == protocol.Permanent {
			permanent[p.Zone] = p.Action
		}
	}
	for _, p := range step.Before.Policies {
		before[p.Zone] = p.Action
	}
	defer func() {
		for zone, action := range permanent {
			if _, err := f.Host.Run(ctx, "firewall-cmd", "--permanent", "--zone="+zone, "--set-target="+nativeTarget(action)); err != nil && result == nil {
				result = err
			}
		}
	}()
	for zone, action := range before {
		if _, e = f.Host.Run(ctx, "firewall-cmd", "--permanent", "--zone="+zone, "--set-target="+nativeTarget(action)); e != nil {
			return e
		}
	}
	_, e = f.Host.Run(ctx, "firewall-cmd", "--reload")
	if e != nil {
		return fmt.Errorf("恢复区域目标：%w", e)
	}
	return nil
}
