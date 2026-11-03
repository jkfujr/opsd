package protocol

import "time"

// ManagedRule 的 ID 属于 OPSD；原生行号和 handle 不作为持久身份。
type ManagedRule struct {
	ID       string   `json:"id"`
	Spec     RuleSpec `json:"spec"`
	Chain    string   `json:"chain,omitempty"`
	Zone     string   `json:"zone,omitempty"`
	Priority *int     `json:"priority,omitempty"`
	// NativeKind 仅在节点和执行器之间保存，不进入 HTTP 视图。
	NativeKind string `json:"native_kind,omitempty"`
}

type ManagedPolicy struct {
	Family AddressFamily `json:"family"`
	Chain  string        `json:"chain,omitempty"`
	Zone   string        `json:"zone,omitempty"`
	Action string        `json:"action"`
}

type FirewallDefinition struct {
	Backend         Backend         `json:"backend"`
	Rules           []ManagedRule   `json:"rules"`
	DefaultPolicies []ManagedPolicy `json:"default_policies"`
}

type FirewallTarget struct {
	Revision      uint64             `json:"revision"`
	Configuration FirewallDefinition `json:"configuration"`
	Automatic     bool               `json:"automatic,omitempty"`
}

type ManagedFirewallState struct {
	FirewallDefinition
	Revision          uint64       `json:"revision"`
	ConfirmedRevision uint64       `json:"confirmed_revision"`
	State             string       `json:"state"`
	ServiceActive     *bool        `json:"service_active,omitempty"`
	CheckedAt         *time.Time   `json:"checked_at,omitempty"`
	OperationID       string       `json:"operation_id,omitempty"`
	Error             *Error       `json:"error,omitempty"`
	Capabilities      Capabilities `json:"capabilities"`
}

type FirewallConfigPatch struct {
	ExpectedRevision uint64         `json:"expected_revision"`
	Change           FirewallChange `json:"change"`
}

// FirewallInspect 只供后台使用。首次导入的配置由执行器从主机读取。
type FirewallInspect struct {
	Import bool `json:"import,omitempty"`
}

func (*FirewallInspect) Kind() Kind { return "firewall.inspect" }
func (*FirewallInspect) isPayload() {}

type FirewallInspected struct {
	Target        *FirewallTarget `json:"target,omitempty"`
	Backend       Backend         `json:"backend"`
	ServiceActive *bool           `json:"service_active,omitempty"`
	Capabilities  Capabilities    `json:"capabilities"`
	CheckedAt     time.Time       `json:"checked_at"`
	InSync        bool            `json:"in_sync"`
	Error         *Error          `json:"error,omitempty"`
}

func (*FirewallInspected) Kind() Kind { return "firewall.inspected" }
func (*FirewallInspected) isPayload() {}
