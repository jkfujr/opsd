package protocol

import (
	"fmt"
	"path"
	"regexp"
	"time"
)

type ComposeRef struct {
	ID               string   `json:"id"`
	EngineID         string   `json:"engine_id"`
	Name             string   `json:"name"`
	Directory        string   `json:"directory"`
	Files            []string `json:"files"`
	EnvironmentFiles []string `json:"environment_files"`
}

var composeName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

func (r ComposeRef) Validate() error {
	if !containerID.MatchString(r.ID) || r.EngineID == "" || !composeName.MatchString(r.Name) || !path.IsAbs(r.Directory) || len(r.Files) < 1 || len(r.Files) > 32 || len(r.EnvironmentFiles) > 32 {
		return fmt.Errorf("Compose 项目标识或定义目录无效")
	}
	for _, file := range append(append([]string{}, r.Files...), r.EnvironmentFiles...) {
		if !path.IsAbs(file) || len(file) > 4096 {
			return fmt.Errorf("Compose 文件须为本机绝对路径")
		}
	}
	return nil
}

type ComposeAction struct {
	Project ComposeRef `json:"project"`
	Action  string     `json:"action"`
}

func (a ComposeAction) Validate() error {
	if e := a.Project.Validate(); e != nil {
		return e
	}
	switch a.Action {
	case "pull", "start", "stop", "recreate", "update":
		return nil
	}
	return fmt.Errorf("Compose 操作无效")
}

type ComposeProject struct {
	ComposeRef
	Registered     bool               `json:"registered"`
	Services       []ContainerSummary `json:"services"`
	ReadOnlyReason string             `json:"read_only_reason,omitempty"`
	CheckedAt      time.Time          `json:"checked_at"`
}
type ComposeRead struct{}

func (*ComposeRead) Kind() Kind { return "compose.read" }
func (*ComposeRead) isPayload() {}

type ComposeReadResult struct {
	Projects []ComposeProject `json:"projects"`
}

func (*ComposeReadResult) Kind() Kind { return "compose.read.result" }
func (*ComposeReadResult) isPayload() {}
