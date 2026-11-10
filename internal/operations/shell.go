package operations

import (
	"context"
	"fmt"
	"github.com/jkfujr/opsd/internal/protocol"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

const MaxExecutionLogBytes int64 = 16 * 1024 * 1024

type limitedLog struct {
	mu   sync.Mutex
	file *os.File
	size int64
}

func (w *limitedLog) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(data)
	left := MaxExecutionLogBytes - w.size
	if left > 0 {
		part := data
		if int64(len(part)) > left {
			part = part[:left]
		}
		written, e := w.file.Write(part)
		w.size += int64(written)
		if e != nil {
			return written, e
		}
	}
	return n, nil
}

func (e *Engine) shell(ctx context.Context, l *Local, boot string) error {
	if l.Record.State == protocol.AttentionRequired && l.RequestRevision != l.Record.Revision {
		return nil
	}
	if l.Request == "cancel" {
		return e.end(ctx, l, protocol.Cancelled, nil)
	}
	if l.Record.State == protocol.AttentionRequired && l.Request == "resolve" {
		return e.end(ctx, l, protocol.ResolvedManually, nil)
	}
	if l.IntentStep != nil {
		return e.attention(ctx, l, fmt.Errorf("任务已开始，进程中断后的结果需要人工核对"))
	}
	if l.BootID != "" && l.BootID != boot {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("主机已重启，未补跑旧任务"))
	}
	if l.Action.Shell == nil || e.LogDirectory == "" {
		return e.end(ctx, l, protocol.Rejected, fmt.Errorf("任务执行参数不完整"))
	}
	a := l.Action.Shell
	return e.runCommand(ctx, l, exec.CommandContext(ctx, "/bin/bash", "-lc", a.Script), a.Directory, "执行任务 "+a.Name)
}

func (e *Engine) runCommand(ctx context.Context, l *Local, command *exec.Cmd, directory, name string) error {
	if err := os.MkdirAll(e.LogDirectory, 0700); err != nil {
		return e.end(ctx, l, protocol.Failed, err)
	}
	file, err := os.OpenFile(filepath.Join(e.LogDirectory, l.Record.ID+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return e.attention(ctx, l, err)
	}
	defer file.Close()
	zero := 0
	l.IntentStep = &zero
	l.Record.State = protocol.Applying
	l.Record.Steps = []protocol.OperationStep{{Name: name, State: "applying"}}
	if err = e.Store.Save(ctx, l); err != nil {
		return err
	}
	writer := &limitedLog{file: file}
	command.Stdout = writer
	command.Stderr = writer
	command.Dir = directory
	err = command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_ = file.Sync()
	if err != nil {
		l.Record.Steps[0].State = "failed"
		return e.end(ctx, l, protocol.Failed, err)
	}
	l.Record.Steps[0].State = "completed"
	return e.end(ctx, l, protocol.Succeeded, nil)
}

func (e *Engine) ExecutionLogs(ctx context.Context, p protocol.ExecutionLogsRead) (protocol.ExecutionLogs, error) {
	result := protocol.ExecutionLogs{Data: []byte{}, Offset: p.Offset, NextOffset: p.Offset}
	l, err := e.Store.Get(ctx, p.OperationID)
	if err != nil {
		return result, err
	}
	if !ValidID(p.OperationID) || l.Action == nil || (l.Action.Kind != "shell" && l.Action.Kind != "compose" && l.Action.Kind != "container") {
		return result, fmt.Errorf("操作没有执行日志")
	}
	file, err := os.Open(filepath.Join(e.LogDirectory, p.OperationID+".log"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer file.Close()
	if _, err = file.Seek(p.Offset, io.SeekStart); err != nil {
		return result, err
	}
	result.Data, err = io.ReadAll(io.LimitReader(file, int64(p.Limit)))
	if err != nil {
		return result, err
	}
	info, err := file.Stat()
	if err != nil {
		return result, err
	}
	result.NextOffset = p.Offset + int64(len(result.Data))
	result.HasMore = result.NextOffset < info.Size()
	result.Truncated = info.Size() >= MaxExecutionLogBytes
	return result, nil
}
