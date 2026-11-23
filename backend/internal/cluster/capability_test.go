package cluster

import (
	"encoding/json"
	"strings"
	"testing"
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
