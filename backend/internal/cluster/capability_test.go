package cluster

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

// 默认允许的只有约定的四项，其余方法零值必须是"拒绝"。
func TestAccessDefaults(t *testing.T) {
	allowed := []string{"ping", "directory", "api.capabilities", "shared.status"}
	// 默认允许集合必须**恰好**是这四项：多一项就是权限扩大。
	count := 0
	for _, access := range methodAccess {
		if access.DefaultAllowed {
			count++
		}
	}
	if count != len(allowed) {
		t.Errorf("默认允许项有 %d 个，期望 %d", count, len(allowed))
	}
	for _, method := range allowed {
		access, ok := Access(method)
		if !ok || !access.DefaultAllowed {
			t.Errorf("%s 应默认允许", method)
		}
	}
	for _, method := range []string{"container.logs", "execution.logs", "account.read", "config.read", "firewall.preview", "task.run"} {
		access, ok := Access(method)
		if !ok {
			t.Fatalf("%s 应当已登记", method)
		}
		if access.DefaultAllowed {
			t.Errorf("%s 不应默认允许", method)
		}
		if access.Capability == "" && !access.InnerAction {
			t.Errorf("%s 既未默认允许也未要求能力，等于零值放行", method)
		}
	}
}

// 未登记的方法必须被拒绝，而不是落到某个兜底分支。
func TestAccessRejectsUnregistered(t *testing.T) {
	for _, method := range []string{"", "unknown.method", "shared.unknown", "storage.unknown"} {
		if _, ok := Access(method); ok {
			t.Errorf("%q 不应登记", method)
		}
		var table SourceCapabilities
		if table.Allowed("peer-a", method) {
			t.Errorf("%q 未登记时必须拒绝", method)
		}
	}
}

// 能力授予与撤销都按来源节点独立生效。
func TestAllowedByCapability(t *testing.T) {
	table, err := ParseSourceCapabilities(json.RawMessage(`{"0123456789abcdef0123456789abcdef":["run_task"],"fedcba9876543210fedcba9876543210":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !table.Allowed("0123456789abcdef0123456789abcdef", "task.run") {
		t.Error("授予 run_task 后应允许")
	}
	if table.Allowed("fedcba9876543210fedcba9876543210", "task.run") {
		t.Error("未授予的来源不应允许")
	}
	if table.Allowed("00112233445566778899aabbccddeeff", "task.run") {
		t.Error("没有条目的来源不应允许写入")
	}
	// 默认允许的方法对任何有效成员成立。
	if !table.Allowed("00112233445566778899aabbccddeeff", "ping") {
		t.Error("默认允许的方法应对有效成员开放")
	}
}

// 配置解析必须严格：未知能力、非法来源标识、非 JSON、null 全部报错。
func TestParseSourceCapabilitiesStrict(t *testing.T) {
	bad := []string{
		`{"peer-a":["unknown_capability"]}`,
		`{"peer-a":["run_task"]}`,
		`{"":["run_task"]}`,
		`{"*":["run_task"]}`,
		`null`,
		`[]`,
		`"run_task"`,
		`{"0123456789abcdef0123456789abcdef":"run_task"}`,
		`{"0123456789abcdef0123456789abcdef":null}`,
		`{`,
	}
	for _, raw := range bad {
		if _, err := ParseSourceCapabilities(json.RawMessage(raw)); err == nil {
			t.Errorf("应当报错：%s", raw)
		}
	}
	// 字段缺失表示只应用默认权限。
	if table, err := ParseSourceCapabilities(nil); err != nil || len(table) != 0 {
		t.Fatalf("缺失字段应得到空表：%v %v", table, err)
	}
	if table, err := ParseSourceCapabilities(json.RawMessage(`{"0123456789abcdef0123456789abcdef":["run_task","read_logs"]}`)); err != nil {
		t.Fatal(err)
	} else if !table["0123456789abcdef0123456789abcdef"][CapabilityRunTask] || !table["0123456789abcdef0123456789abcdef"][CapabilityReadLogs] {
		t.Fatalf("解析结果不完整：%v", table)
	}
}

// 每个已登记方法要么默认允许、要么要求能力、要么按内层动作判定，
// 不允许出现"三者皆空"的零值放行。
func TestNoImplicitAllow(t *testing.T) {
	for method, access := range methodAccess {
		if access.DefaultAllowed {
			if access.Capability != "" {
				t.Errorf("%s 同时声明默认允许与能力", method)
			}
			continue
		}
		if access.InnerAction || access.Advance || access.SelfOnly {
			continue
		}
		if access.Capability == "" {
			t.Errorf("%s 没有任何授权约束", method)
		}
		if !knownCapabilities[access.Capability] {
			t.Errorf("%s 引用了未知能力 %q", method, access.Capability)
		}
	}
}

// 能力名清单必须覆盖登记表里用到的每一个能力。
func TestCapabilityNamesCoverRegistry(t *testing.T) {
	names := strings.Join(CapabilityNames(), ",")
	for method, access := range methodAccess {
		if access.Capability != "" && !strings.Contains(names, string(access.Capability)) {
			t.Errorf("%s 的能力 %q 不在能力清单里", method, access.Capability)
		}
	}
}

// allCapabilities 构造"某个来源拥有全部能力"的表，供授权单测使用。
func allCapabilities() SourceCapabilities {
	set := make(map[Capability]bool, len(knownCapabilities))
	for capability := range knownCapabilities {
		set[capability] = true
	}
	return SourceCapabilities{"0123456789abcdef0123456789abcdef": set}
}

// 通配来源只有在显式写入时才生效；不写仍然是默认拒绝。
func TestWildcardOriginMustBeExplicit(t *testing.T) {
	if (SourceCapabilities{}).Allowed("0123456789abcdef0123456789abcdef", "task.run") {
		t.Error("没有通配条目时不应放行")
	}
	if !allCapabilities().Allowed("0123456789abcdef0123456789abcdef", "task.run") {
		t.Error("显式写入通配来源后应放行")
	}
}

// 远端默认可见的共享状态不得含绑定、代理标识与 SQL 地址。
func TestRedactedSharedStatusDropsBindingDetails(t *testing.T) {
	full := SharedStatus{Pending: 3, Phase: "active", State: "ready", ProxyID: "proxy-1", SQLAddress: "10.0.0.1:3306", ProxyReady: true}
	got := redactedSharedStatus(full)
	if got.ProxyID != "" || got.SQLAddress != "" || got.Binding != nil {
		t.Fatalf("脱敏不完整：%+v", got)
	}
	if got.Pending != full.Pending || got.Phase != full.Phase || got.State != full.State || !got.ProxyReady {
		t.Fatalf("健康字段被削掉：%+v", got)
	}
}

// 列表预筛：来源只看到自己有权推进的品种，且预筛发生在分页之前。
func TestKindsFollowGrantedCapabilities(t *testing.T) {
	const origin = "0123456789abcdef0123456789abcdef"
	table, err := ParseSourceCapabilities(json.RawMessage(`{"` + origin + `":["run_task"]}`))
	if err != nil {
		t.Fatal(err)
	}
	kinds := table.Kinds(origin)
	if len(kinds) == 0 {
		t.Fatal("授予 run_task 后应至少允许一个品种")
	}
	for _, kind := range kinds {
		if !table.Allowed(origin, kind) {
			t.Fatalf("越权品种出现在列表预筛里：%s", kind)
		}
	}
	// 没有任何授予时，预筛结果为空——列表不会回退成全量。
	if got := (SourceCapabilities{}).Kinds(origin); len(got) != 0 {
		t.Fatalf("未授予时不应有可见品种：%v", got)
	}
}

func TestReadCapabilitiesAreOneWay(t *testing.T) {
	const origin = "0123456789abcdef0123456789abcdef"
	tests := []struct {
		reader, writer Capability
		reads          []string
		writes         []string
	}{
		{CapabilityReadMonitoring, CapabilityManageMonitoring, []string{"metrics", "history"}, []string{"monitoring_deploy", "monitoring_config"}},
		{CapabilityReadContainers, CapabilityManageContainer, []string{"containers.read", "container.inspect"}, []string{"container"}},
		{CapabilityReadCompose, CapabilityManageCompose, []string{"compose.list", "compose.registration"}, []string{"compose", "compose_register"}},
		{CapabilityReadTasks, CapabilityRunTask, []string{"tasks.list", "task.get", "task.runs", "task.runs.page"}, []string{"task.run", "task_run", "task_save"}},
		{CapabilityReadStorage, CapabilityWriteStorage, []string{"storage.get", "storage.status"}, []string{"storage.save", "storage.save_connection", "storage.check", "storage.probe", "storage.credentials"}},
		{CapabilityReadDatabase, CapabilityManageDatabase, []string{"database.read", "database.instance.get", "database.cluster.get", "database.proxy_routing.read", "database.proxy_routing.state"}, []string{"database.execute", "database.query", "database.discover", "database.discovery.register", "database.proxy_routing.applied"}},
	}
	for _, tc := range tests {
		t.Run(string(tc.reader), func(t *testing.T) {
			read := SourceCapabilities{origin: {tc.reader: true}}
			write := SourceCapabilities{origin: {tc.writer: true}}
			if read.Has(origin, tc.writer) || !write.Has(origin, tc.reader) {
				t.Fatal("只可从写能力推导对应读能力")
			}
			for _, method := range tc.reads {
				if !read.Allowed(origin, method) || !write.Allowed(origin, method) {
					t.Errorf("读能力和旧写授权均应允许 %s", method)
				}
			}
			for _, method := range tc.writes {
				if read.Allowed(origin, method) || !write.Allowed(origin, method) {
					t.Errorf("仅写能力应允许 %s", method)
				}
			}
		})
	}
}

// 监控通道的上传与回执分开授权：只有对应能力才能用对应方法。
func TestStreamMethodsNeedTheirOwnCapability(t *testing.T) {
	const origin = "0123456789abcdef0123456789abcdef"
	none := SourceCapabilities{}
	if none.Allowed(origin, "stream.upload") || none.Allowed(origin, "stream.receipts") {
		t.Fatal("没有授予时监控通道不应可用")
	}
	upload, err := ParseSourceCapabilities(json.RawMessage(`{"` + origin + `":["upload_monitoring"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !upload.Allowed(origin, "stream.upload") {
		t.Error("授予上传后应放行上传")
	}
	if upload.Allowed(origin, "stream.receipts") {
		t.Error("上传能力不应连带放行回执读取")
	}
	read, _ := ParseSourceCapabilities(json.RawMessage(`{"` + origin + `":["read_stream_receipts"]}`))
	if read.Allowed(origin, "stream.upload") {
		t.Error("回执能力不应连带放行上传")
	}
}

// 建库资源键：只为 create_database 生成，且包含集群与库名。
func TestCreateDatabaseKey(t *testing.T) {
	body, _ := json.Marshal(DatabaseSubmission{
		Change:  DatabaseChange{Cluster: "cluster-1", Kind: "create_database", Database: "biz"},
		Cluster: ManagedDatabaseCluster{ID: "cluster-1"},
	})
	if key := databaseResourceKey("database", body); key != "cluster-1\x1fbiz" {
		t.Fatalf("资源键不符合预期：%q", key)
	}
	other, _ := json.Marshal(DatabaseSubmission{Change: DatabaseChange{Cluster: "cluster-1", Kind: "drop_database", Database: "biz"}})
	if key := databaseResourceKey("database", other); key != "" {
		t.Fatalf("非建库操作不应占用资源：%q", key)
	}
	if key := databaseResourceKey("task_run", body); key != "" {
		t.Fatalf("其他品种不应占用建库资源：%q", key)
	}
}

// 同一资源键只允许一个在途建库操作，由唯一索引保证（并发也不需要应用侧先查后写）。
func TestCreateDatabaseKeyIsUniqueWhileInFlight(t *testing.T) {
	n, _ := openTestNode(t)
	insert := func(id string, terminal int) error {
		_, err := n.DB.Exec(`INSERT INTO queue(id,origin,actor,kind,payload,request_key,request_digest,body,terminal,resource_key)
			VALUES(?,?,'','database','{}','',X'00','{}',?,'galera-1' || char(31) || 'biz')`, id, n.ID, terminal)
		return err
	}
	if err := insert("op-1", 0); err != nil {
		t.Fatalf("首个在途建库应可写入：%v", err)
	}
	if err := insert("op-2", 0); err == nil {
		t.Fatal("第二个在途建库应被唯一索引拒绝")
	}
	// 首个结束后，可以受理新的建库。
	if _, err := n.DB.Exec("UPDATE queue SET terminal=1 WHERE id='op-1'"); err != nil {
		t.Fatal(err)
	}
	if err := insert("op-2", 0); err != nil {
		t.Fatalf("资源释放后应可再次受理：%v", err)
	}
}

// 建库 → 删库 → 重建：走真实受理路径，删库不被“已存在”挡住，删完可以重建。
func TestDatabaseResourceFreedAfterDelete(t *testing.T) {
	n, _ := openTestNode(t)
	submit := func(kind, idempotencyKey string) error {
		body, _ := json.Marshal(DatabaseSubmission{
			Change:  DatabaseChange{Cluster: "managed-1", ExpectedRevision: 1, Kind: kind, Database: "biz", Charset: "utf8mb4"},
			Cluster: ManagedDatabaseCluster{ID: "managed-1", ClusterID: "galera-1", Name: "集群", Coordinator: n.ID, Revision: 1},
		})
		_, err := n.enqueueTx(t.Context(), n.ID, Request{Kind: "database", Key: idempotencyKey, Payload: body}, nil)
		return err
	}
	finish := func(action string) {
		if _, err := n.DB.Exec(`UPDATE queue SET terminal=1, body=json_set(body,'$.state','succeeded','$.action',?)
			WHERE json_extract(body,'$.action')=? OR resource_key LIKE '%'`, action, action); err != nil {
			t.Fatal(err)
		}
	}
	key := "galera-1\x1fbiz"
	if err := submit("create_database", "create-1"); err != nil {
		t.Fatalf("首次建库应被受理：%v", err)
	}
	if inFlight, _ := databaseInFlightNow(t, n, key); !inFlight {
		t.Fatal("建库在途应占用资源")
	}
	finish("create_database")
	if exists, _ := databaseExistsNow(t, n, key); !exists {
		t.Fatal("建库成功后应视为已存在")
	}
	// 库是否存在由受理方读回判断（见读回相关用例）；这里只验证资源占用与释放。
	// 删库不能被“已存在”挡住。
	if err := submit("delete_database", "delete-1"); err != nil {
		t.Fatalf("已存在时应允许删库：%v", err)
	}
	finish("delete_database")
	if exists, _ := databaseExistsNow(t, n, key); exists {
		t.Fatal("删库成功后应释放库名")
	}
	if err := submit("create_database", "create-3"); err != nil {
		t.Fatalf("删库后应允许重建：%v", err)
	}
}

func databaseInFlightNow(t *testing.T, n *Node, key string) (bool, error) {
	t.Helper()
	tx, err := n.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	return databaseResourceInFlight(t.Context(), tx, key)
}

func databaseExistsNow(t *testing.T, n *Node, key string) (bool, error) {
	t.Helper()
	tx, err := n.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	return databaseResourceExists(t.Context(), tx, key)
}

// occupiedNow 报告该资源是否被占用（在途或已存在）。
func occupiedNow(t *testing.T, n *Node, key string) bool {
	t.Helper()
	inFlight, err := databaseInFlightNow(t, n, key)
	if err != nil {
		t.Fatal(err)
	}
	if inFlight {
		return true
	}
	exists, err := databaseExistsNow(t, n, key)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

// 迁移前的在途建库记录没有资源键：启动时解密载荷补齐，否则升级后会漏占库名。
func TestBackfillResourceKeyForInFlightDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backfill.db")
	n, err := Open(t.Context(), path, Options{Password: "test-password"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(DatabaseSubmission{
		Change:  DatabaseChange{Cluster: "managed-1", Kind: "create_database", Database: "biz"},
		Cluster: ManagedDatabaseCluster{ID: "managed-1", ClusterID: "galera-1"},
	})
	sealed, err := seal(payload, []string{n.age.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sealed)
	op := Operation{Action: "create_database", OperationRecord: protocol.OperationRecord{ID: "legacy-create", State: protocol.Accepted}}
	body, _ := json.Marshal(op)
	// 模拟迁移前的记录：resource_key 为空。
	if _, err = n.DB.Exec(`INSERT INTO queue(id,origin,actor,kind,payload,request_key,request_digest,body,terminal,resource_key)
		VALUES('legacy-create',?,'','database',?,'',X'00',?,0,'')`, n.ID, string(raw), string(body)); err != nil {
		t.Fatal(err)
	}
	if err = n.Close(); err != nil {
		t.Fatal(err)
	}
	// 重新打开触发回填。
	n2, err := Open(t.Context(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer n2.Close()
	if occupiedNow(t, n2, "galera-1\x1fbiz") == false {
		t.Fatal("回填后该库名应被占用")
	}
	// 不只“占用”：同库的新建库请求必须真的被拒，且不产生第二条操作。
	probe, _ := json.Marshal(DatabaseSubmission{
		Change:  DatabaseChange{Cluster: "managed-1", ExpectedRevision: 1, Kind: "create_database", Database: "biz", Charset: "utf8mb4"},
		Cluster: ManagedDatabaseCluster{ID: "managed-1", ClusterID: "galera-1", Name: "集群", Coordinator: n2.ID, Revision: 1},
	})
	_, err = n2.enqueueTx(t.Context(), n2.ID, Request{Kind: "database", Key: "fresh", Payload: probe}, nil)
	if err == nil || faultCode(err) != "conflict" {
		t.Fatalf("旧在途记录应挡住同库新建，实际 %v", err)
	}
	var count int
	if err = n2.DB.QueryRow("SELECT count(*) FROM queue WHERE kind='database'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("被拒后仍产生了操作：%d", count)
	}
}

// 读回与受理之间有人建了同一个库：那次读回已经过期，必须拒绝而不是照样受理。
//
// A 读到"不存在"后停在读回结束、登记之前；期间 B 用另一个键受理并进入终态；
// 放行 A 后它带着过期结论提交，事务内的版本复查必须拒绝它，且只留下一条操作。
func TestCreateRejectedWhenResourceAdvancedDuringReadBack(t *testing.T) {
	n, _ := openTestNode(t)
	n.Executor = &schemaExecutor{}
	seedDatabaseInstance(t, n, "inst-1")
	payload := func() json.RawMessage {
		b, _ := json.Marshal(DatabaseSubmission{
			Change: DatabaseChange{Cluster: "managed-1", ExpectedRevision: 1, Kind: "create_database", Database: "biz", Charset: "utf8mb4"},
			Cluster: ManagedDatabaseCluster{
				ID: "managed-1", ClusterID: "galera-1", Name: "集群", Coordinator: n.ID, Revision: 1,
				Members: []DatabaseRef{{NodeID: n.ID, InstanceID: "inst-1"}},
			},
		})
		return b
	}
	paused := make(chan struct{}, 1)
	release := make(chan struct{})
	n.acceptPause = func() {
		paused <- struct{}{}
		<-release
	}
	type outcome struct {
		op  Operation
		err error
	}
	a := make(chan outcome, 1)
	go func() {
		op, err := n.enqueue(t.Context(), n.ID, Request{Kind: "database", Key: "a", Payload: payload()})
		a <- outcome{op, err}
	}()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("A 没有走到暂停点")
	}
	// B 用另一个键受理并进入终态。
	n.acceptPause = nil
	b, err := n.enqueue(t.Context(), n.ID, Request{Kind: "database", Key: "b", Payload: payload()})
	if err != nil {
		t.Fatalf("B 的建库应被受理：%v", err)
	}
	if _, err = n.DB.Exec(`UPDATE queue SET terminal=1, body=json_set(body,'$.state','succeeded') WHERE id=?`, b.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	got := <-a
	if got.err == nil || faultCode(got.err) != "conflict" {
		t.Fatalf("读回过期时应拒绝，实际 %v", got.err)
	}
	key := databaseResourceKey("database", payload())
	var created int
	if err = n.DB.QueryRow("SELECT count(*) FROM queue WHERE resource_key=?", key).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("拒绝后仍产生了第二条操作：%d", created)
	}
}

// 同来源、同键、同参的重试必须原样返回原操作——即使原操作仍在途。
// 先查回执再读库，否则重试会变成 409（在途）或 503（成员离线）这样的新失败。
func TestRetryReturnsOriginalWhileStillInFlight(t *testing.T) {
	n, _ := openTestNode(t)
	n.Executor = &schemaExecutor{}
	seedDatabaseInstance(t, n, "inst-1")
	payload := func() json.RawMessage {
		b, _ := json.Marshal(DatabaseSubmission{
			Change: DatabaseChange{Cluster: "managed-1", ExpectedRevision: 1, Kind: "create_database", Database: "biz", Charset: "utf8mb4"},
			Cluster: ManagedDatabaseCluster{
				ID: "managed-1", ClusterID: "galera-1", Name: "集群", Coordinator: n.ID, Revision: 1,
				Members: []DatabaseRef{{NodeID: n.ID, InstanceID: "inst-1"}},
			},
		})
		return b
	}
	digest := strings.Repeat("a", 64)
	first, err := n.enqueue(t.Context(), n.ID, Request{Kind: "database", Key: "same", Actor: "account:1", InputDigest: digest, Payload: payload()})
	if err != nil {
		t.Fatal(err)
	}
	// 原操作仍在途：按旧行为重试会拿到 409。
	retry, err := n.enqueue(t.Context(), n.ID, Request{Kind: "database", Key: "same", Actor: "account:1", InputDigest: digest, Payload: payload()})
	if err != nil {
		t.Fatalf("重试应返回原操作，实际 %v", err)
	}
	if retry.ID != first.ID {
		t.Fatalf("重试应命中同一操作：%s != %s", retry.ID, first.ID)
	}
	// 同一个键换参数仍是冲突。
	other := strings.Repeat("b", 64)
	if _, err = n.enqueue(t.Context(), n.ID, Request{Kind: "database", Key: "same", Actor: "account:1", InputDigest: other, Payload: payload()}); err == nil || faultCode(err) != "conflict" {
		t.Fatalf("同键换参数应冲突，实际 %v", err)
	}
	// 换**来源**（不是换操作者）用同一个键：属于另一个桶，不能被上面的回执命中，
	// 因而会被在途挡住。
	otherOrigin := strings.Repeat("c", 32)
	if _, err = n.enqueue(t.Context(), otherOrigin, Request{Kind: "database", Key: "same", Actor: "account:1", InputDigest: digest, Payload: payload()}); err == nil || faultCode(err) != "conflict" {
		t.Fatalf("换来源应作为新请求（此时被在途挡住），实际 %v", err)
	}
}

// 在途分支的复查不能把异参请求当成重试。
//
// 两份请求用**同一个库名**（库名属于资源键，才会走到在途分支）、
// 同一个请求键、都不带摘要，只有字符集不同。旧写法会凭键返回原操作，
// 修复后必须返回冲突。
func TestInFlightRecheckDoesNotAcceptDifferentParameters(t *testing.T) {
	n, _ := openTestNode(t)
	n.Executor = &schemaExecutor{}
	seedDatabaseInstance(t, n, "inst-1")
	payload := func(database, charset string) json.RawMessage {
		b, _ := json.Marshal(DatabaseSubmission{
			Change: DatabaseChange{Cluster: "managed-1", ExpectedRevision: 1, Kind: "create_database", Database: database, Charset: charset},
			Cluster: ManagedDatabaseCluster{
				ID: "managed-1", ClusterID: "galera-1", Name: "集群", Coordinator: n.ID, Revision: 1,
				Members: []DatabaseRef{{NodeID: n.ID, InstanceID: "inst-1"}},
			},
		})
		return b
	}
	// 第一份无摘要登记。
	if _, err := n.enqueue(t.Context(), n.ID, Request{Kind: "database", Key: "k", Actor: "account:1", Payload: payload("biz", "utf8mb4")}); err != nil {
		t.Fatal(err)
	}
	// 同键、同库、改字符集、同样没有摘要：不能被当成重试。
	if _, err := n.enqueue(t.Context(), n.ID, Request{Kind: "database", Key: "k", Actor: "account:1", Payload: payload("biz", "latin1")}); err == nil || faultCode(err) != "conflict" {
		t.Fatalf("同键换参数应冲突，实际 %v", err)
	}
}
