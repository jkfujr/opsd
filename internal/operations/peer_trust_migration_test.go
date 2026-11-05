package operations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

type migrationPolicyHost struct {
	*managedPolicyHost
	afterUserWrite func()
}

func (h *migrationPolicyHost) Replace(path string, before, after []byte) error {
	err := h.managedPolicyHost.Replace(path, before, after)
	if err == nil && path == "/etc/ufw/user.rules" && h.afterUserWrite != nil {
		h.afterUserWrite()
	}
	return err
}

func TestPeerTrustMigrationChecksNativeCoverageAndPersistsConfirmedRemoval(t *testing.T) {
	for _, mode := range []string{"success", "disabled_before_execution", "foreign_trust", "ordinary_drift", "write_failure", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			e, _, _ := operationFixture(t)
			h := &migrationPolicyHost{managedPolicyHost: newManagedPolicyHost(false)}
			h.files["/etc/ufw/user.rules"] = []byte("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n### tuple ### allow any any 0.0.0.0/0 any 8.8.8.8/32 in\n-A ufw-user-input -s 8.8.8.8/32 -j ACCEPT\n### END RULES ###\nCOMMIT\n")
			h.files["/etc/ufw/before.rules"] = []byte("*filter\n:ufw-before-input - [0:0]\n-A ufw-before-input -j ACCEPT\nCOMMIT\n")
			h.files["/etc/ufw/before6.rules"] = []byte("*filter\n:ufw6-before-input - [0:0]\nCOMMIT\n")
			e.Manager.Host = h
			first, err := e.InspectFirewall(t.Context(), true)
			if err != nil || first.Target == nil || len(first.Target.Configuration.Rules) != 1 {
				t.Fatal(first, err)
			}
			target := protocol.PeerTrustTarget{Revision: 1, Enabled: true, IPv4: []string{"8.8.8.8"}, IPv6: []string{}}
			status := protocol.PeerTrustUpdated{TargetRevision: 1, AppliedRevision: 1, Results: e.Manager.SyncPeerTrust(t.Context(), target)}
			a, _ := json.Marshal(target)
			b, _ := json.Marshal(status)
			if _, err = e.Store.DB.Exec("INSERT INTO peer_trust(id,target,status) VALUES(1,?,?)", string(a), string(b)); err != nil {
				t.Fatal(err)
			}
			ids := firewall.PeerTrustRuleIDs(first.Target.Configuration, target)
			d, err := firewall.MigratePeerTrustRules(first.Target.Configuration, target, ids)
			if err != nil {
				t.Fatal(err)
			}
			action := protocol.OperationAction{Kind: "firewall_config", FirewallConfig: &protocol.FirewallTarget{Revision: 2, Configuration: d, PeerTrustMigration: &protocol.PeerTrustMigration{RequiredRevision: 1, RuleIDs: ids}}}
			accept(t, e, "migration", action)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "disabled_before_execution":
				if _, err = e.Store.DB.Exec("UPDATE peer_trust SET target=json_set(target,'$.enabled',json('false'),'$.revision',2)"); err != nil {
					t.Fatal(err)
				}
			case "foreign_trust":
				h.files["/etc/ufw/before.rules"] = []byte(strings.ReplaceAll(string(h.files["/etc/ufw/before.rules"]), "-s 8.8.8.8 -j ACCEPT", "-s 8.8.8.8 -j DROP"))
			case "ordinary_drift":
				h.files["/etc/ufw/user.rules"] = []byte(strings.ReplaceAll(string(h.files["/etc/ufw/user.rules"]), "### END RULES ###", "### tuple ### allow any any 0.0.0.0/0 any 9.9.9.9/32 in\n-A ufw-user-input -s 9.9.9.9/32 -j ACCEPT\n### END RULES ###"))
			case "write_failure":
				h.fail = true
			case "interrupted":
				h.afterUserWrite = cancel
			}
			err = e.Run(ctx, "migration")
			if mode == "interrupted" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				h.afterUserWrite = nil
				err = e.Run(t.Context(), "migration")
			}
			if err != nil {
				t.Fatal(err)
			}
			l, err := e.Store.Get(t.Context(), "migration")
			if err != nil {
				t.Fatal(err)
			}
			good := mode == "success" || mode == "interrupted"
			if (l.Record.State == protocol.Succeeded) != good {
				t.Fatal(mode, l.Record)
			}
			confirmed, err := e.Store.ConfirmedFirewall(t.Context())
			if err != nil || confirmed.PeerTrustMigration != nil || (len(confirmed.Configuration.Rules) == 0) != good {
				t.Fatal(confirmed, err)
			}
			if strings.Contains(string(h.files["/etc/ufw/user.rules"]), "8.8.8.8") == good {
				t.Fatal("普通文件迁移状态错误")
			}
			writes := h.writes
			if err = e.Run(t.Context(), "migration"); err != nil || writes != h.writes {
				t.Fatal("结束操作重复执行", err)
			}
			if good {
				read, err := e.InspectFirewall(t.Context(), true)
				if err != nil || !read.InSync || len(read.Target.Configuration.Rules) != 0 {
					t.Fatal("确认配置重新生成旧规则", read, err)
				}
			}
		})
	}
}
