package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Capability 是目标节点授予某个来源节点的一组操作能力。
//
// 授权主体永远是**已认证的来源节点**，请求体里声明的操作者不参与授权。
type Capability string

const (
	CapabilityRunTask            Capability = "run_task"
	CapabilityWriteStorage       Capability = "write_storage"
	CapabilityManageFirewall     Capability = "manage_firewall"
	CapabilityManageDatabase     Capability = "manage_database"
	CapabilityManageCompose      Capability = "manage_compose"
	CapabilityManageContainer    Capability = "manage_container"
	CapabilityManageMonitoring   Capability = "manage_monitoring"
	CapabilityManagePublic       Capability = "manage_public"
	CapabilityManageIdentity     Capability = "manage_identity"
	CapabilityManageTokens       Capability = "manage_tokens"
	CapabilityVerifyTokens       Capability = "verify_tokens"
	CapabilityCoordinateShared   Capability = "coordinate_shared"
	CapabilityTerminal           Capability = "terminal"
	CapabilityReadArchive        Capability = "read_archive"
	CapabilityUploadMonitoring   Capability = "upload_monitoring"
	CapabilityReadStreamReceipts Capability = "read_stream_receipts"
	CapabilityReadLogs           Capability = "read_logs"
	CapabilityReadConfig         Capability = "read_config"
	CapabilityReadAccount        Capability = "read_account"
)

// knownCapabilities 是配置里允许出现的全部能力名。
//
// 不在表里的名字一律视为配置错误：静默忽略会让管理员以为授权已生效。
var knownCapabilities = map[Capability]bool{
	CapabilityRunTask:            true,
	CapabilityWriteStorage:       true,
	CapabilityManageFirewall:     true,
	CapabilityManageDatabase:     true,
	CapabilityManageCompose:      true,
	CapabilityManageContainer:    true,
	CapabilityManageMonitoring:   true,
	CapabilityManagePublic:       true,
	CapabilityManageIdentity:     true,
	CapabilityManageTokens:       true,
	CapabilityVerifyTokens:       true,
	CapabilityCoordinateShared:   true,
	CapabilityTerminal:           true,
	CapabilityReadArchive:        true,
	CapabilityUploadMonitoring:   true,
	CapabilityReadStreamReceipts: true,
	CapabilityReadLogs:           true,
	CapabilityReadConfig:         true,
	CapabilityReadAccount:        true,
}

// MethodAccess 描述一个远端方法需要的授权。
type MethodAccess struct {
	// Capability 为空且 DefaultAllowed 为真时表示有效成员默认可调用。
	Capability Capability
	// DefaultAllowed 必须显式声明：零值不表示"默认允许"。
	DefaultAllowed bool
	// InnerAction 表示授权依据是载荷里的内层动作，而不是方法名本身。
	InnerAction bool
}

// methodAccess 是唯一的方法登记表：分派与授权都读它。
//
// 新增方法必须在这里登记，否则 Authorize 会拒绝它。
var methodAccess = map[string]MethodAccess{
	// —— 默认允许：仅此四项 ——
	"ping":             {DefaultAllowed: true},
	"directory":        {DefaultAllowed: true},
	"api.capabilities": {DefaultAllowed: true},
	"shared.status":    {DefaultAllowed: true},

	// —— 任务 ——
	"submit":        {Capability: CapabilityRunTask, InnerAction: true},
	"task_run":      {Capability: CapabilityRunTask},
	"task_save":     {Capability: CapabilityRunTask},
	"task.run":      {Capability: CapabilityRunTask},
	"tasks.list":    {Capability: CapabilityRunTask},
	"task.get":      {Capability: CapabilityRunTask},
	"task.runs":     {Capability: CapabilityRunTask},
	"task.runs.page": {Capability: CapabilityRunTask},
	"execution.logs": {Capability: CapabilityReadLogs},

	// —— 存储 ——
	"storage.get":         {Capability: CapabilityWriteStorage},
	"storage.save":        {Capability: CapabilityWriteStorage},
	"storage.check":       {Capability: CapabilityWriteStorage},
	"storage.credentials": {Capability: CapabilityWriteStorage},
	"storage.status":      {Capability: CapabilityWriteStorage},

	// —— 防火墙 / 组件 ——
	"firewall":                      {Capability: CapabilityManageFirewall},
	"firewall.plan":                 {Capability: CapabilityManageFirewall},
	"firewall.preview":              {Capability: CapabilityManageFirewall},
	"firewall.read":                 {Capability: CapabilityManageFirewall},
	"firewall.config.read":          {Capability: CapabilityManageFirewall},
	"firewall.config.write":         {Capability: CapabilityManageFirewall},
	"firewall.config.preview_adoption": {Capability: CapabilityManageFirewall},
	"component":                     {Capability: CapabilityManageFirewall},

	// —— 数据库 ——
	"database":                       {Capability: CapabilityManageDatabase},
	"database_step":                  {Capability: CapabilityManageDatabase},
	"database.execute":               {Capability: CapabilityManageDatabase},
	"database.read":                  {Capability: CapabilityManageDatabase},
	"database.discover":              {Capability: CapabilityManageDatabase},
	"database.reconcile":             {Capability: CapabilityManageDatabase},
	"database.recover":               {Capability: CapabilityManageDatabase},
	"database.resolve":               {Capability: CapabilityManageDatabase},
	"database.instance.get":          {Capability: CapabilityManageDatabase},
	"database.instance.save":         {Capability: CapabilityManageDatabase},
	"database.cluster.get":           {Capability: CapabilityManageDatabase},
	"database.cluster.save":          {Capability: CapabilityManageDatabase},
	"database.deployment.check":       {Capability: CapabilityManageDatabase},
	"database.deployment.connectivity": {Capability: CapabilityManageDatabase},
	"database.deployment.execute":     {Capability: CapabilityManageDatabase},
	"database.deployment.preview":     {Capability: CapabilityManageDatabase},
	"database.deployment.profile":     {Capability: CapabilityManageDatabase},
	"database.deployment.register":    {Capability: CapabilityManageDatabase},
	"database.deployment.submit":      {Capability: CapabilityManageDatabase},
	"database.discovery.inspect":      {Capability: CapabilityManageDatabase},
	"database.discovery.register":     {Capability: CapabilityManageDatabase},
	"database.discovery.scan":         {Capability: CapabilityManageDatabase},
	"database.proxy_routing.applied":  {Capability: CapabilityManageDatabase},
	"database.proxy_routing.read":     {Capability: CapabilityManageDatabase},
	"database.proxy_routing.state":    {Capability: CapabilityManageDatabase},

	// —— Compose / 容器 ——
	"compose":             {Capability: CapabilityManageCompose},
	"compose_register":    {Capability: CapabilityManageCompose},
	"compose.list":        {Capability: CapabilityManageCompose},
	"compose.registration": {Capability: CapabilityManageCompose},
	"container":           {Capability: CapabilityManageContainer},
	"containers.read":     {Capability: CapabilityManageContainer},
	"container.inspect":   {Capability: CapabilityManageContainer},
	"container.logs":      {Capability: CapabilityReadLogs},

	// —— 监控 ——
	"monitoring_deploy":   {Capability: CapabilityManageMonitoring},
	"monitoring_config":   {Capability: CapabilityManageMonitoring},
	"metrics":             {Capability: CapabilityManageMonitoring},
	"history":             {Capability: CapabilityManageMonitoring},

	// —— 对外地址 ——
	"public_addresses.read":    {Capability: CapabilityManagePublic},
	"public_addresses.patch":   {Capability: CapabilityManagePublic},
	"public_addresses.refresh": {Capability: CapabilityManagePublic},

	// —— 身份与配置 ——
	"name":            {Capability: CapabilityManageIdentity},
	"config":          {Capability: CapabilityManageIdentity},
	"config.read":     {Capability: CapabilityReadConfig},
	"account":         {Capability: CapabilityManageIdentity},
	"account.read":    {Capability: CapabilityReadAccount},
	"peer_trust.rules": {Capability: CapabilityManageIdentity},
	"shared_initialize": {Capability: CapabilityCoordinateShared},

	// —— 共享协调 ——
	"shared.bootstrap":     {DefaultAllowed: true},
	"shared.prepare":       {Capability: CapabilityCoordinateShared},
	"shared.export":        {Capability: CapabilityCoordinateShared},
	"shared.flush":         {Capability: CapabilityCoordinateShared},
	"shared.recover":       {Capability: CapabilityCoordinateShared},
	"shared.recover_steps": {Capability: CapabilityCoordinateShared},

	// —— 操作推进：继承原操作及其资源的权限，由调用方按操作判定 ——
	"operation.cancel":        {InnerAction: true},
	"operation.confirm":       {InnerAction: true},
	"operation.recover":       {InnerAction: true},
	"operation.resolve":       {InnerAction: true},
	"operation.get":           {InnerAction: true},
	"operation.list":          {InnerAction: true},
	"api.request":             {InnerAction: true},
	"api.operation.decision":  {InnerAction: true},

	// —— API 令牌 ——
	"api_token.create": {Capability: CapabilityManageTokens},
	"api_token.update": {Capability: CapabilityManageTokens},
	"api_token.delete": {Capability: CapabilityManageTokens},
	"api_token.list":   {Capability: CapabilityManageTokens},
	"api_token.get":    {Capability: CapabilityManageTokens},
	"api_token.verify": {Capability: CapabilityVerifyTokens},
}

// prefixAccess 覆盖按前缀分派的方法组。
var prefixAccess = []struct {
	prefix string
	access MethodAccess
}{
	{"archive.", MethodAccess{Capability: CapabilityReadArchive}},
	{"database.", MethodAccess{Capability: CapabilityManageDatabase}},
	{"api_token.", MethodAccess{Capability: CapabilityManageTokens}},
}

// Access 返回某个方法需要的授权；第二个返回值为假表示方法未登记。
func Access(method string) (MethodAccess, bool) {
	if access, ok := methodAccess[method]; ok {
		return access, true
	}
	for _, group := range prefixAccess {
		if strings.HasPrefix(method, group.prefix) {
			return group.access, true
		}
	}
	return MethodAccess{}, false
}

// SourceCapabilities 是"来源节点 ID → 能力集合"的授权表。
type SourceCapabilities map[string]map[Capability]bool

// ParseSourceCapabilities 严格解析配置里的授权表。
//
// 字段缺失表示只应用默认权限；字段存在但内容非法时返回错误，调用方须在
// 启动成员监听之前终止——静默降级会让管理员误以为授权已生效。
func ParseSourceCapabilities(raw json.RawMessage) (SourceCapabilities, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return SourceCapabilities{}, nil
	}
	if string(trimmed) == "null" {
		return nil, fmt.Errorf("source_capabilities 不能为 null，需写明来源节点与能力")
	}
	var decoded map[string][]string
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("source_capabilities 格式无效：%w", err)
	}
	result := make(SourceCapabilities, len(decoded))
	for origin, names := range decoded {
		if !validOriginID(origin) {
			return nil, fmt.Errorf("来源节点标识无效：%q", origin)
		}
		set := make(map[Capability]bool, len(names))
		for _, name := range names {
			capability := Capability(name)
			if !knownCapabilities[capability] {
				return nil, fmt.Errorf("未知能力 %q（来源 %s）", name, origin)
			}
			set[capability] = true
		}
		result[origin] = set
	}
	return result, nil
}

// Allowed 报告某个来源节点能否执行某个方法。
//
// 未登记的方法一律拒绝；已登记但既未声明默认允许、来源也没有对应能力时拒绝。
func (s SourceCapabilities) Allowed(origin, method string) bool {
	access, known := Access(method)
	if !known {
		return false
	}
	if access.DefaultAllowed {
		return true
	}
	if access.InnerAction {
		// 操作推进类由调用方按原操作及其资源判定，这里不额外拦。
		return true
	}
	if access.Capability == "" {
		return false
	}
	return s[origin][access.Capability]
}

// CapabilityNames 返回全部已知能力名，供配置校验与文档使用。
func CapabilityNames() []string {
	names := make([]string, 0, len(knownCapabilities))
	for capability := range knownCapabilities {
		names = append(names, string(capability))
	}
	sort.Strings(names)
	return names
}

// validOriginID 校验来源节点标识的形态。
func validOriginID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
