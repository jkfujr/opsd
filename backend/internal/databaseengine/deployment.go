package databaseengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/deploymentdir"
	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

// 固定模板与现有真实数据库检查共用版本。
const GaleraImage = "mariadb:11.8.9"
const ProxyImage = "proxysql/proxysql:3.0.3"

type deploymentManifest struct{ ID, Fingerprint string }

func CheckDeploymentNetwork(ctx context.Context, request protocol.DatabaseNetworkCheck) error {
	ip, err := netip.ParseAddr(request.LocalAddress)
	if err != nil || ip.IsUnspecified() || ip.IsLoopback() || len(request.Endpoints) > 100 {
		return fmt.Errorf("数据库连接检查参数无效")
	}
	for _, endpoint := range request.Endpoints {
		if err := endpoint.Validate(); err != nil {
			return err
		}
		address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
		dialer := net.Dialer{Timeout: 3 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(request.LocalAddress)}}
		connection, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return fmt.Errorf("成员端口不可达：%s，请检查双方防火墙或云安全组", address)
		}
		connection.Close()
	}
	return nil
}

func deploymentManifestFor(d protocol.DatabaseDeployment) deploymentManifest {
	d.ClusterID = ""
	return deploymentManifest{ID: d.ID, Fingerprint: fingerprint(d)}
}

func VerifyDeploymentRecord(d protocol.DatabaseDeployment) error {
	if err := d.Validate(); err != nil {
		return err
	}
	match, err := deploymentFilesMatch(d)
	if err != nil {
		return err
	}
	if !match {
		return fmt.Errorf("数据库部署记录或配置指纹不同")
	}
	return nil
}

func (m Manager) CheckDeployment(ctx context.Context, d protocol.DatabaseDeployment) (protocol.DatabaseDeploymentEnvironment, error) {
	out := protocol.DatabaseDeploymentEnvironment{Architecture: runtime.GOARCH, Problems: []string{}}
	if err := d.Validate(); err != nil {
		return out, err
	}
	if m.Docker == nil {
		return out, fmt.Errorf("Docker 部署入口不可用")
	}
	state := m.Docker.List(ctx)
	out.DockerState, out.EngineID = state.State, state.EngineID
	id, installErr := dockerengine.DockerInstallationOS()
	out.OS = id
	if runtime.GOOS != "linux" {
		out.Problems = append(out.Problems, "数据库部署需要 Linux 节点")
	}
	if state.State == "not_installed" {
		if !d.InstallDocker {
			out.Problems = append(out.Problems, "请允许安装 Docker")
		}
		if installErr != nil {
			out.Problems = append(out.Problems, installErr.Error())
		}
	} else if state.State != "ready" {
		out.Problems = append(out.Problems, "Docker 服务不可用，请先恢复原服务")
	}
	parent, err := deploymentdir.Open(d.Directory, deploymentdir.ExistingParent)
	if err != nil {
		out.Problems = append(out.Problems, err.Error())
	} else {
		defer parent.Close()
		out.FreeBytes, err = deploymentdir.FreeBytes(parent)
		if err != nil {
			out.Problems = append(out.Problems, "磁盘可用空间读取失败")
		}
		if out.FreeBytes < 512*1024*1024 {
			out.Problems = append(out.Problems, "数据库目录可用空间不足 512 MiB")
		}
		if parent.Name() == d.Directory {
			if entries, readErr := parent.ReadDir(1); len(entries) > 0 {
				out.Problems = append(out.Problems, "数据库目录非空，请选择新的空目录")
			} else if readErr != nil && readErr != io.EOF {
				out.Problems = append(out.Problems, "数据库目录不可读")
			}
		}
	}
	for _, c := range state.Containers {
		if c.Name == d.ContainerName() || c.Name == d.LegacyContainerName() {
			out.Problems = append(out.Problems, "目标容器已经存在")
		}
	}
	ports := []int{d.SQLPort}
	if d.Kind == "galera" {
		ports = append(ports, d.ReplicationPort, d.ISTPort, d.SSTPort)
	} else {
		ports = append(ports, d.AdminPort)
	}
	for _, port := range ports {
		address := net.JoinHostPort(d.Address, strconv.Itoa(port))
		if d.Kind == "proxysql" {
			address = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			out.Problems = append(out.Problems, fmt.Sprintf("地址或 TCP 端口不可用：%s", address))
		} else {
			listener.Close()
		}
	}
	if d.Kind == "galera" {
		listener, err := net.ListenPacket("udp", net.JoinHostPort(d.Address, strconv.Itoa(d.ReplicationPort)))
		if err != nil {
			out.Problems = append(out.Problems, "Galera UDP 复制端口不可用")
		} else {
			listener.Close()
		}
	}
	out.Fingerprint = fingerprint(struct {
		Engine, OS, Architecture string
		Manifest                 deploymentManifest
	}{out.EngineID, out.OS, out.Architecture, deploymentManifestFor(d)})
	return out, nil
}

func (m Manager) prepareDeployment(ctx context.Context, a protocol.DatabaseAction) ([]string, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	out, err := m.CheckDeployment(ctx, *a.Deployment)
	if err != nil {
		return nil, err
	}
	if len(out.Problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(out.Problems, "；"))
	}
	return []string{"deploy_network", "deploy_docker", "deploy_pull", "deploy_files", "deploy_create", "deploy_start", "deploy_ready"}, nil
}

func (m Manager) observeDeployment(ctx context.Context, d protocol.DatabaseDeployment, stage string) (Observation, error) {
	out := Observation{CheckedAt: time.Now().UTC(), Fingerprint: "pending"}
	if m.Docker == nil {
		return out, fmt.Errorf("Docker 部署入口不可用")
	}
	switch stage {
	case "deploy_network":
		for _, address := range d.ReachableSeeds {
			dialer := net.Dialer{Timeout: 3 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(d.Address)}}
			connection, err := dialer.DialContext(ctx, "tcp", address)
			if err != nil {
				return out, fmt.Errorf("成员端口不可达：%s，请检查双方防火墙或云安全组", address)
			}
			connection.Close()
		}
		out.Satisfied = true
	case "deploy_docker":
		state := m.Docker.List(ctx)
		out.Satisfied = state.State == "ready"
		out.Fingerprint = state.State
		if state.State != "ready" && state.State != "not_installed" {
			return out, fmt.Errorf("Docker 安装或启动结果需要核对")
		}
	case "deploy_pull":
		var err error
		out.Satisfied, err = m.Docker.DeploymentImageReady(ctx, d.Image)
		return out, err
	case "deploy_files":
		var err error
		out.Satisfied, err = deploymentFilesMatch(d)
		return out, err
	case "deploy_create", "deploy_start", "deploy_ready":
		_, state, err := m.Docker.DeploymentState(ctx, d)
		if err != nil {
			return out, err
		}
		out.Fingerprint = state
		if stage == "deploy_create" {
			out.Satisfied = state != "absent"
			return out, nil
		}
		if stage == "deploy_start" {
			out.Satisfied = state == "running" || state == "restarting"
			return out, nil
		}
		if state != "running" {
			return out, fmt.Errorf("数据库容器已停止或正在异常重启，请核对原容器日志后重新处理")
		}
		result, err := m.Read(ctx, d.Connection, "health")
		if err != nil {
			return out, nil
		}
		if d.Kind == "galera" {
			if d.ClusterID != "" && result.ClusterID != "" && result.ClusterID != d.ClusterID {
				return out, fmt.Errorf("实际 Galera 集群身份与目标不同")
			}
			out.Satisfied = result.ClusterID != "" && result.Primary && result.Ready && result.Synced
		} else {
			out.Satisfied = result.State == "ready"
		}
	default:
		return out, fmt.Errorf("未知数据库部署阶段")
	}
	return out, nil
}

func (m Manager) applyDeployment(ctx context.Context, d protocol.DatabaseDeployment, stage string) error {
	switch stage {
	case "deploy_docker":
		return dockerengine.InstallDocker(ctx)
	case "deploy_pull":
		return m.Docker.PullDeploymentImage(ctx, d.Image)
	case "deploy_files":
		return writeDeploymentFiles(d)
	case "deploy_create":
		return m.Docker.CreateDeployment(ctx, d)
	case "deploy_start":
		return m.Docker.StartDeployment(ctx, d)
	case "deploy_ready":
		return fmt.Errorf("同步状态由持久操作持续读取")
	}
	return fmt.Errorf("未知数据库部署阶段")
}

func deploymentFiles(d protocol.DatabaseDeployment) (map[string]string, error) {
	files := map[string]string{}
	manifest, _ := json.Marshal(deploymentManifestFor(d))
	files["deployment.json"] = string(manifest)
	profile, _ := json.Marshal(d)
	files["profile.json"] = string(profile)
	if d.Kind == "proxysql" {
		quote := strconv.Quote
		var servers strings.Builder
		connections := 50
		if d.BackendMaxConnections > 0 {
			connections = d.BackendMaxConnections
		}
		for _, backend := range d.Backends {
			fmt.Fprintf(&servers, "{address=%s;port=%d;hostgroup=10;max_connections=%d;},", quote(backend.Host), backend.Port, connections)
		}
		files["database.cnf"] = fmt.Sprintf("datadir=\"/var/lib/proxysql\"\nadmin_variables={admin_credentials=%s;mysql_ifaces=%s;}\nmysql_variables={threads=2;max_connections=128;interfaces=%s;monitor_enabled=true;monitor_username=%s;monitor_password=%s;}\nmysql_servers=(%s)\nmysql_galera_hostgroups=({writer_hostgroup=10;backup_writer_hostgroup=20;reader_hostgroup=30;offline_hostgroup=40;active=1;max_writers=1;writer_is_also_reader=0;max_transactions_behind=100;})\n", quote(d.Connection.Username+":"+d.Connection.Password), quote(net.JoinHostPort("127.0.0.1", strconv.Itoa(d.AdminPort))), quote(net.JoinHostPort("127.0.0.1", strconv.Itoa(d.SQLPort))), quote(d.MonitorUser), quote(d.MonitorPassword), servers.String())
		return files, nil
	}
	if strings.ContainsAny(d.Connection.Password, "\x00\r\n") {
		return nil, fmt.Errorf("数据库管理密码包含无效字符")
	}
	files["password"] = d.Connection.Password
	files["init.sql"] = fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'localhost' IDENTIFIED BY %s;\nGRANT RELOAD, PROCESS, LOCK TABLES, BINLOG MONITOR ON *.* TO %s@'localhost';\n", literal(d.SSTUser), literal(d.SSTPassword), literal(d.SSTUser))
	if d.MonitorUser != "" {
		files["init.sql"] += fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'%%' IDENTIFIED BY %s;\nGRANT USAGE ON *.* TO %s@'%%';\n", literal(d.MonitorUser), literal(d.MonitorPassword), literal(d.MonitorUser))
	}
	options := []string{fmt.Sprintf("gmcast.listen_addr=tcp://%s", net.JoinHostPort(d.Address, strconv.Itoa(d.ReplicationPort))), fmt.Sprintf("ist.recv_addr=%s", net.JoinHostPort(d.Address, strconv.Itoa(d.ISTPort)))}
	if d.ProviderOptions != "" {
		options = append(options, d.ProviderOptions)
	}
	if len(d.TLS) > 0 {
		options = append(options, "socket.ssl=yes", "socket.ssl_ca=/etc/opsd-database-tls/ca.pem", "socket.ssl_cert=/etc/opsd-database-tls/cert.pem", "socket.ssl_key=/etc/opsd-database-tls/key.pem")
	}
	files["database.cnf"] = fmt.Sprintf("[mariadb]\nport=%d\nbind-address=127.0.0.1,%s\nwsrep_on=ON\nwsrep_provider=/usr/lib/galera/libgalera_smm.so\nwsrep_cluster_name=%s\nwsrep_cluster_address=gcomm://%s\nwsrep_node_name=%s\nwsrep_node_address=%s\nwsrep_node_incoming_address=%s\nwsrep_sst_receive_address=%s\nwsrep_provider_options=\"%s\"\nwsrep_sst_method=%s\nwsrep_sst_auth=\"%s:%s\"\nbinlog_format=ROW\ninnodb_autoinc_lock_mode=2\ninnodb_buffer_pool_size=128M\nmax_connections=128\n", d.SQLPort, d.Address, d.ClusterName, strings.Join(d.Seeds, ","), d.ID, d.Address, net.JoinHostPort(d.Address, strconv.Itoa(d.SQLPort)), net.JoinHostPort(d.Address, strconv.Itoa(d.SSTPort)), strings.Join(options, ";"), d.SSTMethod, d.SSTUser, d.SSTPassword)
	if len(d.TLS) > 0 {
		files["database.cnf"] += "[sst]\nencrypt=4\ntca=/etc/opsd-database-tls/ca.pem\ntcert=/etc/opsd-database-tls/cert.pem\ntkey=/etc/opsd-database-tls/key.pem\n"
		for name, value := range d.TLS {
			files["tls/"+name] = value
		}
	}
	files["start.sh"] = "#!/bin/sh\nset -eu\n"
	if d.Bootstrap {
		files["start.sh"] += "if [ ! -e /opsd-state/bootstrap.started ]; then\n  (set -C; printf '%s\\n' started > /opsd-state/bootstrap.started)\n  sync -f /opsd-state/bootstrap.started\n  exec docker-entrypoint.sh mariadbd --wsrep-new-cluster\nfi\n"
	}
	files["start.sh"] += "if [ -s /opsd-state/seeds ]; then\n  IFS= read -r seeds < /opsd-state/seeds\n  exec docker-entrypoint.sh mariadbd --wsrep-cluster-address=\"gcomm://$seeds\"\nfi\nexec docker-entrypoint.sh mariadbd\n"
	return files, nil
}

func deploymentFilesMatch(d protocol.DatabaseDeployment) (bool, error) {
	files, err := deploymentFiles(d)
	if err != nil {
		return false, err
	}
	dir, err := deploymentdir.Open(d.Directory, deploymentdir.Existing)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer dir.Close()
	return checkDeploymentFiles(dir, files)
}

func checkDeploymentFiles(dir *os.File, files map[string]string) (bool, error) {
	all := true
	for name, expected := range files {
		file, err := openDeploymentFile(dir, name, os.O_RDONLY, 0)
		if errors.Is(err, os.ErrNotExist) {
			all = false
			continue
		}
		if err != nil {
			return false, err
		}
		actual, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
		file.Close()
		if err != nil || string(actual) != expected {
			return false, fmt.Errorf("部署文件已经变化：%s", name)
		}
	}
	for _, name := range []string{"data", "state"} {
		var owners []uint32
		if name == "data" {
			owners = []uint32{deploymentdir.GaleraUID}
		}
		child, err := deploymentdir.OpenDirectory(dir, name, 0, owners...)
		if errors.Is(err, os.ErrNotExist) {
			all = false
			continue
		}
		if err != nil {
			return false, err
		}
		child.Close()
	}
	return all, nil
}

func openDeploymentFile(dir *os.File, name string, flags int, perm os.FileMode) (*os.File, error) {
	if base, ok := strings.CutPrefix(name, "tls/"); ok {
		tls, err := deploymentdir.OpenDirectory(dir, "tls", perm, deploymentdir.GaleraUID)
		if err != nil {
			return nil, err
		}
		defer tls.Close()
		return deploymentdir.OpenFile(tls, base, flags, deploymentdir.GaleraUID)
	}
	if name == "deployment.json" {
		return deploymentdir.OpenFile(dir, name, flags)
	}
	return deploymentdir.OpenFile(dir, name, flags, deploymentdir.GaleraUID)
}

func writeDeploymentFiles(d protocol.DatabaseDeployment) error {
	files, err := deploymentFiles(d)
	if err != nil {
		return err
	}
	dir, err := deploymentdir.Open(d.Directory, deploymentdir.Create)
	if err != nil {
		return err
	}
	defer dir.Close()
	if _, err = checkDeploymentFiles(dir, files); err != nil {
		return err
	}
	manifest, err := deploymentdir.OpenFile(dir, "deployment.json", os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		entries, readErr := dir.ReadDir(1)
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		if len(entries) > 0 {
			return fmt.Errorf("数据库目录已被其他内容占用")
		}
	} else if err != nil {
		return err
	} else {
		manifest.Close()
	}
	// 先登记所属操作；部分文件写入后，恢复只补充仍缺失且内容一致的文件。
	names := []string{"deployment.json"}
	for name := range files {
		if name != "deployment.json" {
			names = append(names, name)
		}
	}
	slices.Sort(names[1:])
	for _, name := range names {
		file, err := openDeploymentFile(dir, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0750)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, err = file.WriteString(files[name])
		if err == nil && d.Kind == "galera" && name != "deployment.json" {
			err = file.Chown(deploymentdir.GaleraUID, deploymentdir.GaleraUID)
		}
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	for _, name := range []string{"data", "state"} {
		var owners []uint32
		if name == "data" {
			owners = []uint32{deploymentdir.GaleraUID}
		}
		child, err := deploymentdir.OpenDirectory(dir, name, 0750, owners...)
		if err != nil {
			return err
		}
		child.Close()
	}
	if len(d.TLS) > 0 {
		tls, err := deploymentdir.OpenDirectory(dir, "tls", 0, deploymentdir.GaleraUID)
		if err != nil {
			return err
		}
		defer tls.Close()
		if err = tls.Chown(deploymentdir.GaleraUID, deploymentdir.GaleraUID); err != nil {
			return err
		}
		if err = tls.Sync(); err != nil {
			return err
		}
	}
	match, err := checkDeploymentFiles(dir, files)
	if err != nil {
		return err
	}
	if !match {
		return fmt.Errorf("数据库部署文件写入后与预期不符")
	}
	return dir.Sync()
}
