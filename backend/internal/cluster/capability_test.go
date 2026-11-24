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

// 建库 → 删除 → 重建：删除成功后库名释放，可以再次受理。
func TestDatabaseResourceFreedAfterDelete(t *testing.T) {
	n, _ := openTestNode(t)
	body := func(kind string) json.RawMessage {
		b, _ := json.Marshal(DatabaseSubmission{
			Change:  DatabaseChange{Cluster: "cluster-1", Kind: kind, Database: "biz"},
			Cluster: ManagedDatabaseCluster{ID: "managed-1", ClusterID: "galera-1"},
		})
		return b
	}
	insert := func(id, action string, terminal int, state string) error {
		op := Operation{Action: action, OperationRecord: protocol.OperationRecord{ID: id, State: protocol.OperationState(state)}}
		raw, _ := json.Marshal(op)
		key := databaseResourceKey("database", body(action))
		_, err := n.DB.Exec(`INSERT INTO queue(id,origin,actor,kind,payload,request_key,request_digest,body,terminal,resource_key)
			VALUES(?,?,'','database','{}','',X'00',?,?,?)`, id, n.ID, string(raw), terminal, key)
		return err
	}
	// 建库成功后占用同名库。
	if err := insert("create-1", "create_database", 1, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if !occupiedNow(t, n, databaseResourceKey("database", body("create_database"))) {
		t.Fatal("建库成功后应占用")
	}
	// 删除成功后释放。
	if err := insert("delete-1", "delete_database", 1, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if occupiedNow(t, n, databaseResourceKey("database", body("create_database"))) {
		t.Fatal("删除成功后应释放")
	}
}

// occupiedNow 在一个只读事务里查询资源占用。
func occupiedNow(t *testing.T, n *Node, key string) bool {
	t.Helper()
	tx, err := n.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	occupied, err := databaseResourceOccupied(t.Context(), tx, key)
	if err != nil {
		t.Fatal(err)
	}
	return occupied
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
}
