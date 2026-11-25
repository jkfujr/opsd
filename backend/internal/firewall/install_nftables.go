package firewall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkfujr/opsd/internal/protocol"
)

const (
	nftUnit   = "nftables.service"
	nftConfig = "/etc/nftables.conf"
	nftDropIn = "/etc/systemd/system/nftables.service.d/50-opsd-scope.conf"
	nftStop   = "[Service]\nExecStop=\nExecStop=-/usr/sbin/nft delete table inet filter\n"
	nftScope  = "add table inet filter\ndelete table inet filter\n"
	nftInput  = "inet/filter/input"
	nftBase   = `table inet filter {
	chain input {
		type filter hook input priority filter;
	}
	chain forward {
		type filter hook forward priority filter;
	}
	chain output {
		type filter hook output priority filter;
	}
}
`
)

func (m *Manager) EnableNFT(ctx context.Context, state *InstallState, persist func() error) error {
	if state.Backend != "nftables" || !m.installable() {
		return Unsupported("此后端需要在节点上启用")
	}
	if state.Stage == "complete" || state.Stage == "nft_enabled" {
		return nil
	}
	set := func(stage string) error { state.Stage = stage; return persist() }
	active := func() bool {
		b, err := m.Host.Run(ctx, "systemctl", "is-active", nftUnit)
		return err == nil && strings.TrimSpace(string(b)) == "active"
	}
	if state.Stage == "" && active() {
		return set("complete")
	}
	backend, err := m.Detect(ctx)
	if err != nil {
		return err
	}
	if backend != "none" && backend != "nftables" {
		return Conflict("当前有其他防火墙后端，需先在节点上处理")
	}
	b, err := m.Host.Run(ctx, "dpkg-query", "-W", "-f=${Status}", "nftables")
	if err != nil || strings.TrimSpace(string(b)) != "install ok installed" {
		return Conflict("请先安装 nftables")
	}
	if state.Stage == "" {
		if err = m.checkNFTUnit(ctx, false); err != nil {
			return err
		}
		if m.Host.Exists(nftDropIn) {
			return Conflict("已有 nftables 服务覆盖配置，请在节点上核对")
		}
		b, err := m.Host.Read(nftConfig)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		next, err := nftBootstrapConfig(string(b))
		if err != nil {
			return err
		}
		state.NFTSource = &SourceEdit{Path: nftConfig, Before: string(b), After: next}
		if err = set("nft_enable_prepared"); err != nil {
			return err
		}
	}
	if state.Stage != "nft_enable_prepared" || state.NFTSource == nil || state.NFTSource.Path != nftConfig {
		return Conflict("启用阶段无法核对")
	}
	config := state.NFTSource
	if !active() {
		tables, err := m.Host.Run(ctx, "nft", "list", "tables")
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(tables), "\n") {
			if strings.TrimSpace(line) == "table inet filter" {
				return Conflict("inet filter 已存在于运行规则，请先核对归属")
			}
		}
		h, ok := m.Host.(inputHost)
		if !ok {
			return Unsupported("主机不支持原生规则输入")
		}
		if _, err = h.RunInput(ctx, []byte(config.After), "nft", "-c", "-f", "-"); err != nil {
			return err
		}
		if err = m.putNFTFile(nftConfig, config.Before, config.After); err != nil {
			return err
		}
		if err = m.putNFTFile(nftDropIn, "", nftStop); err != nil {
			return err
		}
		if _, err = m.Host.Run(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if err = m.checkNFTUnit(ctx, true); err != nil {
		return err
	}
	if contents, err := m.Host.Read(nftConfig); err != nil || string(contents) != config.After {
		return Conflict("nftables 加载配置发生变化")
	}
	if _, err = m.Host.Run(ctx, "systemctl", "enable", "--now", nftUnit); err != nil {
		return err
	}
	return set("nft_enabled")
}

func (m *Manager) checkNFTUnit(ctx context.Context, scoped bool) error {
	b, err := m.Host.Run(ctx, "systemctl", "show", nftUnit, "-p", "DropInPaths", "-p", "ExecStart", "-p", "ExecReload", "-p", "ExecStop",
		"-p", "ExecCondition", "-p", "ExecStartPre", "-p", "ExecStartPost", "-p", "ExecStopPost", "-p", "UnitFileState")
	if err != nil {
		return err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, _ := strings.Cut(line, "=")
		values[key] = value
	}
	wantPath, stop := "", "flush ruleset"
	if scoped {
		wantPath, stop = nftDropIn, "delete table inet filter"
	}
	mode := values["UnitFileState"]
	if values["DropInPaths"] != wantPath || values["ExecStartPre"] != "" || values["ExecStartPost"] != "" || values["ExecStopPost"] != "" ||
		values["ExecCondition"] != "" || (mode != "disabled" && !(scoped && mode == "enabled")) {
		return Conflict("nftables 服务配置不是可安全启用的停用状态，请在节点上处理")
	}
	for name, args := range map[string]string{"ExecStart": "-f " + nftConfig, "ExecReload": "-f " + nftConfig, "ExecStop": stop} {
		value := values[name]
		if strings.Count(value, "argv[]=") != 1 || !strings.Contains(value, "argv[]=/usr/sbin/nft "+args+" ;") {
			return Conflict("nftables 服务命令与受支持配置不符，请在节点上处理")
		}
	}
	return nil
}

func nftBootstrapConfig(text string) (string, error) {
	// 仅启用发行版空规则或已有 OPSD 互信的同类配置，自定义策略留给节点管理员。
	stock := []string{}
	lines := []string{}
	inInput := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "" || strings.HasPrefix(t, "#"):
		case t == "flush ruleset":
			continue
		case strings.HasPrefix(t, "meta nfproto ") && inInput:
			rule := parseNFTText(t, "inet")
			if rule == nil || rule.Action != "allow" || rule.Protocol != "any" || rule.Source == "" || rule.Comment != trustMarker {
				return "", Conflict("持久入站链包含自定义规则，请在节点上启用")
			}
		default:
			inInput = t == "chain input {" || inInput && t != "}"
			stock = append(stock, strings.Join(strings.Fields(t), " "))
		}
		lines = append(lines, line)
	}
	clean := strings.Join(lines, "\n")
	normalized := strings.Join(stock, " ")
	if normalized == "" {
		clean += "\n" + nftBase
	} else if strings.Join(strings.Fields(strings.ReplaceAll(normalized, "; policy accept;", ";")), " ") != strings.Join(strings.Fields(nftBase), " ") {
		return "", Conflict("持久 nftables 配置包含自定义链或策略，请在节点上启用")
	}
	config, err := nftConfiguredFile(clean, nftInput, nftManagementRules(), "allow")
	return nftScope + config, err
}

func nftManagementRules() []protocol.FirewallRule {
	rules := []protocol.FirewallRule{}
	for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		for _, port := range []string{"22", "8444", "65533"} {
			spec := protocol.RuleSpec{Family: family, Protocol: "tcp", Port: port, Action: "allow", Comment: "OPSD 管理通道"}
			rules = append(rules, protocol.FirewallRule{Spec: &spec})
		}
	}
	return rules
}

func ImportNFTBootstrap(s protocol.FirewallState) (protocol.FirewallDefinition, error) {
	if s.Backend != "nftables" || s.ServiceActive == nil || !*s.ServiceActive {
		return protocol.FirewallDefinition{}, Conflict("nftables 服务尚未运行")
	}
	policies := []protocol.DefaultPolicy{}
	for _, policy := range s.DefaultPolicies {
		if policy.Chain == nftInput {
			policies = append(policies, policy)
		}
	}
	s.DefaultPolicies = policies
	d, err := ImportConfiguration(s)
	if err != nil {
		return d, err
	}
	want := nftManagementRules()
	if len(d.DefaultPolicies) != 2 || len(d.Rules) != len(want) {
		return d, Conflict("未读到预期的入站链及管理通道")
	}
	for _, policy := range d.DefaultPolicies {
		if policy.Action != "allow" {
			return d, Conflict("入站链或默认策略与启用方案不同")
		}
	}
	for i, rule := range d.Rules {
		if !specEqual(&rule.Spec, want[i].Spec) {
			return d, Conflict("管理通道与启用方案不同")
		}
	}
	return d, nil
}

func (m *Manager) putNFTFile(path, before, after string) error {
	current, err := m.Host.Read(path)
	switch {
	case err == nil && string(current) == after:
		return nil
	case err == nil && path == nftConfig:
		return m.Host.Replace(path, []byte(before), []byte(after))
	case errors.Is(err, os.ErrNotExist) && before == "":
		h, ok := m.Host.(interface{ Create(string, []byte) error })
		if !ok {
			return Unsupported("主机不支持安全创建 nftables 配置")
		}
		return h.Create(path, []byte(after))
	default:
		return Conflict("nftables 配置文件发生变化")
	}
}

func (NativeHost) Create(path string, contents []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".opsd-nft-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(contents); err == nil {
		err = f.Chmod(0644)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(f.Name(), path); err != nil {
		return fmt.Errorf("创建 %s 失败：%w", path, err)
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
