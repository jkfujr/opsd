package operations

import (
	"context"
	"fmt"
	"github.com/jkfujr/opsd/internal/protocol"
	"io"
	"os"
	"path/filepath"
)

type composeRuntime interface {
	Projects(context.Context) ([]protocol.ComposeProject, error)
	ChangeCompose(context.Context, protocol.ComposeAction, io.Writer) error
}
type composeState struct {
	Before []string `json:"before"`
}

func composeSteps(action string) []string {
	if action == "update" {
		return []string{"pull", "recreate"}
	}
	return []string{action}
}

func (e *Engine) compose(ctx context.Context, l *Local, boot string) error {
	runtime, ok := e.Containers.(composeRuntime)
	if !ok || l.Action.Compose == nil {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("Compose 执行入口不可用"))
	}
	a := *l.Action.Compose
	if l.Record.State == protocol.AttentionRequired && l.RequestRevision != l.Record.Revision {
		return nil
	}
	if l.Record.State == protocol.AttentionRequired && l.Request == "resolve" {
		return e.end(ctx, l, protocol.ResolvedManually, nil)
	}
	if l.IntentStep != nil || l.Record.State == protocol.AttentionRequired {
		projects, err := runtime.Projects(ctx)
		if err == nil {
			for _, project := range projects {
				if project.ID == a.Project.ID {
					l.Record.LastObservedAt = &project.CheckedAt
					if composeGoal(a.Action, project, l.ContainerState) {
						return e.end(ctx, l, protocol.Succeeded, nil)
					}
				}
			}
		}
		if err == nil {
			err = fmt.Errorf("Compose 步骤结果尚未确认，请核对项目状态后结束原操作")
		}
		return e.attention(ctx, l, err)
	}
	if l.BootID != "" && l.BootID != boot {
		return e.attention(ctx, l, fmt.Errorf("主机已重启，请核对 Compose 项目状态"))
	}
	if l.ContainerState == nil || l.ContainerState.Project == nil {
		projects, err := runtime.Projects(ctx)
		if err != nil {
			return e.end(ctx, l, protocol.Rejected, err)
		}
		var before []string
		found := false
		for _, project := range projects {
			if project.ID == a.Project.ID {
				found = true
				if a.ExpectedRevision != "" && project.Revision != a.ExpectedRevision {
					return e.end(ctx, l, protocol.Rejected, fmt.Errorf("Compose 定义已经变化，未执行原请求"))
				}
				if project.ReadOnlyReason != "" {
					return e.end(ctx, l, protocol.Rejected, fmt.Errorf("%s", project.ReadOnlyReason))
				}
				for _, service := range project.Services {
					before = append(before, service.ContainerID)
				}
			}
		}
		if !found {
			return e.end(ctx, l, protocol.Rejected, fmt.Errorf("实际 Compose 项目不存在"))
		}
		l.ContainerState = &containerState{Project: &composeState{Before: before}}
		if err = e.Store.Save(ctx, l); err != nil {
			return err
		}
	}
	if e.LogDirectory == "" {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("执行日志目录不可用"))
	}
	if err := os.MkdirAll(e.LogDirectory, 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(e.LogDirectory, l.Record.ID+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return e.attention(ctx, l, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	log := &limitedLog{file: file, size: info.Size()}
	steps := composeSteps(a.Action)
	names := map[string]string{"pull": "拉取项目镜像", "start": "启动项目", "stop": "停止项目", "recreate": "重建项目"}
	for i := l.NextStep; i < len(steps); i++ {
		step := i
		l.IntentStep = &step
		l.Record.State = protocol.Applying
		if len(l.Record.Steps) <= i {
			l.Record.Steps = append(l.Record.Steps, protocol.OperationStep{Name: names[steps[i]], State: "applying"})
		} else {
			l.Record.Steps[i].State = "applying"
		}
		if err = e.Store.Save(ctx, l); err != nil {
			return err
		}
		command := a
		command.Action = steps[i]
		err = runtime.ChangeCompose(ctx, command, log)
		_ = file.Sync()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			l.Record.Steps[i].State = "unknown"
			return e.attention(ctx, l, err)
		}
		l.NextStep = i + 1
		l.IntentStep = nil
		l.Record.Steps[i].State = "completed"
		if err = e.Store.Save(ctx, l); err != nil {
			return err
		}
	}
	return e.end(ctx, l, protocol.Succeeded, nil)
}
func composeGoal(action string, project protocol.ComposeProject, state *containerState) bool {
	if len(project.Services) == 0 {
		return false
	}
	for _, service := range project.Services {
		if action == "stop" {
			if service.State != "exited" && service.State != "created" && service.State != "dead" {
				return false
			}
		} else if action == "start" || action == "recreate" || action == "update" {
			if service.State != "running" || service.Health == "unhealthy" {
				return false
			}
		} else {
			return false
		}
		if action == "recreate" || action == "update" {
			if state == nil || state.Project == nil {
				return false
			}
			for _, old := range state.Project.Before {
				if service.ContainerID == old {
					return false
				}
			}
		}
	}
	return true
}
