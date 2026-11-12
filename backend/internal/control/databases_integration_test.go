//go:build linux

package control

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/executor"
	"github.com/jkfujr/opsd/internal/identity"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

type nativeDatabases struct {
	network    string
	names      []string
	secrets    []string
	galera     []protocol.DatabaseConnection
	proxies    []protocol.DatabaseConnection
	proxyPorts []int
}

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
func (f *nativeDatabases) docker(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	result, e := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if e != nil {
		message := string(result)
		for _, secret := range f.secrets {
			message = strings.ReplaceAll(message, secret, "[已隐藏]")
		}
		t.Fatalf("Docker %s 失败：%v %s", args[0], e, message)
	}
	return strings.TrimSpace(string(result))
}
func waitDatabase(t *testing.T, label string, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatal(label + "超时")
}
func nativeSQL(t *testing.T, c protocol.DatabaseConnection) *sql.DB {
	t.Helper()
	config := mysql.NewConfig()
	config.User = c.Username
	config.Passwd = c.Password
	config.Net = "tcp"
	config.Addr = c.Address
	config.Timeout = 2 * time.Second
	config.ReadTimeout = 3 * time.Second
	config.WriteTimeout = 3 * time.Second
	config.InterpolateParams = true
	connector, e := mysql.NewConnector(config)
	if e != nil {
		t.Fatal(e)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	return db
}
func startNativeDatabases(t *testing.T) *nativeDatabases {
	t.Helper()
	if os.Getenv("OPSD_DATABASE_TEST") != "1" {
		t.Skip("设置 OPSD_DATABASE_TEST=1 运行实际数据库检查")
	}
	f := &nativeDatabases{network: "opsd-db-" + identity.Random()[:10]}
	password := identity.Random()
	f.secrets = []string{password}
	t.Cleanup(func() {
		for i := len(f.names) - 1; i >= 0; i-- {
			if t.Failed() {
				logs, _ := exec.Command("docker", "logs", "--tail", "50", f.names[i]).CombinedOutput()
				message := string(logs)
				for _, secret := range f.secrets {
					message = strings.ReplaceAll(message, secret, "[已隐藏]")
				}
				t.Logf("%s：%s", f.names[i], message)
			}
			_ = exec.Command("docker", "rm", "-f", f.names[i]).Run()
		}
		_ = exec.Command("docker", "network", "rm", f.network).Run()
	})
	f.docker(t, "network", "create", "--subnet", "172.30.91.0/24", f.network)
	f.docker(t, "pull", "mariadb:11.8.9")
	f.docker(t, "pull", "proxysql/proxysql:3.0.3")
	manager := databaseengine.Manager{}
	for i := range 3 {
		name := fmt.Sprintf("%s-g%d", f.network, i+1)
		f.names = append(f.names, name)
		port := freeDatabasePort(t)
		address := fmt.Sprintf("172.30.91.%d", 11+i)
		clusterAddress := "gcomm://172.30.91.11"
		if i == 0 {
			clusterAddress = "gcomm://"
		}
		args := []string{"run", "-d", "--name", name, "--network", f.network, "--ip", address, "-p", fmt.Sprintf("127.0.0.1:%d:3306", port), "-e", "MARIADB_ROOT_PASSWORD=" + password, "-e", "MARIADB_ROOT_HOST=%", "mariadb:11.8.9", "--wsrep-on=ON", "--wsrep-provider=/usr/lib/galera/libgalera_smm.so", "--wsrep-cluster-name=opsd-ci", "--wsrep-cluster-address=" + clusterAddress, "--wsrep-node-address=" + address, "--wsrep-node-incoming-address=" + address + ":3306", "--wsrep-sst-receive-address=" + address, "--wsrep-sst-method=rsync", "--binlog-format=ROW", "--innodb-autoinc-lock-mode=2", "--innodb-buffer-pool-size=64M", "--server-id=" + strconv.Itoa(i+1)}
		if i == 0 {
			args = append(args, "--wsrep-new-cluster")
		}
		f.docker(t, args...)
		c := protocol.DatabaseConnection{Kind: "galera", Address: fmt.Sprintf("127.0.0.1:%d", port), Username: "root", Password: password}
		f.galera = append(f.galera, c)
		waitDatabase(t, "Galera 启动", 2*time.Minute, func() bool {
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			state, e := manager.Read(ctx, c)
			return e == nil && state.Ready && state.Synced && state.ClusterSize == i+1
		})
	}
	for _, c := range f.galera {
		waitDatabase(t, "三成员同步", 30*time.Second, func() bool {
			state, e := manager.Read(t.Context(), c)
			return e == nil && state.ClusterSize == 3 && state.Ready && state.Synced
		})
	}
	for i := range 2 {
		name := fmt.Sprintf("%s-p%d", f.network, i+1)
		f.names = append(f.names, name)
		adminPort, dataPort := freeDatabasePort(t), freeDatabasePort(t)
		file := filepath.Join(t.TempDir(), "proxysql.cnf")
		config := fmt.Sprintf("datadir=\"/var/lib/proxysql\"\nadmin_variables={admin_credentials=\"opsd_admin:%s\";mysql_ifaces=\"0.0.0.0:6032\";}\nmysql_variables={threads=2;max_connections=128;interfaces=\"0.0.0.0:6033\";monitor_enabled=false;}\nmysql_servers=({address=\"172.30.91.11\";port=3306;hostgroup=10;max_connections=50;},{address=\"172.30.91.12\";port=3306;hostgroup=10;max_connections=50;},{address=\"172.30.91.13\";port=3306;hostgroup=10;max_connections=50;})\n", password)
		if e := os.WriteFile(file, []byte(config), 0644); e != nil {
			t.Fatal(e)
		}
		f.docker(t, "run", "-d", "--name", name, "--network", f.network, "-p", fmt.Sprintf("127.0.0.1:%d:6032", adminPort), "-p", fmt.Sprintf("127.0.0.1:%d:6033", dataPort), "-v", file+":/etc/proxysql.cnf:ro", "--entrypoint", "/usr/bin/proxysql", "proxysql/proxysql:3.0.3", "-f", "--initial", "-c", "/etc/proxysql.cnf")
		c := protocol.DatabaseConnection{Kind: "proxysql", Address: fmt.Sprintf("127.0.0.1:%d", adminPort), Username: "opsd_admin", Password: password}
		f.proxies = append(f.proxies, c)
		f.proxyPorts = append(f.proxyPorts, dataPort)
		waitDatabase(t, "ProxySQL 启动", time.Minute, func() bool {
			state, e := manager.Read(t.Context(), c)
			return e == nil && state.State == "ready" && !state.PendingChanges
		})
	}
	return f
}

type databaseServices struct {
	service *executor.Services
	db      *sql.DB
	worker  context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	lose    atomic.Bool
}

func newDatabaseServices(t *testing.T) *databaseServices {
	t.Helper()
	db, e := storage.Open(t.Context(), filepath.Join(t.TempDir(), "executor.db"), storage.Executor)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	out := &databaseServices{service: executor.NewServices(db), db: db, worker: ctx, cancel: cancel}
	start := func(_ context.Context, id string) error {
		out.wg.Add(1)
		go func() {
			defer out.wg.Done()
			out.mu.Lock()
			defer out.mu.Unlock()
			_ = out.service.Engine.Run(out.worker, id)
		}()
		return nil
	}
	out.service.Start = start
	out.service.Restart = start
	t.Cleanup(func() { out.cancel(); out.wg.Wait(); db.Close() })
	return out
}
func (s *databaseServices) Call(ctx context.Context, p protocol.Payload) (protocol.Payload, error) {
	encoded, e := protocol.Encode(protocol.Message{RequestID: identity.Random(), Payload: p})
	if e != nil {
		return nil, e
	}
	request, e := protocol.Decode(encoded)
	if e != nil {
		return nil, e
	}
	reply, e := s.service.Handle(ctx, request.Payload)
	if _, ok := p.(*protocol.OperationSubmit); ok && e == nil && s.lose.CompareAndSwap(true, false) {
		return nil, errors.New("执行器应答丢失")
	}
	return reply, e
}
func (*databaseServices) Storage(context.Context, cluster.StorageConfig) (cluster.Credentials, error) {
	return cluster.Credentials{}, errors.New("未配置 OPSD 数据服务")
}
func nativeOperation(t *testing.T, entry *apiNode, raw, id string, wanted protocol.OperationState) cluster.Operation {
	t.Helper()
	var value cluster.Operation
	waitDatabase(t, "数据库操作", 45*time.Second, func() bool {
		code := apiCall(t, http.DefaultClient, entry.web.URL, "GET", "/api/v2/operations/"+id, raw, "", "", nil, &value)
		if code != 200 {
			t.Fatalf("读取原操作失败：%d", code)
		}
		if value.State == protocol.Rejected || value.State == protocol.Failed {
			t.Fatal("数据库操作被拒绝", value.Error)
		}
		return value.State == wanted
	})
	return value
}
func nativeSubmit(t *testing.T, entry *apiNode, raw, id, key string, change map[string]any) cluster.Operation {
	t.Helper()
	var op cluster.Operation
	if code := apiCall(t, http.DefaultClient, entry.web.URL, "POST", "/api/v2/database-clusters/"+id+"/changes", raw, key, "", change, &op); code != 202 {
		t.Fatal("数据库变更未受理", code)
	}
	return op
}
func operationCount(t *testing.T, s *databaseServices, kind string) int {
	t.Helper()
	var count int
	if e := s.db.QueryRow("SELECT count(*) FROM operations WHERE json_extract(action,'$.database.kind')=?", kind).Scan(&count); e != nil {
		t.Fatal(e)
	}
	return count
}
func stopDatabaseEntry(t *testing.T, node *apiNode) func(cluster.Executor) {
	t.Helper()
	var path string
	if e := node.node.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); e != nil {
		t.Fatal(e)
	}
	address := node.peer.Listener.Addr().String()
	id := node.node.ID
	node.web.Close()
	node.node.Close()
	node.peer.Close()
	return func(worker cluster.Executor) {
		n, e := cluster.Open(t.Context(), path, cluster.Options{Executor: worker})
		if e != nil {
			t.Fatal(e)
		}
		if n.ID != id {
			t.Fatal("重新启动改变了节点身份")
		}
		listener, e := net.Listen("tcp", address)
		if e != nil {
			t.Fatal(e)
		}
		peer := httptest.NewUnstartedServer(n.PeerHandler())
		peer.Listener.Close()
		peer.Listener = listener
		peer.Start()
		s, e := New(t.Context(), n.DB, Options{Node: n, EnforceListenerState: true})
		if e != nil {
			t.Fatal(e)
		}
		web := httptest.NewServer(s.Handler())
		node.node = n
		node.peer = peer
		node.web = web
		n.Start()
		t.Cleanup(func() { web.Close(); s.Close(); n.Close(); peer.Close() })
	}
}

func TestDatabaseNativeClusterAPIAndDashboard(t *testing.T) {
	f := startNativeDatabases(t)
	a := openAPINode(t, "C091 日本\n\"入口\"", "日本", 1, 20)
	b := openAPINode(t, "C041", "美国", 1, 20)
	c := openAPINode(t, "C052", "日本", 1, 20)
	joinAPINodes(t, a, b, c)
	nodes := []*apiNode{a, b, c}
	services := []*databaseServices{}
	for _, node := range nodes {
		s := newDatabaseServices(t)
		services = append(services, s)
		node.node.Executor = s
		node.node.Start()
	}
	manager := databaseengine.Manager{}
	state, e := manager.Read(t.Context(), f.galera[0])
	if e != nil {
		t.Fatal(e)
	}
	definition := cluster.ManagedDatabaseCluster{Name: "实际 Galera", Coordinator: a.node.ID, ClusterID: state.ClusterID, Members: []cluster.DatabaseRef{}, Proxies: []cluster.DatabaseRef{}}
	for i, node := range nodes {
		var item cluster.DatabaseInstance
		password := f.galera[i].Password
		input := cluster.DatabaseInstanceInput{DatabaseInstance: cluster.DatabaseInstance{ID: "galera", Name: "MariaDB", Kind: "galera", Address: f.galera[i].Address, Username: "root"}, Password: &password}
		if e = node.node.Call(t.Context(), node.node.ID, "database.instance.save", cluster.DatabaseRPC{Instance: input}, &item); e != nil {
			t.Fatal(e)
		}
		definition.Members = append(definition.Members, cluster.DatabaseRef{NodeID: node.node.ID, InstanceID: item.ID, Name: item.Name, Address: item.Address})
		if i > 0 {
			proxy := f.proxies[i-1]
			input := cluster.DatabaseInstanceInput{DatabaseInstance: cluster.DatabaseInstance{ID: "proxy", Name: "ProxySQL " + strconv.Itoa(i), Kind: "proxysql", Address: proxy.Address, Username: proxy.Username}, Password: &proxy.Password}
			if e = node.node.Call(t.Context(), node.node.ID, "database.instance.save", cluster.DatabaseRPC{Instance: input}, &item); e != nil {
				t.Fatal(e)
			}
			definition.Proxies = append(definition.Proxies, cluster.DatabaseRef{NodeID: node.node.ID, InstanceID: item.ID, Name: item.Name, Address: item.Address})
		}
	}
	var registered cluster.ManagedDatabaseCluster
	if e = a.node.Call(t.Context(), a.node.ID, "database.cluster.save", cluster.DatabaseRPC{Cluster: definition}, &registered); e != nil {
		t.Fatal(e)
	}
	directory, e := a.node.Directory(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	for _, node := range nodes[1:] {
		if e = a.node.Call(t.Context(), node.node.ID, "directory", cluster.DirectoryExchange{Delta: directory}, nil); e != nil {
			t.Fatal(e)
		}
	}
	_, raw, e := tokenStore(a).Create(t.Context(), apiauth.Input{Name: "数据库管理", Enabled: true, Grants: []apiauth.Grant{{Module: "databases", Level: "manage", Scope: "clusters", Resources: []string{registered.ID}}}})
	if e != nil {
		t.Fatal(e)
	}
	change := map[string]any{"expected_revision": registered.Revision, "kind": "create_database", "database": "opsd_ci_business", "charset": "utf8mb4", "proxies": []any{}}
	services[0].lose.Store(true)
	op := nativeSubmit(t, c, raw, registered.ID, "create-db", change)
	restartEntry := stopDatabaseEntry(t, c)
	unknown := nativeOperation(t, b, raw, op.ID, protocol.AttentionRequired)
	if code := apiCall(t, http.DefaultClient, b.web.URL, "POST", "/api/v2/operations/"+op.ID+"/actions", raw, "", "", map[string]any{"action": "recover", "expected_revision": unknown.Revision}, nil); code != 200 {
		t.Fatal("查询未知应答后的原操作失败", code)
	}
	nativeOperation(t, b, raw, op.ID, protocol.Succeeded)
	restartEntry(services[2])
	same := nativeSubmit(t, b, raw, registered.ID, "create-db", change)
	if same.ID != op.ID || operationCount(t, services[0], "create_database") != 1 {
		t.Fatal("跨入口重复创建了数据库")
	}
	change["database"] = "other"
	if code := apiCall(t, http.DefaultClient, b.web.URL, "POST", "/api/v2/database-clusters/"+registered.ID+"/changes", raw, "create-db", "", change, nil); code != 409 {
		t.Fatal("同一请求接受了其他库", code)
	}
	for _, connection := range f.galera {
		snapshot, e := manager.Read(t.Context(), connection)
		if e != nil || !slices.ContainsFunc(snapshot.Databases, func(db protocol.DatabaseSchema) bool { return db.Name == "opsd_ci_business" }) {
			t.Fatal("Galera 库复制读回失败", e)
		}
	}
	root := nativeSQL(t, f.galera[0])
	defer root.Close()
	if _, e = root.Exec("CREATE TABLE opsd_ci_business.verify_value(id INT PRIMARY KEY)"); e != nil {
		t.Fatal(e)
	}
	proxies := []cluster.DatabaseProxySelection{{NodeID: b.node.ID, InstanceID: "proxy", Hostgroup: 10}, {NodeID: c.node.ID, InstanceID: "proxy", Hostgroup: 10}}
	businessPassword := identity.Random()
	f.secrets = append(f.secrets, businessPassword)
	user := map[string]any{"expected_revision": registered.Revision, "kind": "create_user", "user": "opsd_business", "host": "%", "password": businessPassword, "grants": []protocol.DatabaseGrant{{Database: "opsd_ci_business", Privileges: []string{"SELECT", "INSERT"}}}, "proxies": proxies}
	op = nativeSubmit(t, c, raw, registered.ID, "create-user", user)
	nativeOperation(t, b, raw, op.ID, protocol.Succeeded)
	limited := f.galera[0]
	limited.Username = "opsd_business"
	limited.Password = businessPassword
	health, healthErr := manager.Read(t.Context(), limited, "health")
	if healthErr != nil || health.State != "ready" || len(health.Users) != 0 || len(health.Databases) != 0 {
		t.Fatal("健康摘要依赖账号或库详情权限", healthErr, health.State)
	}
	for _, port := range f.proxyPorts {
		connection := protocol.DatabaseConnection{Kind: "galera", Address: fmt.Sprintf("127.0.0.1:%d", port), Username: "opsd_business", Password: businessPassword}
		db := nativeSQL(t, connection)
		var one int
		if e = db.QueryRow("SELECT COUNT(*) FROM opsd_ci_business.verify_value").Scan(&one); e != nil {
			t.Fatal("业务账号经过代理连接失败", e)
		}
		db.Close()
	}
	proxyDB := nativeSQL(t, f.proxies[1])
	defer proxyDB.Close()
	if _, e = proxyDB.Exec("UPDATE mysql_users SET comment='外部待应用' WHERE username='opsd_business'"); e != nil {
		t.Fatal(e)
	}
	newPassword := identity.Random()
	f.secrets = append(f.secrets, newPassword)
	passwordChange := map[string]any{"expected_revision": registered.Revision, "kind": "password", "user": "opsd_business", "host": "%", "password": newPassword, "proxies": proxies}
	op = nativeSubmit(t, b, raw, registered.ID, "password-partial", passwordChange)
	pending := nativeOperation(t, c, raw, op.ID, protocol.AttentionRequired)
	if len(pending.Steps) != 3 || pending.Steps[0].State != "completed" || pending.Steps[1].State != "completed" {
		t.Fatal("部分同步结果丢失")
	}
	if _, e = proxyDB.Exec("UPDATE mysql_users SET comment='' WHERE username='opsd_business'"); e != nil {
		t.Fatal(e)
	}
	if code := apiCall(t, http.DefaultClient, c.web.URL, "POST", "/api/v2/operations/"+op.ID+"/actions", raw, "", "", map[string]any{"action": "recover", "expected_revision": pending.Revision}, nil); code != 200 {
		t.Fatal("原操作重新处理失败", code)
	}
	nativeOperation(t, b, raw, op.ID, protocol.Succeeded)
	if operationCount(t, services[0], "password") != 1 || operationCount(t, services[1], "proxy_upsert") != 2 || operationCount(t, services[2], "proxy_upsert") != 2 {
		t.Fatal("重做了已完成的数据库或代理步骤")
	}
	for _, port := range f.proxyPorts {
		db := nativeSQL(t, protocol.DatabaseConnection{Kind: "galera", Address: fmt.Sprintf("127.0.0.1:%d", port), Username: "opsd_business", Password: newPassword})
		var count int
		if e = db.QueryRow("SELECT COUNT(*) FROM opsd_ci_business.verify_value").Scan(&count); e != nil {
			t.Fatal("改密后的代理登录失败", e)
		}
		db.Close()
	}
	grant := map[string]any{"expected_revision": registered.Revision, "kind": "revoke", "user": "opsd_business", "host": "%", "database": "opsd_ci_business", "privileges": []string{"INSERT"}, "proxies": []any{}}
	op = nativeSubmit(t, c, raw, registered.ID, "revoke-grant", grant)
	nativeOperation(t, b, raw, op.ID, protocol.Succeeded)
	business := nativeSQL(t, protocol.DatabaseConnection{Kind: "galera", Address: f.galera[1].Address, Username: "opsd_business", Password: newPassword})
	if _, e = business.Exec("INSERT INTO opsd_ci_business.verify_value VALUES(1)"); e == nil {
		t.Fatal("撤权后仍能写入业务库")
	}
	business.Close()
	for _, path := range []string{"/api/v2/containers", "/api/v2/peer-trust", "/api/v2/nodes/" + a.node.ID + "/api-tokens"} {
		if code := apiCall(t, http.DefaultClient, b.web.URL, "GET", path, raw, "", "", nil, nil); code != 403 {
			t.Fatal("数据库授权越过模块边界", code)
		}
	}
	protected := map[string]any{"expected_revision": registered.Revision, "kind": "delete_user", "user": "root", "host": "%", "proxies": []any{}}
	if code := apiCall(t, http.DefaultClient, b.web.URL, "POST", "/api/v2/database-clusters/"+registered.ID+"/changes", raw, "protected", "", protected, nil); code != 422 {
		t.Fatal("受理了系统账号变更", code)
	}
	t.Run("真实看板", func(t *testing.T) { nativeDashboard(t, f, a) })
	registered = nativeProxyRouting(t, f, nodes, raw, newPassword, registered)
	proxies = nil
	for _, proxy := range registered.Proxies {
		proxies = append(proxies, cluster.DatabaseProxySelection{NodeID: proxy.NodeID, InstanceID: proxy.InstanceID, Hostgroup: 10})
	}
	remove := map[string]any{"expected_revision": registered.Revision, "kind": "delete_user", "user": "opsd_business", "host": "%", "proxies": proxies}
	op = nativeSubmit(t, c, raw, registered.ID, "delete-user", remove)
	nativeOperation(t, b, raw, op.ID, protocol.Succeeded)
	for _, connection := range f.proxies {
		snapshot, e := manager.Read(t.Context(), connection)
		if e != nil || slices.ContainsFunc(snapshot.ProxyUsers, func(u protocol.DatabaseProxyUser) bool { return u.Name == "opsd_business" }) {
			t.Fatal("代理账号未清理", e)
		}
	}
	remove = map[string]any{"expected_revision": registered.Revision, "kind": "delete_database", "database": "opsd_ci_business", "proxies": []any{}}
	op = nativeSubmit(t, c, raw, registered.ID, "delete-db", remove)
	nativeOperation(t, b, raw, op.ID, protocol.Succeeded)
	output := filepath.Join("..", "..", "..", "artifacts")
	_ = os.MkdirAll(output, 0755)
	report, _ := json.MarshalIndent(map[string]any{"galera_members": 3, "proxysql": 2, "api_nodes": 4, "idempotency": true, "partial_recovery": true, "grants": true, "proxy_login": true, "dashboard": true, "proxy_routing": true, "proxy_only_node": true, "active_transaction": true, "proxy_failover": true, "proxy_restart": true, "multiwriter_conflict": true}, "", "  ")
	if e = os.WriteFile(filepath.Join(output, "database-result.json"), report, 0644); e != nil {
		t.Fatal(e)
	}
}

func nativeDashboard(t *testing.T, f *nativeDatabases, node *apiNode) {
	t.Helper()
	_, raw, e := tokenStore(node).Create(t.Context(), apiauth.Input{Name: "实际看板", Enabled: true, Grants: []apiauth.Grant{{Module: "monitoring", Level: "read", Scope: "nodes", Resources: []string{node.node.ID}}}})
	if e != nil {
		t.Fatal(e)
	}
	f.secrets = append(f.secrets, raw)
	promPort, grafanaPort := freeDatabasePort(t), freeDatabasePort(t)
	promName, grafanaName := f.network+"-prom", f.network+"-grafana"
	f.names = append(f.names, promName, grafanaName)
	configPath := filepath.Join(t.TempDir(), "prometheus.yml")
	config := fmt.Sprintf("global:\n  scrape_interval: 1s\nscrape_configs:\n  - job_name: opsd\n    authorization:\n      type: Bearer\n      credentials: '%s'\n    static_configs:\n      - targets: ['%s']\n", raw, strings.TrimPrefix(node.web.URL, "http://"))
	if e = os.WriteFile(configPath, []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	f.docker(t, "run", "-d", "--name", promName, "--network", "host", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "-v", configPath+":/etc/prometheus/prometheus.yml:ro", "prom/prometheus:v3.5.0", "--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.path=/tmp/prom-data", "--web.listen-address=127.0.0.1:"+strconv.Itoa(promPort))
	promURL := fmt.Sprintf("http://127.0.0.1:%d", promPort)
	checkProm := func(base, path string, basic string) bool {
		request, _ := http.NewRequest("GET", base+path, nil)
		if basic != "" {
			request.SetBasicAuth("admin", basic)
		}
		client := &http.Client{Timeout: 3 * time.Second}
		prepareTestRequest(t, client, request)
		response, e := client.Do(request)
		if e != nil {
			return false
		}
		defer response.Body.Close()
		var result struct {
			Status string `json:"status"`
			Data   struct {
				Result []struct {
					Metric map[string]string `json:"metric"`
					Value  []json.RawMessage `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		if json.NewDecoder(response.Body).Decode(&result) != nil || result.Status != "success" || len(result.Data.Result) != 1 {
			return false
		}
		return result.Data.Result[0].Metric["node_id"] == node.node.ID && len(result.Data.Result[0].Value) == 2 && string(result.Data.Result[0].Value[1]) == "\"1\""
	}
	waitDatabase(t, "Prometheus 授权采集", time.Minute, func() bool { return checkProm(promURL, "/api/v1/query?query=opsd_node_up", "") })
	password := identity.Random()
	f.secrets = append(f.secrets, password)
	f.docker(t, "run", "-d", "--name", grafanaName, "--network", "host", "-e", "GF_SERVER_HTTP_ADDR=127.0.0.1", "-e", "GF_SERVER_HTTP_PORT="+strconv.Itoa(grafanaPort), "-e", "GF_SECURITY_ADMIN_PASSWORD="+password, "-e", "GF_ANALYTICS_REPORTING_ENABLED=false", "grafana/grafana:12.1.0")
	grafanaURL := fmt.Sprintf("http://127.0.0.1:%d", grafanaPort)
	waitDatabase(t, "Grafana 启动", time.Minute, func() bool {
		client := &http.Client{Timeout: time.Second}
		response, e := client.Get(grafanaURL + "/api/health")
		if e != nil {
			return false
		}
		response.Body.Close()
		return response.StatusCode == 200
	})
	body, _ := json.Marshal(map[string]any{"uid": "opsd-ci", "name": "OPSD", "type": "prometheus", "url": promURL, "access": "proxy", "jsonData": map[string]any{"httpMethod": "GET"}})
	request, _ := http.NewRequest("POST", grafanaURL+"/api/datasources", bytes.NewReader(body))
	request.SetBasicAuth("admin", password)
	request.Header.Set("Content-Type", "application/json")
	response, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("Grafana 数据源接入失败", response.StatusCode)
	}
	waitDatabase(t, "Grafana 查询 OPSD", 30*time.Second, func() bool {
		return checkProm(grafanaURL, "/api/datasources/proxy/uid/opsd-ci/api/v1/query?query="+url.QueryEscape("opsd_node_up"), password)
	})
}
