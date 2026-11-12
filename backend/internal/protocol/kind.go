package protocol

import "fmt"

func newPayload(kind Kind) (Payload, error) {
	switch kind {
	case "compose.read":
		return &ComposeRead{}, nil
	case "compose.read.result":
		return &ComposeReadResult{}, nil
	case "operation.cancel":
		return &OperationCancel{}, nil
	case "execution.logs":
		return &ExecutionLogsRead{}, nil
	case "execution.logs.result":
		return &ExecutionLogsResult{}, nil
	case "terminal.frame":
		return &TerminalFrame{}, nil
	case "database.discover":
		return &DatabaseDiscovery{}, nil
	case "database.discovered":
		return &DatabaseDiscovered{}, nil
	case "database.read":
		return &DatabaseRead{}, nil
	case "database.read.result":
		return &DatabaseReadResult{}, nil
	case "database.proxy_routing.read":
		return &ProxyRoutingRead{}, nil
	case "database.proxy_routing.result":
		return &ProxyRoutingReadResult{}, nil
	case "docker.read":
		return &DockerRead{}, nil
	case "docker.read.result":
		return &DockerReadResult{}, nil
	case "container.inspect":
		return &ContainerInspect{}, nil
	case "container.inspect.result":
		return &ContainerInspectResult{}, nil
	case "container.logs":
		return &ContainerLogsRead{}, nil
	case "container.logs.result":
		return &ContainerLogsResult{}, nil
	case "firewall.inspect":
		return &FirewallInspect{}, nil
	case "firewall.inspected":
		return &FirewallInspected{}, nil
	case "peer.hello":
		return &PeerHello{}, nil
	case "peer.call":
		return &PeerCall{}, nil
	case "peer.reply":
		return &PeerReply{}, nil
	case "monitoring.read":
		return &MonitoringRead{}, nil
	case "monitoring.result":
		return &MonitoringResult{}, nil
	case KindSessionOpen:
		return &SessionOpen{}, nil
	case KindSessionAccepted:
		return &SessionAccepted{}, nil
	case KindHeartbeat:
		return &Heartbeat{}, nil
	case KindFirewallRead:
		return &FirewallRead{}, nil
	case KindFirewallReadResult:
		return &FirewallReadResult{}, nil
	case KindFirewallPreview:
		return &FirewallPreview{}, nil
	case KindFirewallPreviewResult:
		return &FirewallPreviewResult{}, nil
	case KindOperationSubmit:
		return &OperationSubmit{}, nil
	case KindOperationQuery:
		return &OperationQuery{}, nil
	case KindOperationQueryResult:
		return &OperationQueryResult{}, nil
	case KindOperationReconcile:
		return &OperationReconcile{}, nil
	case KindOperationReconcileResult:
		return &OperationReconcileResult{}, nil
	case KindOperationConfirm:
		return &OperationConfirm{}, nil
	case KindOperationRecover:
		return &OperationRecover{}, nil
	case KindOperationResolve:
		return &OperationResolve{}, nil
	case KindOperationUpdated:
		return &OperationUpdated{}, nil
	case KindDeploymentPrepare:
		return &DeploymentPrepare{}, nil
	case KindDeploymentPrepared:
		return &DeploymentPrepared{}, nil
	case KindDeploymentProbe:
		return &DeploymentProbe{}, nil
	case KindDeploymentProbeAccepted:
		return &DeploymentProbeAccepted{}, nil
	case KindDeploymentDecisionQuery:
		return &DeploymentDecisionQuery{}, nil
	case KindDeploymentDecisionUpdated:
		return &DeploymentDecisionUpdated{}, nil
	case KindAddressProbe:
		return &AddressProbe{}, nil
	case KindAddressEcho:
		return &AddressEcho{}, nil
	case KindAddressReport:
		return &AddressReport{}, nil
	case KindPeerTrustTarget:
		return &PeerTrustTarget{}, nil
	case KindPeerTrustStatusQuery:
		return &PeerTrustStatusQuery{}, nil
	case KindPeerTrustUpdated:
		return &PeerTrustUpdated{}, nil
	case KindRequestFailed:
		return &RequestFailed{}, nil
	default:
		return nil, fmt.Errorf("未知消息类型：%s", kind)
	}
}
