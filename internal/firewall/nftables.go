package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	filepath "path"
	"regexp"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

type NFT struct{ Host Host }
type nftSource struct {
	Path, Chain, Line string
	Spec              *protocol.RuleSpec
	Policy            string
	Position          int
}
type nftSources struct {
	Rules  []nftSource
	Chains map[string]nftSource
}
type nftObject struct {
	Family  string            `json:"family"`
	Table   string            `json:"table"`
	Name    string            `json:"name"`
	Chain   string            `json:"chain"`
	Hook    string            `json:"hook"`
	Type    string            `json:"type"`
	Policy  string            `json:"policy"`
	Handle  uint64            `json:"handle"`
	Expr    []json.RawMessage `json:"expr"`
	Comment string            `json:"comment"`
}
type nftDocument struct {
	NFTables []struct {
		Rule  *nftObject `json:"rule,omitempty"`
		Chain *nftObject `json:"chain,omitempty"`
	} `json:"nftables"`
}

var nftIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
var nftTableLine = regexp.MustCompile(`^table\s+(ip|ip6|inet)\s+([A-Za-z_][A-Za-z0-9_-]*)\s*\{$`)
var nftChainLine = regexp.MustCompile(`^chain\s+([A-Za-z_][A-Za-z0-9_-]*)\s*\{$`)
var nftPolicyLine = regexp.MustCompile(`\bpolicy\s+(accept|drop)\s*;`)
var nftPolicyMetadata = regexp.MustCompile(`^(?:type\s+filter\s+hook\s+input\s+priority\s+[A-Za-z0-9_+-]+;\s*)?policy\s+(accept|drop)\s*;$`)
var nftFileArg = regexp.MustCompile(`(?:^|\s)(?:-f|--file)\s+([^\s;\]}]+)`)

func (n *NFT) sources(ctx context.Context) (nftSources, error) {
	result := nftSources{Chains: map[string]nftSource{}}
	unit, e := n.Host.Run(ctx, "systemctl", "show", "nftables.service", "--property=ExecStart", "--value")
	if e != nil {
		return result, e
	}
	match := nftFileArg.FindStringSubmatch(string(unit))
	if len(match) != 2 || !filepath.IsAbs(match[1]) || strings.ContainsAny(match[1], "$*?\"'") {
		return result, Unsupported("无法确定 nftables 服务的持久加载入口")
	}
	seen := map[string]bool{}
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 8 || seen[path] {
			return Unsupported("持久 include 循环或层数过多")
		}
		seen[path] = true
		data, e := n.Host.Read(path)
		if e != nil {
			return e
		}
		family, table, chain := "", "", ""
		level := 0
		chainLevel := 0
		for i, line := range strings.Split(string(data), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if strings.HasPrefix(t, "include ") {
				words, e := splitWords(strings.TrimSuffix(t, ";"))
				if e != nil || len(words) != 2 || strings.ContainsAny(words[1], "$*?") {
					return Unsupported("仅支持明确的逐文件 include 来源")
				}
				child := words[1]
				if !filepath.IsAbs(child) {
					child = filepath.Join(filepath.Dir(path), child)
				}
				if e = visit(filepath.Clean(child), depth+1); e != nil {
					return e
				}
				continue
			}
			if strings.HasPrefix(t, "define ") {
				return Unsupported("存在变量定义，持久规则无法唯一映射")
			}
			if m := nftTableLine.FindStringSubmatch(t); len(m) == 3 && level == 0 {
				family, table = m[1], m[2]
			}
			if m := nftChainLine.FindStringSubmatch(t); len(m) == 2 && level == 1 {
				chain = m[1]
				chainLevel = level + 1
				ref := family + "/" + table + "/" + chain
				if _, ok := result.Chains[ref]; ok {
					return Unsupported("链存在多个持久定义")
				}
				result.Chains[ref] = nftSource{Path: path, Chain: ref, Position: i + 1}
			}
			if chain != "" && level == chainLevel {
				ref := family + "/" + table + "/" + chain
				if m := nftPolicyLine.FindStringSubmatch(t); len(m) == 2 {
					if !nftPolicyMetadata.MatchString(t) {
						return Unsupported("持久策略与其他语句共行，不能无损修改")
					}
					v := result.Chains[ref]
					v.Line = line
					v.Policy = map[string]string{"accept": "allow", "drop": "deny"}[m[1]]
					v.Position = i + 1
					result.Chains[ref] = v
				} else if !strings.HasPrefix(t, "type ") && t != "}" && t != "};" {
					spec := parseNFTText(t, family)
					result.Rules = append(result.Rules, nftSource{Path: path, Chain: ref, Line: line, Spec: spec, Position: i + 1})
				}
			}
			open, close := braceCounts(t)
			level += open - close
			if chain != "" && level < chainLevel {
				chain = ""
			}
			if level == 0 {
				family, table = "", ""
			}
			if level < 0 {
				return Unsupported("持久配置结构无法识别")
			}
		}
		if level != 0 {
			return Unsupported("持久配置括号不完整")
		}
		return nil
	}
	e = visit(filepath.Clean(match[1]), 0)
	return result, e
}
func braceCounts(text string) (int, int) {
	quoted, escaped := false, false
	open, close := 0, 0
	for _, c := range text {
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && quoted {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
			continue
		}
		if !quoted {
			if c == '#' {
				break
			}
			if c == '{' {
				open++
			}
			if c == '}' {
				close++
			}
		}
	}
	return open, close
}

func (n *NFT) Read(ctx context.Context) (protocol.FirewallState, error) {
	s := emptyState("nftables")
	b, e := n.Host.Run(ctx, "systemctl", "is-active", "nftables.service")
	active := e == nil && strings.TrimSpace(string(b)) == "active"
	s.ServiceActive = &active
	sources, sourceErr := n.sources(ctx)
	raw, e := n.Host.Run(ctx, "nft", "-j", "-a", "-s", "list", "ruleset")
	if e != nil {
		return s, e
	}
	var doc nftDocument
	if e = json.Unmarshal(raw, &doc); e != nil {
		return s, fmt.Errorf("nftables JSON 无法解析：%w", e)
	}
	chains := map[string]nftObject{}
	inputChains := map[string]bool{}
	for _, entry := range doc.NFTables {
		if c := entry.Chain; c != nil {
			ref := c.Family + "/" + c.Table + "/" + c.Name
			chains[ref] = *c
			if c.Hook == "input" && c.Type == "filter" && len(nftFamilies(c.Family)) > 0 {
				inputChains[ref] = true
			}
			if inputChains[ref] && c.Policy != "" {
				for _, family := range nftFamilies(c.Family) {
					s.DefaultPolicies = append(s.DefaultPolicies, protocol.DefaultPolicy{Family: family, Chain: ref, Scope: protocol.Runtime, Action: map[string]string{"accept": "allow", "drop": "deny"}[c.Policy], Editable: sourceErr == nil && sources.Chains[ref].Policy != ""})
				}
			}
		}
	}
	// 只展示主机入站基础链及可达链，避免混入转发或容器发布规则。
	for changed := true; changed; {
		changed = false
		for _, entry := range doc.NFTables {
			r := entry.Rule
			if r == nil || !inputChains[r.Family+"/"+r.Table+"/"+r.Chain] {
				continue
			}
			for _, expression := range r.Expr {
				var value any
				if json.Unmarshal(expression, &value) != nil {
					continue
				}
				var visit func(any)
				visit = func(v any) {
					switch item := v.(type) {
					case map[string]any:
						for key, child := range item {
							if key == "jump" || key == "goto" {
								if target, ok := child.(map[string]any); ok {
									if name, ok := target["target"].(string); ok {
										ref := r.Family + "/" + r.Table + "/" + name
										if !inputChains[ref] {
											inputChains[ref] = true
											changed = true
										}
									}
								}
							}
							visit(child)
						}
					case []any:
						for _, child := range item {
							visit(child)
						}
					}
				}
				visit(value)
			}
		}
	}
	for _, entry := range doc.NFTables {
		r := entry.Rule
		if r == nil {
			continue
		}
		ref := r.Family + "/" + r.Table + "/" + r.Chain
		if !inputChains[ref] {
			continue
		}
		item := protocol.FirewallRule{ID: fmt.Sprintf("native/runtime/%s/%d", ref, r.Handle), Scope: protocol.Runtime, Native: protocol.NativePosition{Chain: ref, Handle: strconv.FormatUint(r.Handle, 10)}, Origin: "native", ReadonlyReason: "复杂表达式或持久来源不能唯一映射"}
		item.Spec = parseNFTExpressions(r.Expr, r.Family, r.Comment)
		data, _ := json.Marshal(r.Expr)
		item.NativeExpression = string(data)
		if item.Spec != nil {
			item.ID = string(item.Spec.Family) + "/runtime/" + ref + "/" + item.Native.Handle
		}
		matches := []nftSource{}
		if sourceErr == nil && item.Spec != nil && chains[ref].Hook == "input" {
			for _, source := range sources.Rules {
				if source.Chain == ref && specEqual(source.Spec, item.Spec) {
					matches = append(matches, source)
				}
			}
		}
		if len(matches) == 1 {
			item.Native.SourceFile = matches[0].Path
			item.Native.Position = pointer(matches[0].Position)
			item.Editable = true
			item.ReadonlyReason = ""
		}
		s.Rules = append(s.Rules, item)
	}
	if sourceErr == nil {
		for _, source := range sources.Rules {
			if !inputChains[source.Chain] {
				continue
			}
			r := protocol.FirewallRule{ID: fmt.Sprintf("native/permanent/%s/%d", source.Path, source.Position), Scope: protocol.Permanent, Spec: source.Spec, Native: protocol.NativePosition{Chain: source.Chain, SourceFile: source.Path, Position: pointer(source.Position)}, NativeExpression: strings.TrimSpace(source.Line), Origin: "native", ReadonlyReason: "持久表达式不能无损修改"}
			if source.Spec != nil {
				r.ID = fmt.Sprintf("%s/permanent/%s/%d", source.Spec.Family, source.Path, source.Position)
				count := 0
				for _, v := range sources.Rules {
					if v.Chain == source.Chain && specEqual(v.Spec, source.Spec) {
						count++
					}
				}
				if count == 1 && chains[source.Chain].Hook == "input" {
					r.Editable = true
					r.ReadonlyReason = ""
				}
			}
			s.Rules = append(s.Rules, r)
		}
		for ref, source := range sources.Chains {
			if source.Policy == "" || !inputChains[ref] {
				continue
			}
			family := strings.Split(ref, "/")[0]
			for _, af := range nftFamilies(family) {
				s.DefaultPolicies = append(s.DefaultPolicies, protocol.DefaultPolicy{Family: af, Chain: ref, Scope: protocol.Permanent, Action: source.Policy, Editable: chains[ref].Hook == "input"})
			}
		}
	} else {
		reason(&s, "permanent", sourceErr)
	}
	s.Capabilities.Read = allow()
	s.Capabilities.EditRules = allow()
	s.Capabilities.Reorder = allow()
	s.Capabilities.DefaultPolicy = allow()
	s.Capabilities.Enable = deny("无法证明系统 nftables 服务启停只影响本次可恢复范围，请在节点上管理服务")
	if sourceErr != nil {
		s.Capabilities.EditRules = deny(sourceErr.Error())
		s.Capabilities.Reorder = s.Capabilities.EditRules
		s.Capabilities.DefaultPolicy = s.Capabilities.EditRules
	}
	bindRuleReferences(&s)
	return s, nil
}
func nftFamilies(family string) []protocol.AddressFamily {
	switch family {
	case "ip":
		return []protocol.AddressFamily{protocol.IPv4}
	case "ip6":
		return []protocol.AddressFamily{protocol.IPv6}
	case "inet":
		return []protocol.AddressFamily{protocol.IPv4, protocol.IPv6}
	}
	return nil
}

func parseNFTText(text, family string) *protocol.RuleSpec {
	w, e := splitWords(strings.TrimSuffix(strings.TrimSpace(text), ";"))
	if e != nil {
		return nil
	}
	r := &protocol.RuleSpec{Protocol: "any"}
	seen := map[string]bool{}
	if family == "ip" {
		r.Family = protocol.IPv4
	} else if family == "ip6" {
		r.Family = protocol.IPv6
	}
	for i := 0; i < len(w); i++ {
		key := w[i]
		if key == "meta" && i+1 < len(w) {
			key = w[i+1]
		}
		if (key == "ip" || key == "ip6") && i+1 < len(w) {
			key = w[i+1]
		}
		if key == "tcp" || key == "udp" || key == "l4proto" {
			key = "protocol"
		}
		if key == "accept" || key == "drop" || key == "reject" {
			key = "action"
		}
		if seen[key] {
			return nil
		}
		seen[key] = true
		switch w[i] {
		case "meta":
			if i+2 >= len(w) {
				return nil
			}
			key, value := w[i+1], w[i+2]
			i += 2
			if key == "nfproto" {
				r.Family = protocol.AddressFamily(value)
			} else if key == "l4proto" {
				r.Protocol = value
				if value == "ipv6-icmp" || value == "58" {
					r.Protocol = "icmpv6"
				}
			} else {
				return nil
			}
		case "ip", "ip6":
			if i+2 >= len(w) {
				return nil
			}
			af := protocol.IPv4
			if w[i] == "ip6" {
				af = protocol.IPv6
			}
			if r.Family != "" && r.Family != af {
				return nil
			}
			r.Family = af
			key, value := w[i+1], w[i+2]
			i += 2
			if key == "saddr" {
				r.Source = value
			} else if key == "daddr" {
				r.Destination = value
			} else {
				return nil
			}
		case "tcp", "udp":
			if i+2 >= len(w) || w[i+1] != "dport" {
				return nil
			}
			r.Protocol = w[i]
			r.Port = w[i+2]
			i += 2
		case "accept":
			r.Action = "allow"
		case "drop":
			r.Action = "deny"
		case "reject":
			r.Action = "reject"
		case "comment":
			if i+1 >= len(w) {
				return nil
			}
			i++
			r.Comment = w[i]
		default:
			return nil
		}
	}
	if r.Family == "" {
		return nil
	}
	r.Source = canonicalAddress(r.Source, r.Family)
	r.Destination = canonicalAddress(r.Destination, r.Family)
	if validateChange(protocol.FirewallChange{Rule: r}) != nil {
		return nil
	}
	return r
}
func parseNFTExpressions(expressions []json.RawMessage, family, comment string) *protocol.RuleSpec {
	parts := []string{}
	for _, raw := range expressions {
		var keys map[string]json.RawMessage
		if json.Unmarshal(raw, &keys) != nil || len(keys) != 1 {
			return nil
		}
		var entry struct {
			Match *struct {
				Op   string `json:"op"`
				Left struct {
					Payload *struct{ Protocol, Field string } `json:"payload"`
					Meta    *struct{ Key string }             `json:"meta"`
				} `json:"left"`
				Right json.RawMessage `json:"right"`
			} `json:"match"`
			Accept json.RawMessage `json:"accept"`
			Drop   json.RawMessage `json:"drop"`
			Reject json.RawMessage `json:"reject"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&entry) != nil {
			return nil
		}
		if entry.Match != nil {
			m := entry.Match
			if m.Op != "==" || (m.Left.Payload != nil && m.Left.Meta != nil) {
				return nil
			}
			value := ""
			var str string
			var number int
			if json.Unmarshal(m.Right, &str) == nil {
				value = str
			} else if json.Unmarshal(m.Right, &number) == nil {
				value = strconv.Itoa(number)
			} else {
				var prefix struct {
					Prefix *struct {
						Addr string `json:"addr"`
						Len  int    `json:"len"`
					} `json:"prefix"`
				}
				decoder := json.NewDecoder(bytes.NewReader(m.Right))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&prefix) != nil || prefix.Prefix == nil {
					return nil
				}
				value = prefix.Prefix.Addr + "/" + strconv.Itoa(prefix.Prefix.Len)
			}
			if m.Left.Payload != nil {
				parts = append(parts, m.Left.Payload.Protocol, m.Left.Payload.Field, value)
			} else if m.Left.Meta != nil {
				parts = append(parts, "meta", m.Left.Meta.Key, value)
			} else {
				return nil
			}
		} else if entry.Accept != nil {
			if string(entry.Accept) != "null" {
				return nil
			}
			parts = append(parts, "accept")
		} else if entry.Drop != nil {
			if string(entry.Drop) != "null" {
				return nil
			}
			parts = append(parts, "drop")
		} else if entry.Reject != nil {
			if string(entry.Reject) != "null" && string(entry.Reject) != "{}" {
				return nil
			}
			parts = append(parts, "reject")
		} else {
			return nil
		}
	}
	if comment != "" {
		parts = append(parts, "comment", strconv.Quote(comment))
	}
	return parseNFTText(strings.Join(parts, " "), family)
}
func nftExpression(r *protocol.RuleSpec) string {
	result := "meta nfproto " + string(r.Family)
	prefix := "ip"
	if r.Family == protocol.IPv6 {
		prefix = "ip6"
	}
	if source := canonicalAddress(r.Source, r.Family); source != canonicalAddress("", r.Family) {
		result += " " + prefix + " saddr " + source
	}
	if dest := canonicalAddress(r.Destination, r.Family); dest != canonicalAddress("", r.Family) {
		result += " " + prefix + " daddr " + dest
	}
	if r.Port != "" {
		result += " " + r.Protocol + " dport " + r.Port
	} else if r.Protocol != "any" {
		protocol := r.Protocol
		if protocol == "icmpv6" {
			protocol = "58"
		}
		result += " meta l4proto " + protocol
	}
	result += " " + map[string]string{"allow": "accept", "deny": "drop", "reject": "reject"}[r.Action]
	if r.Comment != "" {
		result += " comment " + strconv.Quote(r.Comment)
	}
	return result
}

func (n *NFT) Prepare(ctx context.Context, p protocol.ChangePlan) ([]Step, error) {
	s, e := n.Read(ctx)
	if e != nil {
		return nil, e
	}
	sel, before, e := requireBaseline(s, p)
	if e != nil {
		return nil, e
	}
	if p.Change.Kind == "set_enabled" {
		return nil, Unsupported(s.Capabilities.Enable.Reason)
	}
	parts := strings.Split(sel.Chain, "/")
	if len(parts) != 3 || len(nftFamilies(parts[0])) == 0 || !nftIdentifier.MatchString(parts[1]) || !nftIdentifier.MatchString(parts[2]) {
		return nil, Unsupported("请选择明确的原生入站链")
	}
	if p.Change.Kind == "set_default_policy" && (p.Change.Action == "reject" || p.Change.Action == "default") {
		return nil, Unsupported("nftables 基础链策略仅支持允许或丢弃")
	}
	sources, e := n.sources(ctx)
	if e != nil {
		return nil, e
	}
	source, exists := sources.Chains[sel.Chain]
	if !exists {
		return nil, Unsupported("找不到此链唯一的持久定义")
	}
	inputChain := false
	for _, policy := range s.DefaultPolicies {
		if policy.Chain == sel.Chain {
			inputChain = true
		}
	}
	if !inputChain {
		return nil, Unsupported("本版仅编辑可定位的入站基础链")
	}
	if p.Change.Rule != nil && parts[0] != "inet" && p.Change.Rule.Family != nftFamilies(parts[0])[0] {
		return nil, Unsupported("规则地址族与原生链不一致")
	}
	if p.Change.Kind == "set_default_policy" && parts[0] == "inet" {
		sel.Family = ""
		before = Capture(s, sel)
		if !Equivalent(before, baselineView(p)) {
			return nil, Unsupported("inet 链默认策略同时影响 IPv4 和 IPv6，请重新预览")
		}
	}
	if s.ServiceActive != nil && !*s.ServiceActive && sel.Scope != protocol.Permanent {
		return nil, Unsupported("管理器未启用时仅允许修改永久配置")
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
		}
		if p.Change.BeforeRuleID != "" {
			old, e := findRule(s, p.Change.BeforeRuleID)
			if e != nil {
				return nil, e
			}
			want := *old
			want.Scope = scope
			match, e := uniqueRule(partBefore.Rules, want)
			if e != nil {
				return nil, e
			}
			partPlan.Change.BeforeRuleID = match.ID
		}
		var batch []Step
		if p.Change.Kind == "set_default_policy" {
			batch, e = policyStep(partPlan, part, partBefore, p.Change.Action)
		} else {
			batch, e = ruleSteps(s, partPlan, part, partBefore)
		}
		if e != nil {
			return nil, e
		}
		for i := range batch {
			step := &batch[i]
			for j := range step.After.Rules {
				if step.After.Rules[j].Native.SourceFile == "" {
					step.After.Rules[j].Native.SourceFile = source.Path
				}
			}
			if scope == protocol.Permanent {
				edit := &SourceEdit{Path: source.Path, Chain: sel.Chain}
				if p.Change.Kind == "set_default_policy" {
					if source.Policy == "" {
						return nil, Unsupported("无法定位持久默认策略")
					}
					edit.Before = source.Line
					edit.After = nftPolicyLine.ReplaceAllString(source.Line, "policy "+map[string]string{"allow": "accept", "deny": "drop"}[p.Change.Action]+";")
				} else {
					c := step.Forward
					if c.Kind == "delete_rule" {
						for _, r := range sources.Rules {
							if r.Chain == sel.Chain && specEqual(r.Spec, c.Rule) {
								if edit.Before != "" {
									return nil, Conflict("持久规则定位不唯一")
								}
								edit.Before = r.Line
							}
						}
						if edit.Before == "" {
							return nil, Conflict("持久规则定位失败")
						}
						if step.ReverseAnchor != nil {
							for _, r := range sources.Rules {
								if r.Chain == sel.Chain && ((r.Spec != nil && specEqual(r.Spec, step.ReverseAnchor.Spec)) || (r.Spec == nil && strings.TrimSpace(r.Line) == step.ReverseAnchor.NativeExpression)) {
									edit.BeforeRule = r.Line
								}
							}
						}
					} else {
						edit.After = "\t\t" + nftExpression(c.Rule)
						if step.ForwardAnchor != nil {
							for _, r := range sources.Rules {
								if r.Chain == sel.Chain && specEqual(r.Spec, step.ForwardAnchor.Spec) {
									edit.BeforeRule = r.Line
								}
							}
							if edit.BeforeRule == "" {
								return nil, Conflict("持久插入位置无法定位")
							}
						}
					}
				}
				step.Source = edit
			}
		}
		steps = append(steps, batch...)
	}
	return steps, nil
}

func (n *NFT) Write(ctx context.Context, step Step, reverse bool) error {
	if step.Forward.Kind == "managed_chain" {
		return n.writeManagedChain(ctx, step, reverse)
	}
	if step.Source != nil {
		return n.writeSource(*step.Source, reverse)
	}
	c, anchor := step.Forward, step.ForwardAnchor
	if reverse {
		c, anchor = step.Reverse, step.ReverseAnchor
	}
	parts := strings.Split(step.Selector.Chain, "/")
	if len(parts) != 3 {
		return Unsupported("链引用无效")
	}
	target := strings.Join(parts, " ")
	command := ""
	if c.Kind == "set_default_policy" {
		command = "add chain " + target + " { policy " + map[string]string{"allow": "accept", "deny": "drop"}[c.Action] + "; }"
	} else {
		s, e := n.Read(ctx)
		if e != nil {
			return e
		}
		rules := Capture(s, step.Selector).Rules
		if c.Kind == "delete_rule" {
			matches := []protocol.FirewallRule{}
			for _, r := range rules {
				if specEqual(r.Spec, c.Rule) {
					matches = append(matches, r)
				}
			}
			if len(matches) != 1 || matches[0].Native.Handle == "" {
				return Conflict("运行规则 handle 无法唯一定位")
			}
			command = "delete rule " + target + " handle " + matches[0].Native.Handle
		} else if c.Kind == "add_rule" {
			command = "add rule " + target + " " + nftExpression(c.Rule)
			if anchor != nil {
				want := *anchor
				want.Scope = protocol.Runtime
				r, e := uniqueRule(rules, want)
				if e != nil {
					return e
				}
				command = "insert rule " + target + " position " + r.Native.Handle + " " + nftExpression(c.Rule)
			}
		} else {
			return Unsupported("nftables 不支持此步骤")
		}
	}
	_, e := n.Host.Run(ctx, "nft", command)
	return e
}
func (n *NFT) writeSource(edit SourceEdit, reverse bool) error {
	data, e := n.Host.Read(edit.Path)
	if e != nil {
		return e
	}
	before, after := edit.Before, edit.After
	if reverse {
		before, after = after, before
	}
	lines := strings.Split(string(data), "\n")
	regionStart, regionEnd, e := nftChainRegion(lines, edit.Chain)
	if e != nil {
		return e
	}
	if before != "" {
		matches := []int{}
		for i := regionStart + 1; i < regionEnd; i++ {
			if lines[i] == before {
				matches = append(matches, i)
			}
		}
		if len(matches) != 1 {
			return Conflict("持久片段发生变化，不能覆盖")
		}
		index := matches[0]
		if after == "" {
			lines = append(lines[:index], lines[index+1:]...)
		} else {
			lines[index] = after
		}
	} else {
		index := regionEnd
		if edit.BeforeRule != "" {
			count := 0
			for i := regionStart + 1; i < regionEnd; i++ {
				if lines[i] == edit.BeforeRule {
					index = i
					count++
				}
			}
			if count != 1 {
				return Conflict("持久插入锚点已改变")
			}
		}
		// 删除的逆操作恢复到原规则的后继；有后继时由 Prepare 保存。
		lines = append(lines[:index], append([]string{after}, lines[index:]...)...)
	}
	return n.Host.Replace(edit.Path, data, []byte(strings.Join(lines, "\n")))
}
func nftChainRegion(lines []string, ref string) (int, int, error) {
	parts := strings.Split(ref, "/")
	if len(parts) != 3 {
		return 0, 0, Unsupported("链引用无效")
	}
	family, table, level := "", "", 0
	start := -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if m := nftTableLine.FindStringSubmatch(t); len(m) == 3 && level == 0 {
			family, table = m[1], m[2]
		}
		if m := nftChainLine.FindStringSubmatch(t); len(m) == 2 && level == 1 && family == parts[0] && table == parts[1] && m[1] == parts[2] {
			start = i
		}
		open, close := braceCounts(t)
		level += open - close
		if start >= 0 && level == 1 {
			return start, i, nil
		}
	}
	return 0, 0, Conflict("持久链结构已改变")
}
