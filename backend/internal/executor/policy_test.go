package executor

import (
	"encoding/json"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
)

// TestParseOperationPolicy 覆盖严格解析：缺省＝兼容模式，非法＝报错。
func TestParseOperationPolicy(t *testing.T) {
	// 缺字段：旧配置升级，按兼容模式允许，并标记为未显式声明。
	policy, explicit, err := ParseOperationPolicy(nil)
	if err != nil || explicit || !policy.ArbitraryCode {
		t.Fatalf("缺省应为兼容模式允许：%v %v %v", policy, explicit, err)
	}
	// 显式关闭。
	policy, explicit, err = ParseOperationPolicy(json.RawMessage(`{"arbitrary_code":false}`))
	if err != nil || !explicit || policy.ArbitraryCode {
		t.Fatalf("显式 false 未生效：%v %v %v", policy, explicit, err)
	}
	// 显式开启。
	policy, _, err = ParseOperationPolicy(json.RawMessage(`{"arbitrary_code":true}`))
	if err != nil || !policy.ArbitraryCode {
		t.Fatalf("显式 true 未生效：%v %v", policy, err)
	}
	// 非法配置一律报错，不能被静默当成默认值。
	for _, raw := range []string{
		`{"arbitrary-code":false}`,        // 未知字段（拼写错误）
		`{"arbitrary_code":"no"}`,         // 类型错误
		`{}`,                              // 未写明
		`{"arbitrary_code":false,"x":1}`,  // 多余字段
		`null`,                            // 显式 null 是配置错误，不是"缺省"
		`not json`,                        // 不是 JSON
	} {
		if _, _, err := ParseOperationPolicy(json.RawMessage(raw)); err == nil {
			t.Fatalf("非法配置未报错：%s", raw)
		}
	}
}

// TestPayloadCapability 覆盖按实际动作分级，以及推进已存操作时回查动作。
func TestPayloadCapability(t *testing.T) {
	shell := &protocol.OperationSubmit{Action: protocol.OperationAction{
		Kind:  "shell",
		Shell: &protocol.ShellAction{TaskID: "job", Script: "uname"},
	}}
	compose := func(action string) *protocol.OperationSubmit {
		return &protocol.OperationSubmit{Action: protocol.OperationAction{
			Kind:    "compose",
			Compose: &protocol.ComposeAction{Action: action},
		}}
	}
	container := func(action string) *protocol.OperationSubmit {
		return &protocol.OperationSubmit{Action: protocol.OperationAction{
			Kind:      "container",
			Container: &protocol.ContainerAction{Action: action},
		}}
	}
	cases := []struct {
		name    string
		payload protocol.Payload
		want    Capability
	}{
		{"shell 任务", shell, ArbitraryCode},
		{"compose start 会启动缺失容器", compose("start"), ArbitraryCode},
		{"compose update 换镜像", compose("update"), ArbitraryCode},
		{"compose recreate 换镜像", compose("recreate"), ArbitraryCode},
		{"compose pull 不换代码", compose("pull"), Regular},
		{"compose stop 只停", compose("stop"), Regular},
		{"容器 update 换镜像", container("update"), ArbitraryCode},
		{"容器 restart 不换镜像", container("restart"), Regular},
		{"只读查询", &protocol.OperationQuery{OperationID: "op"}, Regular},
		{"互信写入属常规能力", &protocol.PeerTrustTarget{}, Regular},
	}
	for _, c := range cases {
		got, err := PayloadCapability(c.payload)
		if err != nil || got != c.want {
			t.Errorf("%s：得到 %v %v，期望 %v", c.name, got, err, c.want)
		}
	}

	// 取消永远允许，即使目标操作是 shell。
	// 推进已有操作的请求不在载荷层拦截：它们内部要处理未知 ID、封存与收尾，
	// 提前拦截会破坏原有流程；真正执行前由 Engine.Run 复核。
	for _, payload := range []protocol.Payload{
		&protocol.OperationCancel{OperationID: "op"},
		&protocol.OperationConfirm{OperationID: "op"},
		&protocol.OperationRecover{OperationID: "op"},
		&protocol.OperationResolve{OperationID: "op"},
		&protocol.OperationReconcile{OperationID: "op"},
	} {
		if got, _ := PayloadCapability(payload); got != Regular {
			t.Errorf("推进已有操作不应在载荷层拦截，得到 %v", got)
		}
	}

	// 数据库部署：镜像配置在内层结构里，外层 Kind 是 database。
	deploy := &protocol.OperationSubmit{Action: protocol.OperationAction{
		Kind: "database",
		Database: &protocol.DatabaseAction{
			Kind:       "deploy_database",
			Deployment: &protocol.DatabaseDeployment{ID: "db", Kind: "galera", Image: "mariadb:11.4"},
		},
	}}
	if got, _ := PayloadCapability(deploy); got != ArbitraryCode {
		t.Errorf("数据库部署应算任意代码，得到 %v", got)
	}
}

// TestDenyActionMatchesPolicy 覆盖执行期复核：策略开启时放行，关闭时只拦任意代码。
func TestDenyActionMatchesPolicy(t *testing.T) {
	// 必须走真实解析：零值的 OperationPolicy 表示"未启用策略"，一律放行。
	open, _, err := ParseOperationPolicy(json.RawMessage(`{"arbitrary_code":true}`))
	if err != nil {
		t.Fatal(err)
	}
	closed, _, err := ParseOperationPolicy(json.RawMessage(`{"arbitrary_code":false}`))
	if err != nil {
		t.Fatal(err)
	}
	shell := protocol.OperationAction{Kind: "shell", Shell: &protocol.ShellAction{TaskID: "j", Script: "x"}}
	firewall := protocol.OperationAction{Kind: "firewall"}

	if err := open.DenyAction(shell); err != nil {
		t.Fatalf("策略开启时不应拒绝：%v", err)
	}
	if err := closed.DenyAction(shell); err == nil {
		t.Fatal("策略关闭时应拒绝 shell")
	}
	if err := closed.DenyAction(firewall); err != nil {
		t.Fatalf("策略关闭不应影响常规能力：%v", err)
	}
}