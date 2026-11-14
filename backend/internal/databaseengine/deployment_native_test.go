//go:build linux

package databaseengine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestDatabaseDeploymentNative(t *testing.T) {
	if os.Getenv("OPSD_DATABASE_TEST") != "1" {
		t.Skip("设置 OPSD_DATABASE_TEST=1 运行实际部署检查")
	}
	if os.Geteuid() != 0 {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "sudo", "-n", "env", "OPSD_DATABASE_TEST=1", binary, "-test.run=^TestDatabaseDeploymentNative$", "-test.v", "-test.timeout=8m")
		output, err := cmd.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Run("已有文件与首次启动", TestDeploymentKeepsExistingFilesAndBootstrapIntent)
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	address := ""
	for _, value := range addresses {
		if ip, ok := value.(*net.IPNet); ok && ip.IP.To4() != nil && !ip.IP.IsLoopback() {
			address = ip.IP.String()
			break
		}
	}
	if address == "" {
		t.Fatal("未找到实际 IPv4 地址")
	}
	port := func() int {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		return listener.Addr().(*net.TCPAddr).Port
	}
	m := Manager{Docker: dockerengine.New()}
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Minute)
	defer cancel()
	wait := func(d protocol.DatabaseDeployment, count int) {
		t.Helper()
		deadline := time.Now().Add(100 * time.Second)
		for time.Now().Before(deadline) {
			state, err := m.Read(ctx, d.Connection, "health")
			if err == nil && state.Ready && state.Synced && state.ClusterSize == count {
				return
			}
			time.Sleep(time.Second)
		}
		t.Fatal("等待实际成员同步超时", count)
	}
	deploy := func(d protocol.DatabaseDeployment) {
		t.Helper()
		action := protocol.DatabaseAction{Kind: "deploy_database", Connection: d.Connection, Deployment: &d}
		stages, err := m.Prepare(ctx, action)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			name := "opsd-" + d.Kind + "-" + d.ID
			if t.Failed() {
				data, _ := exec.Command("docker", "logs", "--tail", "70", name).CombinedOutput()
				message := strings.ReplaceAll(string(data), d.Connection.Password, "[已隐藏]")
				message = strings.ReplaceAll(message, d.SSTPassword, "[已隐藏]")
				t.Log(message)
			}
			_ = exec.Command("docker", "rm", "-f", name).Run()
		})
		for _, stage := range stages {
			if stage == "deploy_ready" {
				break
			}
			observed, err := m.Observe(ctx, action, stage)
			if err != nil {
				t.Fatal(stage, err)
			}
			if !observed.Satisfied {
				if err = m.Apply(ctx, action, stage); err != nil {
					t.Fatal(stage, err)
				}
			}
			observed, err = m.Observe(ctx, action, stage)
			if err != nil || !observed.Satisfied {
				t.Fatal(stage, err)
			}
		}
	}
	base := deploymentExample()
	base.ID = identity.Random()
	base.Address = address
	base.Connection.Password = identity.Random()
	base.SSTPassword = identity.Random()
	base.MonitorUser, base.MonitorPassword = "opsd_monitor", identity.Random()
	base.TLS, err = NewDeploymentTLS()
	if err != nil {
		t.Fatal(err)
	}
	members := []protocol.DatabaseDeployment{}
	for index := range 3 {
		d := base
		d.ID = fmt.Sprintf("%s_%d", base.ID, index)
		d.Directory = filepath.Join(t.TempDir(), "database")
		d.SQLPort, d.ReplicationPort, d.ISTPort, d.SSTPort = port(), port(), port(), port()
		d.Connection.Address = fmt.Sprintf("127.0.0.1:%d", d.SQLPort)
		d.Bootstrap = index == 0
		d.Seeds = nil
		for _, previous := range members {
			d.Seeds = append(d.Seeds, net.JoinHostPort(address, fmt.Sprint(previous.ReplicationPort)))
		}
		d.ReachableSeeds = append([]string{}, d.Seeds...)
		d.Seeds = append(d.Seeds, net.JoinHostPort(address, fmt.Sprint(d.ReplicationPort)))
		if index > 0 {
			state, err := m.Read(ctx, members[0].Connection, "health")
			if err != nil {
				t.Fatal(err)
			}
			d.ClusterID = state.ClusterID
		}
		deploy(d)
		wait(d, index+1)
		members = append(members, d)
		if index == 0 {
			s, err := connect(ctx, d.Connection)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecContext(ctx, "CREATE DATABASE opsd_deployment_keep")
			s.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, d := range members {
		wait(d, 3)
		s, err := connect(ctx, d.Connection)
		if err != nil {
			t.Fatal(err)
		}
		var value string
		err = s.QueryRowContext(ctx, "SHOW DATABASES LIKE 'opsd_deployment_keep'").Scan(&value)
		s.Close()
		if err != nil || value != "opsd_deployment_keep" {
			t.Fatal("扩容未保留业务库", err)
		}
		if err = m.Docker.CreateDeployment(ctx, d); err == nil {
			t.Fatal("重复创建了原容器")
		}
	}
	first := members[0]
	if output, err := exec.CommandContext(ctx, "docker", "restart", "opsd-galera-"+first.ID).CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	wait(first, 3)
	id, _, err := m.Docker.DeploymentState(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := m.DeploymentProfile(ctx, protocol.DatabaseDeploymentProfileRead{Connection: first.Connection, ContainerID: id})
	if err != nil || len(profile.Problems) > 0 || profile.Deployment.Image != GaleraImage || profile.Deployment.SSTPassword != base.SSTPassword || profile.Deployment.ReplicationPort != first.ReplicationPort {
		t.Fatal("扩容资料不完整", profile.Problems, err)
	}
	proxy := protocol.DatabaseDeployment{ID: base.ID + "_proxy", Kind: "proxysql", Image: ProxyImage, Directory: filepath.Join(t.TempDir(), "proxy"), Address: address, SQLPort: port(), AdminPort: port(), InstallDocker: true, MonitorUser: base.MonitorUser, MonitorPassword: base.MonitorPassword}
	proxy.Connection = protocol.DatabaseConnection{Kind: "proxysql", Address: fmt.Sprintf("127.0.0.1:%d", proxy.AdminPort), Username: "opsdadmin", Password: identity.Random()}
	for _, d := range members {
		proxy.Backends = append(proxy.Backends, protocol.ProxyEndpoint{Host: address, Port: d.SQLPort})
	}
	deploy(proxy)
	deadline := time.Now().Add(40 * time.Second)
	for {
		state, err := m.Read(ctx, proxy.Connection, "proxies")
		if err == nil && state.State == "ready" && len(state.Servers) >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("代理部署未完成", err)
		}
		time.Sleep(time.Second)
	}
	root := filepath.Join("..", "..", "..", "artifacts")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(map[string]any{"single_node": true, "joined_members": 3, "tls_sst": true, "data_preserved": true, "restart_without_bootstrap": true, "proxy": true})
	if err = os.WriteFile(filepath.Join(root, "database-deployment-result.json"), result, 0644); err != nil {
		t.Fatal(err)
	}
}
