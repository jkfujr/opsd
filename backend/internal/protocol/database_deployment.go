package protocol

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// DatabaseDeployment 只在认证节点通道及本机执行记录中传递，凭据不进入 HTTP 或共享操作记录。
type DatabaseDeployment struct {
	ID              string             `json:"id"`
	Kind            string             `json:"kind"`
	Image           string             `json:"image"`
	Directory       string             `json:"directory"`
	Address         string             `json:"address"`
	SQLPort         int                `json:"sql_port"`
	ReplicationPort int                `json:"replication_port"`
	ISTPort         int                `json:"ist_port"`
	SSTPort         int                `json:"sst_port"`
	AdminPort       int                `json:"admin_port"`
	ClusterName     string             `json:"cluster_name"`
	ClusterID       string             `json:"cluster_id"`
	Seeds           []string           `json:"seeds"`
	ReachableSeeds  []string           `json:"reachable_seeds,omitempty"`
	Bootstrap       bool               `json:"bootstrap"`
	InstallDocker   bool               `json:"install_docker"`
	Connection      DatabaseConnection `json:"connection"`
	SSTMethod       string             `json:"sst_method"`
	SSTUser         string             `json:"sst_user"`
	SSTPassword     string             `json:"sst_password"`
	ProviderOptions string             `json:"provider_options,omitempty"`
	TLS             map[string]string  `json:"tls,omitempty"`
	Backends        []ProxyEndpoint    `json:"backends"`
	MonitorUser     string             `json:"monitor_user,omitempty"`
	MonitorPassword string             `json:"monitor_password,omitempty"`
}

var deploymentID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)
var deploymentImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:-]*(?:@sha256:[a-f0-9]{64})?$`)

func (d DatabaseDeployment) Validate() error {
	if !deploymentID.MatchString(d.ID) || !slices.Contains([]string{"galera", "proxysql"}, d.Kind) || len(d.Image) > 255 || !deploymentImage.MatchString(d.Image) || strings.HasSuffix(d.Image, ":latest") || !strings.Contains(d.Image, ":") {
		return fmt.Errorf("部署标识或固定镜像版本无效")
	}
	clean := filepath.ToSlash(filepath.Clean(d.Directory))
	if !strings.HasPrefix(clean, "/") || clean == "/" || strings.ContainsAny(clean, "\x00\r\n:") || len(clean) > 1024 || clean != d.Directory {
		return fmt.Errorf("请使用独立的数据库绝对目录")
	}
	ip, err := netip.ParseAddr(d.Address)
	if err != nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() {
		return fmt.Errorf("数据库成员地址无效")
	}
	if err = d.Connection.Validate(); err != nil {
		return err
	}
	if d.Connection.Kind != d.Kind || d.Connection.Password == "" {
		return fmt.Errorf("数据库管理凭据缺失")
	}
	ports := []int{d.SQLPort}
	if d.Kind == "galera" {
		ports = append(ports, d.ReplicationPort, d.ISTPort, d.SSTPort)
		if !deploymentID.MatchString(d.ClusterName) || len(d.Seeds) == 0 || len(d.Seeds) > 100 || d.SSTMethod != "mariabackup" || !databaseUser.MatchString(d.SSTUser) || d.SSTPassword == "" || strings.ContainsAny(d.SSTPassword, "\x00\r\n'\"\\") {
			return fmt.Errorf("Galera 集群及同步配置无效")
		}
		if !d.Bootstrap && d.ClusterID == "" {
			return fmt.Errorf("加入节点缺少目标集群身份")
		}
		for _, seed := range append(append([]string{}, d.Seeds...), d.ReachableSeeds...) {
			if _, err = netip.ParseAddrPort(seed); err != nil {
				return fmt.Errorf("Galera 成员地址或端口无效")
			}
		}
	} else {
		ports = append(ports, d.AdminPort)
		if len(d.Backends) == 0 || len(d.Backends) > 100 {
			return fmt.Errorf("代理缺少数据库后端")
		}
		for _, backend := range d.Backends {
			if err = backend.Validate(); err != nil {
				return err
			}
		}
	}
	for i, port := range ports {
		if port < 1 || port > 65535 || slices.Contains(ports[:i], port) {
			return fmt.Errorf("部署端口无效或重复")
		}
	}
	for name, contents := range d.TLS {
		if !slices.Contains([]string{"ca.pem", "cert.pem", "key.pem"}, name) || len(contents) > 64*1024 {
			return fmt.Errorf("数据库传输证书无效")
		}
	}
	if len(d.TLS) > 0 && (d.TLS["ca.pem"] == "" || d.TLS["cert.pem"] == "" || d.TLS["key.pem"] == "") {
		return fmt.Errorf("请补齐同步 CA、证书和私钥")
	}
	if strings.ContainsAny(d.ProviderOptions, "\x00\r\n\"") || len(d.ProviderOptions) > 8192 {
		return fmt.Errorf("Galera 传输配置无效")
	}
	return nil
}

type DatabaseDeploymentCheck struct {
	Deployment DatabaseDeployment `json:"deployment"`
}

func (*DatabaseDeploymentCheck) Kind() Kind { return "database.deployment.check" }
func (*DatabaseDeploymentCheck) isPayload() {}

type DatabaseDeploymentStateRead struct {
	Deployment DatabaseDeployment `json:"deployment"`
}

func (*DatabaseDeploymentStateRead) Kind() Kind { return "database.deployment.state.read" }
func (*DatabaseDeploymentStateRead) isPayload() {}

type DatabaseDeploymentState struct {
	ContainerID string `json:"container_id"`
	State       string `json:"state"`
}

func (*DatabaseDeploymentState) Kind() Kind { return "database.deployment.state" }
func (*DatabaseDeploymentState) isPayload() {}

type DatabaseDeploymentEnvironment struct {
	DockerState  string   `json:"docker_state"`
	EngineID     string   `json:"engine_id"`
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	FreeBytes    uint64   `json:"free_bytes"`
	Fingerprint  string   `json:"fingerprint"`
	Problems     []string `json:"problems"`
}

func (*DatabaseDeploymentEnvironment) Kind() Kind { return "database.deployment.environment" }
func (*DatabaseDeploymentEnvironment) isPayload() {}

type DatabaseDeploymentProfileRead struct {
	Connection  DatabaseConnection `json:"connection"`
	ContainerID string             `json:"container_id"`
}

func (*DatabaseDeploymentProfileRead) Kind() Kind { return "database.deployment.profile.read" }
func (*DatabaseDeploymentProfileRead) isPayload() {}

type DatabaseDeploymentProfile struct {
	Deployment  DatabaseDeployment `json:"deployment"`
	Managed     bool               `json:"managed"`
	ContainerID string             `json:"container_id,omitempty"`
	State       DatabaseState      `json:"state"`
	DataBytes   uint64             `json:"data_bytes"`
	Problems    []string           `json:"problems"`
}

type DatabaseSeedUpdate struct {
	ContainerID string   `json:"container_id"`
	Seeds       []string `json:"seeds"`
}

func (u DatabaseSeedUpdate) Validate() error {
	if len(u.ContainerID) != 64 || strings.Trim(u.ContainerID, "0123456789abcdef") != "" || len(u.Seeds) == 0 || len(u.Seeds) > 100 {
		return fmt.Errorf("数据库启动地址参数无效")
	}
	for _, address := range u.Seeds {
		if _, err := netip.ParseAddrPort(address); err != nil {
			return fmt.Errorf("数据库成员地址无效")
		}
	}
	return nil
}

func (*DatabaseDeploymentProfile) Kind() Kind { return "database.deployment.profile" }
func (*DatabaseDeploymentProfile) isPayload() {}

type DatabaseNetworkCheck struct {
	LocalAddress string          `json:"local_address"`
	Endpoints    []ProxyEndpoint `json:"endpoints"`
}

func (*DatabaseNetworkCheck) Kind() Kind { return "database.network.check" }
func (*DatabaseNetworkCheck) isPayload() {}

type DatabaseNetworkChecked struct {
	Checked bool `json:"checked"`
}

func (*DatabaseNetworkChecked) Kind() Kind { return "database.network.checked" }
func (*DatabaseNetworkChecked) isPayload() {}
