package cluster

import (
	"encoding/json"
	"testing"
)

// 目标侧授权按已认证来源节点生效：默认拒绝、授予放行、撤销再拒绝，
// 并且本节点自身的本地调用不受限。
func TestAuthorizeByOrigin(t *testing.T) {
	n := &Node{ID: "target"}
	const origin = "peer-source"

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
	table, err := ParseSourceCapabilities(json.RawMessage(`{"peer-source":["run_task"]}`))
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

// faultCode 取出错误里的故障码；非 Fault 返回空串。
func faultCode(err error) string {
	if f, ok := err.(*Fault); ok {
		return f.Code
	}
	return ""
}
