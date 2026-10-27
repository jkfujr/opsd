package firewall

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/opsd-labs/opsd/internal/protocol"
)

type Firewalld struct{ Host Host }

func (f *Firewalld) Read(ctx context.Context) (protocol.FirewallState, error) {
	s := emptyState("firewalld")
	_, e := f.Host.Run(ctx, "firewall-cmd", "--state")
	active := e == nil
	s.ServiceActive = &active
	if active {
		b, e := f.Host.Run(ctx, "firewall-cmd", "--list-all-zones")
		if e != nil {
			reason(&s, "runtime", e)
		} else {
			parseFirewalldZones(&s, string(b), protocol.Runtime)
		}
	}
	command, args := "firewall-cmd", []string{"--permanent", "--list-all-zones"}
	if !active {
		command, args = "firewall-offline-cmd", []string{"--list-all-zones"}
	}
	b, e := f.Host.Run(ctx, command, args...)
	if e != nil {
		reason(&s, "permanent", e)
	} else {
		parseFirewalldZones(&s, string(b), protocol.Permanent)
	}
	s.Capabilities.Read = allow()
	s.Capabilities.EditRules = allow()
	s.Capabilities.DefaultPolicy = allow()
	s.Capabilities.Reorder = allow()
	s.Capabilities.Enable = deny("服务启停涉及区域外的原生对象，本版仅修改规则和区域策略")
	if !s.Complete {
		s.Capabilities.EditRules = deny("区域配置读取不完整")
		s.Capabilities.DefaultPolicy = s.Capabilities.EditRules
		s.Capabilities.Reorder = s.Capabilities.EditRules
		s.Capabilities.Enable = s.Capabilities.EditRules
	}
	bindRuleReferences(&s)
	return s, nil
}

func parseFirewalldZones(s *protocol.FirewallState, text string, scope protocol.ConfigScope) {
	zone := ""
	index := 0
	rich := false
	add := func(expression string, spec *protocol.RuleSpec, priority *int) {
		index++
		r := protocol.FirewallRule{ID: fmt.Sprintf("%s/%s/%s/%d", func() string {
			if spec != nil {
				return string(spec.Family)
			}
			return "native"
		}(), scope, zone, index), Scope: scope, Native: protocol.NativePosition{Zone: zone, Priority: priority}, NativeExpression: expression, Origin: "native", Spec: spec, Editable: spec != nil}
		if spec == nil {
			r.ReadonlyReason = "此区域配置不能由本版规则编辑器无损表达"
		}
		s.Rules = append(s.Rules, r)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			zone = strings.Fields(line)[0]
			index = 0
			rich = false
			continue
		}
		if zone == "" {
			reason(s, string(scope), fmt.Errorf("区域输出格式无法识别"))
			continue
		}
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "rule ") {
			spec, priority := parseRichRule(t)
			add(t, spec, priority)
			rich = true
			continue
		}
		key, value, ok := strings.Cut(t, ":")
		if !ok {
			add(t, nil, nil)
			continue
		}
		value = strings.TrimSpace(value)
		rich = key == "rich rules"
		switch key {
		case "target":
			action := map[string]string{"default": "default", "ACCEPT": "allow", "DROP": "deny", "REJECT": "reject"}[value]
			for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
				s.DefaultPolicies = append(s.DefaultPolicies, protocol.DefaultPolicy{Family: family, Zone: zone, Scope: scope, Action: action, Editable: action != ""})
			}
		case "ports":
			for _, port := range strings.Fields(value) {
				p, proto, ok := strings.Cut(port, "/")
				if !ok {
					add("port:"+port, nil, nil)
					continue
				}
				for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
					r := &protocol.RuleSpec{Family: family, Source: canonicalAddress("", family), Destination: canonicalAddress("", family), Protocol: proto, Port: p, Action: "allow"}
					if validateChange(protocol.FirewallChange{Rule: r}) != nil {
						r = nil
					}
					add("port:"+port, r, nil)
				}
			}
		case "rich rules":
			if value != "" {
				spec, priority := parseRichRule(value)
				add(value, spec, priority)
			}
		case "interfaces", "sources", "services", "protocols", "source-ports", "forward-ports", "icmp-blocks", "icmp-block-inversion", "masquerade", "forward", "ingress-priority", "egress-priority":
			if value != "" && value != "no" && value != "0" {
				add(key+": "+value, nil, nil)
			}
		default:
			if value != "" || !rich {
				add(key+": "+value, nil, nil)
			}
		}
	}
}

func parseRichRule(text string) (*protocol.RuleSpec, *int) {
	w, e := splitWords(text)
	if e != nil || len(w) < 2 || w[0] != "rule" {
		return nil, nil
	}
	r := &protocol.RuleSpec{}
	priority := 0
	section := ""
	familySeen := false
	seen := map[string]bool{}
	for _, token := range w[1:] {
		if key, value, ok := strings.Cut(token, "="); ok {
			identity := section + "/" + key
			if key == "priority" || key == "family" {
				identity = key
			}
			if seen[identity] {
				return nil, nil
			}
			seen[identity] = true
			switch key {
			case "priority":
				n, e := strconv.Atoi(value)
				if e != nil || n < -32768 || n > 32767 {
					return nil, nil
				}
				priority = n
			case "family":
				r.Family = protocol.AddressFamily(value)
				familySeen = true
			case "address":
				if section == "source" {
					r.Source = value
				} else if section == "destination" {
					r.Destination = value
				} else {
					return nil, nil
				}
			case "port":
				if section != "port" {
					return nil, nil
				}
				r.Port = value
			case "protocol":
				if section != "port" {
					return nil, nil
				}
				r.Protocol = value
			case "value":
				if section != "protocol" {
					return nil, nil
				}
				r.Protocol = value
				if value == "ipv6-icmp" || value == "58" {
					r.Protocol = "icmpv6"
				}
			default:
				return nil, nil
			}
			continue
		}
		switch token {
		case "source", "destination", "port", "protocol":
			section = token
		case "accept":
			r.Action = "allow"
		case "drop":
			r.Action = "deny"
		case "reject":
			r.Action = "reject"
		default:
			return nil, nil
		}
	}
	if !familySeen {
		return nil, &priority
	}
	if r.Protocol == "" {
		r.Protocol = "any"
	}
	r.Source = canonicalAddress(r.Source, r.Family)
	r.Destination = canonicalAddress(r.Destination, r.Family)
	if validateChange(protocol.FirewallChange{Rule: r}) != nil {
		return nil, &priority
	}
	return r, &priority
}
func richExpression(r *protocol.RuleSpec, priority *int) (string, error) {
	if r == nil {
		return "", Unsupported("规则内容缺失")
	}
	if r.Comment != "" {
		return "", Unsupported("firewalld rich rule 不支持备注字段")
	}
	p := 0
	if priority != nil {
		p = *priority
	}
	result := fmt.Sprintf("rule family=%s priority=%s", strconv.Quote(string(r.Family)), strconv.Quote(strconv.Itoa(p)))
	if source := canonicalAddress(r.Source, r.Family); source != canonicalAddress("", r.Family) {
		result += " source address=" + strconv.Quote(source)
	}
	if dest := canonicalAddress(r.Destination, r.Family); dest != canonicalAddress("", r.Family) {
		result += " destination address=" + strconv.Quote(dest)
	}
	if r.Port != "" {
		result += " port port=" + strconv.Quote(r.Port) + " protocol=" + strconv.Quote(r.Protocol)
	} else if r.Protocol != "any" {
		protocol := r.Protocol
		if protocol == "icmpv6" {
			protocol = "58"
		}
		result += " protocol value=" + strconv.Quote(protocol)
	}
	result += " " + map[string]string{"allow": "accept", "deny": "drop", "reject": "reject"}[r.Action]
	return result, nil
}

func (f *Firewalld) Prepare(ctx context.Context, p protocol.ChangePlan) ([]Step, error) {
	s, e := f.Read(ctx)
	if e != nil {
		return nil, e
	}
	sel, before, e := requireBaseline(s, p)
	if e != nil {
		return nil, e
	}
	active := s.ServiceActive != nil && *s.ServiceActive
	if !active && sel.Scope != protocol.Permanent && p.Change.Kind != "set_enabled" {
		return nil, Unsupported("firewalld 停用时只能修改永久配置")
	}
	if sel.Zone == "" {
		return nil, Unsupported("请选择区域")
	}
	if p.Change.Kind == "set_default_policy" {
		if sel.Scope == protocol.Runtime {
			return nil, Unsupported("区域目标只能保存永久配置，或保存后有条件重载")
		}
		if sel.Scope == protocol.Both {
			if p.Change.Reload == nil || !*p.Change.Reload {
				return nil, Unsupported("运行目标需要有条件重载")
			}
			if e = f.reloadSafe(ctx, "", false); e != nil {
				return nil, e
			}
			permSel := sel
			permSel.Scope = protocol.Permanent
			permBefore := Capture(s, permSel)
			steps, e := policyStep(p, permSel, permBefore, p.Change.Action)
			if e != nil {
				return nil, e
			}
			runtimeSel := sel
			runtimeSel.Scope = protocol.Runtime
			runtimeBefore := Capture(s, runtimeSel)
			runtimeAfter := cloneView(runtimeBefore)
			for i := range runtimeAfter.Policies {
				runtimeAfter.Policies[i].Action = p.Change.Action
			}
			forward := p.Change
			forward.Kind = "reload"
			reverse := forward
			reverse.Action = runtimeBefore.Policies[0].Action
			steps = append(steps, Step{Name: "有条件重载区域目标", Selector: runtimeSel, Before: runtimeBefore, After: runtimeAfter, Forward: forward, Reverse: reverse})
			return steps, nil
		}
		return policyStep(p, sel, before, p.Change.Action)
	}
	if p.Change.Rule != nil {
		if _, e = richExpression(p.Change.Rule, p.Change.Priority); e != nil {
			return nil, e
		}
	}
	if p.Change.Kind == "move_rule" {
		if p.Change.Priority == nil {
			return nil, Unsupported("firewalld 排序需要指定原生优先级")
		}
		if p.Change.BeforeRuleID != "" {
			return nil, Unsupported("firewalld 不提供统一的前后拖动排序")
		}
	}
	steps := []Step{}
	scopes := []protocol.ConfigScope{sel.Scope}
	if sel.Scope == protocol.Both {
		scopes = []protocol.ConfigScope{protocol.Runtime, protocol.Permanent}
	}
	for _, scope := range scopes {
		part := sel
		part.Scope = scope
		partBefore := Capture(s, part)
		partPlan := p
		partPlan.Change.Scope = scope
		// 两侧使用当前同语义规则，而不是把一侧 ID 当作另一侧身份。
		if p.Change.RuleID != "" {
			old, e := findRule(s, p.Change.RuleID)
			if e != nil {
				return nil, e
			}
			want := *old
			want.Scope = scope
			match, e := uniqueRule(partBefore.Rules, want)
			if e != nil {
				return nil, e
			}
			partPlan.Change.RuleID = match.ID
			if strings.HasPrefix(old.NativeExpression, "port:") {
				if p.Change.Kind == "move_rule" {
					return nil, Unsupported("简单端口没有原生优先级，请改为 rich rule")
				}
				part.Family = ""
				partBefore = Capture(s, part)
				after := cloneView(partBefore)
				after.Rules = nil
				for _, r := range partBefore.Rules {
					if r.NativeExpression != old.NativeExpression {
						after.Rules = append(after.Rules, r)
					}
				}
				forward := protocol.FirewallChange{Kind: "delete_port", Scope: scope, Zone: sel.Zone, Rule: old.Spec}
				reverse := forward
				reverse.Kind = "add_port"
				steps = append(steps, Step{Name: "修改区域端口", Selector: part, Before: partBefore, After: after, Forward: forward, Reverse: reverse})
				if p.Change.Kind == "edit_rule" {
					addPlan := partPlan
					addPlan.Change.Kind = "add_rule"
					addPlan.Change.RuleID = ""
					addSteps, e := ruleSteps(s, addPlan, part, after)
					if e != nil {
						return nil, e
					}
					steps = append(steps, addSteps...)
				}
				continue
			}
		}
		partSteps, e := ruleSteps(s, partPlan, part, partBefore)
		if e != nil {
			return nil, e
		}
		steps = append(steps, partSteps...)
	}
	return steps, nil
}

func (f *Firewalld) Write(ctx context.Context, step Step, reverse bool) error {
	c := step.Forward
	if reverse {
		c = step.Reverse
	}
	if c.Kind == "reload" {
		if e := f.reloadSafe(ctx, c.Zone, true); e != nil {
			return e
		}
		// 逆向顺序先恢复运行侧，再恢复永久侧；重载前仅临时改回本次区域目标。
		if reverse {
			state, e := f.Read(ctx)
			if e != nil {
				return e
			}
			for _, policy := range state.DefaultPolicies {
				if policy.Zone == c.Zone && policy.Scope == protocol.Permanent && policy.Action != step.Forward.Action && policy.Action != c.Action {
					return Conflict("本次区域目标已被外部改动，不能覆盖")
				}
			}
			if _, e := f.Host.Run(ctx, "firewall-cmd", "--permanent", "--zone="+c.Zone, "--set-target="+nativeTarget(c.Action)); e != nil {
				return e
			}
		}
		_, e := f.Host.Run(ctx, "firewall-cmd", "--reload")
		return e
	}
	_, stateErr := f.Host.Run(ctx, "firewall-cmd", "--state")
	command := "firewall-cmd"
	args := []string{}
	if stateErr != nil {
		if c.Scope != protocol.Permanent {
			return Unsupported("firewalld 已停用")
		}
		command = "firewall-offline-cmd"
	} else if c.Scope == protocol.Permanent {
		args = append(args, "--permanent")
	}
	args = append(args, "--zone="+c.Zone)
	switch c.Kind {
	case "set_default_policy":
		args = append(args, "--set-target="+nativeTarget(c.Action))
	case "add_port", "delete_port":
		if c.Rule == nil {
			return Unsupported("端口内容缺失")
		}
		option := "--add-port="
		if c.Kind == "delete_port" {
			option = "--remove-port="
		}
		args = append(args, option+c.Rule.Port+"/"+c.Rule.Protocol)
	case "add_rule", "delete_rule":
		expression, e := richExpression(c.Rule, c.Priority)
		if e != nil {
			return e
		}
		option := "--add-rich-rule="
		if c.Kind == "delete_rule" {
			option = "--remove-rich-rule="
		}
		args = append(args, option+expression)
	default:
		return Unsupported("firewalld 不支持此步骤")
	}
	_, e := f.Host.Run(ctx, command, args...)
	return e
}
func nativeTarget(action string) string {
	return map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT", "default": "default"}[action]
}

// reloadSafe 读取重载会替换的配置，不能用整体保存运行配置来掩盖差异。
func (f *Firewalld) reloadSafe(ctx context.Context, ignoreTargetZone string, ignoreTarget bool) error {
	for _, query := range []string{"--list-all-zones", "--list-all-policies"} {
		a, e := f.Host.Run(ctx, "firewall-cmd", query)
		if e != nil {
			return Unsupported("不能完整读取重载影响：" + e.Error())
		}
		b, e := f.Host.Run(ctx, "firewall-cmd", "--permanent", query)
		if e != nil {
			return Unsupported("不能完整读取永久配置：" + e.Error())
		}
		x, y := normalizeFirewalld(string(a), ignoreTargetZone, ignoreTarget), normalizeFirewalld(string(b), ignoreTargetZone, ignoreTarget)
		if x != y {
			left, right := strings.Split(x, "\n"), strings.Split(y, "\n")
			difference := query
			for i := 0; i < len(left) || i < len(right); i++ {
				a, b := "缺失", "缺失"
				if i < len(left) {
					a = left[i]
				}
				if i < len(right) {
					b = right[i]
				}
				if a != b {
					difference = fmt.Sprintf("运行 %s；永久 %s", a, b)
					break
				}
			}
			return Conflict("存在其他运行与永久配置差异，拒绝重载（" + difference + "）")
		}
	}
	for _, query := range []string{"--get-all-chains", "--get-all-rules", "--get-all-passthroughs"} {
		a, e := f.Host.Run(ctx, "firewall-cmd", "--direct", query)
		if e != nil {
			return Unsupported("无法核对 direct 运行配置")
		}
		b, e := f.Host.Run(ctx, "firewall-cmd", "--permanent", "--direct", query)
		if e != nil {
			return Unsupported("无法核对 direct 永久配置")
		}
		if strings.TrimSpace(string(a)) != strings.TrimSpace(string(b)) {
			return Conflict("存在 direct 临时配置，拒绝重载")
		}
	}
	for _, kind := range []string{"ipset", "service", "helper", "icmptype"} {
		plural := kind + "s"
		a, e := f.Host.Run(ctx, "firewall-cmd", "--get-"+plural)
		if e != nil {
			return Unsupported("无法读取 " + kind + " 运行配置")
		}
		b, e := f.Host.Run(ctx, "firewall-cmd", "--permanent", "--get-"+plural)
		if e != nil {
			return Unsupported("无法读取 " + kind + " 永久配置")
		}
		left, right := strings.Fields(string(a)), strings.Fields(string(b))
		sort.Strings(left)
		sort.Strings(right)
		if strings.Join(left, " ") != strings.Join(right, " ") {
			return Conflict(kind + " 运行与永久对象不同，拒绝重载")
		}
		for _, name := range left {
			x, e := f.Host.Run(ctx, "firewall-cmd", "--info-"+kind+"="+name)
			if e != nil {
				return Unsupported("不能读取运行对象 " + name)
			}
			y, e := f.Host.Run(ctx, "firewall-cmd", "--permanent", "--info-"+kind+"="+name)
			if e != nil {
				return Unsupported("不能读取永久对象 " + name)
			}
			if normalizeFirewalld(string(x), "", false) != normalizeFirewalld(string(y), "", false) {
				return Conflict("对象 " + name + " 存在临时差异，拒绝重载")
			}
		}
	}
	return nil
}
func normalizeFirewalld(text, zone string, ignore bool) string {
	lines := []string{}
	current := ""
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			current = strings.Fields(t)[0]
			t = strings.SplitN(t, " (", 2)[0]
		}
		if ignore && current == zone && strings.HasPrefix(t, "target:") {
			continue
		}
		lines = append(lines, current+"/"+t)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
