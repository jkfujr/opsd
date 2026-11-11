package protocol

import "time"

// Metrics 不把尚无有效差值的 CPU 和网络计数表示为零。
type Metrics struct {
	CollectedAt time.Time `json:"collected_at"`
	CPUCores    int       `json:"cpu_cores"`
	CPUUsage    *float64  `json:"cpu_usage,omitempty"`
	MemoryUsed  *uint64   `json:"memory_used,omitempty"`
	MemoryTotal *uint64   `json:"memory_total,omitempty"`
	DiskUsed    *uint64   `json:"disk_used,omitempty"`
	DiskTotal   *uint64   `json:"disk_total,omitempty"`
	NetworkRX   *float64  `json:"network_rx,omitempty"`
	NetworkTX   *float64  `json:"network_tx,omitempty"`
	Errors      []string  `json:"errors"`
}

type MonitoringRead struct {
	Stream string `json:"stream"`
}

func (*MonitoringRead) Kind() Kind { return "monitoring.read" }
func (*MonitoringRead) isPayload() {}

type MonitoringResult struct {
	Metrics       Metrics `json:"metrics"`
	Firewall      string  `json:"firewall"`
	FirewallError string  `json:"firewall_error,omitempty"`
}

func (*MonitoringResult) Kind() Kind { return "monitoring.result" }
func (*MonitoringResult) isPayload() {}
