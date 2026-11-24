package cluster

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

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
		access, ok := Access(kind)
		if !ok || access.Capability != CapabilityRunTask {
			t.Fatalf("越权品种出现在列表预筛里：%s", kind)
		}
	}
	// 没有任何授予时，预筛结果为空——列表不会回退成全量。
	if got := (SourceCapabilities{}).Kinds(origin); len(got) != 0 {
		t.Fatalf("未授予时不应有可见品种：%v", got)
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
	// 已存在时必须拒绝再次建库。
	if err := submit("create_database", "create-2"); err == nil || faultCode(err) != "conflict" {
		t.Fatalf("库已存在时应拒绝建库，实际 %v", err)
	}
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
func TestCreateRejectedWhenResourceAdvancedDuringReadBack(t *testing.T) {
	n, _ := openTestNode(t)
	body := func() json.RawMessage {
		b, _ := json.Marshal(DatabaseSubmission{
			Change:  DatabaseChange{Cluster: "managed-1", ExpectedRevision: 1, Kind: "create_database", Database: "biz", Charset: "utf8mb4"},
			Cluster: ManagedDatabaseCluster{ID: "managed-1", ClusterID: "galera-1", Name: "集群", Coordinator: n.ID, Revision: 1},
		})
		return b
	}
	// A 读到“不存在”时的序号。
	var observed int64
	key := databaseResourceKey("database", body())
	if err := n.DB.QueryRow("SELECT COALESCE(MAX(seq),0) FROM queue WHERE resource_key=?", key).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	// B 在 A 提交之前建库并结束。
	b := body()
	op, err := n.enqueueTx(t.Context(), n.ID, Request{Kind: "database", Key: "b", Payload: b}, nil)
	if err != nil {
		t.Fatalf("B 的建库应被受理：%v", err)
	}
	if _, err = n.DB.Exec(`UPDATE queue SET terminal=1, body=json_set(body,'$.state','succeeded') WHERE id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	// 放行 A：它带着过期的读回结论提交。
	if _, err = n.enqueueTx(t.Context(), n.ID, Request{Kind: "database", Key: "a", Payload: body(), ResourceObservedSeq: &observed}, nil); err == nil || faultCode(err) != "conflict" {
		t.Fatalf("读回过期时应拒绝，实际 %v", err)
	}
	var created int
	if err = n.DB.QueryRow("SELECT count(*) FROM queue WHERE resource_key=?", key).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("拒绝后仍产生了第二条操作：%d", created)
	}
}
