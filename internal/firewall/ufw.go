package firewall

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

type UFW struct{ Host Host }

func ufwPrefix(family protocol.AddressFamily) string {
	if family == protocol.IPv6 {
		return "ufw6"
	}
	return "ufw"
}

func (u *UFW) Read(ctx context.Context) (protocol.FirewallState, error) {
	s := emptyState("ufw")
	conf, err := u.Host.Read("/etc/ufw/ufw.conf")
	if err != nil {
		return s, err
	}
	active := configValue(string(conf), "ENABLED") == "yes"
	s.ServiceActive = &active
	defaults, err := u.Host.Read("/etc/default/ufw")
	if err != nil {
		return s, err
	}
	families := []protocol.AddressFamily{protocol.IPv4}
	if configValue(string(defaults), "IPV6") == "yes" {
		families = append(families, protocol.IPv6)
	}
	policy := map[string]string{"ACCEPT": "allow", "DROP": "deny", "REJECT": "reject"}[configValue(string(defaults), "DEFAULT_INPUT_POLICY")]
	if policy == "" {
		reason(&s, "permanent", Unsupported("默认入站策略无法识别"))
	}
	permanent := []protocol.FirewallRule{}
	running := []protocol.FirewallRule{}
	for _, family := range families {
		path := "/etc/ufw/user.rules"
		command := "iptables-save"
		if family == protocol.IPv6 {
			path = "/etc/ufw/user6.rules"
			command = "ip6tables-save"
		}
		data, e := u.Host.Read(path)
		if e != nil {
			reason(&s, "permanent/"+string(family), e)
			continue
		}
		permanent = append(permanent, parseUFWFile(string(data), family, path)...)
		s.DefaultPolicies = append(s.DefaultPolicies, protocol.DefaultPolicy{Family: family, Scope: protocol.Permanent, Action: policy, Editable: true})
		if active {
			raw, e := u.Host.Run(ctx, command, "-t", "filter")
			if e != nil {
				reason(&s, "runtime/"+string(family), e)
				continue
			}
			parsed, actualPolicy, present := parseUFWRuntime(string(raw), family)
			if !present {
				reason(&s, "runtime/"+string(family), Conflict("UFW 已启用，但原生链缺失"))
			}
			running = append(running, parsed...)
			s.DefaultPolicies = append(s.DefaultPolicies, protocol.DefaultPolicy{Family: family, Scope: protocol.Runtime, Action: actualPolicy, Editable: true})
		}
	}
	ufwBindComments(running, permanent)
	s.Rules = append(running, permanent...)
	s.Capabilities.Read = allow()
	s.Capabilities.EditRules = allow()
	s.Capabilities.Reorder = allow()
	s.Capabilities.DefaultPolicy = allow()
	s.Capabilities.Enable = deny("启停会重建入站以外的 UFW 链，本版尚不能证明完整恢复范围，请在节点上管理服务")
	writeReason := ""
	for _, r := range s.Rules {
		if r.Spec == nil && (r.Scope == protocol.Permanent || r.Native.Chain == "ufw-user-input" || r.Native.Chain == "ufw6-user-input") {
			writeReason = "用户配置含不能无损重建的原生规则，暂不开放写入"
		}
	}
	if active && !ufwUserSidesAgree(s) {
		writeReason = "运行与永久用户规则或策略不同，暂不开放写入"
	}
	for _, path := range []string{"/etc/ufw/before.init", "/etc/ufw/after.init"} {
		if data, e := u.Host.Read(path); e == nil {
			if !ufwInitSupported(string(data), strings.TrimPrefix(path, "/etc/ufw/")) {
				writeReason = "存在自定义初始化脚本，暂不开放写入"
			}
		}
	}
	if !s.Complete {
		writeReason = "规则读取不完整"
	}
	if writeReason != "" {
		s.Capabilities.EditRules = deny(writeReason)
		s.Capabilities.DefaultPolicy = s.Capabilities.EditRules
		s.Capabilities.Reorder = s.Capabilities.EditRules
		s.Capabilities.Enable = s.Capabilities.EditRules
		for i := range s.Rules {
			if s.Rules[i].Editable {
				s.Rules[i].Editable = false
				s.Rules[i].ReadonlyReason = writeReason
			}
		}
	}
	bindRuleReferences(&s)
	return s, nil
}

func ufwInitSupported(text, name string) bool {
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return true
	}
	// 默认模板的四个钩子均为空；任何额外命令仍按自定义脚本处理。
	expected := `set -e
case "$1" in
start)
;;
stop)
;;
status)
;;
flush-all)
;;
*)
echo "'$1' not supported"
echo "Usage: ` + name + ` {start|stop|flush-all|status}"
;;
esac`
	return strings.Join(lines, "\n") == expected
}

func ufwBindComments(running, permanent []protocol.FirewallRule) {
	groups := map[string][]protocol.FirewallRule{}
	for _, r := range permanent {
		groups[r.Native.Chain] = append(groups[r.Native.Chain], r)
	}
	positions := map[string]int{}
	for i := range running {
		r := &running[i]
		if r.Native.Chain != "ufw-user-input" && r.Native.Chain != "ufw6-user-input" {
			continue
		}
		position := positions[r.Native.Chain]
		positions[r.Native.Chain]++
		group := groups[r.Native.Chain]
		if position >= len(group) || group[position].Spec == nil || r.Spec == nil {
			continue
		}
		// UFW 将备注存入永久元数据；仅在原生条件和顺序吻合时关联备注。
		expected := *group[position].Spec
		expected.Comment = ""
		if specEqual(r.Spec, &expected) {
			spec := *r.Spec
			spec.Comment = group[position].Spec.Comment
			r.Spec = &spec
		}
	}
}

func parseUFWFile(text string, family protocol.AddressFamily, path string) []protocol.FirewallRule {
	result := []protocol.FirewallRule{}
	prefix := ufwPrefix(family)
	lines := strings.Split(text, "\n")
	position := 0
	covered := map[int]bool{}
	for i, line := range lines {
		if !strings.HasPrefix(line, "### tuple ### ") {
			continue
		}
		position++
		r := protocol.FirewallRule{ID: fmt.Sprintf("%s/permanent/%d", family, position), Scope: protocol.Permanent, Native: protocol.NativePosition{Chain: prefix + "-user-input", SourceFile: path, Position: pointer(position)}, NativeExpression: line, Origin: "native", ReadonlyReason: "规则含本版不能无损表达的字段"}
		kernel := []string{}
		for j := i + 1; j < len(lines) && !strings.HasPrefix(lines[j], "###"); j++ {
			if strings.HasPrefix(lines[j], "-A ") {
				kernel = append(kernel, lines[j])
				covered[j] = true
			}
		}
		r.NativeExpression = strings.Join(append([]string{line}, kernel...), "\n")
		fields := strings.Fields(strings.TrimPrefix(line, "### tuple ### "))
		direction := "in"
		if len(fields) > 6 {
			direction = fields[6]
		}
		if direction != "in" {
			r.Native.Chain = prefix + "-user-" + direction
		}
		if len(fields) >= 6 && direction == "in" && fields[4] == "any" {
			spec := protocol.RuleSpec{Family: family, Action: fields[0], Protocol: fields[1], Port: fields[2], Destination: fields[3], Source: fields[5]}
			if spec.Port == "any" {
				spec.Port = ""
			}
			spec.Port = strings.ReplaceAll(spec.Port, ":", "-")
			validExtras := len(fields) <= 7
			if len(fields) == 8 && strings.HasPrefix(fields[7], "comment=") {
				v, e := hex.DecodeString(strings.TrimPrefix(fields[7], "comment="))
				if e == nil {
					spec.Comment = string(v)
					validExtras = true
				}
			}
			if validExtras && validateChange(protocol.FirewallChange{Rule: &spec}) == nil && spec.Protocol != "icmp" && spec.Protocol != "icmpv6" {
				if len(kernel) == 1 {
					actual, ok := parseIPTablesRule(kernel[0], family)
					expected := spec
					expected.Comment = ""
					if ok && specEqual(actual, &expected) {
						r.Spec = &spec
						r.NativeExpression = line
						r.Editable = true
						r.ReadonlyReason = ""
					}
				}
			}
		}
		result = append(result, r)
	}
	for i, line := range lines {
		if !covered[i] && strings.HasPrefix(line, "-A "+prefix+"-user-input ") && strings.TrimSpace(line) != "-A "+prefix+"-user-input -j RETURN" {
			result = append(result, protocol.FirewallRule{ID: fmt.Sprintf("%s/permanent/raw/%d", family, i), Scope: protocol.Permanent, Native: protocol.NativePosition{Chain: prefix + "-user-input", SourceFile: path, Position: pointer(i + 1)}, NativeExpression: line, Origin: "native", ReadonlyReason: "用户链存在未登记的原生表达式"})
		}
	}
	return result
}

func parseUFWRuntime(text string, family protocol.AddressFamily) ([]protocol.FirewallRule, string, bool) {
	result := []protocol.FirewallRule{}
	policy := ""
	present := false
	position := 0
	positions := map[string]int{}
	prefix := ufwPrefix(family)
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, ":INPUT ") {
			f := strings.Fields(line)
			if len(f) > 1 {
				policy = map[string]string{"ACCEPT": "allow", "DROP": "deny"}[f[1]]
			}
		}
		if strings.HasPrefix(line, ":"+prefix+"-user-input ") {
			present = true
		}
		if strings.HasPrefix(line, "-A "+prefix+"-reject-input ") && strings.Contains(line, "-j REJECT") {
			policy = "reject"
		}
		words, e := splitWords(line)
		if e != nil || len(words) < 3 || words[0] != "-A" {
			continue
		}
		chain := words[1]
		if chain != prefix+"-user-input" && chain != prefix+"-before-input" && chain != prefix+"-after-input" && chain != prefix+"-reject-input" && chain != prefix+"-track-input" && chain != prefix+"-skip-to-policy-input" && chain != "INPUT" {
			continue
		}
		if chain == prefix+"-user-input" && strings.TrimSpace(line) == "-A "+prefix+"-user-input -j RETURN" {
			continue
		}
		position++
		positions[chain]++
		r := protocol.FirewallRule{ID: fmt.Sprintf("%s/runtime/%d", family, position), Scope: protocol.Runtime, Native: protocol.NativePosition{Chain: chain, Position: pointer(positions[chain])}, NativeExpression: line, Origin: "native", SystemDefault: ufwSystemDefault(line, prefix), ReadonlyReason: "原生前置、后置或复杂规则只读"}
		if chain == prefix+"-user-input" {
			if spec, ok := parseIPTablesRule(line, family); ok {
				r.Spec = spec
				r.Editable = true
				r.ReadonlyReason = ""
			}
		}
		result = append(result, r)
	}
	return result, policy, present
}

func parseIPTablesRule(line string, family protocol.AddressFamily) (*protocol.RuleSpec, bool) {
	w, e := splitWords(line)
	if e != nil || len(w) < 4 {
		return nil, false
	}
	r := &protocol.RuleSpec{Family: family, Source: canonicalAddress("", family), Destination: canonicalAddress("", family), Protocol: "any"}
	seen := map[string]bool{}
	for i := 2; i < len(w); i++ {
		key := w[i]
		if seen[key] && key != "-m" {
			return nil, false
		}
		seen[key] = true
		if i+1 >= len(w) {
			return nil, false
		}
		i++
		v := w[i]
		switch key {
		case "-p":
			r.Protocol = v
		case "-s":
			r.Source = v
		case "-d":
			r.Destination = v
		case "--dport":
			r.Port = strings.ReplaceAll(v, ":", "-")
		case "-m":
			if v != "tcp" && v != "udp" && v != "comment" {
				return nil, false
			}
		case "--comment":
			r.Comment = v
		case "--reject-with":
			if v != "icmp-port-unreachable" && v != "icmp6-port-unreachable" {
				return nil, false
			}
		case "-j":
			r.Action = map[string]string{"ACCEPT": "allow", "DROP": "deny", "REJECT": "reject"}[v]
		default:
			return nil, false
		}
	}
	return r, validateChange(protocol.FirewallChange{Rule: r}) == nil && r.Action != "" && (r.Protocol == "tcp" || r.Protocol == "udp" || r.Protocol == "any")
}

// splitWords 仅解析原生输出中的引号，不展开变量，也不执行 Shell。
func splitWords(text string) ([]string, error) {
	words := []string{}
	var b strings.Builder
	quote := rune(0)
	escaped := false
	started := false
	for _, ch := range text {
		if escaped {
			b.WriteRune(ch)
			escaped = false
			started = true
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				b.WriteRune(ch)
			}
			started = true
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			started = true
			continue
		}
		if ch == ' ' || ch == '\t' {
			if started {
				words = append(words, b.String())
				b.Reset()
				started = false
			}
			continue
		}
		b.WriteRune(ch)
		started = true
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("原生表达式引号不完整")
	}
	if started {
		words = append(words, b.String())
	}
	return words, nil
}

func (u *UFW) Prepare(ctx context.Context, p protocol.ChangePlan) ([]Step, error) {
	s, e := u.Read(ctx)
	if e != nil {
		return nil, e
	}
	sel, before, e := requireBaseline(s, p)
	if e != nil {
		return nil, e
	}
	active := s.ServiceActive != nil && *s.ServiceActive
	if p.Change.Kind != "set_enabled" && ((active && sel.Scope != protocol.Both) || (!active && sel.Scope != protocol.Permanent)) {
		return nil, Unsupported("UFW 启用时同时修改运行和永久配置，停用时只能修改永久配置")
	}
	switch p.Change.Kind {
	case "set_default_policy":
		if p.Change.Action == "default" {
			return nil, Unsupported("UFW 不支持此默认策略")
		}
		return policyStep(p, sel, before, p.Change.Action)
	case "add_rule", "edit_rule", "delete_rule", "move_rule":
		if p.Change.Rule != nil && (p.Change.Rule.Protocol == "icmp" || p.Change.Rule.Protocol == "icmpv6") {
			return nil, Unsupported("UFW 命令接口不能无损编辑此 ICMP 规则")
		}
		var old *protocol.FirewallRule
		if p.Change.RuleID != "" {
			old, e = findRule(s, p.Change.RuleID)
			if e != nil {
				return nil, e
			}
		}
		// UFW 将同条件的动作或备注变化解释为替换，必须在生成步骤前拒绝隐式覆盖。
		for _, r := range s.Rules {
			if r.Scope != protocol.Permanent || old != nil && specEqual(r.Spec, old.Spec) {
				continue
			}
			if old != nil && ufwSameConditions(r.Spec, old.Spec) {
				return nil, Conflict("原规则的匹配条件不唯一，不能独立修改")
			}
			if ufwSameConditions(r.Spec, p.Change.Rule) {
				return nil, Conflict("已存在相同匹配条件的规则，请编辑原规则")
			}
		}
		steps, e := ruleSteps(s, p, sel, before)
		if e != nil {
			return nil, e
		}
		for i := range steps {
			for j := range steps[i].After.Rules {
				r := &steps[i].After.Rules[j]
				if r.Scope == protocol.Permanent && r.Native.SourceFile == "" {
					r.Native.SourceFile = "/etc/ufw/user.rules"
					if sel.Family == protocol.IPv6 {
						r.Native.SourceFile = "/etc/ufw/user6.rules"
					}
				}
			}
		}
		return steps, nil
	}
	return nil, Unsupported("UFW 不支持此动作")
}

func ufwSameConditions(a, b *protocol.RuleSpec) bool {
	if a == nil || b == nil {
		return false
	}
	value := *a
	value.Action, value.Comment = b.Action, b.Comment
	return specEqual(&value, b)
}

func (u *UFW) Write(ctx context.Context, step Step, reverse bool) error {
	if step.Forward.Kind == "managed_rules" {
		return u.writeManagedRules(ctx, step, reverse)
	}
	if step.Forward.Kind == "managed_policy" {
		return u.writeManagedPolicy(ctx, step, reverse)
	}
	c, anchor := step.Forward, step.ForwardAnchor
	if reverse {
		c, anchor = step.Reverse, step.ReverseAnchor
	}
	args := []string{}
	switch c.Kind {
	case "set_default_policy":
		args = append(args, "default", c.Action, "incoming")
	case "add_rule", "delete_rule":
		if c.Rule == nil {
			return Unsupported("规则参数缺失")
		}
		s, e := u.Read(ctx)
		if e != nil {
			return e
		}
		if !s.Complete {
			return Conflict("规则读取不完整")
		}
		if c.Kind == "delete_rule" {
			count := 0
			for _, r := range s.Rules {
				if r.Scope == protocol.Permanent && r.Native.Chain == step.Selector.Chain && specEqual(r.Spec, c.Rule) {
					count++
				}
			}
			if count != 1 {
				return Conflict("待删除规则不唯一")
			}
			args = append(args, "--force", "delete")
		} else if anchor != nil {
			want := *anchor
			want.Scope = protocol.Permanent
			want.Native.SourceFile = "/etc/ufw/user.rules"
			if c.Rule.Family == protocol.IPv6 {
				want.Native.SourceFile = "/etc/ufw/user6.rules"
			}
			found := []protocol.FirewallRule{}
			v4count := 0
			for _, r := range s.Rules {
				if r.Scope != protocol.Permanent {
					continue
				}
				if strings.HasPrefix(r.ID, "ipv4/") {
					v4count++
				}
				if (r.Spec != nil && specEqual(r.Spec, want.Spec)) || (r.Spec == nil && r.NativeExpression == want.NativeExpression) {
					if r.Native.Chain == want.Native.Chain {
						found = append(found, r)
					}
				}
			}
			if len(found) != 1 || found[0].Native.Position == nil {
				return Conflict("插入位置无法唯一定位")
			}
			position := *found[0].Native.Position
			if c.Rule.Family == protocol.IPv6 {
				position += v4count
			}
			args = append(args, "insert", strconv.Itoa(position))
		}
		r := c.Rule
		args = append(args, r.Action, "in", "proto", r.Protocol, "from", canonicalAddress(r.Source, r.Family), "to", canonicalAddress(r.Destination, r.Family))
		if r.Port != "" {
			args = append(args, "port", strings.ReplaceAll(r.Port, "-", ":"))
		}
		if r.Comment != "" {
			args = append(args, "comment", r.Comment)
		}
	default:
		return Unsupported("UFW 不支持此步骤")
	}
	_, e := u.Host.Run(ctx, "ufw", args...)
	return e
}

func ufwUserSidesAgree(s protocol.FirewallState) bool {
	a, b := View{}, View{}
	for _, r := range s.Rules {
		if r.Native.Chain != "ufw-user-input" && r.Native.Chain != "ufw6-user-input" {
			continue
		}
		if r.Spec == nil {
			return false
		}
		r.Native.SourceFile = ""
		if r.Scope == protocol.Runtime {
			r.Scope = protocol.Permanent
			a.Rules = append(a.Rules, r)
		} else {
			b.Rules = append(b.Rules, r)
		}
	}
	for _, p := range s.DefaultPolicies {
		if p.Scope == protocol.Runtime {
			p.Scope = protocol.Permanent
			a.Policies = append(a.Policies, p)
		} else {
			b.Policies = append(b.Policies, p)
		}
	}
	return Equivalent(a, b)
}
