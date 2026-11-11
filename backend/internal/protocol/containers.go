package protocol

import (
	"fmt"
	"regexp"
	"time"
)

var containerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ContainerRef struct {
	EngineID    string `json:"engine_id"`
	ContainerID string `json:"container_id"`
}

func (r ContainerRef) Validate() error {
	if r.EngineID == "" || len(r.EngineID) > 256 || !containerID.MatchString(r.ContainerID) {
		return fmt.Errorf("Docker 身份或完整容器 ID 无效")
	}
	return nil
}

type ContainerAction struct {
	ContainerRef
	Action           string `json:"action"`
	ExpectedRevision string `json:"expected_revision,omitempty"`
}

func (a ContainerAction) Validate() error {
	if err := a.ContainerRef.Validate(); err != nil {
		return err
	}
	if a.Action != "start" && a.Action != "stop" && a.Action != "restart" && a.Action != "update" {
		return fmt.Errorf("容器动作无效")
	}
	return nil
}

type ContainerPort struct {
	HostIP        string `json:"host_ip"`
	HostPort      uint16 `json:"host_port"`
	ContainerPort uint16 `json:"container_port"`
	Protocol      string `json:"protocol"`
}
type ContainerSummary struct {
	ContainerRef
	Name           string          `json:"name"`
	Image          string          `json:"image"`
	State          string          `json:"state"`
	Health         string          `json:"health"`
	Ports          []ContainerPort `json:"ports"`
	Project        string          `json:"project"`
	Service        string          `json:"service"`
	ReadOnlyReason string          `json:"read_only_reason,omitempty"`
}
type ContainerMount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only"`
}
type ContainerNetwork struct {
	Name string `json:"name"`
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}
type ContainerDetail struct {
	ContainerSummary
	CreatedAt     time.Time          `json:"created_at"`
	StartedAt     *time.Time         `json:"started_at,omitempty"`
	FinishedAt    *time.Time         `json:"finished_at,omitempty"`
	RestartPolicy string             `json:"restart_policy"`
	RestartCount  int                `json:"restart_count"`
	Mounts        []ContainerMount   `json:"mounts"`
	Networks      []ContainerNetwork `json:"networks"`
	TTY           bool               `json:"tty"`
	CheckedAt     time.Time          `json:"checked_at"`
}
type DockerState struct {
	EngineID   string             `json:"engine_id"`
	Version    string             `json:"version"`
	State      string             `json:"state"`
	Containers []ContainerSummary `json:"containers"`
	CheckedAt  time.Time          `json:"checked_at"`
	Error      *Error             `json:"error,omitempty"`
}
type ContainerLogs struct {
	ContainerRef
	Text      string    `json:"text"`
	Truncated bool      `json:"truncated"`
	CheckedAt time.Time `json:"checked_at"`
}
type DockerRead struct{}

func (*DockerRead) Kind() Kind { return "docker.read" }
func (*DockerRead) isPayload() {}

type DockerReadResult struct {
	State DockerState `json:"state"`
}

func (*DockerReadResult) Kind() Kind { return "docker.read.result" }
func (*DockerReadResult) isPayload() {}

type ContainerInspect struct{ ContainerRef }

func (*ContainerInspect) Kind() Kind { return "container.inspect" }
func (*ContainerInspect) isPayload() {}

type ContainerInspectResult struct {
	Container ContainerDetail `json:"container"`
}

func (*ContainerInspectResult) Kind() Kind { return "container.inspect.result" }
func (*ContainerInspectResult) isPayload() {}

type ContainerLogsRead struct {
	ContainerRef
	Tail int `json:"tail"`
}

func (*ContainerLogsRead) Kind() Kind { return "container.logs" }
func (*ContainerLogsRead) isPayload() {}

type ContainerLogsResult struct {
	Logs ContainerLogs `json:"logs"`
}

func (*ContainerLogsResult) Kind() Kind { return "container.logs.result" }
func (*ContainerLogsResult) isPayload() {}
