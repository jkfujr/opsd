package firewall

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

type Adapter interface {
	Read(context.Context) (protocol.FirewallState, error)
	Prepare(context.Context, protocol.ChangePlan) ([]Step, error)
	Write(context.Context, Step, bool) error
}

// View 是一次原生步骤实际涉及的链、策略或启停状态，不是整机备份。
type View struct {
	Rules    []protocol.FirewallRule  `json:"rules,omitempty"`
	Policies []protocol.DefaultPolicy `json:"policies,omitempty"`
	Enabled  *bool                    `json:"enabled,omitempty"`
}
type Selector struct {
	Kind   string                 `json:"kind"`
	Family protocol.AddressFamily `json:"family,omitempty"`
	Scope  protocol.ConfigScope   `json:"scope,omitempty"`
	Chain  string                 `json:"chain,omitempty"`
	Zone   string                 `json:"zone,omitempty"`
}
type SourceEdit struct {
	Path       string `json:"path"`
	Chain      string `json:"chain"`
	Before     string `json:"before"`
	After      string `json:"after"`
	BeforeRule string `json:"before_rule,omitempty"`
	WholeFile  bool   `json:"whole_file,omitempty"`
}
type Step struct {
	Name          string                  `json:"name"`
	Selector      Selector                `json:"selector"`
	Before        View                    `json:"before"`
	After         View                    `json:"after"`
	Forward       protocol.FirewallChange `json:"forward"`
	Reverse       protocol.FirewallChange `json:"reverse"`
	Source        *SourceEdit             `json:"source,omitempty"`
	ForwardAnchor *protocol.FirewallRule  `json:"forward_anchor,omitempty"`
	ReverseAnchor *protocol.FirewallRule  `json:"reverse_anchor,omitempty"`
}

type Manager struct{ Host Host }

func New() *Manager { return &Manager{Host: NativeHost{}} }
func (m *Manager) Detect(ctx context.Context) (protocol.Backend, error) {
	active := []protocol.Backend{}
	if b, e := m.Host.Read("/etc/ufw/ufw.conf"); e == nil && configValue(string(b), "ENABLED") == "yes" {
		active = append(active, "ufw")
	}
	if m.Host.LookPath("firewall-cmd") {
		if _, e := m.Host.Run(ctx, "firewall-cmd", "--state"); e == nil {
			active = append(active, "firewalld")
		}
	}
	if m.Host.LookPath("nft") {
		if b, e := m.Host.Run(ctx, "systemctl", "is-active", "nftables.service"); e == nil && strings.TrimSpace(string(b)) == "active" {
			active = append(active, "nftables")
		}
	}
	if len(active) > 1 {
		return "", Conflict("多个防火墙管理器正在管理过滤规则，请先在节点上解除冲突")
	}
	if len(active) == 1 {
		return active[0], nil
	}
	// 未启用时只沿明确的持久配置识别归属，安装命令本身不是归属依据。
	configured := []protocol.Backend{}
	if m.Host.Exists("/etc/ufw/user.rules") && m.Host.Exists("/etc/ufw/ufw.conf") {
		configured = append(configured, "ufw")
	}
	if m.Host.Exists("/etc/firewalld/zones") && m.Host.LookPath("firewall-offline-cmd") {
		configured = append(configured, "firewalld")
	}
	if m.Host.Exists("/etc/nftables.conf") && m.Host.LookPath("nft") {
		if b, e := m.Host.Read("/etc/nftables.conf"); e == nil && strings.Contains(string(b), "chain ") {
			configured = append(configured, "nftables")
		}
	}
	if len(configured) > 1 {
		return "", Conflict("存在多套未启用的持久配置，无法确定管理归属")
	}
	if len(configured) == 1 {
		return configured[0], nil
	}
	return "none", nil
}
func (m *Manager) Adapter(backend protocol.Backend) (Adapter, error) {
	switch backend {
	case "ufw":
		return &UFW{Host: m.Host}, nil
	case "firewalld":
		return &Firewalld{Host: m.Host}, nil
	case "nftables":
		return &NFT{Host: m.Host}, nil
	}
	return nil, Unsupported("没有可用的防火墙管理器")
}
func (m *Manager) Read(ctx context.Context) (protocol.FirewallState, error) {
	backend, err := m.Detect(ctx)
	if err != nil {
		return emptyState("unknown"), err
	}
	if backend == "none" {
		s := emptyState(backend)
		s.Capabilities.Install = protocol.Capability{Supported: m.installable(), Reason: "仅支持 Debian 或 Ubuntu 的系统软件源"}
		return s, nil
	}
	a, _ := m.Adapter(backend)
	return a.Read(ctx)
}
func (m *Manager) Preview(ctx context.Context, change protocol.FirewallChange) (protocol.ChangePlan, error) {
	s, err := m.Read(ctx)
	if err != nil {
		return protocol.ChangePlan{}, err
	}
	if err = validateChange(change); err != nil {
		return protocol.ChangePlan{}, err
	}
	p := protocol.ChangePlan{ID: identity.Random(), ContractVersion: protocol.Version, CreatedAt: time.Now().UTC(), Backend: s.Backend, Change: change, Reasons: []string{}, AffectedScopes: []string{}}
	selector, err := selectChange(s, change)
	if err != nil {
		return p, err
	}
	v := Capture(s, selector)
	p.Baseline = protocol.PlanBaseline{Rules: v.Rules, DefaultPolicies: v.Policies, ServiceActive: v.Enabled}
	if p.Baseline.Rules == nil {
		p.Baseline.Rules = []protocol.FirewallRule{}
	}
	if p.Baseline.DefaultPolicies == nil {
		p.Baseline.DefaultPolicies = []protocol.DefaultPolicy{}
	}
	p.Baseline.DockerPorts = []protocol.DockerPortState{}
	p.AffectedScopes = []string{string(selector.Scope) + "/" + string(selector.Family) + "/" + selector.Zone + selector.Chain}
	p.RequiresConfirmation = change.Kind == "set_enabled" || selector.Scope == protocol.Runtime || selector.Scope == protocol.Both
	a, err := m.Adapter(s.Backend)
	if err != nil {
		return p, err
	}
	steps, err := a.Prepare(ctx, p)
	if err != nil {
		p.Reasons = append(p.Reasons, err.Error())
		return p, nil
	}
	initial, final := map[Selector]View{}, map[Selector]View{}
	for _, step := range steps {
		if _, ok := initial[step.Selector]; !ok {
			initial[step.Selector] = step.Before
		}
		final[step.Selector] = step.After
	}
	changed := false
	for selector, before := range initial {
		if !Equivalent(before, final[selector]) {
			changed = true
		}
	}
	if !changed {
		p.Reasons = append(p.Reasons, "目标与当前配置相同，无需执行")
		return p, nil
	}
	p.Executable = true
	return p, nil
}
func emptyState(backend protocol.Backend) protocol.FirewallState {
	return protocol.FirewallState{CollectedAt: time.Now().UTC(), Complete: true, Backend: backend, Rules: []protocol.FirewallRule{}, DefaultPolicies: []protocol.DefaultPolicy{}, DockerPorts: []protocol.DockerPortState{}, Errors: []protocol.ReadError{}}
}
func scopeContains(want, got protocol.ConfigScope) bool {
	return want == "" || want == protocol.Both || want == got
}
func Capture(state protocol.FirewallState, selector Selector) View {
	v := View{}
	switch selector.Kind {
	case "ufw_policy":
		policySelector := selector
		policySelector.Kind = "policy"
		v.Policies = Capture(state, policySelector).Policies
		prefix := ufwPrefix(selector.Family)
		for _, r := range state.Rules {
			if r.Scope == protocol.Runtime && (r.Native.Chain == prefix+"-reject-input" || r.Native.Chain == prefix+"-track-input" || r.Native.Chain == prefix+"-skip-to-policy-input") {
				r.NativeExpression = strings.TrimSuffix(strings.TrimSuffix(strings.Join(strings.Fields(r.NativeExpression), " "), " --reject-with icmp-port-unreachable"), " --reject-with icmp6-port-unreachable")
				v.Rules = append(v.Rules, r)
			}
		}
		sort.SliceStable(v.Rules, func(i, j int) bool {
			return v.Rules[i].Native.Chain+v.Rules[i].NativeExpression < v.Rules[j].Native.Chain+v.Rules[j].NativeExpression
		})
	case "managed_chain":
		rules, policies := selector, selector
		rules.Kind, policies.Kind = "rules", "policy"
		v.Rules, v.Policies = Capture(state, rules).Rules, Capture(state, policies).Policies
	case "rules", "managed_rules":
		for _, r := range state.Rules {
			if scopeContains(selector.Scope, r.Scope) && ruleFamilyMatches(r, selector.Family) && (selector.Chain == "" || r.Native.Chain == selector.Chain) && (selector.Zone == "" || r.Native.Zone == selector.Zone) {
				v.Rules = append(v.Rules, r)
			}
		}
		if selector.Kind == "managed_rules" && state.Backend == "ufw" && selector.Scope == protocol.Runtime {
			for i := range v.Rules {
				if v.Rules[i].Spec != nil {
					spec := *v.Rules[i].Spec
					spec.Comment = ""
					v.Rules[i].Spec = &spec
				}
			}
		}
	case "policy":
		for _, p := range state.DefaultPolicies {
			if scopeContains(selector.Scope, p.Scope) && (selector.Family == "" || p.Family == selector.Family) && (selector.Zone == "" || p.Zone == selector.Zone) && (selector.Chain == "" || p.Chain == selector.Chain) {
				v.Policies = append(v.Policies, p)
			}
		}
	case "enabled":
		v.Enabled = state.ServiceActive
		if state.Backend == "ufw" {
			for _, r := range state.Rules {
				if r.Scope == protocol.Permanent {
					v.Rules = append(v.Rules, r)
				}
			}
			for _, p := range state.DefaultPolicies {
				if p.Scope == protocol.Permanent {
					v.Policies = append(v.Policies, p)
				}
			}
		}
	}
	return v
}

func ruleFamilyMatches(r protocol.FirewallRule, family protocol.AddressFamily) bool {
	if family == "" {
		return true
	}
	if r.Spec != nil {
		return r.Spec.Family == family
	}
	if strings.HasPrefix(r.ID, "ipv4/") {
		return family == protocol.IPv4
	}
	if strings.HasPrefix(r.ID, "ipv6/") {
		return family == protocol.IPv6
	}
	return true
}

// 直接编码原生引用和内容，不以可复用的行号或 handle 单独充当规则身份。
func bindRuleReferences(s *protocol.FirewallState) {
	for i := range s.Rules {
		r := &s.Rules[i]
		family := "native"
		if r.Spec != nil {
			family = string(r.Spec.Family)
		} else if strings.HasPrefix(r.ID, "ipv4/") || strings.HasPrefix(r.Native.Chain, "ip/") {
			family = "ipv4"
		} else if strings.HasPrefix(r.ID, "ipv6/") || strings.HasPrefix(r.Native.Chain, "ip6/") {
			family = "ipv6"
		}
		native := r.Native
		if r.Scope == protocol.Runtime && strings.Contains(native.Chain, "/") {
			native.Position = nil
		}
		data, _ := json.Marshal(struct {
			Native     protocol.NativePosition `json:"native"`
			Expression string                  `json:"expression"`
			Spec       *protocol.RuleSpec      `json:"spec"`
		}{native, r.NativeExpression, r.Spec})
		r.ID = family + "/" + string(r.Scope) + "/" + base64.RawURLEncoding.EncodeToString(data)
	}
}
func Equivalent(a, b View) bool { return reflect.DeepEqual(canonical(a), canonical(b)) }
func canonical(v View) View {
	b, _ := json.Marshal(v)
	var result View
	json.Unmarshal(b, &result)
	if len(result.Rules) == 0 {
		result.Rules = nil
	}
	if len(result.Policies) == 0 {
		result.Policies = nil
	}
	zoneRules := false
	for i := range result.Rules {
		r := &result.Rules[i]
		r.ID = ""
		r.Native.Handle = ""
		r.Native.Position = nil
		r.Editable = false
		r.ReadonlyReason = ""
		r.Origin = ""
		r.SystemDefault = false
		if r.Scope == protocol.Runtime && strings.Contains(r.Native.Chain, "/") {
			r.Native.SourceFile = ""
		}
		if r.Native.Zone != "" {
			zoneRules = true
			if r.Native.Priority == nil {
				r.Native.Priority = pointer(0)
			}
		}
		if r.Spec != nil {
			if !strings.HasPrefix(r.NativeExpression, "port:") {
				r.NativeExpression = ""
			}
			r.Spec.Destination = canonicalAddress(r.Spec.Destination, r.Spec.Family)
			r.Spec.Source = canonicalAddress(r.Spec.Source, r.Spec.Family)
		}
	}
	if zoneRules {
		sort.SliceStable(result.Rules, func(i, j int) bool {
			a, b := result.Rules[i], result.Rules[j]
			if a.Scope != b.Scope {
				return a.Scope < b.Scope
			}
			if a.Native.Zone != b.Native.Zone {
				return a.Native.Zone < b.Native.Zone
			}
			if *a.Native.Priority != *b.Native.Priority {
				return *a.Native.Priority < *b.Native.Priority
			}
			return ruleIdentity(a)+a.NativeExpression < ruleIdentity(b)+b.NativeExpression
		})
	}
	for i := range result.Policies {
		result.Policies[i].Editable = false
	}
	sort.Slice(result.Policies, func(i, j int) bool {
		a, _ := json.Marshal(result.Policies[i])
		b, _ := json.Marshal(result.Policies[j])
		return string(a) < string(b)
	})
	return result
}
func canonicalAddress(s string, family protocol.AddressFamily) string {
	if s == "" || s == "any" {
		if family == protocol.IPv6 {
			return "::/0"
		}
		return "0.0.0.0/0"
	}
	if p, e := netip.ParsePrefix(s); e == nil {
		return p.Masked().String()
	}
	if a, e := netip.ParseAddr(s); e == nil {
		return netip.PrefixFrom(a, a.BitLen()).String()
	}
	return s
}
func specEqual(a, b *protocol.RuleSpec) bool {
	if a == nil || b == nil {
		return a == b
	}
	aa, bb := *a, *b
	aa.Source = canonicalAddress(aa.Source, aa.Family)
	bb.Source = canonicalAddress(bb.Source, bb.Family)
	aa.Destination = canonicalAddress(aa.Destination, aa.Family)
	bb.Destination = canonicalAddress(bb.Destination, bb.Family)
	return aa == bb
}
func validateChange(c protocol.FirewallChange) error {
	if c.Scope != "" && c.Scope != protocol.Both && c.Scope != protocol.Runtime && c.Scope != protocol.Permanent {
		return Unsupported("修改范围无效")
	}
	if c.Rule != nil {
		r := c.Rule
		if r.Family != protocol.IPv4 && r.Family != protocol.IPv6 {
			return Unsupported("请选择地址族")
		}
		for _, v := range []string{r.Source, r.Destination} {
			if v == "" || v == "any" {
				continue
			}
			p, e := netip.ParsePrefix(v)
			if e != nil {
				a, e := netip.ParseAddr(v)
				if e != nil || a.Is4() != (r.Family == protocol.IPv4) {
					return Unsupported("地址或地址族无效")
				}
			} else if p.Addr().Is4() != (r.Family == protocol.IPv4) {
				return Unsupported("地址与地址族不一致")
			}
		}
		if r.Protocol != "tcp" && r.Protocol != "udp" && r.Protocol != "any" && r.Protocol != "icmp" && r.Protocol != "icmpv6" {
			return Unsupported("协议不受支持")
		}
		if (r.Protocol == "icmp" && r.Family != protocol.IPv4) || (r.Protocol == "icmpv6" && r.Family != protocol.IPv6) {
			return Unsupported("ICMP 协议与地址族不一致")
		}
		if r.Action != "allow" && r.Action != "deny" && r.Action != "reject" {
			return Unsupported("规则动作无效")
		}
		if len(r.Comment) > 255 || strings.ContainsAny(r.Comment, "\r\n\x00") {
			return Unsupported("备注须为单行且不超过 255 字节")
		}
		if r.Port != "" {
			if r.Protocol != "tcp" && r.Protocol != "udp" {
				return Unsupported("端口必须指定 TCP 或 UDP")
			}
			parts := strings.Split(strings.ReplaceAll(r.Port, ":", "-"), "-")
			if len(parts) > 2 {
				return Unsupported("端口格式无效")
			}
			last := 0
			for _, v := range parts {
				n, e := strconv.Atoi(v)
				if e != nil || n < 1 || n > 65535 || n < last {
					return Unsupported("端口须为 1 至 65535 的单端口或递增范围")
				}
				last = n
			}
		}
	}
	if c.Priority != nil && (*c.Priority < -32768 || *c.Priority > 32767) {
		return Unsupported("优先级须为 -32768 至 32767")
	}
	if c.Kind == "set_default_policy" && c.Action != "allow" && c.Action != "deny" && c.Action != "reject" && c.Action != "default" {
		return Unsupported("默认策略无效")
	}
	return nil
}
func selectChange(s protocol.FirewallState, c protocol.FirewallChange) (Selector, error) {
	sel := Selector{Kind: "rules", Scope: c.Scope, Family: c.Family, Zone: c.Zone, Chain: c.Chain}
	if sel.Scope == "" {
		sel.Scope = protocol.Permanent
		if s.ServiceActive != nil && *s.ServiceActive {
			sel.Scope = protocol.Both
		}
	}
	if c.Rule != nil {
		sel.Family = c.Rule.Family
	}
	if s.Backend == "ufw" && sel.Chain == "" && c.Kind != "set_default_policy" {
		sel.Chain = ufwPrefix(sel.Family) + "-user-input"
	}
	if c.Kind == "set_enabled" {
		if !s.Capabilities.Enable.Supported {
			return sel, Unsupported(s.Capabilities.Enable.Reason)
		}
		sel.Kind = "enabled"
		return sel, nil
	}
	if c.Kind == "set_default_policy" {
		if !s.Capabilities.DefaultPolicy.Supported {
			return sel, Unsupported(s.Capabilities.DefaultPolicy.Reason)
		}
		sel.Kind = "policy"
		if s.Backend == "ufw" || s.Backend == "firewalld" || strings.HasPrefix(sel.Chain, "inet/") {
			sel.Family = ""
		}
		return sel, nil
	}
	if c.Rule != nil {
		sel.Family = c.Rule.Family
	}
	if c.RuleID != "" {
		found := false
		for _, r := range s.Rules {
			if r.ID == c.RuleID {
				if !r.Editable {
					return sel, Unsupported(r.ReadonlyReason)
				}
				sel.Chain = r.Native.Chain
				sel.Zone = r.Native.Zone
				sel.Family = r.Spec.Family
				if c.Rule != nil && c.Rule.Family != r.Spec.Family {
					return sel, Unsupported("修改地址族请删除后重新添加")
				}
				if s.Backend == "firewalld" && strings.HasPrefix(r.NativeExpression, "port:") {
					sel.Family = ""
				}
				found = true
				break
			}
		}
		if !found {
			return sel, Conflict("规则引用已失效，请重新读取")
		}
	}
	if c.Kind == "move_rule" && !s.Capabilities.Reorder.Supported {
		return sel, Unsupported(s.Capabilities.Reorder.Reason)
	}
	if !s.Capabilities.EditRules.Supported {
		return sel, Unsupported(s.Capabilities.EditRules.Reason)
	}
	return sel, nil
}
func baselineView(p protocol.ChangePlan) View {
	return View{Rules: p.Baseline.Rules, Policies: p.Baseline.DefaultPolicies, Enabled: p.Baseline.ServiceActive}
}
func configValue(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return strings.Trim(strings.TrimSpace(strings.SplitN(v, "#", 2)[0]), "\"'")
		}
	}
	return ""
}
func allow() protocol.Capability             { return protocol.Capability{Supported: true} }
func deny(reason string) protocol.Capability { return protocol.Capability{Reason: reason} }
func pointer[T any](v T) *T                  { return &v }
func reason(s *protocol.FirewallState, scope string, err error) {
	s.Complete = false
	s.Errors = append(s.Errors, protocol.ReadError{Scope: scope, Error: protocol.Error{Code: Code(err), Message: err.Error()}})
}
func ruleIdentity(r protocol.FirewallRule) string {
	data, _ := json.Marshal(struct {
		Spec        *protocol.RuleSpec
		Chain, Zone string
		Scope       protocol.ConfigScope
	}{r.Spec, r.Native.Chain, r.Native.Zone, r.Scope})
	return string(data)
}
func requireBaseline(s protocol.FirewallState, p protocol.ChangePlan) (Selector, View, error) {
	if !s.Complete {
		return Selector{}, View{}, Unsupported("当前读取不完整，不能执行修改")
	}
	sel, e := selectChange(s, p.Change)
	if e != nil {
		return sel, View{}, e
	}
	v := Capture(s, sel)
	if !Equivalent(v, baselineView(p)) {
		return sel, v, Conflict("受影响规则或配置已改变，请重新预览")
	}
	return sel, v, nil
}
