package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jkfujr/opsd/internal/protocol"
)

// OperationPolicy 是执行器的本机能力策略。
//
// 只有一项：是否允许"可执行任意代码"的动作（root 终端、Shell 任务、
// 会换镜像的 Compose 动作）。其余能力都属常规能力，不受这里控制。
type OperationPolicy struct {
	// ArbitraryCode 为 false 时拒绝新的任意代码动作，但允许已运行的动作收敛、
	// 也允许取消与清理。
	ArbitraryCode bool
	// enforced 表示策略是否真的生效。零值表示"未配置"，此时一律放行——
	// 否则任何手工构造 Server 的调用方（测试、旧路径）都会意外变成受限。
	enforced bool
}

// AllowsArbitraryCode 报告当前是否允许任意代码动作。
func (p OperationPolicy) AllowsArbitraryCode() bool { return !p.enforced || p.ArbitraryCode }

// ParseOperationPolicy 严格解析 instance.json 里的 operation_policy 子对象。
//
// 返回 explicit=false 表示配置里没有该子对象（旧配置升级），此时按兼容模式
// 允许任意代码，并由调用方记录一条日志；返回错误表示配置存在但不可用，
// 调用方必须拒绝启动——拼写错误不能被静默当成默认值。
func ParseOperationPolicy(raw json.RawMessage) (OperationPolicy, bool, error) {
	trimmed := string(bytes.TrimSpace(raw))
	// 只有"字段缺失"才走兼容模式；显式写成 null 属于配置错误。
	if len(trimmed) == 0 {
		// 旧配置升级：不启用策略（放行），由调用方记录兼容模式日志。
		return OperationPolicy{ArbitraryCode: true}, false, nil
	}
	if trimmed == "null" {
		return OperationPolicy{}, true, fmt.Errorf("operation_policy 不能为 null，需写明 arbitrary_code")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value struct {
		ArbitraryCode *bool `json:"arbitrary_code"`
	}
	if err := decoder.Decode(&value); err != nil {
		return OperationPolicy{}, true, fmt.Errorf("operation_policy 无法解析：%w", err)
	}
	if value.ArbitraryCode == nil {
		return OperationPolicy{}, true, fmt.Errorf("operation_policy 必须写明 arbitrary_code")
	}
	return OperationPolicy{ArbitraryCode: *value.ArbitraryCode, enforced: true}, true, nil
}

// Capability 是请求实际需要的能力档位。
type Capability int

const (
	// Regular 是常规能力：只读查询、受管变更、防火墙与互信写入。
	Regular Capability = iota
	// ArbitraryCode 是能执行任意代码的能力：root 终端、Shell 任务、换镜像的 Compose 动作。
	ArbitraryCode
)

func (c Capability) String() string {
	if c == ArbitraryCode {
		return "arbitrary_code"
	}
	return "regular"
}

// PayloadCapability 判定一个请求载荷需要哪一档能力。
//
// 推进已存操作的请求（确认、取消、恢复、对账等）本身不带动作，必须回查
// 已保存的操作；lookup 返回该操作的 action，无法取得时返回错误。
func PayloadCapability(payload protocol.Payload) (Capability, error) {
	// 只拦"新提交"。推进已有操作的请求（确认、取消、恢复、对账、解决）继续交给
	// 原处理器：它们内部要处理未知 ID、封存与收尾，提前拦截会破坏这些流程。
	// 真正执行 R2 步骤前由 operations.Engine.Run 复核。
	if submit, ok := payload.(*protocol.OperationSubmit); ok {
		return actionCapability(submit.Action), nil
	}
	return Regular, nil
}

// actionCapability 按动作的实际内容判定能力档位。
func actionCapability(action protocol.OperationAction) Capability {
	if protocol.ActionRequiresArbitraryCode(action) {
		return ArbitraryCode
	}
	return Regular
}

// errPolicyClosed 是本机策略拒绝的统一原因，会随操作记录持久化。
var errPolicyClosed = fmt.Errorf("本机策略已关闭可执行代码能力")

// savedAction 返回查询已保存操作动作的函数，供"推进已有操作"的请求回查。
func (s *Server) savedAction(ctx context.Context) func(string) (protocol.OperationAction, error) {
	return func(id string) (protocol.OperationAction, error) {
		record, err := s.Services.Engine.Store.Get(ctx, id)
		if err != nil {
			return protocol.OperationAction{}, err
		}
		if record.Action == nil {
			return protocol.OperationAction{}, fmt.Errorf("操作缺少动作")
		}
		return *record.Action, nil
	}
}

// writeForbidden 按接口约定返回 forbidden，而不是普通 HTTP 错误文本。
func writeForbidden(w http.ResponseWriter, cause error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(protocol.RequestFailed{Error: protocol.Error{
		Code:    "forbidden",
		Message: cause.Error(),
	}})
}

// DenyAction 让操作引擎在执行前复用同一套判定。
func (p OperationPolicy) DenyAction(action protocol.OperationAction) error {
	if p.AllowsArbitraryCode() {
		return nil
	}
	if protocol.ActionRequiresArbitraryCode(action) {
		return errPolicyClosed
	}
	return nil
}
