package cluster

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
	"github.com/jkfujr/opsd/internal/sharedsql"
)

// 目标侧授权按已认证来源节点生效：默认拒绝、授予放行、撤销再拒绝，
// 并且本节点自身的本地调用不受限。
func TestAuthorizeByOrigin(t *testing.T) {
	n := &Node{ID: "target"}
	const origin = "0123456789abcdef0123456789abcdef"

	// 默认权限：协议方法放行，业务写入拒绝，未登记方法拒绝。
	if !n.authorize(origin, "ping") {
		t.Error("ping 应默认允许")
	}
	if n.authorize(origin, "task.run") {
		t.Error("未授予 run_task 时不应放行")
	}
	if n.authorize(origin, "unknown.method") {
		t.Error("未登记方法必须拒绝")
	}

	// 授予后放行。
	table, err := ParseSourceCapabilities(json.RawMessage(`{"0123456789abcdef0123456789abcdef":["run_task"]}`))
	if err != nil {
		t.Fatal(err)
	}
	n.Capabilities = table
	if !n.authorize(origin, "task.run") {
		t.Error("授予后应放行")
	}

	// 撤销后立刻拒绝（真实部署里由重启生效）。
	n.Capabilities = SourceCapabilities{}
	if n.authorize(origin, "task.run") {
		t.Error("撤销后应拒绝")
	}

	// 本节点自身的调用不走来源授权。
	if !n.authorize("target", "task.run") {
		t.Error("本节点本地调用不应被来源授权拦住")
	}
}

func TestFirewallConfigOperationAuthorization(t *testing.T) {
	for _, test := range []struct {
		name, subject, method string
		capability            Capability
		wantState             protocol.OperationState
	}{
		{"可读取接纳操作", "firewall_config", "operation.get", CapabilityManageFirewall, protocol.Accepted},
		{"缺少防火墙授权", "firewall_config", "operation.get", CapabilityReadOperations, ""},
		{"可推进历史操作", "firewall_config", "operation.cancel", CapabilityManageFirewall, protocol.Rejected},
		{"未登记操作品种", "unknown_subject", "operation.get", CapabilityManageFirewall, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, _ := openTestNode(t)
			target, _ := openTestNode(t)
			joinTest(t, target, source)
			target.Capabilities[source.ID] = map[Capability]bool{test.capability: true}
			id := NewOperationID(target.ID)
			op := Operation{
				OperationRecord: protocol.OperationRecord{ID: id, State: protocol.Accepted, Revision: 1},
				NodeID:          target.ID, Subject: test.subject, Action: "adopt_current",
			}
			if _, err := target.DB.ExecContext(t.Context(),
				"INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,body) VALUES(?,?,?,'{}','',?,?)",
				id, source.ID, test.subject, []byte{}, string(payload(op))); err != nil {
				t.Fatal(err)
			}
			var result Operation
			err := source.Call(t.Context(), target.ID, test.method, id, &result)
			if test.wantState == "" {
				if faultCode(err) != "forbidden" {
					t.Fatalf("预期拒绝，实际 %v", err)
				}
				return
			}
			if err != nil || result.ID != id || result.State != test.wantState {
				t.Fatalf("预期原操作进入 %s，实际 %s，错误 %v", test.wantState, result.State, err)
			}
		})
	}
}

func TestMissingOperationAuthorization(t *testing.T) {
	source, _ := openTestNode(t)
	target, _ := openTestNode(t)
	joinTest(t, target, source)
	for _, tc := range []struct {
		method string
		body   any
		code   string
	}{
		{"operation.get", NewOperationID(target.ID), "not_found"},
		{"operation.cancel", NewOperationID(target.ID), "not_found"},
		{"operation.get", NewOperationID(source.ID), "forbidden"},
		{"operation.get", "invalid-id", "forbidden"},
	} {
		var result Operation
		err := source.Call(t.Context(), target.ID, tc.method, tc.body, &result)
		if faultCode(err) != tc.code || result.ID != "" {
			t.Fatalf("%s 应返回 %s 且无操作内容，实际 %v，内容 %+v", tc.method, tc.code, err, result)
		}
	}
}

func TestWrappedDecisionRejectsAmbiguousOperation(t *testing.T) {
	source, _ := openTestNode(t)
	target, _ := openTestNode(t)
	joinTest(t, target, source)
	target.Capabilities[source.ID] = map[Capability]bool{CapabilityManageFirewall: true}
	allowed := Operation{OperationRecord: protocol.OperationRecord{ID: NewOperationID(target.ID), State: protocol.Accepted, Revision: 1}, Subject: "firewall_config"}
	protected := Operation{OperationRecord: protocol.OperationRecord{ID: NewOperationID(target.ID), State: protocol.Accepted, Revision: 1}, Subject: "database"}
	for _, op := range []Operation{allowed, protected} {
		if _, err := target.DB.ExecContext(t.Context(), "INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,body) VALUES(?,?,?,'{}','',?,?)", op.ID, source.ID, op.Subject, []byte{}, string(payload(op))); err != nil {
			t.Fatal(err)
		}
	}
	// 仅发送有歧义的标识，省略动作和写入回执。
	var result Operation
	err := source.Call(t.Context(), target.ID, "api.operation.decision", map[string]any{"id": allowed.ID, "decision": OperationDecision{ID: protected.ID}}, &result)
	if faultCode(err) != "invalid_request" || result.ID != "" {
		t.Errorf("歧义载荷应在处理前拒绝，得到 %v", err)
	}
	current, err := target.operation(t.Context(), protected.ID)
	if err != nil || current.Revision != protected.Revision || current.State != protected.State {
		t.Fatal("受保护操作发生变化", current, err)
	}
	decision := OperationDecision{ID: allowed.ID, ExpectedRevision: allowed.Revision}
	write := requeststate.Write{Actor: "test", Key: "cancel-allowed", Digest: "normal-decision"}
	result, err = source.CallOperationDecision(t.Context(), allowed.ID, "operation.cancel", decision, &write)
	if err != nil || result.ID != allowed.ID || result.State != protocol.Rejected {
		t.Fatal("正常决定未执行", result, err)
	}
}

func TestRemoteSharedDecisionUsesCurrentCoordinator(t *testing.T) {
	source, _ := openTestNode(t)
	coordinator, _ := openTestNode(t)
	joinTest(t, coordinator, source)
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "shared.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`CREATE TABLE shared_flows(id,resource_id,coordinator,epoch,revision,terminal,body);
		CREATE TABLE shared_requests(origin,actor,request_key,digest,kind,resource_id,result,created_at);`); err != nil {
		t.Fatal(err)
	}
	for _, node := range []*Node{source, coordinator} {
		node.shared = &sharedsql.Store{DB: db, Cluster: source.cluster}
		node.sharedLocal = &SharedLocal{Phase: "active"}
	}
	op := Operation{OperationRecord: protocol.OperationRecord{ID: NewOperationID(source.ID), State: protocol.Succeeded, Revision: 2}, Subject: "database"}
	if _, err = db.Exec("INSERT INTO shared_flows VALUES(?,?,?,?,?,?,?)", op.ID, "cluster", coordinator.ID, 2, op.Revision, true, payload(sharedFlowPayload{Operation: op})); err != nil {
		t.Fatal(err)
	}
	decision := OperationDecision{ID: op.ID, ExpectedRevision: op.Revision}
	write := requeststate.Write{Actor: "test", Key: "shared-decision", Digest: "shared-operation"}
	result, err := source.CallOperationDecision(t.Context(), op.ID, "operation.cancel", decision, &write)
	if err != nil || result.ID != op.ID || result.Coordinator != coordinator.ID || result.State != op.State {
		t.Errorf("当前协调节点未处理原共享操作：%v", err)
	}
	request := operationDecisionRequest{Method: "operation.cancel", Decision: decision, Write: write}
	coordinator.Capabilities[source.ID] = map[Capability]bool{CapabilityReadOperations: true}
	if err = source.Call(t.Context(), coordinator.ID, "api.operation.decision", request, nil); faultCode(err) != "forbidden" {
		t.Fatalf("没有品种能力时应拒绝，得到 %v", err)
	}
	coordinator.Capabilities[source.ID] = map[Capability]bool{CapabilityManageDatabase: true}
	op.CoordinatorEpoch, op.Coordinator = 2, coordinator.ID
	if _, err = coordinator.DB.ExecContext(t.Context(), "INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,body,terminal) VALUES(?,?,?,'{}','',?,?,1)", op.ID, source.ID, op.Subject, []byte{}, string(payload(op))); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE shared_flows SET coordinator=?,epoch=3 WHERE id=?", source.ID, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = source.Call(t.Context(), coordinator.ID, "api.operation.decision", request, nil); faultCode(err) != "forbidden" {
		t.Fatalf("旧协调节点的本机副本应被拒绝，得到 %v", err)
	}
}

func TestReadMonitoringFiltersUnrelatedPendingOperations(t *testing.T) {
	source, _ := openTestNode(t)
	target, _ := openTestNode(t)
	joinTest(t, target, source)
	op := Operation{OperationRecord: protocol.OperationRecord{ID: NewOperationID(target.ID), State: protocol.Accepted, Revision: 1}, Subject: "database"}
	if _, err := target.DB.ExecContext(t.Context(), "INSERT INTO queue(id,origin,kind,payload,request_key,request_digest,body) VALUES(?,?,?,'{}','',?,?)", op.ID, target.ID, op.Subject, []byte{}, string(payload(op))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		capability Capability
		pending    int
	}{{CapabilityReadMonitoring, 0}, {CapabilityManageMonitoring, 1}} {
		target.Capabilities[source.ID] = map[Capability]bool{tc.capability: true}
		var got struct {
			Pending []Operation `json:"pending_operations"`
		}
		if err := source.Call(t.Context(), target.ID, "metrics", nil, &got); err != nil || len(got.Pending) != tc.pending {
			t.Fatalf("%s 应该读到 %d 项在途操作：%v，实际 %d 项", tc.capability, tc.pending, err, len(got.Pending))
		}
	}
}

// faultCode 取出错误里的故障码；非 Fault 返回空串。
func faultCode(err error) string {
	if f, ok := err.(*Fault); ok {
		return f.Code
	}
	return ""
}
