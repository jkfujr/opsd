package protocol

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type ShellAction struct {
	TaskID    string `json:"task_id"`
	Name      string `json:"name"`
	Script    string `json:"script"`
	Directory string `json:"directory"`
}

func (a ShellAction) Validate() error {
	if !taskIDPattern.MatchString(a.TaskID) || len(a.Name) < 1 || len(a.Name) > 255 || len(a.Script) < 1 || len(a.Script) > 256*1024 || !path.IsAbs(a.Directory) || strings.ContainsRune(a.Script, 0) {
		return fmt.Errorf("任务标识、名称、脚本或工作目录无效")
	}
	return nil
}

type OperationCancel struct {
	OperationID      string `json:"operation_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

func (*OperationCancel) Kind() Kind { return "operation.cancel" }
func (*OperationCancel) isPayload() {}

type ExecutionLogsRead struct {
	OperationID string `json:"operation_id"`
	Offset      int64  `json:"offset"`
	Limit       int    `json:"limit"`
}

func (*ExecutionLogsRead) Kind() Kind { return "execution.logs" }
func (*ExecutionLogsRead) isPayload() {}

type ExecutionLogs struct {
	Data       []byte `json:"data"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	HasMore    bool   `json:"has_more"`
	Truncated  bool   `json:"truncated"`
}
type ExecutionLogsResult struct {
	Logs ExecutionLogs `json:"logs"`
}

func (*ExecutionLogsResult) Kind() Kind { return "execution.logs.result" }
func (*ExecutionLogsResult) isPayload() {}
