//go:build linux

package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/databaseengine"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

func TestSharedStorageNativeMigrationAndRecovery(t *testing.T) {
	if os.Getenv("OPSD_DATABASE_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("设置 OPSD_DATABASE_TEST=1 并以 root 运行代理自动安装与恢复检查")
	}
	var autoDirectory string
	t.Cleanup(func() {
		if autoDirectory != "" && filepath.Dir(autoDirectory) == "/var/lib/opsd/databases" && strings.HasPrefix(filepath.Base(autoDirectory), "opsd_sql_") {
			_ = os.RemoveAll(autoDirectory)
		}
	})
	f := startNativeDatabases(t)
	for _, connection := range f.proxies {
		db := nativeSQL(t, connection)
		for name, value := range map[string]string{"mysql-monitor_username": "root", "mysql-monitor_password": f.galera[0].Password, "mysql-monitor_enabled": "true", "mysql-monitor_galera_healthcheck_interval": "500"} {
			if _, err := db.Exec("UPDATE global_variables SET variable_value=? WHERE variable_name=?", value, name); err != nil {
				t.Fatal(err)
			}
		}
		for _, query := range []string{"LOAD MYSQL VARIABLES TO RUNTIME", "SAVE MYSQL VARIABLES TO DISK", "INSERT INTO mysql_galera_hostgroups(writer_hostgroup,backup_writer_hostgroup,reader_hostgroup,offline_hostgroup,active,max_writers,writer_is_also_reader,max_transactions_behind) VALUES(10,20,30,40,1,1,0,10)", "LOAD MYSQL SERVERS TO RUNTIME", "SAVE MYSQL SERVERS TO DISK"} {
			if _, err := db.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
		db.Close()
	}
	a, b, c := openAPINode(t, "引导节点", "日本", 1, 1), openAPINode(t, "主用入口", "日本", 1, 1), openAPINode(t, "备用入口", "美国", 1, 1)
	joinAPINodes(t, a, b, c)
	nodes := []*apiNode{a, b, c}
	services := []*databaseServices{}
	manager := databaseengine.Manager{}
	first, err := manager.Read(t.Context(), f.galera[0], "health")
	if err != nil {
		t.Fatal(err)
	}
	definition := cluster.ManagedDatabaseCluster{Name: "共享与业务库", Coordinator: a.node.ID, ClusterID: first.ClusterID, Members: []cluster.DatabaseRef{}, Proxies: []cluster.DatabaseRef{}}
	for i, node := range nodes {
		worker := newDatabaseServices(t)
		services = append(services, worker)
		node.node.Executor = worker
		var instance cluster.DatabaseInstance
		connection := f.galera[i]
		containerID := f.docker(t, "inspect", "--format", "{{.Id}}", f.names[i])
		input := cluster.DatabaseInstanceInput{DatabaseInstance: cluster.DatabaseInstance{ID: "galera", Name: "MariaDB", Kind: "galera", Address: connection.Address, Username: connection.Username, ContainerID: containerID}, Password: &connection.Password}
		if err = node.node.Call(t.Context(), node.node.ID, "database.instance.save", cluster.DatabaseRPC{Instance: input}, &instance); err != nil {
			t.Fatal(err)
		}
		state, err := manager.Read(t.Context(), connection, "health")
		if err != nil {
			t.Fatal(err)
		}
		definition.Members = append(definition.Members, cluster.DatabaseRef{NodeID: node.node.ID, InstanceID: instance.ID, Name: instance.Name, Address: state.MemberAddress})
		if i > 0 {
			connection = f.proxies[i-1]
			containerID = f.docker(t, "inspect", "--format", "{{.Id}}", f.names[i+2])
			input = cluster.DatabaseInstanceInput{DatabaseInstance: cluster.DatabaseInstance{ID: "proxy", Name: "ProxySQL", Kind: "proxysql", Address: connection.Address, Username: connection.Username, ContainerID: containerID}, Password: &connection.Password}
			if err = node.node.Call(t.Context(), node.node.ID, "database.instance.save", cluster.DatabaseRPC{Instance: input}, &instance); err != nil {
				t.Fatal(err)
			}
			definition.Proxies = append(definition.Proxies, cluster.DatabaseRef{NodeID: node.node.ID, InstanceID: instance.ID, Name: instance.Name, Address: connection.Address})
		}
	}
	if err = a.node.Call(t.Context(), a.node.ID, "database.cluster.save", cluster.DatabaseRPC{Cluster: definition}, &definition); err != nil {
		t.Fatal(err)
	}
	directory, err := a.node.Directory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes[1:] {
		if err = a.node.Call(t.Context(), node.node.ID, "directory", cluster.DirectoryExchange{Delta: directory}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, node := range nodes {
		node.node.Start()
	}
	_, raw, err := tokenStore(b).Create(t.Context(), apiauth.Input{Name: "共享管理", Enabled: true, Grants: []apiauth.Grant{{Module: "databases", Level: "manage", Scope: "clusters", Resources: []string{definition.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	f.secrets = append(f.secrets, raw)
	legacyChange := map[string]any{"expected_revision": definition.Revision, "kind": "create_database", "database": "legacy_preserved", "charset": "utf8mb4", "proxies": []any{}}
	legacy := nativeSubmit(t, c, raw, definition.ID, "legacy", legacyChange)
	nativeOperation(t, b, raw, legacy.ID, protocol.Succeeded)
	autoDirectory = "/var/lib/opsd/databases/opsd_sql_" + a.node.ID
	f.names = append(f.names, (protocol.DatabaseDeployment{ID: "opsd_sql_" + a.node.ID, Kind: "proxysql"}).ContainerName())
	op, err := b.node.StartSharedInitialization(t.Context(), cluster.SharedInitialize{ClusterID: definition.ID}, "initialize-shared")
	if err != nil {
		t.Fatal(err)
	}
	lastProgress := ""
	waitDatabase(t, "共享数据迁移", 3*time.Minute, func() bool {
		current, e := b.node.GetOperation(t.Context(), op.ID)
		if e != nil {
			t.Fatal(e)
		}
		statuses := make([]cluster.SharedStatus, len(nodes))
		for i, node := range nodes {
			statuses[i] = node.node.SharedStatus(t.Context())
		}
		progress, _ := json.Marshal(struct {
			State   protocol.OperationState `json:"state"`
			Waiting string                  `json:"waiting"`
			Nodes   []cluster.SharedStatus  `json:"nodes"`
		}{current.State, current.WaitingReason, statuses})
		if string(progress) != lastProgress {
			t.Logf("共享迁移进度：%s", progress)
			lastProgress = string(progress)
		}
		if current.State == protocol.AttentionRequired || current.State == protocol.Rejected || current.State == protocol.Failed {
			t.Fatalf("共享迁移停止：%+v", current.Error)
		}
		return current.State == protocol.Succeeded
	})
	for _, node := range nodes {
		waitDatabase(t, "节点共享代理", time.Minute, func() bool {
			status := node.node.SharedStatus(t.Context())
			return status.State == "ready" && status.ProxyReady
		})
	}
	if repeated := nativeSubmit(t, b, raw, definition.ID, "legacy", legacyChange); repeated.ID != legacy.ID {
		t.Fatal("迁移改变历史请求 ID")
	}
	admin, csrf := apiAdmin(t, b)
	if code := apiCall(t, admin, b.web.URL, "GET", "/api/v2/shared-storage", "", "", "", nil, nil); code != 200 {
		t.Fatal("共享绑定接口不可读", code)
	}
	if code := apiCall(t, admin, b.web.URL, "PUT", "/api/v2/shared-storage/consoles", "", "console-nodes", csrf, map[string]any{"revision": 0, "primary": b.node.ID, "backup": c.node.ID}, nil); code != 200 {
		t.Fatal("主用与备用管理节点未保存", code)
	}
	policy, err := b.node.ChangeTrust(t.Context(), cluster.TrustPolicyPatch{Enabled: true})
	if err != nil {
		t.Fatal("共享互信策略未保存", err)
	}
	waitDatabase(t, "其他入口读取共享互信策略", 15*time.Second, func() bool {
		current, e := c.node.TrustPolicy(t.Context())
		return e == nil && current == policy
	})
	policy, err = c.node.ChangeTrust(t.Context(), cluster.TrustPolicyPatch{ExpectedVersion: policy.Version, Enabled: false})
	if err != nil {
		t.Fatal("备用入口未更新共享互信策略", err)
	}
	if _, err = b.node.ChangeTrust(t.Context(), cluster.TrustPolicyPatch{Enabled: true}); err == nil {
		t.Fatal("旧版本覆盖共享互信策略")
	}
	sharedDB := nativeSQL(t, f.galera[0])
	defer sharedDB.Close()
	var storedPolicy []byte
	if err = sharedDB.QueryRow("SELECT body FROM opsd_core.shared_objects WHERE kind='trust' AND id='policy'").Scan(&storedPolicy); err != nil {
		t.Fatal(err)
	}
	var savedPolicy cluster.TrustPolicy
	if err = json.Unmarshal(storedPolicy, &savedPolicy); err != nil || savedPolicy.TrustPolicyState != policy {
		t.Fatal("共享库未保留最新互信策略", err)
	}
	public, err := b.node.PublicAddresses(t.Context(), b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	public, err = b.node.PatchPublic(t.Context(), cluster.PublicAddressesPatch{ExpectedRevision: public.Revision, IPv4: &cluster.PublicFamilyPatch{Mode: "manual", Addresses: []string{"8.8.4.4"}}})
	if err != nil {
		t.Fatal(err)
	}
	waitDatabase(t, "本机公网配置补入共享库", 15*time.Second, func() bool {
		var revision uint64
		return sharedDB.QueryRow("SELECT revision FROM opsd_core.shared_objects WHERE kind='address' AND id=?", b.node.ID).Scan(&revision) == nil && revision == public.Revision
	})
	nativeSharedSQL(t, f, b)
	restartA := stopDatabaseEntry(t, a)
	var existingAccess cluster.Secret
	if err = b.node.Call(t.Context(), c.node.ID, "shared.bootstrap", nil, &existingAccess); err != nil {
		t.Fatal("原维护节点停止后已准备账号无法复用", err)
	}
	joining := openAPINode(t, "新加入节点", "美国", 1, 1)
	joinAPINodes(t, b, joining)
	var joinedAccess cluster.Secret
	waitDatabase(t, "新成员取得数据库接入资料", time.Minute, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		err = joining.node.Call(ctx, b.node.ID, "shared.bootstrap", nil, &joinedAccess)
		return err == nil
	})
	var repeatedAccess cluster.Secret
	if err = joining.node.Call(t.Context(), c.node.ID, "shared.bootstrap", nil, &repeatedAccess); err != nil {
		t.Fatal("备用成员无法复用新成员账号", err)
	}
	firstAccess, _ := json.Marshal(joinedAccess)
	secondAccess, _ := json.Marshal(repeatedAccess)
	if string(firstAccess) != string(secondAccess) || joining.node.SharedStatus(t.Context()).Binding == nil {
		t.Fatal("重复引导改变新成员接入资料或丢失绑定")
	}
	current, err := b.node.DatabaseCluster(t.Context(), definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	change := map[string]any{"expected_revision": current.Revision, "kind": "create_database", "database": "takeover_business", "charset": "utf8mb4", "proxies": []any{}}
	op = nativeSubmit(t, c, raw, current.ID, "takeover", change)
	nativeOperation(t, b, raw, op.ID, protocol.Succeeded)
	current, err = b.node.DatabaseCluster(t.Context(), definition.ID)
	if err != nil || current.Coordinator != c.node.ID {
		t.Fatal("原协调节点停止后未转交", err, current.Coordinator)
	}
	start := services[1].service.Start
	services[1].service.Start = func(context.Context, string) error { return errors.New("模拟操作服务启动失败") }
	change = map[string]any{"expected_revision": current.Revision, "kind": "create_database", "database": "continued_business", "charset": "utf8mb4", "proxies": []any{}}
	op = nativeSubmit(t, b, raw, current.ID, "continue-original", change)
	waitDatabase(t, "目标持久受理", time.Minute, func() bool {
		var count int
		_ = services[1].db.QueryRow("SELECT COUNT(*) FROM operations WHERE json_extract(action,'$.database.flow_id')=? AND start_error<>''", op.ID).Scan(&count)
		return count == 1
	})
	restartC := stopDatabaseEntry(t, c)
	waitDatabase(t, "备用入口取得新代际", 45*time.Second, func() bool {
		value, e := b.node.GetOperation(t.Context(), op.ID)
		return e == nil && value.Coordinator == b.node.ID && value.CoordinatorEpoch > 1
	})
	services[1].service.Start = start
	if err = services[1].service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	nativeOperation(t, b, raw, op.ID, protocol.Succeeded)
	var accepted int
	if err = services[1].db.QueryRow("SELECT COUNT(*) FROM operations WHERE json_extract(action,'$.database.flow_id')=?", op.ID).Scan(&accepted); err != nil || accepted != 1 {
		t.Fatal("接续改变目标子操作 ID", accepted, err)
	}
	restartC(services[2])
	restartA(services[0])
	for _, node := range []*apiNode{a, c} {
		waitDatabase(t, "旧协调节点读取新归属", time.Minute, func() bool {
			value, e := node.node.GetOperation(t.Context(), op.ID)
			return e == nil && value.State == protocol.Succeeded && value.Coordinator == b.node.ID
		})
	}
	proxyID := a.node.SharedStatus(t.Context()).ProxyID
	var instance cluster.DatabaseInstance
	if err = a.node.Call(t.Context(), a.node.ID, "database.instance.get", cluster.DatabaseRPC{InstanceID: proxyID}, &instance); err != nil {
		t.Fatal(err)
	}
	f.docker(t, "stop", instance.ContainerID)
	consoles := b.node.SharedConsoles(t.Context())
	if _, err = b.node.SaveSharedConsoles(t.Context(), consoles); err != nil {
		t.Fatal("一台代理故障阻塞其他节点事务", err)
	}
	waitDatabase(t, "原代理独立恢复", time.Minute, func() bool {
		return f.docker(t, "inspect", "--format", "{{.State.Running}}", instance.ContainerID) == "true"
	})
	if b.node.SharedStatus(t.Context()).State != "ready" {
		t.Fatal("一台代理恢复影响其他节点")
	}
	write := &requeststate.Write{Actor: "account:" + b.node.ID + ":1", Key: "revoke-c", Digest: strings.Repeat("a", 64)}
	if err = b.node.Revoke(requeststate.With(t.Context(), write), c.node.ID); err != nil {
		t.Fatal(err)
	}
	db := nativeSQL(t, f.galera[0])
	defer db.Close()
	waitDatabase(t, "撤销 SQL 服务账号", time.Minute, func() bool {
		var count int
		return db.QueryRow("SELECT COUNT(*) FROM mysql.user WHERE User IN (?,?)", protocol.SharedUsername(c.node.ID), protocol.SharedMonitorUser(c.node.ID)).Scan(&count) == nil && count == 0
	})
	output := filepath.Join("..", "..", "..", "artifacts")
	_ = os.MkdirAll(output, 0755)
	perNode := sharedsql.MaxConnections + sharedsql.MonitorConnections
	report, _ := json.MarshalIndent(map[string]any{"nodes": 3, "joining_node": true, "local_proxies": 3, "installed_proxy": 1, "reused_proxies": 2, "migration": true, "original_receipt": true, "shared_policy": true, "address_upload": true, "coordinator_handoff": true, "accepted_step_reused": true, "proxy_recovery": true, "account_revocation": true, "new_proxy_connections_per_backend": map[int]int{4: 4 * perNode, 10: 10 * perNode, 40: 40 * perNode}}, "", "  ")
	if err = os.WriteFile(filepath.Join(output, "shared-storage-result.json"), report, 0644); err != nil {
		t.Fatal(err)
	}
}

func nativeSharedSQL(t *testing.T, f *nativeDatabases, b *apiNode) {
	t.Helper()
	manager := databaseengine.Manager{}
	state, err := manager.Read(t.Context(), f.galera[0], "health")
	if err != nil {
		t.Fatal(err)
	}
	nodeID, password := strings.Repeat("f", 32), "shared-test-password"
	action := protocol.DatabaseAction{Connection: f.galera[0], Kind: "shared_database", ClusterID: state.ClusterID, Shared: &protocol.SharedDatabaseAction{Mode: "provision", Cluster: "test", Database: protocol.SharedDatabaseName, NodeID: nodeID, PasswordHash: databaseengine.NativePassword(password), MonitorHash: databaseengine.NativePassword(password + "monitor")}}
	apply := func(a protocol.DatabaseAction) {
		t.Helper()
		stages, e := manager.Prepare(t.Context(), a)
		if e != nil {
			t.Fatal(e)
		}
		for _, stage := range stages {
			if e = manager.Apply(t.Context(), a, stage); e != nil {
				t.Fatal(e)
			}
			observed, e := manager.Observe(t.Context(), a, stage)
			if e != nil || !observed.Satisfied {
				t.Fatal("维护步骤未读回", a.Kind, a.Shared.Mode, stage, e)
			}
		}
	}
	apply(action)
	for _, proxy := range f.proxies {
		a := action
		a.Kind, a.Connection, a.Hostgroup = "shared_proxy", proxy, 10
		apply(a)
	}
	root := nativeSQL(t, f.galera[0])
	defer root.Close()
	for _, query := range []string{"CREATE DATABASE opsdxcore", "CREATE TABLE opsdxcore.keep_value(id INT PRIMARY KEY)", "CREATE TABLE opsd_core.grant_check(id INT PRIMARY KEY,value INT NOT NULL)", "INSERT INTO opsd_core.grant_check VALUES(1,0),(2,0)"} {
		if _, err = root.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	var clusterID string
	if err = b.node.DB.QueryRow("SELECT cluster FROM node_identity WHERE id=1").Scan(&clusterID); err != nil {
		t.Fatal(err)
	}
	stores := []*sharedsql.Store{}
	for _, port := range f.proxyPorts {
		store, e := sharedsql.Open(t.Context(), "127.0.0.1:"+fmt.Sprint(port), protocol.SharedUsername(nodeID), password, protocol.SharedDatabaseName, clusterID)
		if e != nil {
			t.Fatal(e)
		}
		defer store.DB.Close()
		stores = append(stores, store)
	}
	old := time.Now().UTC().AddDate(0, 0, -9)
	for sequence, kind := range []string{"metrics", "operation"} {
		if err = stores[0].PutRecord(t.Context(), "retention-"+kind, nodeID, kind, "retention", uint64(sequence+1), old, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err = stores[0].PruneMetrics(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = stores[0].Latest(t.Context(), nodeID, "metrics", "retention"); err != sql.ErrNoRows {
		t.Fatal("过期监控仍占用共享库", err)
	}
	if _, err = stores[0].Latest(t.Context(), nodeID, "operation", "retention"); err != nil {
		t.Fatal("监控清理影响操作历史", err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i, store := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errors <- store.Transact(t.Context(), b.node.ID, false, func(tx *sharedsql.Tx) error {
				_, e := tx.ExecContext(t.Context(), "UPDATE grant_check SET value=value+1 WHERE id=?", i+1)
				return e
			})
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"SELECT * FROM opsdxcore.keep_value", "CREATE TABLE opsd_core.forbidden_ddl(id INT)"} {
		if _, err := stores[0].DB.Exec(query); err == nil {
			t.Fatal("服务账号越过库级权限", query)
		}
	}
	var sum int
	if err = root.QueryRow("SELECT SUM(value) FROM opsd_core.grant_check").Scan(&sum); err != nil || sum != 2 {
		t.Fatal("不同代理并发写入失败", sum, err)
	}
	for _, proxy := range f.proxies {
		a := action
		a.Kind, a.Connection, a.Shared = "shared_proxy", proxy, &protocol.SharedDatabaseAction{Mode: "revoke", Cluster: "test", Database: protocol.SharedDatabaseName, NodeID: nodeID}
		apply(a)
	}
	action.Shared.Mode = "revoke"
	apply(action)
}
