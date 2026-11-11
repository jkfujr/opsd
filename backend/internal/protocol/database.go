package protocol

import (
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type DatabaseConnection struct {
	PoolKey        string   `json:"pool_key,omitempty"`
	Kind           string   `json:"kind"`
	Address        string   `json:"address"`
	Socket         string   `json:"socket,omitempty"`
	Username       string   `json:"username"`
	Password       string   `json:"password"`
	ProtectedUsers []string `json:"protected_users"`
	BackupRoot     string   `json:"backup_root,omitempty"`
}

func (c DatabaseConnection) Validate() error {
	if !slices.Contains([]string{"galera", "proxysql"}, c.Kind) || c.Username == "" || len(c.Username) > 128 || len(c.Password) > 4096 {
		return fmt.Errorf("数据库连接信息无效")
	}
	if c.Socket != "" {
		if !filepath.IsAbs(c.Socket) || strings.Contains(c.Socket, "\x00") {
			return fmt.Errorf("数据库 Socket 须为本机绝对路径")
		}
	} else {
		host, port, e := net.SplitHostPort(c.Address)
		ip, iperr := netip.ParseAddr(host)
		number, portErr := strconv.Atoi(port)
		if e != nil || iperr != nil || !ip.IsLoopback() || portErr != nil || number < 1 || number > 65535 {
			return fmt.Errorf("数据库须通过所属节点的本机地址连接")
		}
	}
	return nil
}

type DatabaseCandidate struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Connection  DatabaseConnection `json:"connection"`
	ContainerID string             `json:"container_id,omitempty"`
	Running     bool               `json:"running"`
}
type DatabaseSchema struct {
	Name      string `json:"name"`
	Charset   string `json:"charset"`
	Collation string `json:"collation"`
	Bytes     uint64 `json:"bytes"`
	Protected bool   `json:"protected"`
}
type DatabaseGrant struct {
	Database   string   `json:"database"`
	Privileges []string `json:"privileges"`
}
type DatabaseUser struct {
	Name      string          `json:"name"`
	Host      string          `json:"host"`
	Protected bool            `json:"protected"`
	Grants    []DatabaseGrant `json:"grants"`
}
type DatabaseProxyServer struct {
	Hostgroup int    `json:"hostgroup"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	State     string `json:"state"`
}
type DatabaseProxyPool struct {
	Hostgroup int    `json:"hostgroup"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Used      int64  `json:"used"`
	Free      int64  `json:"free"`
	Errors    int64  `json:"errors"`
}
type DatabaseProxyUser struct {
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	Hostgroup int    `json:"hostgroup"`
	State     string `json:"state"`
	Protected bool   `json:"protected"`
}
type DatabaseState struct {
	Read              ReadStatus            `json:"read"`
	State             string                `json:"state"`
	Version           string                `json:"version"`
	ClusterID         string                `json:"cluster_id"`
	ClusterName       string                `json:"cluster_name"`
	ConfiguredMembers string                `json:"configured_members"`
	ActualMembers     string                `json:"actual_members"`
	MemberAddress     string                `json:"member_address"`
	ClusterSize       int                   `json:"cluster_size"`
	Primary           bool                  `json:"primary"`
	Ready             bool                  `json:"ready"`
	Synced            bool                  `json:"synced"`
	ReadOnly          bool                  `json:"read_only"`
	Connections       int64                 `json:"connections"`
	ReceiveQueue      float64               `json:"receive_queue"`
	SendQueue         float64               `json:"send_queue"`
	FlowControl       float64               `json:"flow_control"`
	Databases         []DatabaseSchema      `json:"databases"`
	Users             []DatabaseUser        `json:"users"`
	Servers           []DatabaseProxyServer `json:"servers"`
	Pools             []DatabaseProxyPool   `json:"pools"`
	ProxyUsers        []DatabaseProxyUser   `json:"proxy_users"`
	PendingChanges    bool                  `json:"pending_changes"`
	CheckedAt         time.Time             `json:"checked_at"`
	Error             *Error                `json:"error,omitempty"`
}
type DatabaseBackup struct {
	Name        string     `json:"name"`
	Schedule    string     `json:"schedule"`
	State       string     `json:"state"`
	LastRun     *time.Time `json:"last_run,omitempty"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
	Artifacts   []string   `json:"artifacts"`
	Error       string     `json:"error,omitempty"`
}
type DatabaseDiscovery struct{}

func (*DatabaseDiscovery) Kind() Kind { return "database.discover" }
func (*DatabaseDiscovery) isPayload() {}

type DatabaseDiscovered struct {
	Instances []DatabaseCandidate `json:"instances"`
}

func (*DatabaseDiscovered) Kind() Kind { return "database.discovered" }
func (*DatabaseDiscovered) isPayload() {}

type DatabaseRead struct {
	Scope      string             `json:"scope,omitempty"`
	Connection DatabaseConnection `json:"connection"`
	ClusterID  string             `json:"cluster_id,omitempty"`
	User       string             `json:"user,omitempty"`
	Host       string             `json:"host,omitempty"`
	Backups    bool               `json:"backups"`
}

func (*DatabaseRead) Kind() Kind { return "database.read" }
func (*DatabaseRead) isPayload() {}

type DatabaseReadResult struct {
	State        DatabaseState    `json:"state"`
	Backups      []DatabaseBackup `json:"backups"`
	PasswordHash string           `json:"password_hash,omitempty"`
}

func (*DatabaseReadResult) Kind() Kind { return "database.read.result" }
func (*DatabaseReadResult) isPayload() {}

type DatabaseAction struct {
	Connection   DatabaseConnection `json:"connection"`
	ClusterID    string             `json:"cluster_id"`
	Kind         string             `json:"kind"`
	Database     string             `json:"database,omitempty"`
	Charset      string             `json:"charset,omitempty"`
	User         string             `json:"user,omitempty"`
	Host         string             `json:"host,omitempty"`
	PasswordHash string             `json:"password_hash,omitempty"`
	Privileges   []string           `json:"privileges,omitempty"`
	Hostgroup    int                `json:"hostgroup,omitempty"`
}

var databaseName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_$-]{0,63}$`)
var databaseUser = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_$.-]{0,79}$`)
var databaseHost = regexp.MustCompile(`^[A-Za-z0-9_.:%/-]{1,255}$`)
var databasePasswordHash = regexp.MustCompile(`^\*[0-9A-F]{40}$`)

func SystemDatabase(name string) bool {
	return slices.Contains([]string{"mysql", "information_schema", "performance_schema", "sys"}, strings.ToLower(name))
}
func SystemDatabaseUser(name string, c DatabaseConnection) bool {
	return name == c.Username || slices.Contains(c.ProtectedUsers, name) || slices.Contains([]string{"root", "mysql", "mariadb.sys", "mysql.sys", "sst", "galera_sst", "galera_backup", "proxysql_monitor", "mysqld_exporter", "mysql_exporter"}, name)
}
func (a DatabaseAction) Validate() error {
	if e := a.Connection.Validate(); e != nil {
		return e
	}
	if (a.Connection.Kind == "proxysql") != strings.HasPrefix(a.Kind, "proxy_") {
		return fmt.Errorf("数据库类型与操作不匹配")
	}
	if a.Connection.Kind == "galera" && a.ClusterID == "" {
		return fmt.Errorf("缺少 Galera 集群身份")
	}
	if a.Database != "" && (!databaseName.MatchString(a.Database) || SystemDatabase(a.Database)) {
		return fmt.Errorf("业务库名称无效或属于系统库")
	}
	if a.User != "" && (!databaseUser.MatchString(a.User) || !databaseHost.MatchString(a.Host) || SystemDatabaseUser(a.User, a.Connection)) {
		return fmt.Errorf("业务账号无效或属于受保护账号")
	}
	if a.PasswordHash != "" && !databasePasswordHash.MatchString(a.PasswordHash) {
		return fmt.Errorf("密码验证信息无效")
	}
	for _, p := range a.Privileges {
		if !slices.Contains([]string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "INDEX", "ALTER", "EXECUTE", "CREATE VIEW", "SHOW VIEW", "TRIGGER", "EVENT"}, p) {
			return fmt.Errorf("库级权限无效")
		}
	}
	switch a.Kind {
	case "create_database":
		if a.Database != "" && slices.Contains([]string{"utf8mb4", "utf8mb3", "latin1", "ascii"}, a.Charset) {
			return nil
		}
	case "delete_database":
		if a.Database != "" {
			return nil
		}
	case "create_user", "password":
		if a.User != "" && a.PasswordHash != "" {
			return nil
		}
	case "delete_user":
		if a.User != "" {
			return nil
		}
	case "grant", "revoke":
		if a.User != "" && a.Database != "" && len(a.Privileges) > 0 {
			return nil
		}
	case "proxy_upsert":
		if a.Connection.Kind == "proxysql" && a.User != "" && a.PasswordHash != "" && a.Hostgroup >= 0 {
			return nil
		}
	case "proxy_disable", "proxy_delete":
		if a.Connection.Kind == "proxysql" && a.User != "" {
			return nil
		}
	}
	return fmt.Errorf("数据库操作参数无效")
}
