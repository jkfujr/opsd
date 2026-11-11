package firewall

import (
	"slices"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

func migrationCoverage(target protocol.PeerTrustTarget) protocol.PeerTrustUpdated {
	s := protocol.PeerTrustUpdated{TargetRevision: target.Revision, AppliedRevision: target.Revision}
	for _, family := range []protocol.AddressFamily{protocol.IPv4, protocol.IPv6} {
		for _, scope := range []protocol.ConfigScope{protocol.Permanent, protocol.Runtime} {
			s.Results = append(s.Results, trustResult(family, scope, trustList(target, family), nil))
		}
	}
	return s
}

func TestPeerTrustMigrationSelectsOnlyEquivalentManagedRules(t *testing.T) {
	for _, backend := range []protocol.Backend{"ufw", "nftables", "firewalld"} {
		t.Run(string(backend), func(t *testing.T) {
			d := protocol.FirewallDefinition{Backend: backend, Rules: []protocol.ManagedRule{}}
			base := protocol.RuleSpec{Family: protocol.IPv4, Source: "8.8.8.8/32", Destination: "0.0.0.0/0", Protocol: "any", Action: "allow"}
			for _, name := range []string{"exact", "plain", "network", "port", "destination", "deny", "protocol", "unrelated", "ipv6"} {
				s := base
				switch name {
				case "plain":
					s.Source = "8.8.8.8"
				case "network":
					s.Source = "8.8.8.0/24"
				case "port":
					s.Protocol, s.Port = "tcp", "22"
				case "destination":
					s.Destination = "9.9.9.9/32"
				case "deny":
					s.Action = "deny"
				case "protocol":
					s.Protocol = "udp"
				case "unrelated":
					s.Source = "1.1.1.1/32"
				case "ipv6":
					s.Family, s.Source, s.Destination = protocol.IPv6, "2606:4700:4700::1111/128", "::/0"
				}
				d.Rules = append(d.Rules, protocol.ManagedRule{ID: name, Spec: s})
			}
			target := protocol.PeerTrustTarget{Enabled: true, Revision: 3, IPv4: []string{"8.8.8.8"}, IPv6: []string{"2606:4700:4700::1111"}}
			ids := PeerTrustRuleIDs(d, target)
			if !slices.Equal(ids, []string{"exact", "ipv6", "plain"}) {
				t.Fatal(ids)
			}
			got, err := MigratePeerTrustRules(d, target, ids)
			if err != nil || len(got.Rules) != 6 || len(d.Rules) != 9 {
				t.Fatal(got, err)
			}
			for _, r := range got.Rules {
				if slices.Contains(ids, r.ID) {
					t.Fatal("已迁移规则仍在普通配置", r)
				}
			}
			target.Enabled = false
			if len(PeerTrustRuleIDs(d, target)) != 0 {
				t.Fatal("关闭互信后仍接管普通规则")
			}
		})
	}
}

func TestPeerTrustMigrationRequiresCurrentCoverageAndExactRemoval(t *testing.T) {
	target := protocol.PeerTrustTarget{Enabled: true, Revision: 4, IPv4: []string{"8.8.8.8"}}
	before := protocol.FirewallTarget{Revision: 6, Configuration: protocol.FirewallDefinition{Backend: "ufw", Rules: []protocol.ManagedRule{{ID: "self", Spec: protocol.RuleSpec{Family: protocol.IPv4, Source: "8.8.8.8/32", Protocol: "any", Action: "allow"}}}, DefaultPolicies: []protocol.ManagedPolicy{{Family: protocol.IPv4, Action: "deny"}}}}
	d, err := MigratePeerTrustRules(before.Configuration, target, []string{"self"})
	if err != nil {
		t.Fatal(err)
	}
	after := protocol.FirewallTarget{Revision: 7, Configuration: d, PeerTrustMigration: &protocol.PeerTrustMigration{RequiredRevision: 4, RuleIDs: []string{"self"}}}
	for _, mode := range []string{"valid", "stale", "disabled", "unapplied", "missing_runtime", "extra_change", "duplicate", "automatic"} {
		t.Run(mode, func(t *testing.T) {
			a := after
			a.PeerTrustMigration = &protocol.PeerTrustMigration{RequiredRevision: 4, RuleIDs: []string{"self"}}
			a.Configuration.DefaultPolicies = slices.Clone(d.DefaultPolicies)
			p, status := target, migrationCoverage(target)
			switch mode {
			case "stale":
				p.Revision++
			case "disabled":
				p.Enabled = false
			case "unapplied":
				status.AppliedRevision = 0
			case "missing_runtime":
				status.Results = status.Results[:1]
			case "extra_change":
				a.Configuration.DefaultPolicies[0].Action = "allow"
			case "duplicate":
				a.PeerTrustMigration.RuleIDs = []string{"self", "self"}
			case "automatic":
				a.Automatic = true
			}
			err := ValidatePeerTrustMigration(before, a, p, status)
			if (err == nil) != (mode == "valid") {
				t.Fatal(mode, err)
			}
		})
	}
}
