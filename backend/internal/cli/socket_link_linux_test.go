//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/installation"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/storage"
)

// 第一步：B 的 node 与 executor 都由安装的二进制启动，
// A 是测试进程内的真实节点，经真实 HTTP 加入 B 并能跨节点调用。
//
// 这一步只证明"进程 + Unix Socket + 节点间认证"这条链路通，不涉及数据库。
func TestInstalledNodeAcceptsJoinedPeer(t *testing.T) {
	binary := os.Getenv("OPSD_TEST_BINARY")
	if binary == "" || os.Geteuid() != 0 {
		t.Skip("需要 OPSD_TEST_BINARY 与 root")
	}
	dir := t.TempDir()
	// t.TempDir 建的是 0700：父目录与本身都要可进，nobody 才能读配置。
	for _, path := range []string{filepath.Dir(dir), dir} {
		if e := os.Chmod(path, 0755); e != nil {
			t.Fatal(e)
		}
	}
	data := filepath.Join(dir, "node")
	execDir := filepath.Join(dir, "executor")
	if e := os.MkdirAll(execDir, 0700); e != nil {
		t.Fatal(e)
	}
	socket := filepath.Join(dir, "executor.sock")
	peer := freeAddress(t)

	// A：测试进程内的真实节点，稍后加入 B。
	// 监听与公告必须是同一个地址：分两次 freeAddress 会选到不同端口，
	// B 主动回连时就拨不到 A。
	aListen := freeAddress(t)
	a, e := cluster.Open(t.Context(), filepath.Join(dir, "a", "node.db"), cluster.Options{
		Config:   cluster.Config{Listen: aListen, Address: "http://" + aListen},
		Username: "admin", Password: "test-password", Entrance: "/signin",
	})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	aPeer := httptest.NewServer(a.PeerHandler())
	defer aPeer.Close()

	// B：先在本进程建立身份（拿公钥与接入码），再交给二进制启动。
	bootstrap, e := cluster.Open(t.Context(), filepath.Join(data, "node.db"), cluster.Options{
		Config:   cluster.Config{Listen: peer, Address: "http://" + peer},
		Username: "admin", Password: "test-password", Entrance: "/signin",
	})
	if e != nil {
		t.Fatal(e)
	}
	self, e := bootstrap.Self(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	token, _, _, e := bootstrap.Token(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if e = bootstrap.Close(); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(data)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if e = os.Chown(filepath.Join(data, entry.Name()), 65534, 65534); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.Chown(data, 65534, 65534); e != nil {
		t.Fatal(e)
	}

	uid := 65534
	files := installation.NodeFile{
		DataDir: data, ExecutorDir: execDir, Socket: socket,
		ControlSocket: filepath.Join(data, "control.sock"),
		Binary:        binary, GroupID: 65534, UserID: &uid,
		// 授权表先给最保守的一条：只允许读操作，够 ping 用。
		SourceCapabilities: []byte(`{"` + a.ID + `":["read_operations"]}`),
	}
	config := filepath.Join(dir, "node.json")
	if e = localstate.Write(config, files, 0644); e != nil {
		t.Fatal(e)
	}
	// 节点以 nobody 运行，配置与目录都要可读/可进。
	if e = os.Chown(config, 65534, 65534); e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(dir, 0755); e != nil {
		t.Fatal(e)
	}

	root := launch(t, binary, "executor", "--config", config)
	waitLocal(t, root, func() bool {
		c, e := net.Dial("unix", socket)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	node := launch(t, "runuser", "-u", "nobody", "--", binary, "node", "--config", config)
	waitLocal(t, node, func() bool {
		c, e := net.DialTimeout("tcp", peer, 200*time.Millisecond)
		if e == nil {
			c.Close()
		}
		return e == nil
	})

	// A 加入 B，然后跨节点调用一个只读方法：证明链路与授权都成立。
	if e = a.Join(t.Context(), "http://"+peer, self.Public, token); e != nil {
		t.Fatalf("加入失败：%v", e)
	}
	directory, e := a.Directory(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, member := range directory.Members {
		if member.Address == "http://"+peer {
			found = true
		}
	}
	if !found {
		t.Fatal("目录里看不到 B")
	}
	var answered cluster.Member
	if e = a.Call(t.Context(), self.ID, "ping", nil, &answered); e != nil {
		t.Fatalf("跨节点 ping 失败：%v", e)
	}
	// ping 只回 n.Self：拿到的必须就是引导阶段记录的那个 B。
	if answered.ID != self.ID {
		t.Fatalf("ping 返回的节点不是 B：%s != %s", answered.ID, self.ID)
	}
	// 执行器的 Unix Socket 也要能应答，而且必须是**允许的 UID** 发起的：
	// 测试进程是 root，会被按 UID 拒绝（这正是 A1 的拒绝方式），
	// 所以让测试二进制自己以 nobody 跑一次只读探测。
	probeCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	selfPath, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	// go 测试二进制在 root 的临时目录里，nobody 无法执行：复制到本例可进的目录。
	probeBinary := filepath.Join(dir, "probe.test")
	if e = copyExecutable(selfPath, probeBinary); e != nil {
		t.Fatal(e)
	}
	probe := exec.CommandContext(probeCtx, "runuser", "-u", "nobody", "--", probeBinary,
		"-test.run", "^TestHelperExecutorReadRPC$", "-test.v")
	probe.Env = append(os.Environ(), "OPSD_EXECUTOR_PROBE_SOCKET="+socket)
	output, e := probe.CombinedOutput()
	if e != nil {
		t.Fatalf("执行器 Socket 只读调用失败：%v %s", e, output)
	}
	// 必须看到这条用例自己通过：只有总 "PASS" 时，跳过或未匹配也会出现 PASS。
	if !strings.Contains(string(output), "--- PASS: TestHelperExecutorReadRPC") {
		t.Fatalf("执行器只读探测没有真正执行：%s", output)
	}
	t.Logf("链路已通：A(%s) → B(%s, %s)；执行器 Socket 只读调用成功", a.ID, self.ID, "http://"+peer)

	// 第二步：B 未授予 manage_database 时，A 的数据库提交必须被拒，且 B 零入队。
	payload, e := json.Marshal(cluster.DatabaseSubmission{
		Change: cluster.DatabaseChange{Cluster: "missing", ExpectedRevision: 1, Kind: "create_database", Database: "opsd_link_refused", Charset: "utf8mb4"},
		Cluster: cluster.ManagedDatabaseCluster{
			ID: "missing", Coordinator: self.ID, ClusterID: "galera", Revision: 1, Name: "占位",
			Members: []cluster.DatabaseRef{}, Proxies: []cluster.DatabaseRef{},
		},
	})
	if e != nil {
		t.Fatal(e)
	}
	key := "socket-refused-" + identity.Random()[:8]
	if _, e = a.Submit(t.Context(), self.ID, cluster.Request{
		ID: cluster.NewOperationID(a.ID), Kind: "database", Key: key,
		Actor: "account:" + a.ID + ":1", Payload: payload,
	}); e == nil {
		t.Fatal("未授予 manage_database 的提交不应被受理")
	} else if code := clusterFaultCode(e); code != "forbidden" {
		t.Fatalf("应以 forbidden 拒绝，实际 %s（%v）", code, e)
	}
	// 被拒的请求不能在 B 的队列里留下任何行——在线只读读取 B 的库。
	queued, e := queueRows(t.Context(), filepath.Join(data, "node.db"), key)
	if e != nil {
		t.Fatal(e)
	}
	if queued != 0 {
		t.Fatalf("被拒后 B 仍有入队：%d", queued)
	}
	t.Logf("未授权提交被拒（forbidden），B 的 queue 中该请求键为 0 行")
}

// queueRows 在线只读地数 B 队列里某个请求键的行数。
func queueRows(ctx context.Context, path, key string) (int, error) {
	db, e := storage.ReadOnly(ctx, path)
	if e != nil {
		return 0, e
	}
	defer db.Close()
	var count int
	if e = db.QueryRow("SELECT count(*) FROM queue WHERE request_key=?", key).Scan(&count); e != nil {
		return 0, e
	}
	return count, nil
}

// clusterFaultCode 取出集群故障码，用来断言拒绝的性质而不是只断言"报错了"。
func clusterFaultCode(err error) string {
	var f *cluster.Fault
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}
