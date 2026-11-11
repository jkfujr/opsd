package firewall

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

func managedScope(chain, zone string) string { return chain + "\x00" + zone }

func managedRule(r protocol.FirewallRule, backend protocol.Backend, policies []protocol.DefaultPolicy) bool {
	if r.Spec == nil || r.SystemDefault || isTrustRule(r) {
		return false
	}
	switch backend {
	case "ufw":
		return r.Native.Chain == ufwPrefix(r.Spec.Family)+"-user-input"
	case "firewalld":
		return r.Native.Zone != ""
	case "nftables":
		for _, p := range policies {
			if p.Chain == r.Native.Chain {
				return true
			}
		}
	}
	return false
}

// ImportConfiguration 以当前有效规则为准，不把原生系统条目复制到用户配置。
func ImportConfiguration(s protocol.FirewallState) (protocol.FirewallDefinition, error) {
	d := protocol.FirewallDefinition{Backend: s.Backend, Rules: []protocol.ManagedRule{}, DefaultPolicies: []protocol.ManagedPolicy{}}
	if !s.Complete {
		return d, Conflict("系统防火墙读取不完整")
	}
	scope := protocol.Permanent
	if s.ServiceActive != nil && *s.ServiceActive {
		scope = protocol.Runtime
	}
	for _, r := range s.Rules {
		if r.Scope != scope || !managedRule(r, s.Backend, s.DefaultPolicies) {
			continue
		}
		kind := ""
		if s.Backend == "firewalld" && strings.HasPrefix(r.NativeExpression, "port:") {
			kind = "port"
		}
		d.Rules = append(d.Rules, protocol.ManagedRule{ID: identity.Random(), Spec: *r.Spec, Chain: r.Native.Chain, Zone: r.Native.Zone, Priority: r.Native.Priority, NativeKind: kind})
	}
	for _, p := range s.DefaultPolicies {
		if p.Scope == scope && p.Action != "" {
			d.DefaultPolicies = append(d.DefaultPolicies, protocol.ManagedPolicy{Family: p.Family, Chain: p.Chain, Zone: p.Zone, Action: p.Action})
		}
	}
	return d, ValidateConfiguration(d)
}

func ValidateConfiguration(d protocol.FirewallDefinition) error {
	if d.Backend != "ufw" && d.Backend != "firewalld" && d.Backend != "nftables" && d.Backend != "iptables" && d.Backend != "none" {
		return Unsupported("防火墙后端无效")
	}
	if d.Backend == "iptables" && (len(d.Rules) > 0 || len(d.DefaultPolicies) > 0) {
		return Unsupported("原生 iptables 普通规则只读")
	}
	ids := map[string]bool{}
	for _, r := range d.Rules {
		if r.Spec.Comment == trustMarker {
			return Unsupported("互信标记由集群设置维护")
		}
		if r.ID == "" || ids[r.ID] {
			return Unsupported("规则标识重复或缺失")
		}
		ids[r.ID] = true
		if e := validateChange(protocol.FirewallChange{Rule: &r.Spec, Priority: r.Priority}); e != nil {
			return e
		}
		if d.Backend == "ufw" && (r.Chain != ufwPrefix(r.Spec.Family)+"-user-input" || r.Zone != "" || r.Spec.Protocol == "icmp" || r.Spec.Protocol == "icmpv6") {
			return Unsupported("UFW 规则范围或协议不受支持")
		}
		if d.Backend == "firewalld" && (r.Zone == "" || r.Chain != "" || r.Spec.Comment != "") {
			return Unsupported("区域规则参数不受支持")
		}
		if r.NativeKind != "" && (d.Backend != "firewalld" || r.NativeKind != "port" || r.Spec.Action != "allow" || r.Spec.Port == "" || canonicalAddress(r.Spec.Source, r.Spec.Family) != canonicalAddress("", r.Spec.Family) || canonicalAddress(r.Spec.Destination, r.Spec.Family) != canonicalAddress("", r.Spec.Family)) {
			return Unsupported("原生端口绑定无效")
		}
		located := false
		for _, p := range d.DefaultPolicies {
			if d.Backend == "ufw" && p.Family == r.Spec.Family || d.Backend != "ufw" && p.Chain == r.Chain && p.Zone == r.Zone && p.Family == r.Spec.Family {
				located = true
			}
		}
		if !located {
			return Unsupported("规则所属入站链或区域不存在")
		}
	}
	for i, r := range d.Rules {
		for _, other := range d.Rules[:i] {
			if managedScope(r.Chain, r.Zone) == managedScope(other.Chain, other.Zone) && samePriority(r.Priority, other.Priority) && (specEqual(&r.Spec, &other.Spec) || d.Backend == "ufw" && ufwSameConditions(&r.Spec, &other.Spec)) {
				return Conflict("存在无法独立定位的重复规则")
			}
		}
	}
	for _, p := range d.DefaultPolicies {
		if p.Family != protocol.IPv4 && p.Family != protocol.IPv6 {
			return Unsupported("默认策略地址族无效")
		}
		if p.Action != "allow" && p.Action != "deny" && p.Action != "reject" && !(d.Backend == "firewalld" && p.Action == "default") {
			return Unsupported("默认策略无效")
		}
		if d.Backend == "nftables" && p.Action == "reject" {
			return Unsupported("nftables 默认策略不支持拒绝")
		}
	}
	return nil
}

// ChangeConfiguration 只编辑保存的配置，不访问系统。
func ChangeConfiguration(before protocol.FirewallDefinition, c protocol.FirewallChange) (protocol.FirewallDefinition, error) {
	b, _ := json.Marshal(before)
	var d protocol.FirewallDefinition
	_ = json.Unmarshal(b, &d)
	if c.Scope != "" || c.Kind == "set_enabled" {
		return d, Unsupported("统一配置不接受运行范围或启停修改")
	}
	index := -1
	for i, r := range d.Rules {
		if r.ID == c.RuleID {
			index = i
		}
	}
	if c.Kind == "edit_rule" || c.Kind == "delete_rule" || c.Kind == "move_rule" {
		if index < 0 {
			return d, Conflict("规则已经改变，请刷新")
		}
		// 简单端口同时作用于两个地址族；独立编辑时将这对规则表达为 rich rules。
		old := d.Rules[index]
		if old.NativeKind == "port" && c.Kind != "move_rule" {
			for i := range d.Rules {
				r := &d.Rules[i]
				if r.NativeKind == "port" && r.Zone == old.Zone && r.Spec.Port == old.Spec.Port && r.Spec.Protocol == old.Spec.Protocol {
					r.NativeKind = ""
				}
			}
		}
	}
	switch c.Kind {
	case "add_rule", "edit_rule":
		if c.Rule == nil {
			return d, Unsupported("缺少规则内容")
		}
		r := protocol.ManagedRule{ID: identity.Random(), Spec: *c.Rule, Chain: c.Chain, Zone: c.Zone, Priority: c.Priority}
		r.Spec.Source = canonicalAddress(r.Spec.Source, r.Spec.Family)
		r.Spec.Destination = canonicalAddress(r.Spec.Destination, r.Spec.Family)
		if index >= 0 {
			old := d.Rules[index]
			r.ID, r.Chain, r.Zone = old.ID, old.Chain, old.Zone
			if old.Spec.Family != r.Spec.Family {
				return d, Unsupported("修改地址族请删除后重新添加")
			}
			if r.Priority == nil {
				r.Priority = old.Priority
			}
			d.Rules[index] = r
		} else {
			if d.Backend == "ufw" {
				r.Chain = ufwPrefix(r.Spec.Family) + "-user-input"
			}
			d.Rules = append(d.Rules, r)
		}
	case "delete_rule":
		d.Rules = slices.Delete(d.Rules, index, index+1)
	case "move_rule":
		r := d.Rules[index]
		if d.Backend == "firewalld" {
			if r.NativeKind == "port" {
				return d, Unsupported("简单端口不支持排序")
			}
			if c.Priority == nil {
				return d, Unsupported("缺少优先级")
			}
			d.Rules[index].Priority = c.Priority
		} else {
			if c.BeforeRuleID == r.ID {
				return d, Unsupported("不能移动到规则自身之前")
			}
			d.Rules = slices.Delete(d.Rules, index, index+1)
			at := len(d.Rules)
			if c.BeforeRuleID != "" {
				at = -1
				for i, v := range d.Rules {
					if v.ID == c.BeforeRuleID {
						if v.Chain != r.Chain || v.Zone != r.Zone || v.Spec.Family != r.Spec.Family {
							return d, Unsupported("只能在同一入站范围内排序")
						}
						at = i
					}
				}
				if at < 0 {
					return d, Conflict("排序位置已经改变")
				}
			}
			d.Rules = slices.Insert(d.Rules, at, r)
		}
	case "set_default_policy":
		found := false
		for i, p := range d.DefaultPolicies {
			if d.Backend == "ufw" || p.Chain == c.Chain && p.Zone == c.Zone {
				d.DefaultPolicies[i].Action = c.Action
				found = true
			}
		}
		if !found {
			return d, Unsupported("默认策略范围不存在")
		}
	default:
		return d, Unsupported("不支持此配置修改")
	}
	if e := ValidateConfiguration(d); e != nil {
		return d, e
	}
	return d, nil
}

func managedNative(r protocol.ManagedRule, scope protocol.ConfigScope) protocol.FirewallRule {
	v := protocol.FirewallRule{Spec: &r.Spec, Scope: scope, Native: protocol.NativePosition{Chain: r.Chain, Zone: r.Zone, Priority: r.Priority}, Editable: true}
	if r.NativeKind == "port" {
		v.NativeExpression = "port:" + r.Spec.Port + "/" + r.Spec.Protocol
	}
	return v
}

func configurationGroup(d protocol.FirewallDefinition, family protocol.AddressFamily, chain, zone string, scope protocol.ConfigScope) []protocol.FirewallRule {
	out := []protocol.FirewallRule{}
	for _, r := range d.Rules {
		if r.Chain == chain && r.Zone == zone && (family == "" || r.Spec.Family == family) {
			out = append(out, managedNative(r, scope))
		}
	}
	return out
}

// 将不能编辑的片段留在原位置，只替换可表达的用户规则。
func mergeManagedRules(current, target []protocol.FirewallRule) []protocol.FirewallRule {
	out := []protocol.FirewallRule{}
	next := 0
	for _, r := range current {
		if r.Spec == nil || isTrustRule(r) {
			out = append(out, r)
		} else if next < len(target) {
			out = append(out, target[next])
			next++
		}
	}
	return append(out, target[next:]...)
}

func configError(scope string, e error) error { return fmt.Errorf("%s：%w", scope, e) }
