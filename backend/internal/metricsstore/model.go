// Package metricsstore 管理秒级样本、持久补传和受控时序查询。
package metricsstore

import (
	"fmt"
	"math"
	"path"
	"strings"
	"time"
)

const (
	ThanosVersion         = "v0.42.5"
	BufferBytes     int64 = 64 << 20
	BufferAge             = time.Hour
	MaxBatchSamples       = 10000
	MaxQueryPoints        = 10000
)

func DiskPaused(paused bool, used, budget, available int64) bool {
	if budget <= 0 {
		return true
	}
	if paused {
		return used*10 >= budget*8 || available < budget/5
	}
	return used*10 >= budget*9 || available < budget/10
}

type Config struct {
	Revision        uint64 `json:"revision"`
	Enabled         bool   `json:"enabled"`
	NodeID          string `json:"node_id"`
	StorageTargetID string `json:"storage_target_id"`
	StorageRootID   string `json:"storage_root_id,omitempty"`
	Prefix          string `json:"prefix"`
	Directory       string `json:"directory"`
	DiskBudgetBytes int64  `json:"disk_budget_bytes"`
	LocalDays       int    `json:"local_days"`
	ArchiveDays     int    `json:"archive_days"`
	OperationID     string `json:"operation_id,omitempty"`
	State           string `json:"state"`
}

func DefaultConfig(cluster string) Config {
	return Config{Prefix: "opsd/metrics/" + cluster + "/", Directory: "/var/lib/opsd-monitoring", DiskBudgetBytes: 80 << 30, LocalDays: 30, ArchiveDays: 365, State: "disabled"}
}

func ValidPrefix(v string) bool {
	return v != "" && len(v) <= 768 && !strings.HasPrefix(v, "/") && strings.HasSuffix(v, "/") && path.Clean(v)+"/" == v && !strings.ContainsAny(v, "\\\x00\r\n")
}

func (c Config) Validate() error {
	if !ValidPrefix(c.Prefix) || !strings.HasPrefix(c.Directory, "/") || path.Clean(c.Directory) != c.Directory || c.Directory == "/" || strings.ContainsAny(c.Directory, "\x00\r\n") {
		return fmt.Errorf("监控归档目录或本地目录无效")
	}
	if c.LocalDays != 30 || c.ArchiveDays != 365 || c.DiskBudgetBytes < 1<<30 || c.DiskBudgetBytes > 1<<50 {
		return fmt.Errorf("本地保留 30 天，归档保留 365 天，磁盘预算至少 1GiB")
	}
	if c.Enabled && (c.NodeID == "" || c.StorageTargetID == "") {
		return fmt.Errorf("请选择监控节点和 S3 存储目标")
	}
	return nil
}

// Sample 只允许稳定资源标识；节点及集群标签由认证接收端添加。
type Sample struct {
	Metric     string  `json:"metric"`
	ResourceID string  `json:"resource_id,omitempty"`
	Backend    string  `json:"backend,omitempty"`
	At         int64   `json:"at"`
	Value      float64 `json:"value"`
}
type Batch struct {
	ID       string   `json:"id"`
	Sequence int64    `json:"sequence"`
	At       int64    `json:"at"`
	Samples  []Sample `json:"samples"`
	Errors   []string `json:"errors,omitempty"`
}
type Gap struct {
	NodeID  string `json:"node_id"`
	From    int64  `json:"from"`
	To      int64  `json:"to"`
	Reason  string `json:"reason"`
	BatchID string `json:"batch_id,omitempty"`
}
type Receipt struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
	Error  string `json:"error,omitempty"`
}
type BufferStatus struct {
	Batches int64 `json:"batches"`
	Bytes   int64 `json:"bytes"`
	Oldest  int64 `json:"oldest"`
	Latest  int64 `json:"latest"`
	Gaps    []Gap `json:"gaps"`
}

var MetricModules = map[string]string{
	"opsd_node_up": "monitoring", "opsd_node_cpu_cores": "monitoring", "opsd_node_cpu_usage_percent": "monitoring",
	"opsd_node_memory_used_bytes": "monitoring", "opsd_node_memory_total_bytes": "monitoring",
	"opsd_node_disk_used_bytes": "monitoring", "opsd_node_disk_total_bytes": "monitoring",
	"opsd_node_network_receive_bytes_per_second": "monitoring", "opsd_node_network_transmit_bytes_per_second": "monitoring",
	"opsd_docker_up": "containers", "opsd_container_running": "containers",
	"opsd_database_up": "databases", "opsd_database_connections": "databases",
	"opsd_proxysql_server_connections": "databases", "opsd_proxysql_queries_total": "databases",
	"opsd_galera_members": "databases", "opsd_galera_receive_queue": "databases", "opsd_galera_send_queue": "databases", "opsd_galera_flow_control_ratio": "databases",
	"opsd_proxysql_up": "databases", "opsd_proxysql_connections": "databases", "opsd_proxysql_pool_used": "databases", "opsd_proxysql_pool_free": "databases",
}

func (b Batch) Validate(now time.Time) error {
	if len(b.ID) != 43 || strings.Trim(b.ID, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-") != "" || b.Sequence < 1 || len(b.Samples) > MaxBatchSamples || len(b.Errors) > 32 {
		return fmt.Errorf("监控批次标识、序号或大小无效")
	}
	if b.At > now.Add(time.Minute).UnixMilli() || b.At < now.Add(-2*time.Hour).UnixMilli() {
		return fmt.Errorf("监控批次超出接收时间窗口")
	}
	seen := map[string]bool{}
	for _, s := range b.Samples {
		module := MetricModules[s.Metric]
		if module == "" || len(s.ResourceID) > 128 || strings.ContainsAny(s.ResourceID, "\x00\r\n") || math.IsNaN(s.Value) || math.IsInf(s.Value, 0) || s.At != b.At {
			return fmt.Errorf("监控样本、资源或时间无效")
		}
		if (module == "monitoring" || s.Metric == "opsd_docker_up") && s.ResourceID != "" {
			return fmt.Errorf("主机指标不能携带其他资源标识")
		}
		if module != "monitoring" && s.Metric != "opsd_docker_up" && s.ResourceID == "" {
			return fmt.Errorf("资源指标缺少所属资源")
		}
		if len(s.Backend) > 256 || strings.ContainsAny(s.Backend, "\x00\r\n") || s.Backend != "" && s.Metric != "opsd_proxysql_pool_used" && s.Metric != "opsd_proxysql_pool_free" {
			return fmt.Errorf("指标后端标识无效")
		}
		key := s.Metric + "\x00" + s.ResourceID + "\x00" + s.Backend
		if seen[key] {
			return fmt.Errorf("同一时间的指标重复")
		}
		seen[key] = true
	}
	for _, e := range b.Errors {
		if len(e) > 1024 {
			return fmt.Errorf("采集错误过长")
		}
	}
	return nil
}
