package operations

import (
	"context"
	"fmt"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

type DatabaseRuntime interface {
	Prepare(context.Context, protocol.DatabaseAction) ([]string, error)
	Observe(context.Context, protocol.DatabaseAction, string) (databaseengine.Observation, error)
	Apply(context.Context, protocol.DatabaseAction, string) error
}
type databaseState struct {
	Stages    []string `json:"stages"`
	Index     int      `json:"index"`
	Attempted bool     `json:"attempted"`
	Before    string   `json:"before"`
	Source    string   `json:"source"`
}

func (e *Engine) database(ctx context.Context, l *Local) error {
	if e.Databases == nil || l.Action.Database == nil {
		return e.attention(ctx, l, fmt.Errorf("数据库执行入口不可用"))
	}
	if l.Record.State == protocol.AttentionRequired && l.RequestRevision != l.Record.Revision {
		return nil
	}
	a := *l.Action.Database
	if l.Record.State == protocol.AttentionRequired && l.Request == "resolve" && l.DatabaseState == nil {
		return e.end(ctx, l, protocol.ResolvedManually, nil)
	}
	if l.DatabaseState == nil {
		stages, err := e.Databases.Prepare(ctx, a)
		if err != nil {
			return e.attention(ctx, l, err)
		}
		l.DatabaseState = &databaseState{Stages: stages}
		l.Record.Steps = []protocol.OperationStep{}
		for _, stage := range stages {
			name := map[string]string{"database": "更新数据库", "memory": "更新代理账号", "runtime": "应用代理运行配置", "disk": "保存代理磁盘配置"}[stage]
			l.Record.Steps = append(l.Record.Steps, protocol.OperationStep{Name: name, State: "pending"})
		}
		l.Record.State = protocol.Applying
		if err = e.Store.Save(ctx, l); err != nil {
			return err
		}
	}
	state := l.DatabaseState
	for state.Index < len(state.Stages) {
		stage := state.Stages[state.Index]
		observed, err := e.Databases.Observe(ctx, a, stage)
		if err != nil {
			return e.attention(ctx, l, err)
		}
		l.Record.LastObservedAt = &observed.CheckedAt
		if l.Record.State == protocol.AttentionRequired && l.Request == "resolve" {
			return e.end(ctx, l, protocol.ResolvedManually, nil)
		}
		if state.Source != "" && stage != "memory" && observed.Source != state.Source {
			return e.attention(ctx, l, fmt.Errorf("ProxySQL 账号配置出现外部修改"))
		}
		if !observed.Satisfied {
			if state.Attempted {
				if l.Request != "recover" || l.RequestRevision != l.Record.Revision || observed.Fingerprint != state.Before {
					return e.attention(ctx, l, fmt.Errorf("原步骤结果待核对，请重新处理或接受现状"))
				}
			}
			state.Before = observed.Fingerprint
			state.Attempted = true
			l.Record.State = protocol.Applying
			l.Record.Steps[state.Index].State = "applying"
			if err = e.Store.Save(ctx, l); err != nil {
				return err
			}
			err = e.Databases.Apply(ctx, a, stage)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			observed, readErr := e.Databases.Observe(ctx, a, stage)
			if readErr != nil {
				return e.attention(ctx, l, readErr)
			}
			l.Record.LastObservedAt = &observed.CheckedAt
			if !observed.Satisfied {
				if err == nil {
					err = fmt.Errorf("数据库目标状态尚未读回")
				}
				l.Record.Steps[state.Index].State = "unknown"
				return e.attention(ctx, l, err)
			}
		}
		if stage == "memory" {
			state.Source = observed.Source
		}
		l.Record.Steps[state.Index].State = "completed"
		l.Record.Steps[state.Index].ObservedAt = &observed.CheckedAt
		state.Index++
		state.Attempted = false
		state.Before = ""
		l.Request = ""
		l.RequestRevision = 0
		if err = e.Store.Save(ctx, l); err != nil {
			return err
		}
	}
	return e.end(ctx, l, protocol.Succeeded, nil)
}
