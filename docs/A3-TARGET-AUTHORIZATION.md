# A3：目标侧按来源节点授权（第一阶段）

| 项目 | 内容 |
|---|---|
| 状态 | **设计稿（评审后修订），未实施**。 |
| 机制 | **A3-a：按来源节点的能力集合**。操作者级签名断言留作后续阶段。 |
| 前置 | A1（本机调用方准入）、A2（本机能力策略）已结项。 |
| 目标 | 远端请求到达目标节点后，由目标节点判断"**这个来源节点**能不能做这件事"。 |

## 1. 授权主体：已认证的来源节点

目标节点从**成员通道**取得来源 ID（`servePeer` 建立连接时已认证），这是唯一的授权主体。

- 请求体里的 `Actor`／操作者字段只作为**声明信息**处理：既不用于授权，也不记录成"已认证的操作者"。
- 因此第一阶段的粒度是"来源节点"，不是"某个人"。人员级判定属于后续阶段（A3-b），届时仍由目标节点判定人员与资源权限。

### 按内层动作授权，不按外层方法名

`submit` 这类方法会把**载荷里的内层动作**分派到不同处理器（`queue.go` 的 `case "submit"` → `task_run`／`storage.save`／`firewall`／`database`／`compose`／`container`／`component`／`monitoring_deploy` …）。只按外层方法名查表，会把"提交只读核对"和"提交 shell 任务"归成同一个权限位。

规则：

1. `submit` 的授权依据是 **内层动作类型 + 资源标识**；
2. `operation.cancel`／`confirm`／`recover`／`resolve`／`get`／`list` 等**继承原操作的权限**：能提交某类操作，才能推进或查询它；
3. 无法解析出内层动作的 `submit` 直接 `forbidden`。

## 2. 能力存放：目标节点的 root 配置

能力表写在**目标节点**的 `instance.json`，结构就是"来源节点 ID → 能力集合"的平铺白名单，由目标主机管理员写入：

```json
{ "source_capabilities": { "peer-abc": ["run_task", "manage_database"] } }
```

某一来源没有条目时，只应用第 3 节的默认权限——**不会**因为"没配置"而放行任何写入。

不放进共享库的原因（已核实）：共享库账号本身拥有库级 `INSERT`/`UPDATE`/`DELETE`（`databaseengine/shared_database.go`），在共享表里加能力列，等于让被约束的节点同时拥有**修改自己授权**的能力。

首版语义：

- 配置**重启生效**；
- 撤销验收写成"目标完成重启后，下一次请求被拒"；
- 配置非法（未知能力名、未知来源 ID 格式）**拒绝启动**，与 A2 的严格解析一致。

## 3. 默认值：默认只读、写入逐项授予

新旧成员一律默认最小：

| 类别 | 默认 | 说明 |
|---|---|---|
| 节点协议 | 允许 | 仅 `ping`、`directory`（签名目录同步）、`api.capabilities` |
| 共享状态 | 允许 | `shared.status`，**脱敏后**返回 |
| 其余一切读取 | 显式授予 | `container.logs`、`execution.logs`、账号与配置正文、任务正文都算 |
| 非敏感读取 | **逐项确认后允许** | 明确列举，不含凭据、不含持久化副作用 |
| `run_task` | 显式授予 | 提交任务类操作 |
| 其他业务写入 | 显式授予 | 防火墙、存储、数据库、监控部署等 |

两条容易归错类的：

- `database.query` 查的是**数据库操作结果**，权限继承原操作及其资源，而不是"默认只读"；
- `firewall.preview` **会持久化计划**，因此不在默认读清单里。

`operation.list` 必须先按权限过滤、再分页，不能先分页再过滤（否则会泄漏总数）。

升级前的准备工作：为仍需使用的远端流程**先准备授权清单**，否则升级后远端写入会立即被拒——这是刻意的，避免"升级即放权"。

现有的管理版本检查继续生效：共享管理启用时，低版本来源目前只通过 `ping` 与 `directory`。混合版本验收要同时覆盖新目标与旧目标，并写明旧目标实际的保护范围。

## 4. 方法清单与能力映射

清单从代码归纳（`queue.go` 的 `switch method` 与 `method ==` 分支、`shared_dispatch.go`、`object_storage.go`、`api_tokens.go`、以及 `archive.`／`database.` 前缀分派）。**权威版本由代码里的登记表生成**，本表只用于评审边界。

### 4.1 节点协议与非敏感读取（默认允许）

```text
ping, directory, api.capabilities
account.read, config.read, containers.read, container.inspect, container.logs
compose.list, compose.registration
tasks.list, task.get, task.runs, task.runs.page, execution.logs
operation.get, operation.list
firewall.read, firewall.config.read, firewall.plan, firewall.preview
public_addresses.read, metrics, history
database.read, database.instance.get, database.cluster.get
database.deployment.preview, database.deployment.profile, database.deployment.check,
database.deployment.connectivity
database.discovery.scan, database.discovery.inspect
database.proxy_routing.read
shared.status
```

### 4.2 需要显式授予的写入

| 能力 | 方法 |
|---|---|
| `run_task` | `submit`（内层为 `task_run`）、`task_run`、`task_save` |
| `write_storage` | `storage.save`、`storage.check`（**会持久化检查记录**）、`storage.credentials`（**会发放凭据**，另见敏感组） |
| `manage_firewall` | `firewall`、`component`、`firewall.config.write`、`firewall.config.preview_adoption` |
| `manage_database` | `database`、`database_step`、`database.deployment.execute`、`database.deployment.register`、`database.deployment.submit`、`database.discovery.register`、`database.instance.save`、`database.cluster.save`、`database.proxy_routing.state`、`database.proxy_routing.applied`、`database.reconcile`、`database.recover`、`database.resolve` |
| `manage_compose` | `compose`、`compose_register` |
| `manage_container` | `container` |
| `manage_monitoring` | `monitoring_deploy`、`monitoring_config`（写入侧） |
| `manage_public` | `public_addresses.patch`、`public_addresses.refresh` |
| `manage_identity` | `name`、`config`、`account`、`peer_trust.rules`、`config.read` 之外的配置写入 |
| `coordinate_shared` | `shared_initialize`、`shared.prepare`、`shared.export`、`shared.flush`、`shared.recover`、`shared.recover_steps` |

### 4.3 `shared.*` 逐方法分类

| 方法 | 授权 |
|---|---|
| `shared.status` | 默认允许（只读状态） |
| `directory` | 有效成员（签名目录同步） |
| `shared.bootstrap` | 有效成员为**自己**申请接入资料，并核对当前绑定关系 |
| `shared.prepare`／`export`／`flush`／`recover`／`recover_steps` | 只放行给对应受权协调方，并核对迁移／恢复流程的归属 |
| `api_token.*` | 独立权限 `manage_tokens`；`api_token.verify` 单列为"所属节点的身份核验调用" |

### 4.4 独立通道

| 通道 | 归属 |
|---|---|
| 终端（`serveTerminal`） | 独立能力 `terminal`；**高风险开关由 A4 单列** |
| 监控流（`serveArchive`／`monitoring_stream.go`） | 独立能力 `read_monitoring_stream` |
| `archive.*` 前缀 | 归入上述监控流能力 |

### 4.5 未知方法

登记表之外的方法**直接 `forbidden`**。当前 `dispatch` 末尾是 `switch` + 兜底错误，改造后必须保证不存在"落到兜底分支"的路径。

## 5. 登记表同时供分派与授权

方法登记是一份**数据结构**，同时被 `dispatch`（决定路由）与授权判定（决定权限）使用：

```go
type Method struct {
	Name        string
	Capability  Capability   // 空表示默认允许
	Route       func(...)    // 现有分派逻辑
	InnerAction bool         // true 表示按载荷内层动作再判一次
}
```

好处：新增方法若未登记，`dispatch` 与授权都会拒绝，**测试不需要手工维护第二份清单**。

## 6. 拒绝日志（结构化）

拒绝时记录：

```text
origin（已认证来源节点 ID）、target（本节点 ID）、method、resource（资源标识）、
request_id、reason
```

不记录：请求体、令牌、凭据。首批沿用现有日志渠道，不落库。

## 7. 验收条件

| 编号 | 条件 |
|---|---|
| 1 | 只有默认能力的来源调用 `task.run` 被拒，`forbidden`，且不产生半受理操作 |
| 2 | 显式授予 `run_task`（并重启目标）后同一请求成功 |
| 3 | 撤销能力并重启目标后，下一次请求被拒 |
| 4 | 未登记方法一律 `forbidden`，无兜底路径 |
| 5 | `submit` 按内层动作判定：只读核对放行、shell 任务被拒 |
| 6 | `operation.cancel`／`confirm`／`recover`／`resolve` 继承原操作权限 |
| 7 | `api_token.*` 需要 `manage_tokens`，普通成员无法自我提权 |
| 8 | `storage.check`／`storage.credentials` 不被当作只读 |
| 9 | 旧版本来源只允许 `ping`／`directory`；混合版本下新目标与旧目标行为都被覆盖 |
| 10 | 登记表中的每个方法都被授权测试覆盖，新增方法未登记即测试失败 |
| 11 | 拒绝日志含来源、目标、方法、资源、请求 ID 与原因，且不含请求体与凭据 |

## 8. 已定事项

1. 默认允许的读取清单：**只有** `ping`、`directory`、`api.capabilities` 与脱敏 `shared.status`。
2. 配置结构：`instance.json` 内"来源节点 ID → 能力集合"的平铺白名单。
3. 生效方式：**首版重启生效**；热更新若要做，必须是一条不能由远端触发的受保护路径。
4. A3-b：保留为操作者级授权阶段；A3-a 只记录声明，且标明未核验。
