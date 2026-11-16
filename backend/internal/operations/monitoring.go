package operations

import (
	"context"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/metricsstore"
	"github.com/jkfujr/opsd/internal/protocol"
)

type monitoringRuntime interface {
	PrepareMonitoring(context.Context, protocol.MonitoringDeployment) error
	ApplyMonitoring(context.Context, protocol.MonitoringDeployment) error
	MonitoringState(context.Context, protocol.MonitoringDeployment) (protocol.MonitoringState, error)
	DeploymentImageReady(context.Context, string) (bool, error)
	PullDeploymentImage(context.Context, string) error
}

func (e *Engine) monitoring(ctx context.Context, l *Local) error {
	runtime, ok := e.Containers.(monitoringRuntime)
	if !ok || l.Action.Monitoring == nil {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("监控部署入口不可用"))
	}
	d := *l.Action.Monitoring
	if l.Record.State == protocol.AttentionRequired && l.RequestRevision != l.Record.Revision {
		return nil
	}
	if l.Record.State == protocol.AttentionRequired && l.Request == "resolve" {
		return e.end(ctx, l, protocol.ResolvedManually, nil)
	}
	state, err := runtime.MonitoringState(ctx, d)
	if err != nil {
		return e.attention(ctx, l, err)
	}
	if (!d.Enabled && state.State == "stopped") || (d.Enabled && state.State == "ready") {
		return e.end(ctx, l, protocol.Succeeded, nil)
	}
	if l.IntentStep != nil && l.Request != "recover" {
		return e.attention(ctx, l, fmt.Errorf("监控部署结果尚未确认，请检查组件后重新处理原操作"))
	}
	l.IntentStep = nil
	steps := []struct {
		name string
		run  func() error
	}{}
	if d.Enabled {
		steps = append(steps, struct {
			name string
			run  func() error
		}{"准备监控目录", func() error { return runtime.PrepareMonitoring(ctx, d) }})
		steps = append(steps, struct {
			name string
			run  func() error
		}{"拉取 Thanos 镜像", func() error {
			image := "quay.io/thanos/thanos:" + metricsstore.ThanosVersion
			ready, err := runtime.DeploymentImageReady(ctx, image)
			if err != nil || ready {
				return err
			}
			return runtime.PullDeploymentImage(ctx, image)
		}})
	}
	name := "停止监控组件"
	if d.Enabled {
		name = "启动监控组件"
	}
	steps = append(steps, struct {
		name string
		run  func() error
	}{name, func() error { return runtime.ApplyMonitoring(ctx, d) }})
	for i := l.NextStep; i < len(steps); i++ {
		step := i
		l.IntentStep = &step
		l.Record.State = protocol.Applying
		if len(l.Record.Steps) <= i {
			l.Record.Steps = append(l.Record.Steps, protocol.OperationStep{Name: steps[i].name, State: "applying"})
		} else {
			l.Record.Steps[i].State = "applying"
		}
		if err = e.Store.Save(ctx, l); err != nil {
			return err
		}
		if err = steps[i].run(); err != nil {
			return e.attention(ctx, l, err)
		}
		l.NextStep = i + 1
		l.IntentStep = nil
		l.Record.Steps[i].State = "completed"
		if err = e.Store.Save(ctx, l); err != nil {
			return err
		}
	}
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		state, err = runtime.MonitoringState(ctx, d)
		if err == nil && (d.Enabled && state.State == "ready" || !d.Enabled && state.State == "stopped") {
			return e.end(ctx, l, protocol.Succeeded, nil)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if err == nil {
				err = fmt.Errorf("监控组件尚未到达目标状态")
			}
			return e.attention(ctx, l, err)
		case <-ticker.C:
		}
	}
}
