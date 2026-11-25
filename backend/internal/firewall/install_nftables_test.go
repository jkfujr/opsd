package firewall

import (
	"strings"
	"testing"
)

func TestNFTBootstrapKeepsTrustAndLimitsScope(t *testing.T) {
	trust := `meta nfproto ipv4 ip saddr 192.0.2.10/32 accept comment "opsd-peer-trust"`
	withTrust := strings.Replace(nftBase, "hook input priority filter;\n", "hook input priority filter;\n\t\t"+trust+"\n", 1)
	for _, input := range []string{"", "#!/usr/sbin/nft -f\nflush ruleset\n" + withTrust} {
		config, err := nftBootstrapConfig(input)
		if err != nil || strings.Contains(config, "flush ruleset") || !strings.HasPrefix(config, nftScope) ||
			strings.Count(config, "tcp dport 22 accept") != 2 || strings.Count(config, "tcp dport 8444 accept") != 2 ||
			strings.Count(config, "tcp dport 65533 accept") != 2 || !strings.Contains(config, "hook input priority filter; policy accept;") {
			t.Fatalf("生成配置不符合启用方案：%v\n%s", err, config)
		}
		if input != "" && !strings.Contains(config, trust) {
			t.Fatal("原有互信规则丢失")
		}
	}
	unsafe := []string{
		strings.Replace(nftBase, "hook input priority filter;", "hook input priority filter; policy drop;", 1),
		"include \"/etc/other.nft\"\n" + nftBase,
		strings.Replace(nftBase, "hook input priority filter;\n", "hook input priority filter;\n\t\tdrop\n", 1),
	}
	for _, input := range unsafe {
		if _, err := nftBootstrapConfig(input); err == nil {
			t.Fatalf("接受了自定义配置：%s", input)
		}
	}
}
