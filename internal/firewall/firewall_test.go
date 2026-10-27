package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/opsd-labs/opsd/internal/protocol"
)

type nativeFixture struct {
	files map[string][]byte
	paths map[string]bool
	run   func(string, ...string) ([]byte, error)
}

func (h *nativeFixture) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	return h.run(name, args...)
}
func (h *nativeFixture) Read(path string) ([]byte, error) {
	v, ok := h.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}
func (h *nativeFixture) Exists(path string) bool   { _, ok := h.files[path]; return ok }
func (h *nativeFixture) LookPath(name string) bool { return h.paths[name] }
func (h *nativeFixture) Replace(path string, before, after []byte) error {
	if !bytes.Equal(h.files[path], before) {
		return Conflict("夹具配置已改变")
	}
	h.files[path] = bytes.Clone(after)
	return nil
}
func nativeData(t *testing.T, name string) []byte {
	t.Helper()
	v, e := os.ReadFile("testdata/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func planFor(s protocol.FirewallState, c protocol.FirewallChange) protocol.ChangePlan {
	sel, _ := selectChange(s, c)
	v := Capture(s, sel)
	return protocol.ChangePlan{Backend: s.Backend, Change: c, Baseline: protocol.PlanBaseline{Rules: v.Rules, DefaultPolicies: v.Policies, ServiceActive: v.Enabled}}
}

func ufwFixture(t *testing.T) *nativeFixture {
	h := &nativeFixture{files: map[string][]byte{"/etc/ufw/ufw.conf": []byte("ENABLED=yes\n"), "/etc/default/ufw": []byte("IPV6=no\nDEFAULT_INPUT_POLICY=DROP\n"), "/etc/ufw/user.rules": nativeData(t, "ufw-user.rules")}}
	h.run = func(name string, args ...string) ([]byte, error) {
		if name == "iptables-save" {
			return nativeData(t, "ufw-runtime.rules"), nil
		}
		return nil, fmt.Errorf("夹具未支持命令 %s", name)
	}
	return h
}
func TestUFWNativeReadAndOldReference(t *testing.T) {
	h := ufwFixture(t)
	u := &UFW{Host: h}
	s, e := u.Read(t.Context())
	if e != nil || !s.Complete {
		t.Fatalf("读取失败：%+v %v", s, e)
	}
	if !ufwUserSidesAgree(s) {
		t.Fatal("相同的运行和永久规则应等价")
	}
	var old protocol.FirewallRule
	for _, r := range s.Rules {
		if r.Scope == protocol.Permanent && r.Spec != nil {
			old = r
			break
		}
	}
	change := protocol.FirewallChange{Kind: "delete_rule", RuleID: old.ID, Scope: protocol.Both}
	steps, e := u.Prepare(t.Context(), planFor(s, change))
	if e != nil || len(steps) != 1 || len(steps[0].After.Rules) != 2 {
		t.Fatalf("双侧单项删除错误：%+v %v", steps, e)
	}
	changed := strings.Replace(string(h.files["/etc/ufw/user.rules"]), "tcp 22", "tcp 2222", 1)
	h.files["/etc/ufw/user.rules"] = []byte(strings.Replace(changed, "--dport 22", "--dport 2222", 1))
	s, e = u.Read(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = selectChange(s, change); Code(e) != "conflict" {
		t.Fatalf("旧行号不能复用成新规则：%v", e)
	}
}
func TestUFWIPv6UsesActualNativeChains(t *testing.T) {
	h := ufwFixture(t)
	h.files["/etc/default/ufw"] = []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=DROP\n")
	h.files["/etc/ufw/user6.rules"] = nativeData(t, "ufw-user6.rules")
	previous := h.run
	h.run = func(name string, args ...string) ([]byte, error) {
		if name == "ip6tables-save" {
			return nativeData(t, "ufw-runtime6.rules"), nil
		}
		return previous(name, args...)
	}
	u := &UFW{Host: h}
	s, err := u.Read(t.Context())
	if err != nil || !s.Complete || !s.Capabilities.EditRules.Supported {
		t.Fatalf("双栈原生读取错误：%+v %v", s, err)
	}
	for _, rule := range s.Rules {
		if rule.Spec != nil && rule.Spec.Family == protocol.IPv6 && rule.Native.Chain != "ufw6-user-input" {
			t.Fatalf("IPv6 使用了错误的链：%+v", rule)
		}
	}
	r := protocol.RuleSpec{Family: protocol.IPv6, Source: "2001:db8:1::/48", Destination: "::/0", Protocol: "tcp", Port: "8443", Action: "allow"}
	c := protocol.FirewallChange{Kind: "add_rule", Scope: protocol.Both, Rule: &r}
	steps, err := u.Prepare(t.Context(), planFor(s, c))
	if err != nil || steps[0].Selector.Chain != "ufw6-user-input" || len(steps[0].Before.Rules) != 2 {
		t.Fatalf("IPv6 预览不能混入 IPv4：%+v %v", steps, err)
	}
}

func TestUFWGlobalEnableStaysReadonly(t *testing.T) {
	h := ufwFixture(t)
	u := &UFW{Host: h}
	s, _ := u.Read(t.Context())
	c := protocol.FirewallChange{Kind: "set_enabled", Enabled: pointer(false)}
	if _, e := u.Prepare(t.Context(), planFor(s, c)); e == nil || s.Capabilities.Enable.Supported {
		t.Fatal("尚不能完整恢复的全局启停必须保持只读")
	}
	if _, ok := parseIPTablesRule("-A ufw-user-input -s 192.0.2.0/24 -s 198.51.100.0/24 -j ACCEPT", protocol.IPv4); ok {
		t.Fatal("重复条件不可被简化")
	}
}

func TestUFWRejectsImplicitRuleReplacement(t *testing.T) {
	for _, field := range []string{"action", "comment"} {
		t.Run(field, func(t *testing.T) {
			u := &UFW{Host: ufwFixture(t)}
			s, err := u.Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var target protocol.RuleSpec
			for _, r := range s.Rules {
				if r.Scope == protocol.Permanent && r.Spec != nil {
					target = *r.Spec
					break
				}
			}
			if field == "action" {
				target.Action = "deny"
			} else {
				target.Comment = "修改备注"
			}
			change := protocol.FirewallChange{Kind: "add_rule", Scope: protocol.Both, Rule: &target}
			if _, err := u.Prepare(t.Context(), planFor(s, change)); Code(err) != "conflict" {
				t.Fatalf("新增不能隐式替换已有规则：%v", err)
			}
		})
	}
}

func TestUFWNativeCommandSyntax(t *testing.T) {
	h := ufwFixture(t)
	previous := h.run
	var command []string
	h.run = func(name string, args ...string) ([]byte, error) {
		if name == "ufw" {
			command = append([]string{}, args...)
			return nil, nil
		}
		return previous(name, args...)
	}
	rule := &protocol.RuleSpec{Family: protocol.IPv4, Source: "192.0.2.0/24", Protocol: "tcp", Port: "49153", Action: "allow"}
	step := Step{Forward: protocol.FirewallChange{Kind: "add_rule", Rule: rule}}
	if err := (&UFW{Host: h}).Write(t.Context(), step, false); err != nil {
		t.Fatal(err)
	}
	if len(command) == 0 || command[0] != "allow" {
		t.Fatalf("新增规则不接受 --force 前缀：%v", command)
	}
}

func TestUFWMetadataComments(t *testing.T) {
	h := ufwFixture(t)
	h.files["/etc/ufw/user.rules"] = []byte(strings.Replace(string(h.files["/etc/ufw/user.rules"]), "192.0.2.0/24 in\n", "192.0.2.0/24 in comment=6f7073642d636f6e736f6c65\n", 1))
	u := &UFW{Host: h}
	s, err := u.Read(t.Context())
	if err != nil || !s.Capabilities.EditRules.Supported {
		t.Fatalf("UFW 备注元数据不应造成两侧冲突：%+v %v", s.Capabilities.EditRules, err)
	}
	var old protocol.FirewallRule
	comments := 0
	for _, r := range s.Rules {
		if r.Spec != nil && r.Spec.Comment == "opsd-console" {
			comments++
			if r.Scope == protocol.Permanent {
				old = r
			}
		}
	}
	if comments != 2 {
		t.Fatal("备注应绑定到同一原生规则的运行与永久读数")
	}
	change := protocol.FirewallChange{Kind: "delete_rule", RuleID: old.ID, Scope: protocol.Both}
	steps, err := u.Prepare(t.Context(), planFor(s, change))
	if err != nil || len(steps) != 1 || len(steps[0].After.Rules) != 2 {
		t.Fatalf("带备注规则必须在两侧同时删除：%+v %v", steps, err)
	}
	previous := h.run
	h.run = func(name string, args ...string) ([]byte, error) {
		v, err := previous(name, args...)
		return []byte(strings.Replace(string(v), "--dport 22 -j", "--dport 22 -m comment --comment external -j", 1)), err
	}
	s, err = u.Read(t.Context())
	if err != nil || s.Capabilities.EditRules.Supported {
		t.Fatal("运行规则中的外部备注不能被永久元数据覆盖")
	}
}

func TestUFWDefaultInitTemplates(t *testing.T) {
	h := ufwFixture(t)
	for _, name := range []string{"before.init", "after.init"} {
		h.files["/etc/ufw/"+name] = []byte(fmt.Sprintf(`#!/bin/sh
# UFW 安装生成的空钩子模板。
set -e

case "$1" in
start)
    # 启动钩子为空。
    ;;
stop)
    ;;
status)
    ;;
flush-all)
    ;;
*)
    echo "'$1' not supported"
    echo "Usage: %s {start|stop|flush-all|status}"
    ;;
esac
`, name))
	}
	s, err := (&UFW{Host: h}).Read(t.Context())
	if err != nil || !s.Capabilities.EditRules.Supported {
		t.Fatalf("默认空钩子不应阻止编辑：%+v %v", s.Capabilities.EditRules, err)
	}
	h.files["/etc/ufw/before.init"] = []byte(strings.Replace(string(h.files["/etc/ufw/before.init"]), "start)\n", "start)\n    iptables -F\n", 1))
	s, err = (&UFW{Host: h}).Read(t.Context())
	if err != nil || s.Capabilities.EditRules.Supported {
		t.Fatalf("加入实际命令的钩子必须只读：%+v %v", s.Capabilities.EditRules, err)
	}
}

func TestUFWUnsafeUserConfigurationRemainsReadonly(t *testing.T) {
	for _, custom := range []string{"tuple", "raw", "script"} {
		t.Run(custom, func(t *testing.T) {
			h := ufwFixture(t)
			switch custom {
			case "tuple":
				h.files["/etc/ufw/user.rules"] = []byte(strings.Replace(string(h.files["/etc/ufw/user.rules"]), "--dport 22", "--dport 22 -m limit --limit 1/min", 1))
			case "raw":
				h.files["/etc/ufw/user.rules"] = append(h.files["/etc/ufw/user.rules"], []byte("\n-A ufw-user-input -m set --match-set external src -j DROP\n")...)
			case "script":
				h.files["/etc/ufw/before.init"] = []byte("#!/bin/sh\n执行外部逻辑\n")
			}
			s, err := (&UFW{Host: h}).Read(t.Context())
			if err != nil || !s.Complete {
				t.Fatalf("仍应能读取真实数据：%v", err)
			}
			if s.Capabilities.EditRules.Supported || s.Capabilities.DefaultPolicy.Supported || s.Capabilities.Enable.Supported {
				t.Fatal("无法无损重建的环境不能通过其他入口写入")
			}
			for _, r := range s.Rules {
				if r.Editable {
					t.Fatal("页面规则能力必须与执行能力一致")
				}
			}
			if custom == "tuple" {
				found := false
				for _, r := range s.Rules {
					if r.Scope == protocol.Permanent && strings.Contains(r.NativeExpression, "--limit 1/min") {
						found = true
					}
				}
				if !found {
					t.Fatal("复杂原生表达式不能在读取时被丢弃")
				}
			}
		})
	}
}
func TestFirewalldNativeScopesAndPorts(t *testing.T) {
	raw := nativeData(t, "firewalld-zones.txt")
	h := &nativeFixture{files: map[string][]byte{}, run: func(name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "--state" {
			return nil, nil
		}
		return raw, nil
	}}
	f := &Firewalld{Host: h}
	s, e := f.Read(t.Context())
	if e != nil || !s.Complete {
		t.Fatal(e)
	}
	var port protocol.FirewallRule
	readonly := false
	for _, r := range s.Rules {
		if r.Scope == protocol.Runtime && r.NativeExpression == "port:8080/tcp" && r.Spec.Family == protocol.IPv4 {
			port = r
		}
		if strings.Contains(r.NativeExpression, "ipset=") && !r.Editable {
			readonly = true
		}
	}
	if !readonly {
		t.Fatal("复杂原生规则应保持只读")
	}
	c := protocol.FirewallChange{Kind: "delete_rule", Scope: protocol.Both, RuleID: port.ID}
	steps, e := f.Prepare(t.Context(), planFor(s, c))
	if e != nil || len(steps) != 2 {
		t.Fatalf("双侧端口删除错误：%+v %v", steps, e)
	}
	for _, step := range steps {
		if step.Selector.Family != "" || len(step.Before.Rules)-len(step.After.Rules) != 2 {
			t.Fatal("简单端口必须展开双栈影响")
		}
	}
	policies := Capture(s, Selector{Kind: "policy", Zone: "public"})
	if len(policies.Policies) != 4 || policies.Policies[0].Action != "default" {
		t.Fatal("原生 default 不得映射成允许或拒绝")
	}
	h.run = func(name string, args ...string) ([]byte, error) {
		text := string(raw)
		if len(args) > 0 && args[0] == "--permanent" {
			text = strings.Replace(text, "services: ssh", "services: ssh https", 1)
		}
		return []byte(text), nil
	}
	if e = f.reloadSafe(t.Context(), "", false); Code(e) != "conflict" || !strings.Contains(e.Error(), "services:") {
		t.Fatalf("重载应指出具体临时差异：%v", e)
	}
}

func TestFirewalldConditionalReloadAndThirdValue(t *testing.T) {
	runtimeTarget, permanentTarget := "default", "default"
	failReload := true
	h := &nativeFixture{files: map[string][]byte{}}
	h.run = func(name string, args ...string) ([]byte, error) {
		permanent := false
		for _, arg := range args {
			if arg == "--permanent" {
				permanent = true
			}
			if strings.HasPrefix(arg, "--set-target=") {
				permanentTarget = strings.TrimPrefix(arg, "--set-target=")
				return nil, nil
			}
		}
		for _, arg := range args {
			switch arg {
			case "--state":
				return nil, nil
			case "--list-all-zones":
				target := runtimeTarget
				if permanent {
					target = permanentTarget
				}
				return []byte("public\n  target: " + target + "\n  rich rules:\n"), nil
			case "--reload":
				runtimeTarget = permanentTarget
				if failReload {
					failReload = false
					return nil, fmt.Errorf("重载已生效但应答失败")
				}
				return nil, nil
			}
		}
		return nil, nil
	}
	f := &Firewalld{Host: h}
	s, err := f.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := protocol.FirewallChange{Kind: "set_default_policy", Scope: protocol.Both, Family: protocol.IPv4, Zone: "public", Action: "deny", Reload: pointer(true)}
	steps, err := f.Prepare(t.Context(), planFor(s, c))
	if err != nil || len(steps) != 2 {
		t.Fatalf("区域目标须分开记录保存与重载：%+v %v", steps, err)
	}
	if err = f.Write(t.Context(), steps[0], false); err != nil {
		t.Fatal(err)
	}
	if runtimeTarget != "default" || permanentTarget != "DROP" {
		t.Fatal("保存永久配置不能伪称运行生效")
	}
	if err = f.Write(t.Context(), steps[1], false); err == nil {
		t.Fatal("部分失败不能假报命令成功")
	}
	s, err = f.Read(t.Context())
	if err != nil || !Equivalent(Capture(s, steps[1].Selector), steps[1].After) {
		t.Fatal("应答失败后应按实际读回判定副作用")
	}
	permanentTarget = "ACCEPT"
	if err = f.Write(t.Context(), steps[1], true); Code(err) != "conflict" || permanentTarget != "ACCEPT" {
		t.Fatalf("逆向重载不能覆盖外部第三值：%v", err)
	}
	permanentTarget = "DROP"
	if err = f.Write(t.Context(), steps[1], true); err != nil {
		t.Fatal(err)
	}
	if runtimeTarget != "default" || permanentTarget != "default" {
		t.Fatal("逆向重载未恢复本次区域目标")
	}
}

func nftFixture(t *testing.T) *nativeFixture {
	h := &nativeFixture{files: map[string][]byte{"/etc/nftables.conf": nativeData(t, "nftables.conf")}}
	h.run = func(name string, args ...string) ([]byte, error) {
		if name == "nft" {
			return nativeData(t, "nftables.json"), nil
		}
		if len(args) > 0 && args[0] == "show" {
			return []byte("/usr/sbin/nft -f /etc/nftables.conf"), nil
		}
		return []byte("active"), nil
	}
	return h
}
func TestNFTSourceMappingAndDirectedRestore(t *testing.T) {
	h := nftFixture(t)
	n := &NFT{Host: h}
	s, e := n.Read(t.Context())
	if e != nil || !s.Complete {
		t.Fatalf("读取失败：%+v %v", s, e)
	}
	var middle protocol.FirewallRule
	reachable := false
	for _, r := range s.Rules {
		if strings.Contains(r.Native.Chain, "docker") || strings.Contains(r.Native.Chain, "/nat/") {
			t.Fatal("主机页不能混入转发链或 NAT 链")
		}
		if strings.HasSuffix(r.Native.Chain, "/external") {
			reachable = true
			if r.Editable {
				t.Fatal("被引用的外部链保持只读")
			}
		}
		if r.Scope == protocol.Permanent && r.Spec != nil && r.Spec.Port == "443" {
			middle = r
		}
	}
	if !reachable || !middle.Editable {
		t.Fatal("入站可达链或持久映射丢失")
	}
	bothChange := protocol.FirewallChange{Kind: "delete_rule", RuleID: middle.ID, Scope: protocol.Both}
	bothSteps, err := n.Prepare(t.Context(), planFor(s, bothChange))
	if err != nil || len(bothSteps) != 2 || bothSteps[0].Source != nil || bothSteps[1].Source == nil {
		t.Fatalf("内核和文件必须分开记录：%+v %v", bothSteps, err)
	}
	c := protocol.FirewallChange{Kind: "delete_rule", RuleID: middle.ID, Scope: protocol.Permanent}
	steps, e := n.Prepare(t.Context(), planFor(s, c))
	if e != nil || len(steps) != 1 {
		t.Fatalf("准备失败：%v", e)
	}
	original := bytes.Clone(h.files["/etc/nftables.conf"])
	if e = n.Write(t.Context(), steps[0], false); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(h.files["/etc/nftables.conf"], []byte("443")) {
		t.Fatal("未移除目标片段")
	}
	if e = n.Write(t.Context(), steps[0], true); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(original, h.files["/etc/nftables.conf"]) {
		t.Fatal("中间规则撤销未恢复原有顺序")
	}
	c = protocol.FirewallChange{Kind: "set_default_policy", Chain: "inet/host/input", Family: protocol.IPv4, Scope: protocol.Permanent, Action: "allow"}
	p := planFor(s, c)
	if len(p.Baseline.DefaultPolicies) != 2 {
		t.Fatal("inet 基础链策略必须同时展示双栈")
	}
	if _, e = n.Prepare(t.Context(), p); e != nil {
		t.Fatal(e)
	}
}
func TestNFTUnsupportedExpressionsAndSources(t *testing.T) {
	for _, expression := range []string{`{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport","offset":2}},"right":22}}`, `{"accept":{"extra":true}}`, `{"counter":{}}`} {
		if r := parseNFTExpressions([]json.RawMessage{json.RawMessage(expression), json.RawMessage(`{"accept":null}`)}, "ip", ""); r != nil {
			t.Fatalf("未知语义不能变成可编辑规则：%s", expression)
		}
	}
	if parseNFTText("ip saddr 192.0.2.0/24 ip saddr 198.51.100.0/24 accept", "ip") != nil {
		t.Fatal("重复条件不能被简化")
	}
	h := nftFixture(t)
	h.files["/etc/nftables.conf"] = []byte("include \"/etc/nftables.d/*.nft\"\n")
	s, e := (&NFT{Host: h}).Read(t.Context())
	if e != nil || s.Complete || s.Capabilities.EditRules.Supported {
		t.Fatal("来源不明确必须拒绝修改")
	}
	h.files["/etc/nftables.conf"] = []byte(strings.Replace(string(nativeData(t, "nftables.conf")), "policy drop;", "policy drop; tcp dport 80 accept", 1))
	if _, e := (&NFT{Host: h}).sources(t.Context()); e == nil {
		t.Fatal("策略与规则共行不能替换整行")
	}
}

func TestInstallResumesAndPreservesExistingMask(t *testing.T) {
	for _, preMasked := range []bool{false, true} {
		t.Run(fmt.Sprint(preMasked), func(t *testing.T) {
			mode := "disabled"
			if preMasked {
				mode = "masked"
			}
			installed := false
			installs, unmasks := 0, 0
			interrupted := false
			h := &nativeFixture{files: map[string][]byte{"/etc/os-release": []byte("ID=debian\n")}, paths: map[string]bool{"apt-get": true}}
			h.run = func(name string, args ...string) ([]byte, error) {
				switch name {
				case "dpkg-query":
					if installed {
						return []byte("install ok installed"), nil
					}
					return nil, fmt.Errorf("尚未安装")
				case "apt-get":
					if mode != "masked" {
						t.Fatal("安装前未限制目标服务启动")
					}
					installs++
					installed = true
					h.files["/etc/ufw/ufw.conf"] = []byte("ENABLED=no\n")
					h.files["/etc/ufw/user.rules"] = []byte("*filter\nCOMMIT\n")
					return nil, nil
				case "systemctl":
					switch args[0] {
					case "is-active":
						return []byte("inactive"), fmt.Errorf("未启动")
					case "is-enabled":
						return []byte(mode), nil
					case "mask":
						mode = "masked"
					case "disable":
					case "unmask":
						mode = "disabled"
						unmasks++
					default:
						t.Fatalf("不允许的安装副作用：%v", args)
					}
					return nil, nil
				}
				t.Fatalf("不允许的命令：%s %v", name, args)
				return nil, nil
			}
			m := &Manager{Host: h}
			state := InstallState{Backend: "ufw"}
			persist := func() error {
				if state.Stage == "disabling" && !interrupted {
					interrupted = true
					return fmt.Errorf("模拟安装完成后进程中断")
				}
				return nil
			}
			if e := m.Install(t.Context(), &state, persist); e == nil {
				t.Fatal("中断不能算成功")
			}
			if e := m.Install(t.Context(), &state, persist); e != nil {
				t.Fatal(e)
			}
			if state.Stage != "complete" || installs != 1 {
				t.Fatalf("恢复不应重复安装：%+v 次数=%d", state, installs)
			}
			if preMasked && (mode != "masked" || unmasks != 0) {
				t.Fatal("不能解除外部预先设置的屏蔽")
			}
			if !preMasked && (mode != "disabled" || unmasks != 1) {
				t.Fatal("未移除本次安装建立的限制")
			}
		})
	}
}

func TestICMPv6NativeProtocol(t *testing.T) {
	for _, value := range []string{"58", "ipv6-icmp"} {
		r := parseNFTText("meta nfproto ipv6 meta l4proto "+value+" accept", "inet")
		if r == nil || r.Protocol != "icmpv6" {
			t.Fatalf("nftables 原生 IPv6 ICMP 无法读取：%s", value)
		}
		if got := nftExpression(r); got != "meta nfproto ipv6 meta l4proto 58 accept" {
			t.Fatalf("nftables 原生协议编号错误：%s", got)
		}
		r, _ = parseRichRule(`rule family="ipv6" protocol value="` + value + `" accept`)
		if r == nil || r.Protocol != "icmpv6" {
			t.Fatalf("firewalld 原生 IPv6 ICMP 无法读取：%s", value)
		}
		got, err := richExpression(r, nil)
		if err != nil || !strings.Contains(got, `protocol value="58"`) {
			t.Fatalf("firewalld 原生协议编号错误：%s %v", got, err)
		}
	}
}

func TestInterruptedMaskIntentDoesNotClaimExistingMask(t *testing.T) {
	h := &nativeFixture{files: map[string][]byte{"/etc/os-release": []byte("ID=debian\n")}, paths: map[string]bool{"apt-get": true}}
	h.run = func(name string, args ...string) ([]byte, error) {
		if name == "systemctl" && args[0] == "is-active" {
			return []byte("inactive"), fmt.Errorf("停用")
		}
		if name == "systemctl" && args[0] == "is-enabled" {
			return []byte("masked"), nil
		}
		t.Fatalf("归属不明时不能继续副作用：%s %v", name, args)
		return nil, nil
	}
	state := InstallState{Backend: "ufw", Stage: "mask_intent", MaskOwned: true}
	if err := (&Manager{Host: h}).Install(t.Context(), &state, func() error { return nil }); Code(err) != "conflict" {
		t.Fatalf("不能把中断窗口中的同值屏蔽当作自己所有：%v", err)
	}
}
