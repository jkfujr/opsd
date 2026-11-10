package operations

import (
	"context"
	"fmt"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/protocol"
	"io"
	"os"
	"path/filepath"
)

type containerUpdater interface {
	Update(context.Context, protocol.ContainerRef, string, *dockerengine.UpdateState, func() error, io.Writer) error
	CheckUpdate(context.Context, protocol.ContainerRef, *dockerengine.UpdateState) (bool, error)
}

func (e *Engine) updateContainer(ctx context.Context, l *Local, boot string) error {
	runtime, ok := e.Containers.(containerUpdater)
	if !ok {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("镜像更新入口不可用"))
	}
	if l.Record.State == protocol.AttentionRequired && l.RequestRevision != l.Record.Revision {
		return nil
	}
	if l.Record.State == protocol.AttentionRequired && l.Request == "resolve" {
		return e.end(ctx, l, protocol.ResolvedManually, nil)
	}
	if l.ContainerState != nil && l.ContainerState.Update != nil && l.ContainerState.Update.Stage != "" {
		goal, err := runtime.CheckUpdate(ctx, l.Action.Container.ContainerRef, l.ContainerState.Update)
		if goal {
			return e.end(ctx, l, protocol.Succeeded, nil)
		}
		if err == nil {
			err = fmt.Errorf("镜像更新尚未完成，请通过节点终端核对原容器和候选容器")
		}
		return e.attention(ctx, l, err)
	}
	if l.BootID != "" && l.BootID != boot {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("主机已重启，未执行旧的镜像更新"))
	}
	if e.LogDirectory == "" {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("执行日志目录不可用"))
	}
	if err := os.MkdirAll(e.LogDirectory, 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(e.LogDirectory, l.Record.ID+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return e.attention(ctx, l, err)
	}
	defer file.Close()
	l.ContainerState = &containerState{Attempted: true, Update: &dockerengine.UpdateState{}}
	state := l.ContainerState.Update
	persist := func() error {
		l.Record.State = protocol.Applying
		names := map[string]string{"pull": "拉取镜像", "create": "创建候选容器", "stop": "停止原容器", "rename_old": "保留原容器名称", "rename_new": "设置新容器名称", "start": "启动新容器", "remove_old": "移除原容器"}
		if state.Intent != "" {
			name := names[state.Intent]
			if len(l.Record.Steps) == 0 || l.Record.Steps[len(l.Record.Steps)-1].Name != name {
				l.Record.Steps = append(l.Record.Steps, protocol.OperationStep{Name: name, State: "applying"})
			}
		} else if len(l.Record.Steps) > 0 {
			l.Record.Steps[len(l.Record.Steps)-1].State = "completed"
		}
		return e.Store.Save(ctx, l)
	}
	err = runtime.Update(ctx, l.Action.Container.ContainerRef, l.Record.ID, state, persist, &limitedLog{file: file})
	_ = file.Sync()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		if state.Stage == "" {
			return e.end(ctx, l, protocol.Rejected, err)
		}
		if len(l.Record.Steps) > 0 {
			l.Record.Steps[len(l.Record.Steps)-1].State = "unknown"
		}
		return e.attention(ctx, l, err)
	}
	return e.end(ctx, l, protocol.Succeeded, nil)
}
