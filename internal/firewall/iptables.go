package firewall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

// Iptables 保留外部规则归属，普通规则只读；互信只维护所属链和 INPUT 引用。
type Iptables struct{ Host Host }

func (a *Iptables) Read(ctx context.Context) (protocol.FirewallState, error) {
	s := emptyState("iptables")
	active := false
	for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		save, _ := iptablesCommands(family)
		if !a.Host.LookPath(save) {
			continue
		}
		b, err := a.Host.Run(ctx, save, "-t", "filter")
		if err != nil {
			reason(&s, "runtime/"+string(family), err)
			continue
		}
		positions := map[string]int{}
		for _, line := range strings.Split(string(b), "\n") {
			fields, err := splitWords(line)
			if err != nil || len(fields) < 3 || fields[0] != "-A" {
				continue
			}
			chain := fields[1]
			if chain != "INPUT" && !strings.HasPrefix(chain, "1PANEL_") && chain != trustChain && !strings.HasPrefix(chain, "OPSD_INPUT") {
				continue
			}
			active = true
			positions[chain]++
			spec, _ := parseIPTablesRule(line, family)
			s.Rules = append(s.Rules, protocol.FirewallRule{ID: fmt.Sprintf("%s/%s/%d", family, chain, positions[chain]), Spec: spec, Scope: protocol.Runtime, Native: protocol.NativePosition{Chain: chain, Position: pointer(positions[chain])}, NativeExpression: line, Origin: "native", ReadonlyReason: "原生规则由现有配置管理"})
		}
	}
	s.ServiceActive = &active
	s.Capabilities.Read = allow()
	s.Capabilities.EditRules = deny("原生规则由现有配置管理")
	s.Capabilities.Reorder = s.Capabilities.EditRules
	s.Capabilities.DefaultPolicy = s.Capabilities.EditRules
	s.Capabilities.Enable = s.Capabilities.EditRules
	bindRuleReferences(&s)
	return s, nil
}
func (*Iptables) Prepare(context.Context, protocol.ChangePlan) ([]Step, error) {
	return nil, Unsupported("原生 iptables 普通规则只读")
}
func (*Iptables) Write(context.Context, Step, bool) error {
	return Unsupported("原生 iptables 普通规则只读")
}

func iptablesCommands(family protocol.AddressFamily) (string, string) {
	if family == protocol.IPv6 {
		return "ip6tables-save", "ip6tables-restore"
	}
	return "iptables-save", "iptables-restore"
}

func nativeTrustHooksCurrent(text string, hooks []string) bool {
	if len(hooks) != 1 {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "-A INPUT ") {
			return line == "-A INPUT -m comment --comment "+trustMarker+" -j "+trustChain
		}
	}
	return false
}

func nativeTrustConfig(addresses []string) string {
	lines := []string{"# " + trustMarker, "*filter"}
	if len(addresses) > 0 {
		lines = append(lines, ":"+trustChain+" - [0:0]", "-A INPUT -m comment --comment "+trustMarker+" -j "+trustChain)
		for _, ip := range addresses {
			lines = append(lines, "-A "+trustChain+" -s "+ip+" -j ACCEPT")
		}
	}
	return strings.Join(append(lines, "COMMIT", ""), "\n")
}

func nativeTrustOwned(text string, family protocol.AddressFamily) bool {
	if !strings.HasPrefix(text, "# "+trustMarker+"\n") {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		if line == "" || line == "# "+trustMarker || line == "*filter" || line == "COMMIT" || line == ":"+trustChain+" - [0:0]" {
			continue
		}
		if !strings.HasPrefix(line, "-A INPUT ") && !strings.HasPrefix(line, "-A "+trustChain+" ") {
			return false
		}
		if strings.HasPrefix(line, "-A INPUT ") && line != "-A INPUT -m comment --comment "+trustMarker+" -j "+trustChain {
			return false
		}
	}
	_, _, _, err := readUFWTrustRuntime(text, family)
	return err == nil
}

func syncIptablesTrust(ctx context.Context, h Host, target protocol.PeerTrustTarget) []protocol.ScopeResult {
	out := []protocol.ScopeResult{}
	for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		wanted := trustList(target, family)
		save, restore := iptablesCommands(family)
		if !h.LookPath(save) || !h.LookPath(restore) {
			if len(wanted) == 0 {
				out = append(out, trustResult(family, protocol.Permanent, nil, nil), trustResult(family, protocol.Runtime, nil, nil))
				continue
			}
			out = append(out, trustResult(family, protocol.Permanent, nil, Unsupported("缺少原生规则命令")), trustResult(family, protocol.Runtime, nil, Unsupported("缺少原生规则命令")))
			continue
		}
		path := "/etc/opsd/firewall/peer-trust." + string(family) + ".rules"
		next := []byte(nativeTrustConfig(wanted))
		before, err := h.Read(path)
		if err == nil && !nativeTrustOwned(string(before), family) {
			err = Conflict("互信持久路径含外部内容")
		}
		if os.IsNotExist(err) {
			if creator, ok := h.(interface{ CreateOwned(string, []byte) error }); ok {
				err = creator.CreateOwned(path, next)
			} else {
				err = h.Replace(path, nil, next)
			}
		} else if err == nil && string(before) != string(next) {
			err = h.Replace(path, before, next)
		}
		if err == nil {
			var read []byte
			read, err = h.Read(path)
			if err == nil && string(read) != string(next) {
				err = Conflict("互信持久配置读回不一致")
			}
		}
		permanent := []string{}
		if err == nil {
			permanent = append(permanent, wanted...)
		}
		out = append(out, trustResult(family, protocol.Permanent, permanent, err))
		raw, readErr := h.Run(ctx, save, "-t", "filter")
		if readErr != nil {
			out = append(out, trustResult(family, protocol.Runtime, nil, readErr))
			continue
		}
		actual, hooks, exists, readErr := readUFWTrustRuntime(string(raw), family)
		for _, hook := range hooks {
			if !strings.HasPrefix(hook, "-A INPUT ") {
				readErr = Conflict("原生互信链存在外部引用")
			}
		}
		if readErr == nil && err != nil {
			readErr = err
		}
		current := func() bool {
			if len(wanted) == 0 {
				return !exists && len(hooks) == 0
			}
			return slices.Equal(actual, wanted) && nativeTrustHooksCurrent(string(raw), hooks)
		}
		if readErr == nil && !current() {
			commands := []string{"*filter"}
			if len(wanted) > 0 && !exists {
				commands = append(commands, ":"+trustChain+" - [0:0]")
			}
			if exists {
				commands = append(commands, "-F "+trustChain)
			}
			for _, hook := range hooks {
				commands = append(commands, strings.Replace(hook, "-A ", "-D ", 1))
			}
			if len(wanted) > 0 {
				commands = append(commands, "-I INPUT 1 -m comment --comment "+trustMarker+" -j "+trustChain)
				for _, ip := range wanted {
					commands = append(commands, "-A "+trustChain+" -s "+ip+" -j ACCEPT")
				}
			} else if exists {
				commands = append(commands, "-X "+trustChain)
			}
			readErr = inputRules(h, ctx, strings.Join(append(commands, "COMMIT", ""), "\n"), restore, "--noflush", "--wait", "5")
			if readErr == nil {
				raw, readErr = h.Run(ctx, save, "-t", "filter")
				if readErr == nil {
					actual, hooks, exists, readErr = readUFWTrustRuntime(string(raw), family)
					if readErr == nil && !current() {
						readErr = Conflict("互信运行规则读回不一致")
					}
				}
			}
		}
		out = append(out, trustResult(family, protocol.Runtime, actual, readErr))
	}
	return out
}

// 新建仅用于 OPSD 自己的持久配置，已存在文件仍通过比较后原子替换。
func (NativeHost) CreateOwned(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".opsd-new-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
