package cluster

import (
	"encoding/json"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
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

// faultCode 取出错误里的故障码；非 Fault 返回空串。
func faultCode(err error) string {
	if f, ok := err.(*Fault); ok {
		return f.Code
	}
	return ""
}
