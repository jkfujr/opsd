# A3：目标侧按操作授权

| 项目 | 内容 |
|---|---|
| 状态 | **设计稿，未实施**。 |
| 前置 | A1（本机调用方准入）、A2（本机能力策略）已完成。 |
| 目标 | 远端请求到达目标节点后，由**目标节点**判断"这个来源能不能做这件事"，而不是只认成员身份。 |

## 1. 现状（已核实）

远端请求只有一个入口，`internal/cluster/queue.go`：

```go
func (n *Node) dispatch(ctx context.Context, origin, method string, body json.RawMessage) (any, error) {
	if !n.Active(ctx, origin) {
		return nil, fault("forbidden", "节点未加入或已撤销")
	}
	if n.sharedEnabled() && origin != n.ID {
		m, err := n.Member(ctx, origin)
		if err != nil { return nil, err }
		if m.ManagementVersion < protocol.SharedManagementVersion && method != "ping" && method != "directory" {
			return nil, fault("unsupported", "节点版本不支持共享管理")
		}
	}
	// 之后按前缀／方法名分派到 sharedDispatch / storageDispatch / taskDispatch / …
}
```

已校验的维度：

| 维度 | 位置 | 说明 |
|---|---|---|
| 成员身份与撤销 | `Active(ctx, origin)` | 只回答"是不是有效成员" |
| 管理版本兼容 | `Member(ctx, origin)` | 只回答"版本够不够新" |

**没有**的维度：这个来源**能否执行某个具体操作**。

远端可调用的方法（按 `queue.go` 的分类归纳）：

```text
只读：  api.capabilities, directory, ping, containers.read, container.inspect,
        container.logs, compose.list, compose.registration, tasks.list,
        task.get, task.runs, task.runs.page, execution.logs,
        database.discovery.scan, database.deployment.preview, storage.get, storage.check
写入：  task.run, storage.save, api_token.*, database.deployment.submit,
        database.discovery.register, database.recover, database.resolve,
        operation.cancel, operation.recover, shared.*
```

其中写入类里既有"受管变更"（任务、存储、数据库部署），也有"能被用来扩大权限"的（`api_token.create`、`shared.*` 的成员与账号改动）。

## 2. 两种候选机制

### A3-a：按节点的能力集合（推荐先做）

每个成员在共享库里有一组能力，例如：

```text
read_only        只读方法
run_task         提交任务
manage_database  数据库发现、部署、修复
write_storage    存储写入
manage_members   成员与账号、api token
```

`dispatch` 在分派**之前**查表：`method → 所需能力`，再比对来源节点的能力集合。默认拒绝（未知方法与未列入白名单一律 `forbidden`）。

- 优点：改动集中在一个函数 + 一张表；不引入密钥；撤销即时（共享库 + 短 TTL 缓存）。
- 缺点：粒度是"节点"，不是"哪个管理员"。来源节点上的任何管理员都继承该节点的能力。

### A3-b：传递操作者身份

来源节点把"谁在操作"连同权限一起签名传给目标节点，目标节点验签后按资源判定。

- 优点：粒度到人，目标节点可以拒绝"某个管理员对某台机器的操作"。
- 缺点：需要节点密钥与请求签名（新协议面）、防重放（nonce/时间窗）、密钥轮换；与"账号只留在自己节点"的既有所有权约定冲突，需要重新界定谁能代表谁。

## 3. 实施落点（以 A3-a 为准）

| 位置 | 改动 |
|---|---|
| `cluster/queue.go` | `dispatch` 在 `Active`/版本检查之后、分派之前，查 `Capability(method)` 并要求来源节点具备 |
| 新增 `cluster/capability.go` | 方法 → 能力表；未知方法默认 `forbidden`；`read_only` 覆盖所有只读方法 |
| 共享库 | 成员记录增加能力列；默认给新成员 `read_only + run_task` |
| `shared.*` | 成员管理类方法要求 `manage_members`，避免"有节点身份就能自我提权" |

注意点：

1. **默认拒绝**：现在 `dispatch` 末尾是 `switch method` + 兜底错误，改造后必须保证"表里没有的方法"返回 `forbidden` 而不是走到别处。
2. **兼容**：旧节点不发送能力声明。升级期按 `ManagementVersion` 走兼容：旧来源只允许只读，写入需要显式升级——比"默认放行"安全，也比"直接拒绝"可运维。
3. **撤销传播**：能力变更通过共享库广播；`Active()` 已有的撤销路径可复用。
4. **与 A2 的关系**：A2 是"本机允不允许执行任意代码"，A3 是"远端请求方允不允许在这里做这件事"。两者独立，都需要，判定顺序是先 A3（来源能不能做）再 A2（本机肯不肯执行）。

## 4. 验收条件

| 编号 | 条件 |
|---|---|
| 1 | 只有 `read_only` 的来源调用 `task.run` 被拒，`forbidden`，且不产生半受理操作 |
| 2 | 授予 `run_task` 后同一请求成功 |
| 3 | 撤销能力后立即生效（下一次请求即被拒） |
| 4 | 未知方法一律 `forbidden`，不存在"落到兜底分支"的路径 |
| 5 | `api_token.create`／`shared.*` 需要 `manage_members`，普通成员无法自我提权 |
| 6 | 旧版本来源（无能力声明）只允许只读，写入返回明确错误而不是静默放行 |
| 7 | 表驱动的单元测试覆盖每个已知方法，新增方法若未登记会让测试失败 |

## 5. 未决问题（需要评审确认）

1. **选 A3-a 还是 A3-b**：先只做节点级能力，还是直接做操作者级？
2. **能力存哪里**：共享库成员表，还是每节点配置 + 共享库只存签名摘要？
3. **默认能力**：现有安装升级时给什么默认值？我倾向"只读 + 提交任务"，与当前实际用法一致。
4. **`shared.*` 的边界**：哪些算 `manage_members`，哪些是所有成员都该能做的（例如目录读取）？
5. **是否需要审计**：拒绝事件是否要像 A2 那样落库，还是只写日志？
