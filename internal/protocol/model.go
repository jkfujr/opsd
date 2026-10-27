// Package protocol 定义认证通道内的消息，不负责连接、权限或业务执行。
package protocol

import "time"

const (
	Version         = 1
	MaxMessageBytes = 8 * 1024 * 1024
	MaxRecordBytes  = 60 * 1024
)

type AddressFamily string
type ConfigScope string
type Backend string
type OperationState string

const (
	IPv4      AddressFamily = "ipv4"
	IPv6      AddressFamily = "ipv6"
	Runtime   ConfigScope   = "runtime"
	Permanent ConfigScope   = "permanent"
	Both      ConfigScope   = "both"

	Accepted             OperationState = "accepted"
	Prepared             OperationState = "prepared"
	Applying             OperationState = "applying"
	AwaitingConfirmation OperationState = "awaiting_confirmation"
	Reverting            OperationState = "reverting"
	AttentionRequired    OperationState = "attention_required"
	Succeeded            OperationState = "succeeded"
	Rejected             OperationState = "rejected"
	Failed               OperationState = "failed"
	Reverted             OperationState = "reverted"
	ResolvedManually     OperationState = "resolved_manually"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Capability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

type Capabilities struct {
	Read                Capability `json:"read"`
	EditRules           Capability `json:"edit_rules"`
	Reorder             Capability `json:"reorder"`
	DefaultPolicy       Capability `json:"default_policy"`
	Enable              Capability `json:"enable"`
	Install             Capability `json:"install"`
	DockerPorts         Capability `json:"docker_ports"`
	PeerTrustHostIPv4   Capability `json:"peer_trust_host_ipv4"`
	PeerTrustHostIPv6   Capability `json:"peer_trust_host_ipv6"`
	PeerTrustDockerIPv4 Capability `json:"peer_trust_docker_ipv4"`
	PeerTrustDockerIPv6 Capability `json:"peer_trust_docker_ipv6"`
}

type NativePosition struct {
	Chain      string `json:"chain,omitempty"`
	Zone       string `json:"zone,omitempty"`
	Handle     string `json:"handle,omitempty"`
	Position   *int   `json:"position,omitempty"`
	Priority   *int   `json:"priority,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
}

type RuleSpec struct {
	Family      AddressFamily `json:"family"`
	Source      string        `json:"source"`
	Destination string        `json:"destination,omitempty"`
	Protocol    string        `json:"protocol"`
	Port        string        `json:"port,omitempty"`
	Action      string        `json:"action"`
	Comment     string        `json:"comment,omitempty"`
}

type FirewallRule struct {
	ID               string         `json:"id"`
	Spec             *RuleSpec      `json:"spec,omitempty"`
	Scope            ConfigScope    `json:"scope"`
	Native           NativePosition `json:"native"`
	NativeExpression string         `json:"native_expression"`
	Editable         bool           `json:"editable"`
	ReadonlyReason   string         `json:"readonly_reason,omitempty"`
	Origin           string         `json:"origin"`
}

type DefaultPolicy struct {
	Family   AddressFamily `json:"family"`
	Zone     string        `json:"zone,omitempty"`
	Chain    string        `json:"chain,omitempty"`
	Scope    ConfigScope   `json:"scope"`
	Action   string        `json:"action"`
	Editable bool          `json:"editable"`
}

type PortTarget struct {
	HostAddress string `json:"host_address"`
	Protocol    string `json:"protocol"`
	Port        uint16 `json:"port"`
}

type DockerPortPolicy struct {
	Target  PortTarget `json:"target"`
	Mode    string     `json:"mode"`
	Sources []string   `json:"sources"`
}

type ScopeResult struct {
	Family    AddressFamily `json:"family"`
	Component string        `json:"component"`
	Scope     ConfigScope   `json:"scope"`
	State     string        `json:"state"`
	Error     *Error        `json:"error,omitempty"`
}

type DockerPortState struct {
	Policy             DockerPortPolicy `json:"policy"`
	ContainerIDs       []string         `json:"container_ids"`
	State              string           `json:"state"`
	Coverage           []ScopeResult    `json:"coverage"`
	PeerTrustException bool             `json:"peer_trust_exception"`
}

type ReadError struct {
	Scope string `json:"scope"`
	Error Error  `json:"error"`
}

type FirewallState struct {
	CollectedAt     time.Time         `json:"collected_at"`
	Complete        bool              `json:"complete"`
	Backend         Backend           `json:"backend"`
	ServiceActive   *bool             `json:"service_active,omitempty"`
	Capabilities    Capabilities      `json:"capabilities"`
	Rules           []FirewallRule    `json:"rules"`
	DefaultPolicies []DefaultPolicy   `json:"default_policies"`
	DockerPorts     []DockerPortState `json:"docker_ports"`
	Errors          []ReadError       `json:"errors"`
}

// FirewallChange 只承载结构化业务参数；具体组合和能力在生成预览时校验。
type FirewallChange struct {
	Kind         string            `json:"kind"`
	Scope        ConfigScope       `json:"scope,omitempty"`
	Rule         *RuleSpec         `json:"rule,omitempty"`
	RuleID       string            `json:"rule_id,omitempty"`
	BeforeRuleID string            `json:"before_rule_id,omitempty"`
	Family       AddressFamily     `json:"family,omitempty"`
	Zone         string            `json:"zone,omitempty"`
	Chain        string            `json:"chain,omitempty"`
	Priority     *int              `json:"priority,omitempty"`
	Action       string            `json:"action,omitempty"`
	Reload       *bool             `json:"reload,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	Policy       *DockerPortPolicy `json:"policy,omitempty"`
}

type PlanBaseline struct {
	Rules           []FirewallRule    `json:"rules"`
	DefaultPolicies []DefaultPolicy   `json:"default_policies"`
	DockerPorts     []DockerPortState `json:"docker_ports"`
	ServiceActive   *bool             `json:"service_active,omitempty"`
}

type ChangePlan struct {
	ID                   string         `json:"id"`
	ContractVersion      int            `json:"contract_version"`
	CreatedAt            time.Time      `json:"created_at"`
	Backend              Backend        `json:"backend"`
	Change               FirewallChange `json:"change"`
	Baseline             PlanBaseline   `json:"baseline"`
	AffectedScopes       []string       `json:"affected_scopes"`
	RequiresConfirmation bool           `json:"requires_confirmation"`
	Executable           bool           `json:"executable"`
	Reasons              []string       `json:"reasons"`
}

type DeploymentConfig struct {
	ListenAddress string `json:"listen_address"`
	Port          uint16 `json:"port"`
	PublicAddress string `json:"public_address"`
	DataDir       string `json:"data_dir"`
}

type DeploymentProgress struct {
	Phase           string            `json:"phase"`
	Decision        string            `json:"decision"`
	ActiveConfig    *DeploymentConfig `json:"active_config,omitempty"`
	CandidateConfig *DeploymentConfig `json:"candidate_config,omitempty"`
	CleanupComplete bool              `json:"cleanup_complete"`
}

type OperationStep struct {
	Name       string     `json:"name"`
	State      string     `json:"state"`
	Message    string     `json:"message,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

type OperationRecord struct {
	ID                           string              `json:"id"`
	State                        OperationState      `json:"state"`
	Revision                     uint64              `json:"revision"`
	Decision                     string              `json:"decision"`
	ConfirmationDeadline         *time.Time          `json:"confirmation_deadline,omitempty"`
	Deployment                   *DeploymentProgress `json:"deployment,omitempty"`
	Steps                        []OperationStep     `json:"steps"`
	Error                        *Error              `json:"error,omitempty"`
	LastObservedAt               *time.Time          `json:"last_observed_at,omitempty"`
	ConfirmationRemainingSeconds *int64              `json:"confirmation_remaining_seconds,omitempty"`
}

// OperationAction 的三个载荷互斥，编解码层拒绝不匹配组合。
type OperationAction struct {
	Kind       string            `json:"kind"`
	Firewall   *ChangePlan       `json:"firewall,omitempty"`
	Deployment *DeploymentAction `json:"deployment,omitempty"`
	Component  *ComponentAction  `json:"component,omitempty"`
}

type DeploymentAction struct {
	Kind    string            `json:"kind"`
	Config  *DeploymentConfig `json:"config,omitempty"`
	DataDir string            `json:"data_dir,omitempty"`
	Version string            `json:"version,omitempty"`
}

type ComponentAction struct {
	Component string  `json:"component"`
	Action    string  `json:"action"`
	Backend   Backend `json:"backend,omitempty"`
	Version   string  `json:"version,omitempty"`
}

type AddressObservation struct {
	Source     string    `json:"source"`
	Address    string    `json:"address,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	Error      *Error    `json:"error,omitempty"`
}
