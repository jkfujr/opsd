package protocol

import (
	"fmt"
	"path"
	"strings"
	"time"
)

type MonitoringDeployment struct {
	ClusterID       string        `json:"cluster_id"`
	NodeID          string        `json:"node_id"`
	Enabled         bool          `json:"enabled"`
	Directory       string        `json:"directory"`
	Prefix          string        `json:"prefix"`
	DiskBudgetBytes int64         `json:"disk_budget_bytes"`
	Storage         S3Connection  `json:"storage"`
	Credentials     S3Credentials `json:"credentials"`
}

func (d MonitoringDeployment) Validate() error {
	if len(d.ClusterID) != 43 || strings.Trim(d.ClusterID, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-") != "" || len(d.NodeID) != 32 || strings.Trim(d.NodeID, "0123456789abcdef") != "" {
		return fmt.Errorf("监控部署身份无效")
	}
	if !path.IsAbs(d.Directory) || path.Clean(d.Directory) != d.Directory || d.Directory == "/" || len(d.Directory) > 2048 || strings.ContainsAny(d.Directory, "\x00\r\n") {
		return fmt.Errorf("监控数据目录无效")
	}
	if !d.Enabled {
		return nil
	}
	if d.Prefix == "" || path.Clean(d.Prefix)+"/" != d.Prefix || strings.HasPrefix(d.Prefix, "/") || strings.ContainsAny(d.Prefix, "\\\x00\r\n") || d.DiskBudgetBytes < 1<<30 {
		return fmt.Errorf("监控前缀或容量预算无效")
	}
	if err := d.Storage.Validate(); err != nil {
		return err
	}
	return d.Credentials.Validate()
}

type MonitoringStateRead struct {
	Deployment MonitoringDeployment `json:"deployment"`
}

func (*MonitoringStateRead) Kind() Kind { return "monitoring.deployment.read" }
func (*MonitoringStateRead) isPayload() {}

type MonitoringComponent struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}
type MonitoringState struct {
	State                string                `json:"state"`
	Components           []MonitoringComponent `json:"components"`
	DiskUsedBytes        int64                 `json:"disk_used_bytes"`
	DiskAvailableBytes   int64                 `json:"disk_available_bytes"`
	ArchivePendingBlocks int                   `json:"archive_pending_blocks"`
	OldestUnshippedAt    *time.Time            `json:"oldest_unshipped_at,omitempty"`
	CheckedAt            time.Time             `json:"checked_at"`
}

func (*MonitoringState) Kind() Kind { return "monitoring.deployment.state" }
func (*MonitoringState) isPayload() {}

type ResourceMetricsRead struct {
	Connections map[string]DatabaseConnection `json:"connections"`
}

func (*ResourceMetricsRead) Kind() Kind { return "monitoring.resources.read" }
func (*ResourceMetricsRead) isPayload() {}

type ResourceMetric struct {
	Metric     string  `json:"metric"`
	ResourceID string  `json:"resource_id,omitempty"`
	Backend    string  `json:"backend,omitempty"`
	Value      float64 `json:"value"`
}
type ResourceMetrics struct {
	Docker  *DockerState     `json:"docker,omitempty"`
	Metrics []ResourceMetric `json:"metrics"`
	At      time.Time        `json:"at"`
	Errors  []string         `json:"errors"`
}

func (*ResourceMetrics) Kind() Kind { return "monitoring.resources.result" }
func (*ResourceMetrics) isPayload() {}
