package protocol

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"time"
)

type ProxyHostgroups struct {
	Writer       int `json:"writer"`
	BackupWriter int `json:"backup_writer"`
	Reader       int `json:"reader"`
	Offline      int `json:"offline"`
}

func (g ProxyHostgroups) Contains(group int) bool {
	return slices.Contains([]int{g.Writer, g.BackupWriter, g.Reader, g.Offline}, group)
}

func (g ProxyHostgroups) Validate() error {
	values := []int{g.Writer, g.BackupWriter, g.Reader, g.Offline}
	for i, value := range values {
		if value < 0 || value > 2147483647 || i > 0 && value == 0 || slices.Contains(values[:i], value) {
			return fmt.Errorf("代理主机组无效或重复")
		}
	}
	return nil
}

type ProxyEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func (p ProxyEndpoint) Key() string { return p.Host + ":" + strconv.Itoa(p.Port) }

var proxyHost = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,255}$`)

func (p ProxyEndpoint) Validate() error {
	if p.Port < 1 || p.Port > 65535 || !proxyHost.MatchString(p.Host) {
		return fmt.Errorf("代理后端地址无效")
	}
	return nil
}

type ProxyBackend struct {
	ProxyEndpoint
	NodeID     string `json:"node_id"`
	InstanceID string `json:"instance_id"`
}

type ProxyAccountBinding struct {
	Name       string `json:"name"`
	NodeID     string `json:"node_id"`
	InstanceID string `json:"instance_id"`
}

type ProxyRoutingPolicy struct {
	Enabled    bool                  `json:"enabled"`
	Automatic  bool                  `json:"automatic"`
	Hostgroups ProxyHostgroups       `json:"hostgroups"`
	Backends   []ProxyBackend        `json:"backends"`
	Retired    []ProxyEndpoint       `json:"retired"`
	Accounts   []ProxyAccountBinding `json:"accounts"`
}

func (p ProxyRoutingPolicy) Validate() error {
	if err := p.Hostgroups.Validate(); err != nil {
		return err
	}
	if len(p.Backends) < 1 || len(p.Backends) > 100 || len(p.Retired) > 100 || len(p.Accounts) > 1000 {
		return fmt.Errorf("代理后端或账号数量无效")
	}
	seen := map[string]bool{}
	for _, backend := range p.Backends {
		if err := backend.ProxyEndpoint.Validate(); err != nil {
			return err
		}
		ip, err := netip.ParseAddr(backend.Host)
		if backend.NodeID == "" || backend.InstanceID == "" || err == nil && (ip.IsLoopback() || ip.IsUnspecified()) || seen[backend.Key()] {
			return fmt.Errorf("请为每个数据库成员设置代理可达的独立 SQL 地址")
		}
		seen[backend.Key()] = true
	}
	for _, backend := range p.Retired {
		if err := backend.Validate(); err != nil {
			return err
		}
		if seen[backend.Key()] {
			return fmt.Errorf("退出成员与当前后端重复")
		}
		seen[backend.Key()] = true
	}
	return nil
}

type ProxyRouteServer struct {
	ProxyEndpoint
	Hostgroup      int    `json:"hostgroup"`
	State          string `json:"state"`
	Weight         int    `json:"weight"`
	MaxConnections int    `json:"max_connections"`
	UseSSL         bool   `json:"use_ssl"`
}

type ProxyPingSample struct {
	ProxyEndpoint
	At        time.Time `json:"at"`
	LatencyUS int64     `json:"latency_us"`
}

type ProxyRouteMapping struct {
	Hostgroups            ProxyHostgroups `json:"hostgroups"`
	MaxWriters            int             `json:"max_writers"`
	WriterIsAlsoReader    int             `json:"writer_is_also_reader"`
	MaxTransactionsBehind int             `json:"max_transactions_behind"`
}

type ProxyRoutingObservation struct {
	Hostgroups           ProxyHostgroups     `json:"hostgroups"`
	Mappings             []ProxyRouteMapping `json:"mappings"`
	Servers              []ProxyRouteServer  `json:"servers"`
	RuntimeServers       []ProxyRouteServer  `json:"runtime_servers"`
	Accounts             []string            `json:"accounts"`
	Samples              []ProxyPingSample   `json:"samples"`
	Fingerprint          string              `json:"fingerprint"`
	UnmanagedFingerprint string              `json:"unmanaged_fingerprint"`
	PendingChanges       bool                `json:"pending_changes"`
	ClusterSync          bool                `json:"cluster_sync"`
	SplitRouting         bool                `json:"split_routing"`
	CheckedAt            time.Time           `json:"checked_at"`
}

type ProxyRouteAction struct {
	Hostgroups            ProxyHostgroups    `json:"hostgroups"`
	Servers               []ProxyRouteServer `json:"servers"`
	Remove                []ProxyEndpoint    `json:"remove"`
	MaxTransactionsBehind int                `json:"max_transactions_behind"`
	ExpectedFingerprint   string             `json:"expected_fingerprint"`
	UnmanagedFingerprint  string             `json:"unmanaged_fingerprint"`
	PolicyVersion         string             `json:"policy_version"`
}

func (a ProxyRouteAction) Validate() error {
	if err := a.Hostgroups.Validate(); err != nil {
		return err
	}
	if len(a.Servers) < 1 || len(a.Servers) > 100 || len(a.Remove) > 100 || a.MaxTransactionsBehind < 0 || len(a.ExpectedFingerprint) != 64 || len(a.UnmanagedFingerprint) != 64 || len(a.PolicyVersion) != 64 {
		return fmt.Errorf("代理路由配置无效，请重新预览")
	}
	seen := map[string]bool{}
	for _, server := range a.Servers {
		if err := server.ProxyEndpoint.Validate(); err != nil {
			return err
		}
		if server.Hostgroup != a.Hostgroups.Writer || server.Weight < 1 || server.Weight > 1000000 || server.MaxConnections < 1 || server.MaxConnections > 10000000 || seen[server.Key()] {
			return fmt.Errorf("代理后端权重或连接上限无效")
		}
		seen[server.Key()] = true
	}
	for _, endpoint := range a.Remove {
		if err := endpoint.Validate(); err != nil {
			return err
		}
		if seen[endpoint.Key()] {
			return fmt.Errorf("代理退出成员重复")
		}
		seen[endpoint.Key()] = true
	}
	return nil
}

type ProxyRoutingRead struct {
	Connection DatabaseConnection `json:"connection"`
	Hostgroups *ProxyHostgroups   `json:"hostgroups,omitempty"`
}

func (*ProxyRoutingRead) Kind() Kind { return "database.proxy_routing.read" }
func (*ProxyRoutingRead) isPayload() {}

type ProxyRoutingReadResult struct {
	Routing ProxyRoutingObservation `json:"routing"`
}

func (*ProxyRoutingReadResult) Kind() Kind { return "database.proxy_routing.result" }
func (*ProxyRoutingReadResult) isPayload() {}
