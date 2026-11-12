//go:build linux

package control

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/protocol"
)

// nativeProxyRouting 复用同批 Galera 和代理，覆盖无数据库实例的代理所属节点。
func nativeProxyRouting(t *testing.T, f *nativeDatabases, nodes []*apiNode, raw, password string, registered cluster.ManagedDatabaseCluster) cluster.ManagedDatabaseCluster {
	t.Helper()
	standalone := openAPINode(t, "仅代理节点", "日本", 1, 20)
	joinAPINodes(t, nodes[0], standalone)
	service := newDatabaseServices(t)
	standalone.node.Executor = service
	standalone.node.Start()
	connection := f.proxies[0]
	var instance cluster.DatabaseInstance
	input := cluster.DatabaseInstanceInput{DatabaseInstance: cluster.DatabaseInstance{ID: "standalone-proxy", Name: "独立 ProxySQL", Kind: "proxysql", Address: connection.Address, Username: connection.Username}, Password: &connection.Password}
	if err := standalone.node.Call(t.Context(), standalone.node.ID, "database.instance.save", cluster.DatabaseRPC{Instance: input}, &instance); err != nil {
		t.Fatal(err)
	}
	registered.Proxies[0] = cluster.DatabaseRef{NodeID: standalone.node.ID, InstanceID: instance.ID, Name: instance.Name, Address: instance.Address}
	if err := nodes[0].node.Call(t.Context(), nodes[0].node.ID, "database.cluster.save", cluster.DatabaseRPC{Cluster: registered}, &registered); err != nil {
		t.Fatal(err)
	}
	directory, err := nodes[0].node.Directory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	all := append(append([]*apiNode{}, nodes...), standalone)
	for _, node := range all[1:] {
		if err = nodes[0].node.Call(t.Context(), node.node.ID, "directory", cluster.DirectoryExchange{Delta: directory}, nil); err != nil {
			t.Fatal(err)
		}
	}

	admins := []*sql.DB{}
	for _, connection := range f.proxies {
		db := nativeSQL(t, connection)
		admins = append(admins, db)
		defer db.Close()
		for name, value := range map[string]string{"mysql-monitor_username": "root", "mysql-monitor_password": f.galera[0].Password, "mysql-monitor_enabled": "true", "mysql-monitor_ping_interval": "500", "mysql-monitor_galera_healthcheck_interval": "500", "mysql-monitor_galera_healthcheck_timeout": "500"} {
			if _, err = db.Exec("UPDATE global_variables SET variable_value=? WHERE variable_name=?", value, name); err != nil {
				t.Fatal(err)
			}
		}
		for _, query := range []string{"LOAD MYSQL VARIABLES TO RUNTIME", "SAVE MYSQL VARIABLES TO DISK", "INSERT INTO mysql_galera_hostgroups(writer_hostgroup,backup_writer_hostgroup,reader_hostgroup,offline_hostgroup,active,max_writers,writer_is_also_reader,max_transactions_behind) VALUES(10,20,30,40,1,1,0,10)", "INSERT INTO mysql_servers(hostgroup_id,hostname,port,weight,max_connections) VALUES(10,'172.30.91.71',3306,2,50)", "INSERT INTO mysql_servers(hostgroup_id,hostname,port,weight,max_connections,comment) VALUES(99,'172.30.91.11',3306,7,7,'外部配置')", "LOAD MYSQL SERVERS TO RUNTIME", "SAVE MYSQL SERVERS TO DISK"} {
			if _, err = db.Exec(query); err != nil {
				t.Fatal("准备代理监控失败", err)
			}
		}
	}
	manager := databaseengine.Manager{}
	for _, connection := range f.proxies {
		waitDatabase(t, "代理监控采集", 30*time.Second, func() bool {
			observation, err := manager.ReadProxyRouting(t.Context(), connection, nil)
			return err == nil && len(observation.Samples) >= 9 && !observation.PendingChanges
		})
	}
	if _, err = admins[0].Exec("UPDATE mysql_servers SET weight=8 WHERE hostgroup_id=99"); err != nil {
		t.Fatal(err)
	}
	previewInput := map[string]any{"expected_revision": registered.Revision, "enabled": true, "automatic": true}
	path := "/api/v2/database-clusters/" + registered.ID + "/proxy-routing"
	if code := apiCall(t, http.DefaultClient, nodes[0].web.URL, "POST", path+"/previews", raw, "", "", previewInput, nil); code != 409 {
		t.Fatal("预览接受了其他主机组的待应用配置", code)
	}
	if _, err = admins[0].Exec("UPDATE mysql_servers SET weight=7 WHERE hostgroup_id=99"); err != nil {
		t.Fatal(err)
	}
	var preview cluster.ProxyRoutingPreview
	if code := apiCall(t, http.DefaultClient, nodes[0].web.URL, "POST", path+"/previews", raw, "", "", previewInput, &preview); code != 200 {
		t.Fatal("路由预览失败", code)
	}
	if len(preview.Policy.Backends) != 3 || !slices.ContainsFunc(preview.Policy.Retired, func(endpoint protocol.ProxyEndpoint) bool { return endpoint.Host == "172.30.91.71" }) {
		t.Fatal("退出成员没有进入预览", preview.Policy)
	}
	for i := range preview.Targets {
		if preview.Targets[i].NodeID != standalone.node.ID {
			continue
		}
		for j := range preview.Targets[i].Action.Servers {
			server := &preview.Targets[i].Action.Servers[j]
			server.Weight = 800 - j*10
			if server.Host == "172.30.91.11" {
				server.Weight = 1000
			}
		}
	}
	client := nativeSQL(t, protocol.DatabaseConnection{Kind: "galera", Address: "127.0.0.1:" + strconv.Itoa(f.proxyPorts[0]), Username: "opsd_business", Password: password})
	defer client.Close()
	session, err := client.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err = session.ExecContext(t.Context(), "START TRANSACTION"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = session.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM opsd_ci_business.verify_value").Scan(&count); err != nil {
		t.Fatal(err)
	}
	var before string
	if err = session.QueryRowContext(t.Context(), "SELECT @@wsrep_node_address").Scan(&before); err != nil {
		t.Fatal(err)
	}
	// 令独立代理的新写节点与当前活动事务不同。
	for i := range preview.Targets {
		if preview.Targets[i].NodeID != standalone.node.ID {
			continue
		}
		for j := range preview.Targets[i].Action.Servers {
			server := &preview.Targets[i].Action.Servers[j]
			if server.Host == before {
				server.Weight = 100
			}
		}
	}
	service.lose.Store(true)
	var operation cluster.Operation
	if code := apiCall(t, http.DefaultClient, nodes[0].web.URL, "POST", path+"/actions", raw, "apply-native-routing", "", preview, &operation); code != 202 {
		t.Fatal("代理路由未受理", code)
	}
	nativeOperation(t, nodes[0], raw, operation.ID, protocol.Succeeded)
	if operationCount(t, service, "proxy_route") != 1 {
		t.Fatal("应答丢失后重复创建了代理步骤")
	}
	var after string
	if err = session.QueryRowContext(t.Context(), "SELECT @@wsrep_node_address").Scan(&after); err != nil || after != before {
		t.Fatal("权重切换中断或迁移了活动事务", before, after, err)
	}
	if _, err = session.ExecContext(t.Context(), "COMMIT"); err != nil {
		t.Fatal(err)
	}
	var duplicate cluster.Operation
	if code := apiCall(t, http.DefaultClient, nodes[0].web.URL, "POST", path+"/actions", raw, "apply-native-routing", "", preview, &duplicate); code != 202 || duplicate.ID != operation.ID {
		t.Fatal("重复应用生成了新操作", code, duplicate.ID)
	}
	if err = nodes[0].node.Call(t.Context(), nodes[0].node.ID, "database.cluster.get", cluster.DatabaseRPC{ResourceID: registered.ID}, &registered); err != nil || registered.ProxyRouting == nil {
		t.Fatal("共享代理策略未保存", err)
	}
	for i, db := range admins {
		var retired, foreign int
		if err = db.QueryRow("SELECT COUNT(*) FROM mysql_servers WHERE hostname='172.30.91.71'").Scan(&retired); err != nil || retired != 0 {
			t.Fatal("退出成员仍在代理路由中", retired, err)
		}
		if err = db.QueryRow("SELECT weight FROM mysql_servers WHERE hostgroup_id=99").Scan(&foreign); err != nil || foreign != 7 {
			t.Fatal("修改了外部主机组", foreign, err)
		}
		waitDatabase(t, "Galera 单写节点读回", 20*time.Second, func() bool {
			var online int
			return db.QueryRow("SELECT count(*) FROM runtime_mysql_servers WHERE hostgroup_id=10 AND status='ONLINE'").Scan(&online) == nil && online == 1
		})
		if i == 1 {
			var host string
			if err = db.QueryRow("SELECT hostname FROM runtime_mysql_servers WHERE hostgroup_id=10 AND status='ONLINE'").Scan(&host); err != nil || host != "172.30.91.13" {
				t.Fatal("代理未优先选择本机数据库", host, err)
			}
		}
	}
	// 数据库暂时不可达时，本机队列继续执行，代理使用原生 Galera 故障切换。
	var writer string
	if err = admins[0].QueryRow("SELECT hostname FROM runtime_mysql_servers WHERE hostgroup_id=10 AND status='ONLINE'").Scan(&writer); err != nil {
		t.Fatal(err)
	}
	index, err := strconv.Atoi(writer[len("172.30.91."):])
	if err != nil || index < 11 || index > 13 {
		t.Fatal("写节点地址异常", writer, err)
	}
	name := f.names[index-11]
	f.docker(t, "pause", name)
	t.Cleanup(func() { _ = exec.Command("docker", "unpause", name).Run() })
	op, err := standalone.node.Submit(t.Context(), standalone.node.ID, cluster.Request{Key: "database-unavailable-local", Kind: "name", Payload: jsonPayload("独立代理继续运行")})
	if err != nil {
		t.Fatal(err)
	}
	waitDatabase(t, "断库期间本机操作", 10*time.Second, func() bool {
		result, err := standalone.node.GetOperation(t.Context(), op.ID)
		return err == nil && result.State == protocol.Succeeded
	})
	waitDatabase(t, "代理故障切换", 20*time.Second, func() bool {
		var current string
		return admins[0].QueryRow("SELECT hostname FROM runtime_mysql_servers WHERE hostgroup_id=10 AND status='ONLINE'").Scan(&current) == nil && current != writer
	})
	f.docker(t, "unpause", name)
	for _, connection := range f.galera {
		waitDatabase(t, "数据库恢复同步", 30*time.Second, func() bool {
			state, err := manager.Read(t.Context(), connection, "health")
			return err == nil && state.Ready && state.Synced && state.ClusterSize == 3
		})
	}
	nativeGaleraWriteConflict(t, f)

	// 保存后的代理配置经实际进程重启读回。
	for i, connection := range f.proxies {
		f.docker(t, "restart", f.names[len(f.galera)+i])
		waitDatabase(t, "代理重启持久配置", 20*time.Second, func() bool {
			observation, err := manager.ReadProxyRouting(t.Context(), connection, &preview.Policy.Hostgroups)
			return err == nil && databaseengine.ProxyRouteActionSatisfied(observation, preview.Targets[i].Action)
		})
	}
	var state cluster.ProxyRoutingState
	if err = standalone.node.Call(t.Context(), standalone.node.ID, "database.proxy_routing.state", cluster.DatabaseRPC{ResourceID: registered.ID, InstanceID: instance.ID}, &state); err != nil || state.CheckedAt == nil {
		t.Fatal(state, err)
	}
	oldChecked := *state.CheckedAt
	config, err := standalone.node.Config(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	config.Console, config.API = false, false
	op, err = standalone.node.Submit(t.Context(), standalone.node.ID, cluster.Request{Key: "disable-proxy-web", Kind: "config", Payload: jsonPayload(config)})
	if err != nil {
		t.Fatal(err)
	}
	waitDatabase(t, "关闭代理节点网页", 10*time.Second, func() bool {
		result, err := standalone.node.GetOperation(t.Context(), op.ID)
		return err == nil && result.State == protocol.Succeeded
	})
	waitDatabase(t, "网页关闭后后台排序继续", 45*time.Second, func() bool {
		if err := standalone.node.Call(t.Context(), standalone.node.ID, "database.proxy_routing.state", cluster.DatabaseRPC{ResourceID: registered.ID, InstanceID: instance.ID}, &state); err != nil {
			return false
		}
		return state.CheckedAt != nil && state.CheckedAt.After(oldChecked) && state.State == "ready"
	})
	return registered
}

func jsonPayload(value any) []byte { data, _ := json.Marshal(value); return data }

func nativeGaleraWriteConflict(t *testing.T, f *nativeDatabases) {
	t.Helper()
	first, second := nativeSQL(t, f.galera[0]), nativeSQL(t, f.galera[1])
	defer first.Close()
	defer second.Close()
	if _, err := first.Exec("CREATE TABLE opsd_ci_business.write_conflict(id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	waitDatabase(t, "多写事务准备", 10*time.Second, func() bool {
		var count int
		return second.QueryRow("SELECT COUNT(*) FROM opsd_ci_business.write_conflict").Scan(&count) == nil && count == 0
	})
	one, err := first.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Rollback()
	two, err := second.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Rollback()
	for _, tx := range []*sql.Tx{one, two} {
		if _, err = tx.Exec("SET SESSION innodb_lock_wait_timeout=2"); err != nil {
			t.Fatal("设置并发事务期限", err)
		}
		if _, err = tx.Exec("INSERT INTO opsd_ci_business.write_conflict VALUES(1)"); err != nil {
			t.Fatal("准备不同入口的未提交写入", err)
		}
	}
	// 两个入口提交同一行，各事务只发送一次，认证失败由调用方显式处理。
	start, results := make(chan struct{}), make(chan error, 2)
	for _, tx := range []*sql.Tx{one, two} {
		go func() { <-start; results <- tx.Commit() }()
	}
	close(start)
	succeeded, conflicts := 0, 0
	for range 2 {
		if err = <-results; err == nil {
			succeeded++
			continue
		}
		var fault *mysql.MySQLError
		if !errors.As(err, &fault) || fault.Number != 1213 && fault.Number != 1205 {
			t.Fatalf("多写事务返回非认证冲突：%v", err)
		}
		conflicts++
	}
	if succeeded != 1 || conflicts != 1 {
		t.Fatalf("多写事务结果：成功 %d，冲突 %d", succeeded, conflicts)
	}
	for _, db := range []*sql.DB{first, second} {
		waitDatabase(t, "冲突事务收敛", 10*time.Second, func() bool {
			var count int
			return db.QueryRow("SELECT COUNT(*) FROM opsd_ci_business.write_conflict WHERE id=1").Scan(&count) == nil && count == 1
		})
	}
}
