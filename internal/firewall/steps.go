package firewall

import (
	"fmt"
	"github.com/jkfujr/opsd/internal/protocol"
	"slices"
	"strings"
)

func cloneView(v View) View {
	r := v
	r.Rules = slices.Clone(v.Rules)
	r.Policies = slices.Clone(v.Policies)
	return r
}
func findRule(state protocol.FirewallState, id string) (*protocol.FirewallRule, error) {
	for _, r := range state.Rules {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, Conflict("规则引用已失效")
}
func sameRule(a, b protocol.FirewallRule) bool {
	return Equivalent(View{Rules: []protocol.FirewallRule{a}}, View{Rules: []protocol.FirewallRule{b}})
}

func samePriority(a, b *int) bool {
	x, y := 0, 0
	if a != nil {
		x = *a
	}
	if b != nil {
		y = *b
	}
	return x == y
}
func sameNativeRuleType(a, b protocol.FirewallRule) bool {
	return strings.HasPrefix(a.NativeExpression, "port:") == strings.HasPrefix(b.NativeExpression, "port:")
}
func uniqueRule(rules []protocol.FirewallRule, want protocol.FirewallRule) (protocol.FirewallRule, error) {
	found := []protocol.FirewallRule{}
	for _, r := range rules {
		if sameRule(r, want) {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		return protocol.FirewallRule{}, Conflict("无法唯一定位受影响规则")
	}
	return found[0], nil
}

// ruleSteps 将单项编辑拆为有读回边界的删除与插入；每一步分别保存必要逆向参数。
func ruleSteps(s protocol.FirewallState, p protocol.ChangePlan, sel Selector, before View) ([]Step, error) {
	c := p.Change
	var old, anchor *protocol.FirewallRule
	var err error
	if c.RuleID != "" {
		old, err = findRule(s, c.RuleID)
		if err != nil {
			return nil, err
		}
		if old.Spec == nil {
			return nil, Unsupported("此规则不能无损还原")
		}
	}
	if c.BeforeRuleID != "" {
		anchor, err = findRule(s, c.BeforeRuleID)
		if err != nil {
			return nil, err
		}
		if !scopeContains(sel.Scope, anchor.Scope) || anchor.Native.Chain != sel.Chain || anchor.Native.Zone != sel.Zone {
			return nil, Unsupported("只能在同一条链或区域内排序")
		}
	}
	if old != nil && anchor != nil && sameRule(*old, *anchor) {
		return nil, Unsupported("规则不能移动到自身之前")
	}
	steps := []Step{}
	current := cloneView(before)
	if old != nil {
		after := cloneView(current)
		after.Rules = nil
		var next *protocol.FirewallRule
		for i, r := range current.Rules {
			if r.Scope == old.Scope && specEqual(r.Spec, old.Spec) && samePriority(r.Native.Priority, old.Native.Priority) && sameNativeRuleType(r, *old) && r.Native.Chain == old.Native.Chain && r.Native.Zone == old.Native.Zone {
				if _, e := uniqueRule(current.Rules, r); e != nil {
					return nil, e
				}
				if i+1 < len(current.Rules) && current.Rules[i+1].Scope == r.Scope {
					v := current.Rules[i+1]
					next = &v
				}
			}
		}
		for _, r := range current.Rules {
			if specEqual(r.Spec, old.Spec) && samePriority(r.Native.Priority, old.Native.Priority) && sameNativeRuleType(r, *old) && r.Native.Chain == old.Native.Chain && r.Native.Zone == old.Native.Zone {
				continue
			}
			after.Rules = append(after.Rules, r)
		}
		f := protocol.FirewallChange{Kind: "delete_rule", Scope: sel.Scope, Rule: old.Spec, Zone: sel.Zone, Chain: sel.Chain, Priority: old.Native.Priority}
		u := f
		u.Kind = "add_rule"
		steps = append(steps, Step{Name: "移除原规则", Selector: sel, Before: current, After: after, Forward: f, Reverse: u, ReverseAnchor: next})
		current = after
		if c.Kind == "edit_rule" && anchor == nil {
			anchor = next
		}
	}
	if c.Kind != "delete_rule" {
		var spec *protocol.RuleSpec
		priority := c.Priority
		if c.Kind == "move_rule" {
			spec = old.Spec
			if priority == nil {
				priority = old.Native.Priority
			}
		} else {
			spec = c.Rule
		}
		if spec == nil {
			return nil, Unsupported("规则内容缺失")
		}
		for _, r := range current.Rules {
			if specEqual(r.Spec, spec) && samePriority(r.Native.Priority, priority) {
				return nil, Conflict("目标范围已有相同规则，拒绝建立无法区分的重复条目")
			}
		}
		after := cloneView(current)
		after.Rules = nil
		scopes := []protocol.ConfigScope{sel.Scope}
		if sel.Scope == protocol.Both {
			scopes = []protocol.ConfigScope{protocol.Runtime, protocol.Permanent}
		}
		// 保持读取中的原生范围顺序，每个范围独立计算插入位置。
		for _, scope := range scopes {
			group := []protocol.FirewallRule{}
			for _, r := range current.Rules {
				if r.Scope == scope {
					group = append(group, r)
				}
			}
			item := protocol.FirewallRule{Spec: spec, Scope: scope, Native: protocol.NativePosition{Chain: sel.Chain, Zone: sel.Zone, Priority: priority}, Editable: true}
			if len(group) > 0 {
				item.Native.SourceFile = group[0].Native.SourceFile
			}
			index := len(group)
			if anchor != nil {
				found := false
				for i, r := range group {
					if (r.Spec != nil && specEqual(r.Spec, anchor.Spec)) || (r.Spec == nil && r.NativeExpression == anchor.NativeExpression) {
						index = i
						found = true
						break
					}
				}
				if !found {
					return nil, Conflict("插入位置已失效")
				}
			}
			group = slices.Insert(group, index, item)
			after.Rules = append(after.Rules, group...)
		}
		f := protocol.FirewallChange{Kind: "add_rule", Scope: sel.Scope, Rule: spec, Zone: sel.Zone, Chain: sel.Chain, Priority: priority}
		u := f
		u.Kind = "delete_rule"
		steps = append(steps, Step{Name: "写入目标规则", Selector: sel, Before: current, After: after, Forward: f, Reverse: u, ForwardAnchor: anchor})
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("没有可执行的修改")
	}
	return steps, nil
}

func policyStep(p protocol.ChangePlan, sel Selector, before View, action string) ([]Step, error) {
	if len(before.Policies) == 0 {
		return nil, Unsupported("无法读取目标策略")
	}
	old := before.Policies[0].Action
	for _, v := range before.Policies {
		if v.Action != old {
			return nil, Unsupported("运行与永久策略不同，请先选择单侧修改")
		}
	}
	after := cloneView(before)
	for i := range after.Policies {
		after.Policies[i].Action = action
	}
	f := p.Change
	f.Scope = sel.Scope
	f.Action = action
	u := f
	u.Action = old
	return []Step{{Name: "修改默认入站策略", Selector: sel, Before: before, After: after, Forward: f, Reverse: u}}, nil
}
