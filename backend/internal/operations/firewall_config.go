package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

type confirmedWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func confirmedFrom(ctx context.Context, q querier) (protocol.FirewallTarget, error) {
	var body string
	var target protocol.FirewallTarget
	e := q.QueryRowContext(ctx, "SELECT body FROM firewall_confirmed WHERE id=1").Scan(&body)
	if e == nil {
		e = json.Unmarshal([]byte(body), &target)
	}
	return target, e
}

func sameTarget(a, b protocol.FirewallTarget) bool {
	a.AdoptFingerprint, b.AdoptFingerprint = "", ""
	a.Automatic, b.Automatic = false, false
	a.PeerTrustMigration, b.PeerTrustMigration = nil, nil
	return reflect.DeepEqual(a, b)
}

func saveConfirmed(ctx context.Context, q confirmedWriter, target protocol.FirewallTarget) error {
	target.AdoptFingerprint = ""
	target.Automatic = false
	target.PeerTrustMigration = nil
	body, e := json.Marshal(target)
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "INSERT INTO firewall_confirmed(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", string(body))
	return e
}

func (s Store) ConfirmedFirewall(ctx context.Context) (protocol.FirewallTarget, error) {
	return confirmedFrom(ctx, s.DB)
}

func (e *Engine) InspectFirewall(ctx context.Context, importCurrent bool, previewCurrent ...bool) (*protocol.FirewallInspected, error) {
	s, err := e.Manager.Read(ctx)
	if err != nil {
		return nil, err
	}
	result := &protocol.FirewallInspected{Backend: s.Backend, ServiceActive: s.ServiceActive, Capabilities: s.Capabilities, CheckedAt: s.CollectedAt}
	if !s.Complete {
		return nil, firewall.Conflict("系统防火墙读取不完整")
	}
	if len(previewCurrent) > 0 && previewCurrent[0] {
		current, err := firewall.ImportConfiguration(s)
		if err != nil {
			return nil, err
		}
		result.Current, result.Fingerprint = &current, firewall.AdoptionFingerprint(s)
		return result, nil
	}
	if s.Backend == "none" {
		return result, nil
	}
	target, err := e.Store.ConfirmedFirewall(ctx)
	if err == sql.ErrNoRows && importCurrent {
		// 首次接管不与存量执行交叉，也不接收控制台提供的自动批准配置。
		pending, pendingErr := e.Store.Pending(ctx)
		if pendingErr != nil {
			return nil, pendingErr
		}
		if len(pending) > 0 {
			return nil, ErrBusy
		}
		d, readErr := firewall.ImportConfiguration(s)
		if readErr != nil {
			return nil, readErr
		}
		target = protocol.FirewallTarget{Revision: 1, Configuration: d}
		tx, beginErr := e.Store.DB.BeginTx(ctx, nil)
		if beginErr != nil {
			return nil, beginErr
		}
		defer tx.Rollback()
		body, _ := json.Marshal(target)
		if _, err = tx.ExecContext(ctx, "INSERT INTO firewall_confirmed(id,body) VALUES(1,?) ON CONFLICT(id) DO NOTHING", string(body)); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		target, err = e.Store.ConfirmedFirewall(ctx)
	}
	if err != nil {
		return nil, err
	}
	steps, err := e.Manager.PrepareConfiguration(ctx, s, target.Configuration)
	result.Target = &target
	if err != nil {
		result.Error = &protocol.Error{Code: firewall.Code(err), Message: err.Error()}
		return result, nil
	}
	result.InSync = len(steps) == 0
	if s.Backend == "iptables" {
		return result, nil
	}
	// 托管配置可以修复运行/永久差异，原生单步编辑的限制不用于页面保存。
	result.Capabilities.EditRules = protocol.Capability{Supported: true}
	result.Capabilities.Reorder = protocol.Capability{Supported: true}
	result.Capabilities.DefaultPolicy = protocol.Capability{Supported: true}
	return result, nil
}

func (e *Engine) adoptFirewall(ctx context.Context, l *Local) error {
	target := l.Action.FirewallConfig
	state, err := e.Manager.Read(ctx)
	if err != nil {
		return e.end(ctx, l, protocol.Rejected, err)
	}
	current, err := firewall.ImportConfiguration(state)
	if err != nil {
		return e.end(ctx, l, protocol.Rejected, err)
	}
	if target.Automatic || target.PeerTrustMigration != nil || target.AdoptFingerprint != firewall.AdoptionFingerprint(state) || firewall.ConfigurationFingerprint(target.Configuration) != firewall.ConfigurationFingerprint(current) {
		return e.end(ctx, l, protocol.Rejected, firewall.Conflict("实际防火墙配置已经变化，请重新预览"))
	}
	l.Record.LastObservedAt = &state.CollectedAt
	l.Record.Steps = []protocol.OperationStep{{Name: "读回并接纳当前防火墙配置", State: "completed"}}
	return e.end(ctx, l, protocol.Succeeded, nil)
}

func managedConfirmation(target *protocol.FirewallTarget, steps []firewall.Step) bool {
	if target.Automatic || target.PeerTrustMigration != nil {
		return false
	}
	for _, step := range steps {
		if step.Selector.Scope != protocol.Permanent {
			return true
		}
	}
	return false
}

func migrationTrust(ctx context.Context, q querier) (protocol.PeerTrustTarget, protocol.PeerTrustUpdated, error) {
	var target protocol.PeerTrustTarget
	var status protocol.PeerTrustUpdated
	var a, b string
	err := q.QueryRowContext(ctx, "SELECT target,status FROM peer_trust WHERE id=1").Scan(&a, &b)
	if err == nil {
		err = json.Unmarshal([]byte(a), &target)
	}
	if err == nil {
		err = json.Unmarshal([]byte(b), &status)
	}
	return target, status, err
}

func validateMigration(ctx context.Context, q querier, before, after protocol.FirewallTarget) error {
	target, status, err := migrationTrust(ctx, q)
	if err != nil {
		return firewall.Conflict("互信规则尚未完成实际读回")
	}
	return firewall.ValidatePeerTrustMigration(before, after, target, status)
}

// prepareMigration 在持有操作写锁时维护并读回互信，再准备普通规则移除。
func (e *Engine) prepareMigration(ctx context.Context, after protocol.FirewallTarget) error {
	before, err := e.Store.ConfirmedFirewall(ctx)
	if err != nil {
		return err
	}
	if err = validateMigration(ctx, e.Store.DB, before, after); err != nil {
		return err
	}
	target, status, err := migrationTrust(ctx, e.Store.DB)
	if err != nil {
		return err
	}
	status.Results = e.Manager.SyncPeerTrust(ctx, target)
	for _, result := range status.Results {
		if result.State != "in_sync" && result.State != "inactive" {
			return firewall.Conflict("互信规则实际检查失败，原 IP 规则保留")
		}
	}
	if err = firewall.ValidatePeerTrustMigration(before, after, target, status); err != nil {
		return err
	}
	return validateMigration(ctx, e.Store.DB, before, after)
}
