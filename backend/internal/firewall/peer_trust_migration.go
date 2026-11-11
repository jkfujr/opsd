package firewall

import (
	"reflect"
	"slices"
	"sort"

	"github.com/jkfujr/opsd/internal/protocol"
)

// PeerTrustRuleIDs 只选取与互信放行范围完全重合的已管理入站规则。
func PeerTrustRuleIDs(d protocol.FirewallDefinition, target protocol.PeerTrustTarget) []string {
	ids := []string{}
	if !target.Enabled {
		return ids
	}
	for _, r := range d.Rules {
		ip, err := exactSource(r.Spec.Source, r.Spec.Family)
		if err != nil || r.Spec.Action != "allow" || r.Spec.Protocol != "any" || r.Spec.Port != "" || canonicalAddress(r.Spec.Destination, r.Spec.Family) != canonicalAddress("", r.Spec.Family) {
			continue
		}
		if slices.Contains(trustList(target, r.Spec.Family), ip) {
			ids = append(ids, r.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func MigratePeerTrustRules(d protocol.FirewallDefinition, target protocol.PeerTrustTarget, ids []string) (protocol.FirewallDefinition, error) {
	candidates := PeerTrustRuleIDs(d, target)
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || !slices.Contains(candidates, id) {
			return d, Conflict("待迁移规则已改变")
		}
		seen[id] = true
	}
	if len(seen) == 0 {
		return d, Conflict("没有待迁移的互信规则")
	}
	rules := make([]protocol.ManagedRule, 0, len(d.Rules)-len(ids))
	for _, r := range d.Rules {
		if !seen[r.ID] {
			rules = append(rules, r)
		}
	}
	d.Rules = rules
	return d, nil
}

// ValidatePeerTrustMigration 验证迁移只移除已由当前互信覆盖的普通规则。
func ValidatePeerTrustMigration(before, after protocol.FirewallTarget, target protocol.PeerTrustTarget, status protocol.PeerTrustUpdated) error {
	m := after.PeerTrustMigration
	if m == nil || after.Automatic || after.Revision != before.Revision+1 || !target.Enabled || m.RequiredRevision == 0 || m.RequiredRevision != target.Revision || status.TargetRevision != target.Revision || status.AppliedRevision != target.Revision {
		return Conflict("互信版本尚未生效或已经改变")
	}
	want, err := MigratePeerTrustRules(before.Configuration, target, m.RuleIDs)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(want, after.Configuration) {
		return Conflict("互信迁移包含额外配置变更")
	}
	for _, r := range before.Configuration.Rules {
		if !slices.Contains(m.RuleIDs, r.ID) {
			continue
		}
		ip, _ := exactSource(r.Spec.Source, r.Spec.Family)
		for _, scope := range []protocol.ConfigScope{protocol.Permanent, protocol.Runtime} {
			covered := false
			for _, result := range status.Results {
				if result.Family == r.Spec.Family && result.Component == "host" && result.Scope == scope && result.Error == nil && (result.State == "in_sync" && slices.Contains(result.Addresses, ip) || scope == protocol.Runtime && result.State == "inactive") {
					covered = true
				}
			}
			if !covered {
				return Conflict("互信规则尚未完成实际读回")
			}
		}
	}
	return nil
}
