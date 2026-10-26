package protocol

import "fmt"

func newPayload(kind Kind) (Payload, error) {
	switch kind {
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
