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
	a.Automatic, b.Automatic = false, false
	return reflect.DeepEqual(a, b)
}

func saveConfirmed(ctx context.Context, q confirmedWriter, target protocol.FirewallTarget) error {
	target.Automatic = false
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

func (e *Engine) InspectFirewall(ctx context.Context, importCurrent bool) (*protocol.FirewallInspected, error) {
	s, err := e.Manager.Read(ctx)
	if err != nil {
		return nil, err
	}
	result := &protocol.FirewallInspected{Backend: s.Backend, ServiceActive: s.ServiceActive, Capabilities: s.Capabilities, CheckedAt: s.CollectedAt}
	if !s.Complete {
		return nil, firewall.Conflict("系统防火墙读取不完整")
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
	// 托管配置可以修复运行/永久差异，原生单步编辑的限制不用于页面保存。
	result.Capabilities.EditRules = protocol.Capability{Supported: true}
	result.Capabilities.Reorder = protocol.Capability{Supported: true}
	result.Capabilities.DefaultPolicy = protocol.Capability{Supported: true}
	return result, nil
}

func managedConfirmation(target *protocol.FirewallTarget, steps []firewall.Step) bool {
	if target.Automatic {
		return false
	}
	for _, step := range steps {
		if step.Selector.Scope != protocol.Permanent {
			return true
		}
	}
	return false
}
