package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

func (n *Node) trustMigrationTarget(ctx context.Context, q firewallQuerier, state protocol.ManagedFirewallState, confirmed protocol.FirewallDefinition) (protocol.FirewallTarget, error) {
	var out protocol.FirewallTarget
	if state.Revision != state.ConfirmedRevision || state.State != "in_sync" && !(state.Migration != nil && state.Migration.State == "failed" && state.State == "failed") {
		return out, fault("conflict", "普通防火墙配置尚未确认")
	}
	p, err := readPolicy(ctx, q)
	if err != nil {
		return out, err
	}
	var revoked int
	if err = q.QueryRowContext(ctx, "SELECT count(*) FROM revocations WHERE id=?", n.ID).Scan(&revoked); err != nil {
		return out, err
	}
	if !p.Enabled || revoked > 0 {
		return out, fault("conflict", "集群互信已关闭或节点已撤销")
	}
	var a, b string
	var target protocol.PeerTrustTarget
	var status protocol.PeerTrustUpdated
	if err = q.QueryRowContext(ctx, "SELECT body FROM peer_trust_target WHERE id=1").Scan(&a); err != nil {
		return out, fault("conflict", "互信规则尚未生效")
	}
	if err = q.QueryRowContext(ctx, "SELECT body FROM peer_trust_status WHERE id=1").Scan(&b); err != nil {
		return out, fault("conflict", "互信规则尚未完成实际读回")
	}
	if err = json.Unmarshal([]byte(a), &target); err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(b), &status); err != nil {
		return out, err
	}
	ids := firewall.PeerTrustRuleIDs(confirmed, target)
	d, err := firewall.MigratePeerTrustRules(confirmed, target, ids)
	if err != nil {
		return out, err
	}
	out = protocol.FirewallTarget{Revision: state.ConfirmedRevision + 1, Configuration: d, PeerTrustMigration: &protocol.PeerTrustMigration{RequiredRevision: target.Revision, RuleIDs: ids}}
	err = firewall.ValidatePeerTrustMigration(protocol.FirewallTarget{Revision: state.ConfirmedRevision, Configuration: confirmed}, out, target, status)
	return out, err
}

func (n *Node) migrateTrust(ctx context.Context) {
	state, confirmed, err := firewallState(ctx, n.DB)
	if err != nil || state.Revision == 0 {
		return
	}
	target, err := n.trustMigrationTarget(ctx, n.DB, state, confirmed)
	if err != nil {
		return
	}
	// 同一组输入复用同一个队列记录，失败与未知结果均不生成新操作。
	_, _ = n.writeFirewall(ctx, n.ID, FirewallWrite{ExpectedRevision: state.Revision, Key: trustMigrationKey(target), migration: true}, false)
}

func trustMigrationKey(target protocol.FirewallTarget) string {
	input, _ := json.Marshal(target)
	sum := sha256.Sum256(input)
	return "peer-trust-migration-" + hex.EncodeToString(sum[:])
}
