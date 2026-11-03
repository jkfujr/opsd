package operations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

type Engine struct {
	Store   Store
	Manager *firewall.Manager
	Clock   func() (string, int64, error)
	Install func(context.Context, *Local, func() error) error
}

func (e *Engine) clock() (string, int64, error) {
	if e.Clock != nil {
		return e.Clock()
	}
	return SystemClock()
}
func (e *Engine) Record(ctx context.Context, id string) (protocol.OperationRecord, error) {
	l, err := e.Store.Get(ctx, id)
	if err != nil {
		return l.Record, err
	}
	if l.Record.State == protocol.AwaitingConfirmation {
		boot, n, err := e.clock()
		if err != nil {
			return l.Record, err
		}
		left := int64(0)
		if boot == l.BootID && n < l.DeadlineNS {
			left = (l.DeadlineNS - n + int64(time.Second) - 1) / int64(time.Second)
		}
		l.Record.ConfirmationRemainingSeconds = &left
	}
	return l.Record, nil
}
func (e *Engine) attention(ctx context.Context, l *Local, err error) error {
	l.Record.State = protocol.AttentionRequired
	l.Record.Error = &protocol.Error{Code: firewall.Code(err), Message: err.Error()}
	if l.Action != nil && l.Action.Firewall != nil && l.Action.Firewall.RequiresConfirmation {
		l.Record.Decision = "revert"
	}
	if l.Action != nil && l.Action.FirewallConfig != nil && managedConfirmation(l.Action.FirewallConfig, l.Recipe) {
		l.Record.Decision = "revert"
	}
	return e.Store.Save(ctx, l)
}
func (e *Engine) end(ctx context.Context, l *Local, state protocol.OperationState, err error) error {
	l.Record.State = state
	if state == protocol.Succeeded && l.Action != nil && l.Action.FirewallConfig != nil {
		l.Record.Decision = "keep"
	}
	l.IntentStep = nil
	if err != nil {
		l.Record.Error = &protocol.Error{Code: firewall.Code(err), Message: err.Error()}
	} else {
		l.Record.Error = nil
	}
	return e.Store.Save(ctx, l)
}

// Run 的调用者必须持有跨进程执行锁。退出与取消都不释放持久未决责任。
func (e *Engine) Run(ctx context.Context, id string) error {
	l, err := e.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if Terminal(l.Record.State) {
		return nil
	}
	if l.Action == nil {
		return fmt.Errorf("未决操作缺少动作")
	}
	boot, now, err := e.clock()
	if err != nil {
		return err
	}
	if l.Action.Kind == "component" {
		return e.install(ctx, &l, boot)
	}
	p := l.Action.Firewall
	if target := l.Action.FirewallConfig; target != nil {
		p = &protocol.ChangePlan{Backend: target.Configuration.Backend, RequiresConfirmation: managedConfirmation(target, l.Recipe), Executable: true}
	}
	if p == nil {
		return e.attention(ctx, &l, fmt.Errorf("操作动作无法解释"))
	}
	a, err := e.Manager.Adapter(p.Backend)
	if err != nil {
		return e.attention(ctx, &l, err)
	}
	if l.Record.State == protocol.AttentionRequired {
		if l.RequestRevision != l.Record.Revision {
			return nil
		}
		if l.Request == "resolve" {
			s, err := a.Read(ctx)
			if err != nil || !s.Complete {
				if err == nil {
					err = fmt.Errorf("实际规则读取不完整")
				}
				return e.attention(ctx, &l, err)
			}
			t := time.Now().UTC()
			l.Record.LastObservedAt = &t
			return e.end(ctx, &l, protocol.ResolvedManually, nil)
		}
		if l.Request != "recover" {
			return nil
		}
		if len(l.Recipe) == 0 {
			return e.end(ctx, &l, protocol.Failed, fmt.Errorf("未执行原生变更，已结束原操作"))
		}
		if !p.RequiresConfirmation && e.target(ctx, a, l.Recipe) == nil {
			return e.end(ctx, &l, protocol.Succeeded, nil)
		}
		l.Record.Decision = "revert"
		l.Record.State = protocol.Reverting
		if err = e.Store.Save(ctx, &l); err != nil {
			return err
		}
	}
	fresh := false
	if len(l.Recipe) == 0 {
		if l.BootID != "" && l.BootID != boot {
			return e.end(ctx, &l, protocol.Rejected, fmt.Errorf("主机已重启，未执行旧的准备动作"))
		}
		backend, err := e.Manager.Detect(ctx)
		if err != nil {
			return e.end(ctx, &l, protocol.Rejected, err)
		}
		if backend != p.Backend {
			return e.end(ctx, &l, protocol.Rejected, firewall.Conflict("防火墙管理归属发生变化"))
		}
		if target := l.Action.FirewallConfig; target != nil {
			var state protocol.FirewallState
			state, err = e.Manager.Read(ctx)
			if err == nil {
				l.Recipe, err = e.Manager.PrepareConfiguration(ctx, state, target.Configuration)
			}
			p.RequiresConfirmation = managedConfirmation(target, l.Recipe)
		} else {
			l.Recipe, err = a.Prepare(ctx, *p)
		}
		if err != nil {
			return e.end(ctx, &l, protocol.Rejected, err)
		}
		if l.Action.FirewallConfig != nil && len(l.Recipe) == 0 {
			return e.end(ctx, &l, protocol.Succeeded, nil)
		}
		requires := false
		for _, step := range l.Recipe {
			if step.Selector.Scope == protocol.Runtime || step.Selector.Scope == protocol.Both || step.Forward.Kind == "set_enabled" {
				requires = true
			}
		}
		if l.Action.FirewallConfig != nil && l.Action.FirewallConfig.Automatic {
			requires = false
		}
		if !p.Executable || requires != p.RequiresConfirmation {
			return e.end(ctx, &l, protocol.Rejected, firewall.Conflict("预览的执行或确认条件不匹配，请重新生成预览"))
		}
		l.Record.Steps = []protocol.OperationStep{}
		for _, step := range l.Recipe {
			l.Record.Steps = append(l.Record.Steps, protocol.OperationStep{Name: step.Name, State: "pending"})
		}
		l.BootID = boot
		l.Record.State = protocol.Applying
		if p.RequiresConfirmation {
			boot, now, err = e.clock()
			if err != nil {
				return err
			}
			if boot != l.BootID {
				return e.end(ctx, &l, protocol.Rejected, fmt.Errorf("准备期间主机启动标识发生变化"))
			}
			l.DeadlineNS = now + int64(120*time.Second)
			deadline := time.Now().UTC().Add(120 * time.Second)
			l.Record.ConfirmationDeadline = &deadline
		}
		if err = e.Store.Save(ctx, &l); err != nil {
			return err
		}
		fresh = true
	}
	if l.Record.State == protocol.Applying && !fresh {
		if l.IntentStep != nil {
			i := *l.IntentStep
			if i < 0 || i >= len(l.Recipe) {
				return e.attention(ctx, &l, fmt.Errorf("执行步骤记录无法解释"))
			}
			match, err := e.inspect(ctx, a, l.Recipe[i])
			if err != nil {
				return e.attention(ctx, &l, err)
			}
			switch match {
			case "after":
				l.NextStep = i + 1
				l.Record.Steps[i].State = "applied"
			case "before":
				l.NextStep = i
			default:
				return e.attention(ctx, &l, firewall.Conflict("中断步骤存在外部修改，不能自动覆盖"))
			}
			l.IntentStep = nil
			if err = e.Store.Save(ctx, &l); err != nil {
				return err
			}
		}
		if l.NextStep < len(l.Recipe) {
			return e.revert(ctx, a, &l, fmt.Errorf("执行中断，不继续尚未完成的正向步骤"))
		}
	}
	if l.Record.State == protocol.Applying {
		for l.NextStep < len(l.Recipe) {
			if err = ctx.Err(); err != nil {
				return err
			}
			if expired, err := e.expired(l); err != nil {
				return err
			} else if expired {
				return e.revert(ctx, a, &l, fmt.Errorf("确认期限已到"))
			}
			i := l.NextStep
			step := l.Recipe[i]
			match, err := e.inspect(ctx, a, step)
			if err != nil {
				return e.attention(ctx, &l, err)
			}
			if match != "before" {
				return e.attention(ctx, &l, firewall.Conflict("执行前实际规则与预览不一致"))
			}
			l.IntentStep = &i
			l.Record.Steps[i].State = "applying"
			if err = e.Store.Save(ctx, &l); err != nil {
				return err
			}
			writeErr := a.Write(ctx, step, false)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			match, err = e.inspect(ctx, a, step)
			if err != nil {
				return e.attention(ctx, &l, err)
			}
			if match == "after" {
				l.NextStep++
				l.IntentStep = nil
				l.Record.Steps[i].State = "applied"
				t := time.Now().UTC()
				l.Record.Steps[i].ObservedAt = &t
				l.Record.LastObservedAt = &t
				if err = e.Store.Save(ctx, &l); err != nil {
					return err
				}
				if writeErr != nil {
					return e.revert(ctx, a, &l, writeErr)
				}
			} else if match == "before" {
				l.IntentStep = nil
				if writeErr == nil {
					writeErr = fmt.Errorf("原生命令结束，但目标没有成立")
				}
				return e.revert(ctx, a, &l, writeErr)
			} else {
				return e.attention(ctx, &l, firewall.Conflict("步骤部分生效或被外部改变，需要核对原生配置"))
			}
		}
		if err = e.target(ctx, a, l.Recipe); err != nil {
			return e.attention(ctx, &l, err)
		}
		if !p.RequiresConfirmation {
			return e.end(ctx, &l, protocol.Succeeded, nil)
		}
		l.Record.State = protocol.AwaitingConfirmation
		if err = e.Store.Save(ctx, &l); err != nil {
			return err
		}
	}
	if l.Record.State == protocol.Reverting {
		return e.revert(ctx, a, &l, nil)
	}
	for l.Record.State == protocol.AwaitingConfirmation {
		if expired, err := e.expired(l); err != nil {
			return err
		} else if expired {
			return e.revert(ctx, a, &l, fmt.Errorf("未在期限内确认，开始撤销"))
		}
		latest, err := e.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		if latest.Request == "confirm" && latest.RequestRevision == l.Record.Revision {
			if err = e.target(ctx, a, l.Recipe); err != nil {
				return e.attention(ctx, &l, err)
			}
			// 读回后再次裁决时间；请求抵达时刻不能代替持久提交前的期限检查。
			if expired, err := e.expired(l); err != nil {
				return err
			} else if expired {
				return e.revert(ctx, a, &l, fmt.Errorf("确认处理时期限已到"))
			}
			_, err = e.Store.Keep(ctx, id, l.Record.Revision, e.clock)
			if errors.Is(err, ErrExpired) {
				return e.revert(ctx, a, &l, err)
			}
			return err
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}
func (e *Engine) expired(l Local) (bool, error) {
	if l.DeadlineNS == 0 {
		return false, nil
	}
	boot, n, err := e.clock()
	return boot != l.BootID || n >= l.DeadlineNS, err
}
func (e *Engine) inspect(ctx context.Context, a firewall.Adapter, step firewall.Step) (string, error) {
	s, err := a.Read(ctx)
	if err != nil {
		return "", err
	}
	if !s.Complete {
		return "", fmt.Errorf("原生配置读取不完整")
	}
	v := firewall.Capture(s, step.Selector)
	if firewall.Equivalent(v, step.After) {
		return "after", nil
	}
	if firewall.Equivalent(v, step.Before) {
		return "before", nil
	}
	return "other", nil
}
func (e *Engine) target(ctx context.Context, a firewall.Adapter, steps []firewall.Step) error {
	seen := map[firewall.Selector]bool{}
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if seen[step.Selector] {
			continue
		}
		seen[step.Selector] = true
		match, err := e.inspect(ctx, a, step)
		if err != nil {
			return err
		}
		if match != "after" {
			return firewall.Conflict("目标规则或持久配置已被改变")
		}
	}
	return nil
}
func (e *Engine) revert(ctx context.Context, a firewall.Adapter, l *Local, cause error) error {
	l.Record.Decision = "revert"
	l.Record.State = protocol.Reverting
	if cause != nil {
		l.Record.Error = &protocol.Error{Code: firewall.Code(cause), Message: cause.Error()}
	}
	if l.IntentStep != nil {
		i := *l.IntentStep
		match, err := e.inspect(ctx, a, l.Recipe[i])
		if err != nil {
			return e.attention(ctx, l, err)
		}
		if match == "after" {
			l.NextStep = i + 1
		} else if match == "before" {
			l.NextStep = i
		} else {
			return e.attention(ctx, l, firewall.Conflict("未完成步骤无法安全撤销"))
		}
		l.IntentStep = nil
	}
	if err := e.Store.Save(ctx, l); err != nil {
		return err
	}
	changed := l.NextStep > 0
	for _, step := range l.Record.Steps {
		if step.State == "applied" || step.State == "reverting" || step.State == "reverted" {
			changed = true
		}
	}
	for l.NextStep > 0 {
		i := l.NextStep - 1
		step := l.Recipe[i]
		match, err := e.inspect(ctx, a, step)
		if err != nil {
			return e.attention(ctx, l, err)
		}
		if match == "other" {
			return e.attention(ctx, l, firewall.Conflict("外部修改阻止自动撤销"))
		}
		if match == "after" {
			l.Record.Steps[i].State = "reverting"
			if err = e.Store.Save(ctx, l); err != nil {
				return err
			}
			writeErr := a.Write(ctx, step, true)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			match, err = e.inspect(ctx, a, step)
			if err != nil {
				return e.attention(ctx, l, err)
			}
			if match != "before" {
				if writeErr == nil {
					writeErr = fmt.Errorf("撤销后原值未成立")
				}
				return e.attention(ctx, l, writeErr)
			}
		}
		l.NextStep--
		l.Record.Steps[i].State = "reverted"
		t := time.Now().UTC()
		l.Record.LastObservedAt = &t
		if err = e.Store.Save(ctx, l); err != nil {
			return err
		}
	}
	if changed {
		return e.end(ctx, l, protocol.Reverted, cause)
	}
	return e.end(ctx, l, protocol.Failed, cause)
}
func (e *Engine) install(ctx context.Context, l *Local, boot string) error {
	if l.Record.State == protocol.AttentionRequired {
		if l.RequestRevision != l.Record.Revision {
			return nil
		}
		if l.Request == "resolve" {
			state, err := e.Manager.Read(ctx)
			if err != nil || !state.Complete {
				if err == nil {
					err = fmt.Errorf("无法完整读取实际防火墙状态")
				}
				return e.attention(ctx, l, err)
			}
			if l.ComponentState != nil && l.ComponentState.MaskOwned {
				b, _ := e.Manager.Host.Run(ctx, "systemctl", "is-enabled", string(l.ComponentState.Backend)+".service")
				if strings.TrimSpace(string(b)) == "masked" {
					return e.attention(ctx, l, fmt.Errorf("本次安装屏蔽尚未解除，请先重新处理或在本机修复"))
				}
			}
			return e.end(ctx, l, protocol.ResolvedManually, nil)
		}
		if l.Request != "recover" {
			return nil
		}
	}
	if e.Install == nil {
		return e.attention(ctx, l, fmt.Errorf("缺少组件安装执行入口"))
	}
	l.Record.State = protocol.Applying
	if err := e.Store.Save(ctx, l); err != nil {
		return err
	}
	if err := e.Install(ctx, l, func() error { return e.Store.Save(ctx, l) }); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return e.attention(ctx, l, err)
	}
	return e.end(ctx, l, protocol.Succeeded, nil)
}
