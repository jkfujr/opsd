//go:build linux

package cli

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
)

// galeraFixture 起一个**单成员** Galera，供跨节点建库验收使用。
//
// 关键点是 --wsrep-new-cluster：没有它，引导节点形成空集群后即退出
// （我先前用 docker run 直接起，容器起完就干净关闭，就是这个原因）。
type galeraFixture struct {
	name     string
	network  string
	address  string
	port     int
	password string
}

func startGaleraFixture(t *testing.T) galeraFixture {
	t.Helper()
	if _, e := exec.LookPath("docker"); e != nil {
		t.Skip("需要 docker")
	}
	f := galeraFixture{
		name:     "opsd-galera-" + identity.Random()[:8],
		network:  "opsd-galera-net-" + identity.Random()[:6],
		address:  "172.31.20.11",
		port:     freeDatabasePort(t),
		password: identity.Random(),
	}
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := exec.Command("docker", "logs", "--tail", "30", f.name).CombinedOutput()
			t.Logf("%s 日志：%s", f.name, logs)
		}
		_ = exec.Command("docker", "rm", "-f", f.name).Run()
		_ = exec.Command("docker", "network", "rm", f.network).Run()
	})
	run := func(args ...string) {
		if out, e := exec.Command("docker", args...).CombinedOutput(); e != nil {
			t.Fatalf("docker %v 失败：%v %s", args, e, out)
		}
	}
	run("network", "create", "--subnet", "172.31.20.0/24", f.network)
	run("run", "-d", "--name", f.name, "--network", f.network, "--ip", f.address,
		"-p", fmt.Sprintf("127.0.0.1:%d:3306", f.port),
		"-e", "MARIADB_ROOT_PASSWORD="+f.password, "-e", "MARIADB_ROOT_HOST=%",
		"mariadb:11.8.9",
		"--wsrep-on=ON", "--wsrep-provider=/usr/lib/galera/libgalera_smm.so",
		"--wsrep-cluster-name=opsd-acceptance", "--wsrep-cluster-address=gcomm://",
		"--wsrep-node-address="+f.address, "--wsrep-node-incoming-address="+f.address+":3306",
		"--wsrep-sst-receive-address="+f.address, "--wsrep-sst-method=rsync",
		"--binlog-format=ROW", "--innodb-autoinc-lock-mode=2",
		"--innodb-buffer-pool-size=64M", "--server-id=1", "--wsrep-new-cluster")
	connection := f.connection()
	waitFor(t, "Galera 就绪", 2*time.Minute, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		state, e := (databaseengine.Manager{}).Read(ctx, connection)
		return e == nil && state.Ready && state.Synced && state.ClusterSize == 1
	})
	return f
}

// connection 返回指向该成员的连接；凭据只留在使用它的节点上。
func (f galeraFixture) connection() protocol.DatabaseConnection {
	return protocol.DatabaseConnection{
		Kind: "galera", Address: fmt.Sprintf("127.0.0.1:%d", f.port),
		Username: "root", Password: f.password,
	}
}

// waitFor 轮询直到条件成立；失败时把最后的错误一并带出来。
func waitFor(t *testing.T, what string, limit time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s 超时（%s）", what, limit)
}

func TestSingleMemberGaleraFixture(t *testing.T) {
	f := startGaleraFixture(t)
	state, e := (databaseengine.Manager{}).Read(t.Context(), f.connection())
	if e != nil {
		t.Fatal(e)
	}
	if !state.Ready || !state.Synced || state.ClusterSize != 1 || state.ClusterID == "" {
		t.Fatalf("Galera 状态不符：%+v", state)
	}
	t.Logf("单成员 Galera 就绪：集群身份 %s，成员 %s", state.ClusterID, f.address)
}

// freeDatabasePort 取一个当前空闲的本地端口给容器映射。
func freeDatabasePort(t *testing.T) int {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}
