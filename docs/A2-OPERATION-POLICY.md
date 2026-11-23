# A2：本机操作策略（设计稿）

| 项目 | 内容 |
|---|---|
| 状态 | **首批已实施**：策略解析、三个入口判定、执行期复核。 |
| 前提 | A1 已实施并复审通过：执行器只放行节点服务账号（`SO_PEERCRED` 校验 UID） |
| 边界 | A2 **只做本机策略**：决定"已获准的调用方能执行哪类操作"。远端请求的授权属于 A3，远端能力开关属于 A4。 |
| 评审结论 | 机制选 A2-a（按实际动作判定）；首批只给 R2 加开关；R2 新装默认关闭、升级保留现状；策略放 `instance.json`。 |
| 审查条目 | `docs/ARCHITECTURE-REVIEW.md` 方案 A2 |

---

## 1. 三个入口，不是三个档位

执行器有三个 HTTP 入口（`executor/local.go`），能力差别比"载荷种类"更重要：

| 入口 | 现状 | 能力本质 |
|---|---|---|
| `GET /terminal` | 先 `websocket.Accept`，之后才进 `Terminals.Serve` | 直接拿到宿主 root shell |
| `POST /rpc` | 解码后交 `Services.Handle` 分派 27 种载荷 | 依载荷而定：读状态、受管变更，或**执行任意代码** |
| `POST /storage` | 写入本机存储状态 | 改本机状态，但不执行代码 |

因此 A2 不能按"载荷种类"分级，而要按**实际要做的动作**判定：同一个 `OperationSubmit` 既可能是"改防火墙配置"（受管变更），也可能是"跑一段 shell"（任意代码）。

## 2. 两级能力

### R1：常规能力（默认开启）

节点正常运行需要的一切，包括所有只读查询、受管变更操作、防火墙与互信写入。

`PeerTrustTarget` 属于 **R1**：它是维持集群互信的必要写入，不是任意代码执行。

### R2：可执行任意代码（首批唯一开关）

只有三类动作归 R2，判据是"能让调用方在本机执行任意命令或换上任意镜像"：

| 动作 | 依据 |
|---|---|
| `GET /terminal` | WebSocket 直接提供 root shell（`executor/local.go:131`） |
| `OperationAction.Shell` | `ShellAction.Script` 字段就是要执行的脚本（`protocol` `ShellAction`） |
| `OperationAction.Compose` 且 `Action` 为 `start`／`update`／`recreate` | `start` 走 `up --no-recreate --no-build`，仍会**创建并启动缺失容器**；`update`／`recreate` 直接换镜像。只有 `pull`／`stop` 留在 R1 |
| `OperationAction.Container` 且 `Action` 为 `update` | 拉取镜像并创建替换容器 |
| `OperationAction.Database` 且 `Kind` 为 `deploy_database` | 用请求里的 `Image` 拉取并启动容器，当前只校验镜像引用格式 |

其余动作（`business`、容器 `start/stop/restart`、数据库其余子动作、`deployment`、`firewall`、`firewall_config`、`component`、`upgrade`、`monitoring`、Compose `pull/stop`）都是受管变更，留在 R1。

判定实现放在 `protocol.ActionRequiresArbitraryCode`，`executor` 与 `operations` 共用同一份，避免两处口径漂移。

### 需要看已保存动作的请求

`OperationConfirm`、`OperationCancel`、`OperationRecover`、`OperationResolve`、`OperationReconcile` 本身不带动作，它们推进的是**已经受理的操作**。判定必须回到已保存的操作记录：该操作的 `Action` 属 R2 时，这一类请求也算 R2。

`POST /storage` 归 **R1**，但已核对它不是"只写文件"那么轻：`executor/storage.go` 会

- 写 `00-opsd.ini` 配置（`:153`）与 env 临时文件（`:164`）；
- 以 `docker run --network host` 启动存储容器（`:196`），镜像取自固定常量 `CouchImage`。

镜像是常量、调用方不能指定任意镜像，所以不构成"任意代码"；但它确实是宿主级变更（host 网络 + 容器创建），因此明确留在 R1 的受管变更档，而不是 R2。

## 3. 策略关闭后，三种状态怎么处理

这是 A2 最容易出事的地方：关掉 R2 之后，本机可能已经存在 shell 操作。规则按"越接近执行越严格、越接近收敛越宽松"：

| 状态 | 处理 | 理由 |
|---|---|---|
| **尚未开始**（已受理、未执行） | 不启动，直接标记失败并写明"本机策略已关闭可执行代码能力" | 拒绝的目的是不让代码跑起来；此时拒绝代价最小 |
| **正在运行** | **允许跑完**，不中断 | 中途杀掉会留下半完成的副作用，比让它收敛更危险 |
| **取消与清理** | **始终允许**，即使 R2 关闭 | 清理是减少风险的方向；若连取消都要 R2，关闭策略后反而无法收拾残留 |

也就是说：R2 关闭后**不能开始新的**任意代码动作，但**能收尾和清理已经存在的**。

## 4. 配置与默认值

策略放现有 root 管理的 `/etc/opsd/instance.json`（与 A1 的 `user_id` 同文件，安装器以 `root:opsd`、`0640` 写入）：

```json
{ "operation_policy": { "arbitrary_code": false } }
```

| 场景 | 默认值 | 理由 |
|---|---|---|
| **全新安装** | `false`（写入明确值） | 新部署不应默认具备任意代码能力 |
| **已有安装升级**（配置缺该字段） | `true`，并记一条"兼容模式"日志 | 升级不能悄悄关掉现网正在用的功能；由管理员显式关闭 |

**严格校验**：未知字段、非布尔值、未知子对象一律**拒绝启动**并给出明确错误——拼写错误不能被静默忽略成"默认值"。

## 5. 拒绝语义

| 入口 | 拒绝方式 |
|---|---|
| `POST /rpc` | 返回 `forbidden`，消息注明"本机策略已关闭可执行代码能力"，**不进入 handler** |
| `GET /terminal` | **在 `websocket.Accept` 之前**返回 `403` | 一旦升级成功，客户端已拿到可交互通道，事后拒绝会留下半开的会话 |
| `POST /storage` | 无策略 |

## 6. 验收

1. **新装默认**：新安装的 `instance.json` 含 `"arbitrary_code": false`；`GET /terminal` 得到 `403`（且**没有** WebSocket 升级），`ShellAction` 的 `OperationSubmit` 被拒并标记失败，`ComposeAction(action=update)` 被拒。
2. **R1 不受影响**：同一配置下只读查询、`PeerTrustTarget`、`ComposeAction(action=start)`、防火墙写入全部照常工作。
3. **升级兼容**：配置缺 `operation_policy` 时按 `true` 运行（shell 可用），日志出现兼容模式提示。
4. **已运行的操作跑完**：在 R2 开启时启动一个 shell 操作，运行中把策略改为关闭并重启执行器，该操作仍能跑完，不会因策略变化被中断。
5. **取消始终可用**：R2 关闭时，取消一个已受理的 shell 操作仍然成功。
6. **未开始的不启动**：R2 关闭后重新受理的 shell 操作直接失败，且本机没有产生执行痕迹。
7. **配置非法拒绝启动**：`{"operation_policy": {"arbitrary-code": false}}`（拼写错误）与 `{"operation_policy": {"arbitrary_code": "no"}}`（类型错误）都必须让执行器启动失败。
8. **拒绝不影响后续**：一次被拒的请求之后，R1 请求仍成功。

## 7. 实施落点（已完成）

| 位置 | 作用 |
|---|---|
| `protocol/policy.go` | `ActionRequiresArbitraryCode`，按实际执行效果分级 |
| `executor/policy.go` | 严格解析 `operation_policy`；`AllowsArbitraryCode`；`PayloadCapability`（含回查已存动作） |
| `executor/local.go` | `/terminal` 在升级前 403；`/rpc` 在分派前拒绝 |
| `operations/engine.go` | `Engine.Run` 在执行前复核策略，拒绝原因经 `e.end(..., Failed, ...)` 持久化 |
| `installation/node.go` | `NodeFile.OperationPolicy`（原始 JSON）；新装写入 `{"arbitrary_code":false}` |
| `cli/node.go` | 启动时解析策略、缺省记兼容模式日志、装配 `Server.Policy` 与 `Engine.SetDenyAction` |

**零值语义**：`OperationPolicy{}` 表示"未启用策略"，一律放行——否则任何手工构造 `Server` 的调用方（测试、其他路径）都会意外变成受限。只有经 `ParseOperationPolicy` 解析后才置 `enforced`。

## 8. 未决问题

1. **R2 的初始值是不是太快？** 新装默认关闭意味着新部署的 shell 任务与 root 终端默认不可用，这是功能层面的变化。我按评审结论执行，但需要确认文档与发布说明都写到这一条。
2. ~~`POST /storage` 的档位~~：评审确认为 **R1**——镜像取自固定常量 `CouchImage`，调用方不能指定镜像；host 网络与宿主目录挂载作为 R1 的受管副作用记录。
3. **策略变更是否需要重启**：本设计按"启动时读取"处理，改配置需重启执行器；热更新会引入"同一进程内能力在运行中变化"的语义，暂不做。
4. **拒绝是否要审计**：目前只记日志。若需要"谁在何时试图执行被禁能力"，那属于审计流（安全边界文档第 2 节第 6 条）。
