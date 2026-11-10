package operations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/protocol"
)

type ContainerRuntime interface {
	Inspect(context.Context, protocol.ContainerRef) (protocol.ContainerDetail, error)
	Change(context.Context, protocol.ContainerAction) error
}
type containerState struct {
	Project         *composeState             `json:"project,omitempty"`
	Update          *dockerengine.UpdateState `json:"update,omitempty"`
	BeforeStartedAt *time.Time                `json:"before_started_at,omitempty"`
	Attempted       bool                      `json:"attempted"`
	Acknowledged    bool                      `json:"acknowledged"`
}

func operationCode(err error) string {
	var native *dockerengine.Fault
	if errors.As(err, &native) {
		return native.Code
	}
	return firewall.Code(err)
}
func containerGoal(a protocol.ContainerAction, current protocol.ContainerDetail, state *containerState) bool {
	switch a.Action {
	case "start":
		return current.State == "running"
	case "stop":
		return current.State == "exited" || current.State == "created" || current.State == "dead"
	case "restart":
		return state != nil && state.Acknowledged && state.BeforeStartedAt != nil && current.StartedAt != nil && current.State == "running" && current.StartedAt.After(*state.BeforeStartedAt)
	}
	return false
}
func (e *Engine) container(ctx context.Context, l *Local, boot string) error {
	if l.Action.Container != nil && l.Action.Container.Action == "update" {
		return e.updateContainer(ctx, l, boot)
	}
	attention := func(err error) error {
		if len(l.Record.Steps) > 0 {
			l.Record.Steps[0].State = "unknown"
		}
		return e.attention(ctx, l, err)
	}
	if e.Containers == nil {
		return e.attention(ctx, l, fmt.Errorf("Docker 执行入口不可用"))
	}
	a := l.Action.Container
	if a == nil {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("容器操作缺少参数"))
	}
	if l.Record.State == protocol.AttentionRequired && l.RequestRevision != l.Record.Revision {
		return nil
	}
	current, err := e.Containers.Inspect(ctx, a.ContainerRef)
	if err != nil {
		if l.Record.State == protocol.AttentionRequired && l.Request == "resolve" && operationCode(err) == "not_found" {
			return e.end(ctx, l, protocol.ResolvedManually, nil)
		}
		if l.ContainerState == nil && l.Record.State != protocol.AttentionRequired {
			return e.end(ctx, l, protocol.Rejected, err)
		}
		return attention(err)
	}
	l.Record.LastObservedAt = &current.CheckedAt
	if l.Record.State == protocol.AttentionRequired && l.Request == "resolve" {
		return e.end(ctx, l, protocol.ResolvedManually, nil)
	}
	if l.ContainerState == nil && current.ReadOnlyReason != "" {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("%s", current.ReadOnlyReason))
	}
	if containerGoal(*a, current, l.ContainerState) {
		if len(l.Record.Steps) > 0 {
			l.Record.Steps[0].State = "completed"
		}
		return e.end(ctx, l, protocol.Succeeded, nil)
	}
	// 已发送和人工重新处理只读回现状，旧操作永远不再次提交 Docker。
	if l.Record.State == protocol.AttentionRequired || (l.ContainerState != nil && l.ContainerState.Attempted) {
		return attention(fmt.Errorf("容器操作结果尚未确认，请核对现状后结束原操作"))
	}
	if l.BootID != "" && l.BootID != boot {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("主机已重启，未执行旧的容器操作"))
	}
	if current.State == "paused" || current.State == "restarting" || current.State == "removing" || (a.Action == "restart" && current.State != "running") {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("容器当前状态不支持此操作"))
	}
	if a.Action == "restart" && current.StartedAt == nil {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("容器启动时间不可用，请刷新列表"))
	}
	name := map[string]string{"start": "启动", "stop": "停止", "restart": "重启"}[a.Action]
	l.Record.State = protocol.Applying
	l.Record.Steps = []protocol.OperationStep{{Name: name + "容器 " + current.Name, State: "applying"}}
	l.ContainerState = &containerState{BeforeStartedAt: current.StartedAt, Attempted: true}
	if err = e.Store.Save(ctx, l); err != nil {
		return err
	}
	err = e.Containers.Change(ctx, *a)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		l.ContainerState.Acknowledged = true
		if saveErr := e.Store.Save(ctx, l); saveErr != nil {
			return saveErr
		}
	}
	current, readErr := e.Containers.Inspect(ctx, a.ContainerRef)
	if readErr != nil {
		return attention(readErr)
	}
	l.Record.LastObservedAt = &current.CheckedAt
	if containerGoal(*a, current, l.ContainerState) {
		l.Record.Steps[0].State = "completed"
		return e.end(ctx, l, protocol.Succeeded, nil)
	}
	if err == nil {
		err = fmt.Errorf("Docker 已应答，容器目标状态尚未成立")
	}
	return attention(err)
}
