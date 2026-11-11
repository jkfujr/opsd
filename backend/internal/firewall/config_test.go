package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

type managedFixture struct {
	*nativeFixture
	input func([]byte, string, ...string) ([]byte, error)
}

func (h *managedFixture) RunInput(_ context.Context, b []byte, name string, args ...string) ([]byte, error) {
	return h.input(b, name, args...)
}

func applyManaged(t *testing.T, m *Manager, d protocol.FirewallDefinition, reverse bool) []Step {
	t.Helper()
	s, e := m.Read(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	steps, e := m.PrepareConfiguration(t.Context(), s, d)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := m.Adapter(s.Backend)
	for _, step := range steps {
		actual, e := m.Read(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		if !Equivalent(Capture(actual, step.Selector), step.Before) {
			t.Fatalf("执行前状态不一致：%s\n%+v\n%+v", step.Name, Capture(actual, step.Selector), step.Before)
		}
		if e = a.Write(t.Context(), step, false); e != nil {
			t.Fatal(e)
		}
		actual, e = m.Read(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		if !Equivalent(Capture(actual, step.Selector), step.After) {
			t.Fatalf("读回不一致：%s\nactual=%+v\nafter=%+v", step.Name, Capture(actual, step.Selector), step.After)
		}
	}
	if reverse {
		for i := len(steps) - 1; i >= 0; i-- {
			if e = a.Write(t.Context(), steps[i], true); e != nil {
				t.Fatal(e)
			}
			actual, e := m.Read(t.Context())
			if e != nil {
				t.Fatal(e)
			}
			if !Equivalent(Capture(actual, steps[i].Selector), steps[i].Before) {
				t.Fatalf("撤销读回不一致：%s", steps[i].Name)
			}
		}
	} else {
		actual, e := m.Read(t.Context())
		if e != nil {
			t.Fatal(e)
		}
		again, e := m.PrepareConfiguration(t.Context(), actual, d)
		if e != nil || len(again) != 0 {
			t.Fatalf("相同配置仍需写入：%+v %v", again, e)
		}
	}
	return steps
}

func TestManagedUFWKeepsSystemRulesAndConvergesBothSides(t *testing.T) {
	h := ufwFixture(t)
	h.files["/etc/default/ufw"] = []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=DROP\n")
	h.files["/etc/ufw/user6.rules"] = nativeData(t, "ufw-user6.rules")
	runtime := map[string][]byte{"iptables-save": nativeData(t, "ufw-runtime.rules"), "ip6tables-save": nativeData(t, "ufw-runtime6.rules")}
	for name, data := range runtime {
		prefix := "ufw"
		if strings.HasPrefix(name, "ip6") {
			prefix = "ufw6"
		}
		runtime[name] = []byte(strings.Replace(string(data), "COMMIT", "-A "+prefix+"-skip-to-policy-input -j DROP\nCOMMIT", 1))
	}
	h.run = func(name string, args ...string) ([]byte, error) {
		if b, ok := runtime[name]; ok {
			return bytes.Clone(b), nil
		}
		return nil, fmt.Errorf("未知命令 %s", name)
	}
	wrapper := &managedFixture{nativeFixture: h}
	wrapper.input = func(b []byte, name string, args ...string) ([]byte, error) {
		if !slices.Equal(args, []string{"--noflush", "--wait", "5"}) {
			t.Fatalf("未限制事务范围：%s %v", name, args)
		}
		command := strings.Replace(name, "-restore", "-save", 1)
		chain := "ufw-user-input"
		if strings.HasPrefix(name, "ip6") {
			chain = "ufw6-user-input"
		}
		prefix := strings.TrimSuffix(chain, "-user-input")
		flush := map[string]bool{}
		policy := ""
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, ":INPUT ") {
				policy = line
			}
			if strings.HasPrefix(line, "-F ") {
				name := strings.TrimPrefix(line, "-F ")
				if name != chain && name != prefix+"-reject-input" && name != prefix+"-track-input" && name != prefix+"-skip-to-policy-input" {
					t.Fatalf("事务影响其他链：%s", b)
				}
				flush[name] = true
			}
		}
		out := []string{}
		for _, line := range strings.Split(string(runtime[command]), "\n") {
			parts := strings.Fields(line)
			if len(parts) > 1 && parts[0] == "-A" && flush[parts[1]] {
				continue
			}
			if policy != "" && strings.HasPrefix(line, ":INPUT ") {
				line = policy
			}
			if line == "COMMIT" {
				for _, r := range strings.Split(string(b), "\n") {
					if strings.HasPrefix(r, "-A ") {
						out = append(out, r)
					}
				}
			}
			out = append(out, line)
		}
		runtime[command] = []byte(strings.Join(out, "\n"))
		return nil, nil
	}
	m := &Manager{Host: wrapper}
	s, e := m.Read(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	d, e := ImportConfiguration(s)
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Rules) != 3 {
		t.Fatalf("导入了系统规则或漏掉用户规则：%+v", d.Rules)
	}
	for _, r := range d.Rules {
		if len(r.ID) > 64 || strings.Contains(r.ID, "/") {
			t.Fatal("仍使用原生表达式作为 ID")
		}
	}
	// 首次导入两侧一致时不产生写操作。
	if steps, e := m.PrepareConfiguration(t.Context(), s, d); e != nil || len(steps) != 0 {
		t.Fatalf("导入后不一致：%+v %v", steps, e)
	}
	base := d
	newRule := protocol.RuleSpec{Family: protocol.IPv4, Source: "198.51.100.0/24", Protocol: "any", Action: "allow", Comment: "用户规则"}
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "add_rule", Rule: &newRule})
	if e != nil {
		t.Fatal(e)
	}
	before4, before6 := bytes.Clone(runtime["iptables-save"]), bytes.Clone(runtime["ip6tables-save"])
	applyManaged(t, m, d, true)
	foreign := func(b []byte) string {
		out := []string{}
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(line, "-A ufw-user-input ") && !strings.HasPrefix(line, "-A ufw6-user-input ") {
				out = append(out, line)
			}
		}
		return strings.Join(out, "\n")
	}
	if foreign(runtime["iptables-save"]) != foreign(before4) || foreign(runtime["ip6tables-save"]) != foreign(before6) {
		t.Fatal("撤销未保留原始系统链")
	}
	applyManaged(t, m, d, false)
	newID := d.Rules[len(d.Rules)-1].ID
	newRule.Source = "203.0.113.10/32"
	newRule.Comment = "修改后的备注"
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "edit_rule", RuleID: newID, Rule: &newRule})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, false)
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "move_rule", RuleID: newID, BeforeRuleID: d.Rules[0].ID})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, false)
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "delete_rule", RuleID: newID})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, false)
	if len(d.Rules) != len(base.Rules) {
		t.Fatal("删除规则失败")
	}
	// 模拟命令行删除运行规则，永久配置仍在；后台必须能修复这类差异。
	lines := strings.Split(string(runtime["iptables-save"]), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "-A ufw-user-input ") {
			lines = slices.Delete(lines, i, i+1)
			break
		}
	}
	runtime["iptables-save"] = []byte(strings.Join(lines, "\n"))
	if len(applyManaged(t, m, d, false)) == 0 {
		t.Fatal("没有修复外部修改")
	}
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "set_default_policy", Action: "allow"})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, true)
	applyManaged(t, m, d, false)
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "set_default_policy", Action: "reject"})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, true)
	applyManaged(t, m, d, false)
}

func TestManagedFirewalldPortPairsPoliciesAndForeignObjects(t *testing.T) {
	h := &nativeFixture{files: map[string][]byte{}, paths: map[string]bool{"firewall-cmd": true}}
	zones := map[protocol.ConfigScope]string{protocol.Runtime: string(nativeData(t, "firewalld-zones.txt")), protocol.Permanent: string(nativeData(t, "firewalld-zones.txt"))}
	h.run = func(name string, args ...string) ([]byte, error) {
		if name != "firewall-cmd" {
			return nil, fmt.Errorf("未知命令 %s", name)
		}
		scope := protocol.Runtime
		if slices.Contains(args, "--permanent") {
			scope = protocol.Permanent
		}
		if slices.Contains(args, "--state") {
			return []byte("running"), nil
		}
		if slices.Contains(args, "--list-all-zones") {
			return []byte(zones[scope]), nil
		}
		if slices.Contains(args, "--reload") {
			zones[protocol.Runtime] = zones[protocol.Permanent]
			return nil, nil
		}
		lines := strings.Split(zones[scope], "\n")
		for _, arg := range args {
			switch {
			case strings.HasPrefix(arg, "--add-rich-rule="):
				zones[scope] += "\n    " + strings.TrimPrefix(arg, "--add-rich-rule=")
				return nil, nil
			case strings.HasPrefix(arg, "--remove-rich-rule="):
				expression := strings.TrimPrefix(arg, "--remove-rich-rule=")
				want, priority := parseRichRule(expression)
				found := false
				out := []string{}
				for _, line := range lines {
					actual, p := parseRichRule(strings.TrimSpace(line))
					if actual != nil && specEqual(actual, want) && samePriority(priority, p) {
						found = true
						continue
					}
					out = append(out, line)
				}
				if !found {
					return nil, fmt.Errorf("缺少 rich rule")
				}
				zones[scope] = strings.Join(out, "\n")
				return nil, nil
			case strings.HasPrefix(arg, "--remove-port="), strings.HasPrefix(arg, "--add-port="):
				port := strings.SplitN(arg, "=", 2)[1]
				for i, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "ports:") {
						ports := strings.Fields(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
						if strings.HasPrefix(arg, "--remove") {
							ports = slices.DeleteFunc(ports, func(s string) bool { return s == port })
						} else {
							ports = append(ports, port)
						}
						lines[i] = "  ports: " + strings.Join(ports, " ")
					}
				}
				zones[scope] = strings.Join(lines, "\n")
				return nil, nil
			case strings.HasPrefix(arg, "--set-target="):
				for i, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "target:") {
						lines[i] = "  target: " + strings.TrimPrefix(arg, "--set-target=")
					}
				}
				zones[scope] = strings.Join(lines, "\n")
				return nil, nil
			}
		}
		return nil, nil // 未修改的 policy、direct 及服务对象查询均为空。
	}
	m := &Manager{Host: h}
	s, e := m.Read(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	d, e := ImportConfiguration(s)
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Rules) != 3 {
		t.Fatalf("导入范围错误：%+v", d.Rules)
	}
	var port protocol.ManagedRule
	for _, r := range d.Rules {
		if r.NativeKind == "port" && r.Spec.Family == protocol.IPv4 {
			port = r
		}
	}
	changed := port.Spec
	changed.Source = "198.51.100.1/32"
	changed.Port = "8081"
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "edit_rule", RuleID: port.ID, Rule: &changed})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, true)
	applyManaged(t, m, d, false)
	if !strings.Contains(zones[protocol.Runtime], "services: ssh") || !strings.Contains(zones[protocol.Runtime], "source ipset=") {
		t.Fatal("覆盖了保留对象")
	}
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "set_default_policy", Zone: "public", Action: "deny"})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, true)
	applyManaged(t, m, d, false)
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "delete_rule", RuleID: port.ID})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, false)
	priority := 20
	id := d.Rules[0].ID
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "move_rule", RuleID: id, Priority: &priority})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, false)
}

func nftTestExpression(spec *protocol.RuleSpec) []json.RawMessage {
	items := []json.RawMessage{}
	match := func(left any, right any) {
		b, _ := json.Marshal(map[string]any{"match": map[string]any{"op": "==", "left": left, "right": right}})
		items = append(items, b)
	}
	match(map[string]any{"meta": map[string]string{"key": "nfproto"}}, string(spec.Family))
	ip := "ip"
	if spec.Family == protocol.IPv6 {
		ip = "ip6"
	}
	for field, value := range map[string]string{"saddr": spec.Source, "daddr": spec.Destination} {
		if value != "" && value != canonicalAddress("", spec.Family) {
			match(map[string]any{"payload": map[string]string{"protocol": ip, "field": field}}, value)
		}
	}
	if spec.Port != "" {
		n, _ := strconv.Atoi(spec.Port)
		match(map[string]any{"payload": map[string]string{"protocol": spec.Protocol, "field": "dport"}}, n)
	} else if spec.Protocol != "any" {
		match(map[string]any{"meta": map[string]string{"key": "l4proto"}}, spec.Protocol)
	}
	b, _ := json.Marshal(map[string]any{map[string]string{"allow": "accept", "deny": "drop", "reject": "reject"}[spec.Action]: nil})
	return append(items, b)
}

func TestManagedNFTReconcilesWithoutTouchingForeignChains(t *testing.T) {
	h := &nativeFixture{files: map[string][]byte{"/etc/nftables.conf": nativeData(t, "nftables.conf")}, paths: map[string]bool{"nft": true}}
	var doc nftDocument
	if e := json.Unmarshal(nativeData(t, "nftables.json"), &doc); e != nil {
		t.Fatal(e)
	}
	nextHandle := uint64(100)
	h.run = func(name string, args ...string) ([]byte, error) {
		if name == "systemctl" && args[0] == "is-active" {
			return []byte("active\n"), nil
		}
		if name == "systemctl" {
			return []byte("/usr/sbin/nft -f /etc/nftables.conf"), nil
		}
		if name == "nft" {
			return json.Marshal(doc)
		}
		return nil, fmt.Errorf("未知命令")
	}
	wrapper := &managedFixture{nativeFixture: h}
	wrapper.input = func(b []byte, name string, args ...string) ([]byte, error) {
		if name != "nft" || strings.Contains(string(b), "flush") {
			t.Fatalf("重建了外部规则：%s", b)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			words := strings.Fields(line)
			if len(words) < 5 {
				t.Fatal(line)
			}
			if words[0] == "delete" {
				handle, _ := strconv.ParseUint(words[len(words)-1], 10, 64)
				doc.NFTables = slices.DeleteFunc(doc.NFTables, func(v struct {
					Rule  *nftObject `json:"rule,omitempty"`
					Chain *nftObject `json:"chain,omitempty"`
				}) bool {
					return v.Rule != nil && v.Rule.Handle == handle
				})
				continue
			}
			if words[1] == "chain" {
				for _, v := range doc.NFTables {
					if v.Chain != nil && v.Chain.Name == words[4] {
						v.Chain.Policy = words[7][:len(words[7])-1]
					}
				}
				continue
			}
			start := 5
			index := len(doc.NFTables)
			if words[0] == "insert" {
				handle, _ := strconv.ParseUint(words[6], 10, 64)
				start = 7
				for i, v := range doc.NFTables {
					if v.Rule != nil && v.Rule.Handle == handle {
						index = i
					}
				}
			} else {
				for i, v := range doc.NFTables {
					if v.Rule != nil && v.Rule.Chain == words[4] {
						index = i + 1
					}
				}
			}
			spec := parseNFTText(strings.Join(words[start:], " "), words[2])
			if spec == nil {
				t.Fatalf("生成了不能解析的规则：%s", line)
			}
			nextHandle++
			entry := struct {
				Rule  *nftObject `json:"rule,omitempty"`
				Chain *nftObject `json:"chain,omitempty"`
			}{Rule: &nftObject{Family: words[2], Table: words[3], Chain: words[4], Handle: nextHandle, Expr: nftTestExpression(spec), Comment: spec.Comment}}
			doc.NFTables = slices.Insert(doc.NFTables, index, entry)
		}
		return nil, nil
	}
	m := &Manager{Host: wrapper}
	s, e := m.Read(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	d, e := ImportConfiguration(s)
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Rules) != 3 {
		t.Fatalf("导入了其他链：%+v", d.Rules)
	}
	newRule := protocol.RuleSpec{Family: protocol.IPv6, Source: "2001:db8::/64", Protocol: "tcp", Port: "8444", Action: "allow", Comment: "节点"}
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "add_rule", Chain: "inet/host/input", Rule: &newRule})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, true)
	applyManaged(t, m, d, false)
	_, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "move_rule", RuleID: d.Rules[len(d.Rules)-1].ID, BeforeRuleID: d.Rules[0].ID})
	if e == nil {
		t.Fatal("跨地址族排序未被拒绝")
	}
	priorityTarget := d.Rules[0]
	priorityTarget.Spec.Port = "2222"
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "edit_rule", RuleID: priorityTarget.ID, Rule: &priorityTarget.Spec})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, false)
	d, e = ChangeConfiguration(d, protocol.FirewallChange{Kind: "set_default_policy", Chain: "inet/host/input", Action: "allow"})
	if e != nil {
		t.Fatal(e)
	}
	applyManaged(t, m, d, true)
	applyManaged(t, m, d, false)
	for _, v := range doc.NFTables {
		if v.Rule != nil && v.Rule.Handle == 9 {
			return
		}
	}
	t.Fatal("Docker 链被修改")
}
