package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
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
	CapabilityReadMonitoring     Capability = "read_monitoring"
	CapabilityReadContainers     Capability = "read_containers"
	CapabilityReadCompose        Capability = "read_compose"
	CapabilityReadTasks          Capability = "read_tasks"
	CapabilityReadStorage        Capability = "read_storage"
	CapabilityReadDatabase       Capability = "read_database"
	CapabilityReadArchive        Capability = "read_archive"
	CapabilityUploadMonitoring   Capability = "upload_monitoring"
	CapabilityReadStreamReceipts Capability = "read_stream_receipts"
	CapabilityReadLogs           Capability = "read_logs"
	CapabilityReadConfig         Capability = "read_config"
	CapabilityReadAccount        Capability = "read_account"
	CapabilityReadOperations     Capability = "read_operations"
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
	CapabilityReadMonitoring:     true,
	CapabilityReadContainers:     true,
	CapabilityReadCompose:        true,
	CapabilityReadTasks:          true,
	CapabilityReadStorage:        true,
	CapabilityReadDatabase:       true,
	CapabilityReadArchive:        true,
	CapabilityUploadMonitoring:   true,
	CapabilityReadStreamReceipts: true,
	CapabilityReadLogs:           true,
	CapabilityReadConfig:         true,
	CapabilityReadAccount:        true,
	CapabilityReadOperations:     true,
}

// MethodAccess 描述一个远端方法需要的授权。
type MethodAccess struct {
	// Capability 为空且 DefaultAllowed 为真时表示有效成员默认可调用。
	Capability Capability
	// DefaultAllowed 必须显式声明：零值不表示"默认允许"。
	DefaultAllowed bool
	// InnerAction 表示授权依据是载荷里的内层动作品种（submit）。
	InnerAction bool
	// Advance 表示推进已有操作：权限继承原操作及其资源。
	Advance bool
	// SelfOnly 表示只允许来源节点申请**自己**的资料，由处理器核对绑定关系。
	SelfOnly bool
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
	"submit":         {Capability: CapabilityRunTask, InnerAction: true},
	"task_run":       {Capability: CapabilityRunTask},
	"task_save":      {Capability: CapabilityRunTask},
	"task.run":       {Capability: CapabilityRunTask},
	"tasks.list":     {Capability: CapabilityReadTasks},
	"task.get":       {Capability: CapabilityReadTasks},
	"task.runs":      {Capability: CapabilityReadTasks},
	"task.runs.page": {Capability: CapabilityReadTasks},
	"execution.logs": {Capability: CapabilityReadLogs},

	// —— 存储 ——
	"storage.get":         {Capability: CapabilityReadStorage},
	"storage.save":        {Capability: CapabilityWriteStorage},
	"storage.check":       {Capability: CapabilityWriteStorage},
	"storage.credentials": {Capability: CapabilityWriteStorage},
	"storage.status":      {Capability: CapabilityReadStorage},

	// —— 防火墙 / 组件 ——
	"firewall":                         {Capability: CapabilityManageFirewall},
	"firewall_config":                  {Capability: CapabilityManageFirewall},
	"firewall.plan":                    {Capability: CapabilityManageFirewall},
	"firewall.preview":                 {Capability: CapabilityManageFirewall},
	"firewall.read":                    {Capability: CapabilityManageFirewall},
	"firewall.config.read":             {Capability: CapabilityManageFirewall},
	"firewall.config.write":            {Capability: CapabilityManageFirewall},
	"firewall.config.preview_adoption": {Capability: CapabilityManageFirewall},
	"component":                        {Capability: CapabilityManageFirewall},

	// —— 数据库 ——
	"database":                         {Capability: CapabilityManageDatabase},
	"database_step":                    {Capability: CapabilityManageDatabase},
	"database.execute":                 {Capability: CapabilityManageDatabase},
	"database.read":                    {Capability: CapabilityReadDatabase},
	"database.query":                   {Capability: CapabilityManageDatabase},
	"database.exists":                  {Capability: CapabilityManageDatabase},
	"database.discover":                {Capability: CapabilityManageDatabase},
	"database.reconcile":               {Capability: CapabilityManageDatabase},
	"database.recover":                 {Capability: CapabilityManageDatabase},
	"database.resolve":                 {Capability: CapabilityManageDatabase},
	"database.instance.get":            {Capability: CapabilityReadDatabase},
	"database.instance.save":           {Capability: CapabilityManageDatabase},
	"database.cluster.get":             {Capability: CapabilityReadDatabase},
	"database.cluster.save":            {Capability: CapabilityManageDatabase},
	"database.deployment.check":        {Capability: CapabilityManageDatabase},
	"database.deployment.connectivity": {Capability: CapabilityManageDatabase},
	"database.deployment.execute":      {Capability: CapabilityManageDatabase},
	"database.deployment.preview":      {Capability: CapabilityManageDatabase},
	"database.deployment.profile":      {Capability: CapabilityManageDatabase},
	"database.deployment.register":     {Capability: CapabilityManageDatabase},
	"database.deployment.submit":       {Capability: CapabilityManageDatabase},
	"database.discovery.inspect":       {Capability: CapabilityManageDatabase},
	"database.discovery.register":      {Capability: CapabilityManageDatabase},
	"database.discovery.scan":          {Capability: CapabilityManageDatabase},
	"database.proxy_routing.applied":   {Capability: CapabilityManageDatabase},
	"database.proxy_routing.read":      {Capability: CapabilityReadDatabase},
	"database.proxy_routing.state":     {Capability: CapabilityReadDatabase},

	// —— Compose / 容器 ——
	"compose":              {Capability: CapabilityManageCompose},
	"compose_register":     {Capability: CapabilityManageCompose},
	"compose.list":         {Capability: CapabilityReadCompose},
	"compose.registration": {Capability: CapabilityReadCompose},
	"container":            {Capability: CapabilityManageContainer},
	"containers.read":      {Capability: CapabilityReadContainers},
	"container.inspect":    {Capability: CapabilityReadContainers},
	"container.logs":       {Capability: CapabilityReadLogs},

	// —— 监控数据通道（上传与回执是两种能力） ——
	"stream.upload":   {Capability: CapabilityUploadMonitoring},
	"stream.receipts": {Capability: CapabilityReadStreamReceipts},

	// —— 归档读取（逐方法登记，不再按前缀兜底） ——
	"archive.config":    {Capability: CapabilityReadArchive},
	"archive.status":    {Capability: CapabilityReadArchive},
	"archive.inventory": {Capability: CapabilityReadArchive},
	"archive.series":    {Capability: CapabilityReadArchive},

	// —— 监控 ——
	"monitoring_deploy": {Capability: CapabilityManageMonitoring},
	"monitoring_config": {Capability: CapabilityManageMonitoring},
	"metrics":           {Capability: CapabilityReadMonitoring},
	"history":           {Capability: CapabilityReadMonitoring},

	// —— 对外地址 ——
	"public_addresses.read":    {Capability: CapabilityManagePublic},
	"public_addresses.patch":   {Capability: CapabilityManagePublic},
	"public_addresses.refresh": {Capability: CapabilityManagePublic},

	// —— 身份与配置 ——
	"name":              {Capability: CapabilityManageIdentity},
	"config":            {Capability: CapabilityManageIdentity},
	"config.read":       {Capability: CapabilityReadConfig},
	"account":           {Capability: CapabilityManageIdentity},
	"account.read":      {Capability: CapabilityReadAccount},
	"peer_trust.rules":  {Capability: CapabilityManageIdentity},
	"shared_initialize": {Capability: CapabilityCoordinateShared},

	// —— 共享协调 ——
	"shared.bootstrap":     {SelfOnly: true},
	"shared.prepare":       {Capability: CapabilityCoordinateShared},
	"shared.export":        {Capability: CapabilityCoordinateShared},
	"shared.flush":         {Capability: CapabilityCoordinateShared},
	"shared.recover":       {Capability: CapabilityCoordinateShared},
	"shared.recover_steps": {Capability: CapabilityCoordinateShared},

	// —— 操作推进：继承原操作及其资源的权限，由调用方按操作判定 ——
	"operation.cancel":       {Advance: true},
	"operation.confirm":      {Advance: true},
	"operation.recover":      {Advance: true},
	"operation.resolve":      {Advance: true},
	"operation.get":          {Advance: true},
	"operation.list":         {Capability: CapabilityReadOperations},
	"api.request":            {Capability: CapabilityReadOperations},
	"api.operation.decision": {Advance: true},

	// —— API 令牌 ——
	"api_token.create": {Capability: CapabilityManageTokens},
	"api_token.update": {Capability: CapabilityManageTokens},
	"api_token.delete": {Capability: CapabilityManageTokens},
	"api_token.list":   {Capability: CapabilityManageTokens},
	"api_token.get":    {Capability: CapabilityManageTokens},
	"api_token.verify": {Capability: CapabilityVerifyTokens},
}

// Access 返回某个方法需要的授权；第二个返回值为假表示方法未登记。
func Access(method string) (MethodAccess, bool) {
	if access, ok := methodAccess[method]; ok {
		return access, true
	}
	return MethodAccess{}, false
}

// SourceCapabilities 是"来源节点 ID → 能力集合"的授权表。
type SourceCapabilities map[string]map[Capability]bool

// 写能力仅向对应的读能力推导。
var capabilityImpliedBy = map[Capability]Capability{
	CapabilityReadMonitoring: CapabilityManageMonitoring,
	CapabilityReadContainers: CapabilityManageContainer,
	CapabilityReadCompose:    CapabilityManageCompose,
	CapabilityReadTasks:      CapabilityRunTask,
	CapabilityReadStorage:    CapabilityWriteStorage,
	CapabilityReadDatabase:   CapabilityManageDatabase,
}

func (s SourceCapabilities) Has(origin string, capability Capability) bool {
	impliedBy, implied := capabilityImpliedBy[capability]
	return s[origin][capability] || implied && s[origin][impliedBy]
}

type CapabilityGrant struct {
	Origin    string   `json:"origin"`
	Explicit  []string `json:"explicit"`
	Effective []string `json:"effective"`
}

func (s SourceCapabilities) Grants() []CapabilityGrant {
	names := CapabilityNames()
	origins := make([]string, 0, len(s))
	for origin := range s {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	grants := make([]CapabilityGrant, 0, len(s))
	for _, origin := range origins {
		grant := CapabilityGrant{Origin: origin, Explicit: []string{}, Effective: []string{}}
		for _, name := range names {
			capability := Capability(name)
			if s[origin][capability] {
				grant.Explicit = append(grant.Explicit, name)
			}
			if s.Has(origin, capability) {
				grant.Effective = append(grant.Effective, name)
			}
		}
		grants = append(grants, grant)
	}
	return grants
}

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
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, fmt.Errorf("source_capabilities 格式无效：%w", err)
	}
	result := make(SourceCapabilities, len(decoded))
	for origin, names := range decoded {
		if !validOriginID(origin) {
			return nil, fmt.Errorf("来源节点标识无效：%q", origin)
		}
		if names == nil {
			return nil, fmt.Errorf("来源 %s 的能力必须是数组", origin)
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
	if access.SelfOnly {
		// 只允许来源节点申请自身资料；具体归属由处理器核对绑定后放行。
		return true
	}
	if access.InnerAction || access.Advance {
		// 具体判定在 dispatch 里按载荷进行（authorizeSubmit / authorizeAdvance）。
		return true
	}
	if access.Capability == "" {
		return false
	}
	// 只按认证来源精确匹配：没有条目就是没有授予，不存在任何通配回查。
	return s.Has(origin, access.Capability)
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
	// 成员 ID 是 32 位小写十六进制；不接受通配或任意别名。
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Kinds 返回该来源有权推进的操作品种，用于列表在分页前预筛。
// 与 operation.get 共用同一张登记表，避免两处口径不一致。
func (s SourceCapabilities) Kinds(origin string) []string {
	granted := s[origin]
	if len(granted) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for method, access := range methodAccess {
		if access.Capability == "" || !s.Has(origin, access.Capability) {
			continue
		}
		seen[method] = true
	}
	kinds := make([]string, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
