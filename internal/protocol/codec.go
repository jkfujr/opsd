package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

type Message struct {
	RequestID string
	ReplyTo   string
	Payload   Payload
}

type envelope struct {
	Version   int             `json:"version"`
	Kind      Kind            `json:"kind"`
	RequestID string          `json:"request_id"`
	ReplyTo   string          `json:"reply_to,omitempty"`
	Body      json.RawMessage `json:"body"`
}

func Encode(message Message) ([]byte, error) {
	if message.RequestID == "" || message.Payload == nil {
		return nil, fmt.Errorf("消息缺少请求标识或载荷")
	}
	body, err := json.Marshal(message.Payload)
	if err != nil {
		return nil, fmt.Errorf("编码消息载荷：%w", err)
	}
	if bytes.Equal(body, []byte("null")) {
		return nil, fmt.Errorf("消息载荷不能为空")
	}
	if err := validatePayload(message.Payload); err != nil {
		return nil, err
	}
	data, err := json.Marshal(envelope{Version: Version, Kind: message.Payload.Kind(), RequestID: message.RequestID, ReplyTo: message.ReplyTo, Body: body})
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMessageBytes {
		return nil, fmt.Errorf("消息超过 8 MiB")
	}
	return data, nil
}

func Decode(data []byte) (Message, error) {
	if len(data) > MaxMessageBytes {
		return Message{}, fmt.Errorf("消息超过 8 MiB")
	}
	if !utf8.Valid(data) {
		return Message{}, fmt.Errorf("消息不是有效 UTF-8")
	}
	var wire envelope
	if err := decodeJSON(data, &wire); err != nil {
		return Message{}, fmt.Errorf("解码消息：%w", err)
	}
	if wire.Version != Version {
		return Message{}, fmt.Errorf("不支持协议版本：%d", wire.Version)
	}
	if wire.RequestID == "" || len(wire.Body) == 0 || bytes.Equal(bytes.TrimSpace(wire.Body), []byte("null")) {
		return Message{}, fmt.Errorf("消息缺少请求标识或载荷")
	}
	payload, err := newPayload(wire.Kind)
	if err != nil {
		return Message{}, err
	}
	if err := decodeJSON(wire.Body, payload); err != nil {
		return Message{}, fmt.Errorf("解码 %s 载荷：%w", wire.Kind, err)
	}
	if err := validatePayload(payload); err != nil {
		return Message{}, err
	}
	return Message{RequestID: wire.RequestID, ReplyTo: wire.ReplyTo, Payload: payload}, nil
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("消息包含多余或损坏的数据")
	}
	return nil
}

func validatePayload(payload Payload) error {
	var operationID string
	switch body := payload.(type) {
	case *ContainerInspect:
		return body.ContainerRef.Validate()
	case *ContainerLogsRead:
		if err := body.ContainerRef.Validate(); err != nil {
			return err
		}
		if body.Tail < 1 || body.Tail > 1000 {
			return fmt.Errorf("日志行数须为 1 至 1000")
		}
	case *SessionOpen:
		switch body.Purpose {
		case "business":
		case "migration_probe":
			operationID = body.MigrationOperationID
		case "address_probe":
			if body.ProbeFamily != IPv4 && body.ProbeFamily != IPv6 {
				return fmt.Errorf("地址探测缺少有效地址族")
			}
		default:
			return fmt.Errorf("未知连接目的：%s", body.Purpose)
		}
	case *SessionAccepted:
		if body.NodeID == "" || body.ConnectionID == "" {
			return fmt.Errorf("身份应答缺少节点或连接标识")
		}
	case *FirewallPreview:
		return body.Change.validate()
	case *FirewallPreviewResult:
		return body.Plan.Change.validate()
	case *OperationSubmit:
		operationID = body.OperationID
		if body.ContractVersion != Version {
			return fmt.Errorf("不支持操作契约版本：%d", body.ContractVersion)
		}
		if err := body.Action.validate(); err != nil {
			return err
		}
	case *OperationQuery:
		operationID = body.OperationID
	case *OperationQueryResult:
		operationID = body.OperationID
		if body.Found != (body.Record != nil) {
			return fmt.Errorf("操作查询结果与记录不匹配")
		}
		if body.Record != nil && body.Record.ID != body.OperationID {
			return fmt.Errorf("操作查询返回了其他操作")
		}
	case *OperationReconcile:
		operationID = body.OperationID
	case *OperationReconcileResult:
		operationID = body.Record.ID
	case *OperationConfirm:
		operationID = body.OperationID
		if body.ExpectedRevision == 0 {
			return fmt.Errorf("确认缺少阶段修订号")
		}
	case *OperationRecover:
		operationID = body.OperationID
		if body.ExpectedRevision == 0 {
			return fmt.Errorf("重新处理缺少阶段修订号")
		}
	case *OperationResolve:
		operationID = body.OperationID
		if body.ExpectedRevision == 0 || !body.AcceptCurrent {
			return fmt.Errorf("人工处理缺少阶段修订号或明确同意")
		}
	case *OperationUpdated:
		operationID = body.Record.ID
	case *DeploymentPrepare:
		operationID = body.OperationID
	case *DeploymentPrepared:
		operationID = body.OperationID
	case *DeploymentProbe:
		operationID = body.OperationID
	case *DeploymentProbeAccepted:
		operationID = body.OperationID
	case *DeploymentDecisionQuery:
		operationID = body.OperationID
	case *DeploymentDecisionUpdated:
		operationID = body.OperationID
		if body.Decision != "undecided" && body.Decision != "commit" && body.Decision != "revert" {
			return fmt.Errorf("未知部署决定")
		}
	case *AddressProbe:
		if body.Family != IPv4 && body.Family != IPv6 {
			return fmt.Errorf("无效地址族")
		}
	case *AddressEcho:
		if body.Family != IPv4 && body.Family != IPv6 {
			return fmt.Errorf("无效地址族")
		}
	default:
		return nil
	}
	// 只有上面关联操作的消息需要操作 ID，普通读取与持续策略没有该字段。
	switch payload.(type) {
	case *OperationSubmit, *OperationQuery, *OperationQueryResult, *OperationReconcile, *OperationReconcileResult, *OperationConfirm, *OperationRecover, *OperationResolve, *OperationUpdated,
		*DeploymentPrepare, *DeploymentPrepared, *DeploymentProbe, *DeploymentProbeAccepted, *DeploymentDecisionQuery, *DeploymentDecisionUpdated:
		if operationID == "" {
			return fmt.Errorf("消息缺少操作标识")
		}
	case *SessionOpen:
		if payload.(*SessionOpen).Purpose == "migration_probe" && operationID == "" {
			return fmt.Errorf("迁移试连缺少操作标识")
		}
	}
	return nil
}

func (action OperationAction) validate() error {
	count := 0
	if action.FirewallConfig != nil {
		count++
	}
	if action.Firewall != nil {
		count++
	}
	if action.Deployment != nil {
		count++
	}
	if action.Component != nil {
		count++
	}
	if action.Container != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("操作必须包含且只包含一种业务载荷")
	}
	switch action.Kind {
	case "container":
		if action.Container == nil {
			return fmt.Errorf("容器操作缺少参数")
		}
		return action.Container.Validate()
	case "firewall_config":
		if action.FirewallConfig == nil || action.FirewallConfig.Revision == 0 {
			return fmt.Errorf("防火墙配置缺少版本")
		}
		return nil
	case "firewall":
		if action.Firewall == nil {
			return fmt.Errorf("防火墙操作缺少预览")
		}
		if action.Firewall.ID == "" || action.Firewall.ContractVersion != Version {
			return fmt.Errorf("预览标识或版本无效")
		}
		return action.Firewall.Change.validate()
	case "deployment":
		if action.Deployment == nil {
			return fmt.Errorf("部署操作缺少参数")
		}
		switch action.Deployment.Kind {
		case "entrance_change":
			if action.Deployment.Config != nil {
				return nil
			}
		case "data_directory_change":
			if action.Deployment.DataDir != "" {
				return nil
			}
		case "upgrade":
			if action.Deployment.Version != "" {
				return nil
			}
		}
		return fmt.Errorf("部署动作或载荷无效")
	case "component":
		if action.Component == nil {
			return fmt.Errorf("组件操作缺少参数")
		}
		return nil
	default:
		return fmt.Errorf("未知操作动作：%s", action.Kind)
	}
}

func (change FirewallChange) validate() error {
	switch change.Kind {
	case "add_rule":
		if change.Rule != nil {
			return nil
		}
	case "edit_rule":
		if change.RuleID != "" && change.Rule != nil {
			return nil
		}
	case "delete_rule", "move_rule":
		if change.RuleID != "" {
			return nil
		}
	case "set_default_policy":
		if change.Action != "" && change.Reload != nil {
			return nil
		}
	case "set_enabled":
		if change.Enabled != nil {
			return nil
		}
	case "set_docker_port_policy":
		if change.Policy != nil {
			return nil
		}
	default:
		return fmt.Errorf("未知防火墙动作：%s", change.Kind)
	}
	return fmt.Errorf("防火墙动作缺少对应业务载荷：%s", change.Kind)
}
