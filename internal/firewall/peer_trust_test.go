package firewall

import (
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

func TestUFWTrustRejectsForeignConditions(t *testing.T) {
	base := "*filter\n:ufw-before-input - [0:0]\n-A ufw-before-input -j ACCEPT\nCOMMIT\n"
	text, e := ufwTrustFile(base, protocol.IPv4, []string{"8.8.8.8"})
	if e != nil {
		t.Fatal(e)
	}
	for _, changed := range []string{
		strings.Replace(text, "-s 8.8.8.8 -j ACCEPT", "-s 8.8.8.8 -j DROP", 1),
		strings.Replace(text, "-s 8.8.8.8 -j ACCEPT", "-s 8.8.8.8/24 -j ACCEPT", 1),
		strings.Replace(text, "-s 8.8.8.8 -j ACCEPT", "-s 8.8.8.8 -p tcp -j ACCEPT", 1),
	} {
		if _, e := ufwTrustFile(changed, protocol.IPv4, nil); Code(e) != "conflict" {
			t.Fatalf("外部内容被覆盖：%v", e)
		}
	}
	cleared, e := ufwTrustFile(text, protocol.IPv4, nil)
	if e != nil || cleared != base {
		t.Fatalf("撤除改变了原配置：%v %s", e, cleared)
	}
}

func TestNFTTrustPreservedByOrdinaryConfiguration(t *testing.T) {
	base := "table inet filter {\n chain input {\n  type filter hook input priority 0; policy drop;\n  meta nfproto ipv4 tcp dport 22 accept\n }\n}\n"
	target := protocol.PeerTrustTarget{Enabled: true, IPv4: []string{"8.8.8.8"}}
	text, e := nftTrustFile(base, "inet/filter/input", target)
	if e != nil {
		t.Fatal(e)
	}
	text, e = nftConfiguredFile(text, "inet/filter/input", nil, "deny")
	if e != nil || !strings.Contains(text, trustMarker) || strings.Contains(text, "tcp dport 22") {
		t.Fatalf("普通配置改变互信：%v %s", e, text)
	}
	text, e = nftTrustFile(text, "inet/filter/input", protocol.PeerTrustTarget{})
	if e != nil || strings.Contains(text, trustMarker) {
		t.Fatalf("互信未撤除：%v %s", e, text)
	}
}
