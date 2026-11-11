package cluster

import (
	"encoding/json"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

func migrationNode(t *testing.T) (*Node, *configExecutor) {
	t.Helper()
	n, _ := openTestNode(t)
	manualAddress(t, n, "8.8.8.8")
	if _, err := n.ChangeTrust(t.Context(), TrustPolicyPatch{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	d := protocol.FirewallDefinition{Backend: "ufw", Rules: []protocol.ManagedRule{
		{ID: "self", Chain: "ufw-user-input", Spec: protocol.RuleSpec{Family: protocol.IPv4, Source: "8.8.8.8/32", Protocol: "any", Action: "allow"}},
		{ID: "network", Chain: "ufw-user-input", Spec: protocol.RuleSpec{Family: protocol.IPv4, Source: "100.64.0.0/10", Protocol: "any", Action: "allow"}},
	}, DefaultPolicies: []protocol.ManagedPolicy{{Family: protocol.IPv4, Action: "deny"}}}
	x := &configExecutor{inspection: protocol.FirewallInspected{Target: &protocol.FirewallTarget{Revision: 1, Configuration: d}, Backend: "ufw", InSync: true}}
	n.Executor = x
	n.checkFirewall(t.Context())
	target := protocol.PeerTrustTarget{Revision: 1, Enabled: true, IPv4: []string{"8.8.8.8"}, IPv6: []string{}}
	status := protocol.PeerTrustUpdated{TargetRevision: 1, AppliedRevision: 1}
	for _, scope := range []protocol.ConfigScope{protocol.Permanent, protocol.Runtime} {
		status.Results = append(status.Results, protocol.ScopeResult{Family: protocol.IPv4, Component: "host", Scope: scope, State: "in_sync", Addresses: target.IPv4})
	}
	for table, value := range map[string]any{"peer_trust_target": target, "peer_trust_status": status} {
		b, _ := json.Marshal(value)
		if _, err := n.DB.Exec("INSERT INTO "+table+"(id,body) VALUES(1,?)", string(b)); err != nil {
			t.Fatal(err)
		}
	}
	return n, x
}

func TestTrustMigrationPersistsUntilSuccessAndRetriesExplicitly(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure_retry"}[fail], func(t *testing.T) {
			n, x := migrationNode(t)
			x.fail = fail
			n.migrateTrust(t.Context())
			pending, _ := n.FirewallConfig(t.Context())
			if len(pending.Rules) != 2 || pending.Revision != 1 || pending.Migration == nil || pending.Migration.State != "pending" {
				t.Fatal(pending)
			}
			if _, err := n.writeFirewall(t.Context(), n.ID, FirewallWrite{ExpectedRevision: 1, Key: "concurrent", Change: &protocol.FirewallChange{Kind: "delete_rule", RuleID: "network"}}, false); err == nil {
				t.Fatal("迁移与普通写入交叉")
			}
			n.advance() // 执行器已受理，提交应答丢失。
			n.advance() // 读取原操作。
			state, _ := n.FirewallConfig(t.Context())
			if fail {
				if len(state.Rules) != 2 || state.Revision != 1 || state.Migration.State != "failed" {
					t.Fatal(state)
				}
				for range 3 {
					n.checkFirewall(t.Context())
					n.migrateTrust(t.Context())
				}
				var count int
				if err := n.DB.QueryRow("SELECT count(*) FROM queue").Scan(&count); err != nil || count != 1 || x.submits != 1 {
					t.Fatal("失败输入生成了新操作", count, x.submits, err)
				}
				if _, err := n.writeFirewall(t.Context(), n.ID, FirewallWrite{ExpectedRevision: 1, Key: "retry-migration"}, false); err != nil {
					t.Fatal(err)
				}
				x.fail = false
				n.advance()
				n.advance()
				state, _ = n.FirewallConfig(t.Context())
			}
			if len(state.Rules) != 1 || state.Rules[0].ID != "network" || state.Revision != 2 || state.ConfirmedRevision != 2 || state.Migration.State != "in_sync" {
				t.Fatal(state)
			}
			trust, err := n.LocalTrustRules(t.Context())
			if err != nil || trust.Migration == nil || trust.Migration.State != "in_sync" {
				t.Fatal(trust, err)
			}
			x.inspection.Target = &protocol.FirewallTarget{Revision: 2, Configuration: state.FirewallDefinition}
			var path string
			if err = n.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(t.Context(), path, Options{Executor: x})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			reopened.checkFirewall(t.Context())
			reopened.migrateTrust(t.Context())
			after, _ := reopened.FirewallConfig(t.Context())
			if len(after.Rules) != 1 || after.Rules[0].ID != "network" {
				t.Fatal("重启恢复了已转交规则", after)
			}
		})
	}
}

func TestTrustMigrationWaitsForAppliedTrustAndStopsWhenDisabled(t *testing.T) {
	for _, mode := range []string{"unapplied", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			n, _ := migrationNode(t)
			if mode == "unapplied" {
				if _, err := n.DB.Exec("UPDATE peer_trust_status SET body=json_set(body,'$.applied_revision',0)"); err != nil {
					t.Fatal(err)
				}
			} else {
				p, _ := n.TrustPolicy(t.Context())
				if _, err := n.ChangeTrust(t.Context(), TrustPolicyPatch{ExpectedVersion: p.Version, Enabled: false}); err != nil {
					t.Fatal(err)
				}
			}
			n.migrateTrust(t.Context())
			var count int
			if err := n.DB.QueryRow("SELECT count(*) FROM queue").Scan(&count); err != nil || count != 0 {
				t.Fatal("覆盖完成前迁移了普通规则", count, err)
			}
		})
	}
}
