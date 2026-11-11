package protocol

import "time"

type Kind string

// Payload 只允许协议定义的消息，不能携带任意任务对象。
type Payload interface {
	Kind() Kind
	isPayload()
}

const KindSessionOpen Kind = "session.open"

type SessionOpen struct {
	MonitoringVersion    int           `json:"monitoring_version,omitempty"`
	Hostname             string        `json:"hostname"`
	Purpose              string        `json:"purpose"`
	EnrollmentToken      string        `json:"enrollment_token,omitempty"`
	AgentVersion         string        `json:"agent_version"`
	OS                   string        `json:"os"`
	Arch                 string        `json:"arch"`
	Capabilities         Capabilities  `json:"capabilities"`
	PendingOperationIDs  []string      `json:"pending_operation_ids"`
	MigrationOperationID string        `json:"migration_operation_id,omitempty"`
	ProbeFamily          AddressFamily `json:"probe_family,omitempty"`
}

func (*SessionOpen) Kind() Kind { return KindSessionOpen }
func (*SessionOpen) isPayload() {}

const KindSessionAccepted Kind = "session.accept"

type SessionAccepted struct {
	NodeID              string   `json:"node_id"`
	ConnectionID        string   `json:"connection_id"`
	PendingOperationIDs []string `json:"pending_operation_ids"`
	Writable            bool     `json:"writable"`
}

func (*SessionAccepted) Kind() Kind { return KindSessionAccepted }
func (*SessionAccepted) isPayload() {}

const KindHeartbeat Kind = "heartbeat"

type Heartbeat struct {
	SentAt time.Time `json:"sent_at"`
}

func (*Heartbeat) Kind() Kind { return KindHeartbeat }
func (*Heartbeat) isPayload() {}

const KindFirewallRead Kind = "firewall.read"

type FirewallRead struct {
	Family AddressFamily `json:"family,omitempty"`
	Scope  ConfigScope   `json:"scope,omitempty"`
}

func (*FirewallRead) Kind() Kind { return KindFirewallRead }
func (*FirewallRead) isPayload() {}

const KindFirewallReadResult Kind = "firewall.read.result"

type FirewallReadResult struct {
	State FirewallState `json:"state"`
}

func (*FirewallReadResult) Kind() Kind { return KindFirewallReadResult }
func (*FirewallReadResult) isPayload() {}

const KindFirewallPreview Kind = "firewall.preview"

type FirewallPreview struct {
	Change FirewallChange `json:"change"`
}

func (*FirewallPreview) Kind() Kind { return KindFirewallPreview }
func (*FirewallPreview) isPayload() {}

const KindFirewallPreviewResult Kind = "firewall.preview.result"

type FirewallPreviewResult struct {
	Plan ChangePlan `json:"plan"`
}

func (*FirewallPreviewResult) Kind() Kind { return KindFirewallPreviewResult }
func (*FirewallPreviewResult) isPayload() {}

const KindOperationSubmit Kind = "operation.submit"

type OperationSubmit struct {
	OperationID     string          `json:"operation_id"`
	ContractVersion int             `json:"contract_version"`
	Action          OperationAction `json:"action"`
}

func (*OperationSubmit) Kind() Kind { return KindOperationSubmit }
func (*OperationSubmit) isPayload() {}

const KindOperationQuery Kind = "operation.query"

type OperationQuery struct {
	OperationID string `json:"operation_id"`
}

func (*OperationQuery) Kind() Kind { return KindOperationQuery }
func (*OperationQuery) isPayload() {}

const KindOperationQueryResult Kind = "operation.query.result"

type OperationQueryResult struct {
	OperationID string           `json:"operation_id"`
	Found       bool             `json:"found"`
	Record      *OperationRecord `json:"record,omitempty"`
}

func (*OperationQueryResult) Kind() Kind { return KindOperationQueryResult }
func (*OperationQueryResult) isPayload() {}

const KindOperationReconcile Kind = "operation.reconcile"

type OperationReconcile struct {
	OperationID string `json:"operation_id"`
}

func (*OperationReconcile) Kind() Kind { return KindOperationReconcile }
func (*OperationReconcile) isPayload() {}

const KindOperationReconcileResult Kind = "operation.reconcile.result"

type OperationReconcileResult struct {
	Record OperationRecord `json:"record"`
}

func (*OperationReconcileResult) Kind() Kind { return KindOperationReconcileResult }
func (*OperationReconcileResult) isPayload() {}

const KindOperationConfirm Kind = "operation.confirm"

type OperationConfirm struct {
	OperationID      string `json:"operation_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

func (*OperationConfirm) Kind() Kind { return KindOperationConfirm }
func (*OperationConfirm) isPayload() {}

const KindOperationRecover Kind = "operation.recover"

type OperationRecover struct {
	OperationID      string `json:"operation_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

func (*OperationRecover) Kind() Kind { return KindOperationRecover }
func (*OperationRecover) isPayload() {}

const KindOperationResolve Kind = "operation.resolve"

type OperationResolve struct {
	OperationID      string `json:"operation_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	AcceptCurrent    bool   `json:"accept_current"`
}

func (*OperationResolve) Kind() Kind { return KindOperationResolve }
func (*OperationResolve) isPayload() {}

const KindOperationUpdated Kind = "operation.update"

type OperationUpdated struct {
	Record OperationRecord `json:"record"`
}

func (*OperationUpdated) Kind() Kind { return KindOperationUpdated }
func (*OperationUpdated) isPayload() {}

const KindDeploymentPrepare Kind = "deployment.prepare"

type DeploymentPrepare struct {
	OperationID string           `json:"operation_id"`
	Original    DeploymentConfig `json:"original"`
	Candidate   DeploymentConfig `json:"candidate"`
}

func (*DeploymentPrepare) Kind() Kind { return KindDeploymentPrepare }
func (*DeploymentPrepare) isPayload() {}

const KindDeploymentPrepared Kind = "deployment.prepared"

type DeploymentPrepared struct {
	OperationID string    `json:"operation_id"`
	Deadline    time.Time `json:"deadline"`
}

func (*DeploymentPrepared) Kind() Kind { return KindDeploymentPrepared }
func (*DeploymentPrepared) isPayload() {}

const KindDeploymentProbe Kind = "deployment.probe"

type DeploymentProbe struct {
	OperationID string `json:"operation_id"`
}

func (*DeploymentProbe) Kind() Kind { return KindDeploymentProbe }
func (*DeploymentProbe) isPayload() {}

const KindDeploymentProbeAccepted Kind = "deployment.probe.accept"

type DeploymentProbeAccepted struct {
	OperationID  string `json:"operation_id"`
	ConnectionID string `json:"connection_id"`
}

func (*DeploymentProbeAccepted) Kind() Kind { return KindDeploymentProbeAccepted }
func (*DeploymentProbeAccepted) isPayload() {}

const KindDeploymentDecisionQuery Kind = "deployment.decision.query"

type DeploymentDecisionQuery struct {
	OperationID string `json:"operation_id"`
}

func (*DeploymentDecisionQuery) Kind() Kind { return KindDeploymentDecisionQuery }
func (*DeploymentDecisionQuery) isPayload() {}

const KindDeploymentDecisionUpdated Kind = "deployment.decision"

type DeploymentDecisionUpdated struct {
	OperationID  string            `json:"operation_id"`
	Decision     string            `json:"decision"`
	ActiveConfig *DeploymentConfig `json:"active_config,omitempty"`
}

func (*DeploymentDecisionUpdated) Kind() Kind { return KindDeploymentDecisionUpdated }
func (*DeploymentDecisionUpdated) isPayload() {}

const KindAddressProbe Kind = "address.probe"

type AddressProbe struct {
	Family AddressFamily `json:"family"`
}

func (*AddressProbe) Kind() Kind { return KindAddressProbe }
func (*AddressProbe) isPayload() {}

const KindAddressEcho Kind = "address.echo"

type AddressEcho struct {
	Family     AddressFamily `json:"family"`
	Address    string        `json:"address"`
	ObservedAt time.Time     `json:"observed_at"`
}

func (*AddressEcho) Kind() Kind { return KindAddressEcho }
func (*AddressEcho) isPayload() {}

const KindAddressReport Kind = "address.report"

type AddressReport struct {
	IPv4 []AddressObservation `json:"ipv4"`
	IPv6 []AddressObservation `json:"ipv6"`
}

func (*AddressReport) Kind() Kind { return KindAddressReport }
func (*AddressReport) isPayload() {}

const KindPeerTrustTarget Kind = "peer_trust.target"

type PeerTrustTarget struct {
	Revision uint64   `json:"revision"`
	Enabled  bool     `json:"enabled"`
	IPv4     []string `json:"ipv4"`
	IPv6     []string `json:"ipv6"`
}

func (*PeerTrustTarget) Kind() Kind { return KindPeerTrustTarget }
func (*PeerTrustTarget) isPayload() {}

const KindPeerTrustStatusQuery Kind = "peer_trust.query"

type PeerTrustStatusQuery struct {
}

func (*PeerTrustStatusQuery) Kind() Kind { return KindPeerTrustStatusQuery }
func (*PeerTrustStatusQuery) isPayload() {}

const KindPeerTrustUpdated Kind = "peer_trust.update"

type PeerTrustUpdated struct {
	TargetRevision   uint64        `json:"target_revision"`
	ReceivedRevision uint64        `json:"received_revision"`
	AppliedRevision  uint64        `json:"applied_revision"`
	Results          []ScopeResult `json:"results"`
	CheckedAt        *time.Time    `json:"checked_at,omitempty"`
}

func (*PeerTrustUpdated) Kind() Kind { return KindPeerTrustUpdated }
func (*PeerTrustUpdated) isPayload() {}

const KindRequestFailed Kind = "request.error"

type RequestFailed struct {
	Error Error `json:"error"`
}

func (*RequestFailed) Kind() Kind { return KindRequestFailed }
func (*RequestFailed) isPayload() {}
