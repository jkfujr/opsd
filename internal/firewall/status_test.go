package firewall

import "testing"

func TestUFWSystemDefaultDoesNotHideCustomRules(t *testing.T) {
	for _, line := range []string{"-A INPUT -j ufw-before-input", "-A ufw-before-input -i lo -j ACCEPT", "-A ufw-before-input -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT"} {
		if !ufwSystemDefault(line, "ufw") {
			t.Fatalf("默认规则未识别：%s", line)
		}
	}
	for _, line := range []string{"-A INPUT -j CODEX_GALERA", "-A ufw-before-input -s 192.0.2.1/32 -j ACCEPT", "-A ufw-user-input -p tcp --dport 22 -j ACCEPT", "-A INPUT -j ufw-before-input -m comment --comment custom"} {
		if ufwSystemDefault(line, "ufw") {
			t.Fatalf("自定义规则被隐藏：%s", line)
		}
	}
	rules, _, _ := parseUFWRuntime("-A INPUT -j ufw-before-input\n-A INPUT -j CODEX_GALERA\n-A ufw-user-input -s 192.0.2.1/32 -j ACCEPT", "ipv4")
	if len(rules) != 3 || !rules[0].SystemDefault || rules[1].SystemDefault || rules[2].SystemDefault {
		t.Fatal("默认规则分类没有保留完整原生列表")
	}
}
