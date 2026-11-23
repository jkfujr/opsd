# A2：本机操作策略（设计稿）

| 项目 | 内容 |
|---|---|
| 状态 | **设计稿，未实施**。评审选定机制后再动代码。 |
| 前提 | A1 已实施：执行器只放行节点服务账号（`SO_PEERCRED` 校验 UID） |
| 边界 | A2 **只做本机策略**：决定"已获准的调用方能执行哪些操作"。远端请求的授权属于 A3，远端能力开关属于 A4。 |
| 审查条目 | `docs/ARCHITECTURE-REVIEW.md` 方案 A2 |

---

## 1. 现状

执行器只有一个入口 `POST /rpc`（`executor/local.go:143`），把请求体解码成 `protocol.Payload` 后交给 `Services.Handle` 分派。`service.go` 目前分派 **27 种载荷**，彼此**没有任何权限差别**——只要通过 A1 的 UID 校验，任何载荷都能调用。

节点正常运行时确实需要全部能力（它既读状态，也提交操作、改防火墙、部署数据库）。所以 A2 不是"给节点加限制"，而是**把能力分级，让部署方可以选择把高危档关掉**；默认行为必须与现在一致，否则会破坏现网。

## 2. 载荷分级

按"读到写、写系统"的次序分三档。分档依据是 handler 是否改变本机状态，以及改动影响的范围。

### 档 1：只读（不改本机状态）

`MonitoringStateRead`、`ResourceMetricsRead`、`ComposeRead`、`ExecutionLogsRead`、`DatabaseDiscovery`、`DatabaseDeploymentCheck`、`DatabaseDeploymentStateRead`、`DatabaseDeploymentProfileRead`、`DatabaseRead`、`ProxyRoutingRead`、`DockerRead`、`ContainerInspect`、`ContainerLogsRead`、`PeerTrustStatusQuery`、`FirewallInspect`、`FirewallRead`、`FirewallPreview`、`MonitoringRead`、`OperationQuery`

三个名字带"检查"的载荷已核对为只读：

- `FirewallPreview` → `Manager.Preview(ctx, body.Change)`，只返回计划 `Plan`；
- `DatabaseDeploymentCheck` → `CheckDeployment`，只返回检查结果；
- `DatabaseNetworkCheck` → `CheckDeploymentNetwork`，只返回 `Checked` 布尔。

它们都不写本机状态，因此留在档 1。

### 档 2：受管变更（改变可回滚的本机状态）

`OperationSubmit`、`OperationCancel`、`OperationConfirm`、`OperationRecover`、`OperationResolve`、`OperationReconcile`

这些经由 `operations` 引擎执行，有持久受理、幂等键与恢复语义，影响面受操作模型约束。

### 档 3：系统高危（改变宿主或网络边界）

`PeerTrustTarget` → `s.receiveTrust(ctx, *body)`，会写入互信状态，已核对。

其余载荷经逐项核对后没有新增档 3 成员；后续新增会改变宿主状态的载荷必须在这里登记。

档 3 的判断标准：失败会切断现有连接、改变宿主网络边界，或让其他节点失去可达性。

## 3. 机制候选

### 方案 A：策略文件 + 档位开关（推荐）

执行器配置文件增加：

```json
{ "operation_policy": { "read": true, "managed": true, "host": true } }
```

- 缺省三项全 `true` → 行为与现在完全一致；
- `Handle` 分派前查表，档位关闭时返回明确的 `forbidden` 错误，不进入 handler；
- 档位值非法或未知字段 → **拒绝启动**（避免拼写错误被静默忽略）。

优点：改动小、可单机验收、失败模式清晰。缺点：不能按操作细分，只能整档开关。

### 方案 B：按载荷精确允许列表

配置里列出允许的载荷 kind。优点最细；缺点是 27 条清单要靠人工维护，新增载荷时容易忘记授权而中断生产功能（默认拒绝）。

### 方案 C：高危操作需要额外凭据

档 3 操作要求额外令牌（本机签发、一次性）。这已经接近 A3 的形状，会把"本机策略"和"跨节点授权"混在一起，超出 A2 范围。

## 4. 推荐与验收

推荐 **方案 A**：它把"能力可见、可关"这件事先做出来，粒度留到有真实需求再加。

验收（全部可在单机完成）：

1. **默认不变**：不写策略字段时，27 种载荷全部照常工作。
2. **关掉档 3**：`host=false` 时 `PeerTrustTarget` 被拒且**未进入 handler**（用副作用断言：本机互信状态不变），档 1／2 不受影响。
3. **关掉档 2**：`managed=false` 时 `OperationSubmit` 被拒，档 1 仍可用。
4. **全部关闭**：只剩档 1 可读，节点仍在运行（不因拒绝而崩溃）。
5. **配置非法**：未知识别符或非布尔值 → 执行器拒绝启动并给出明确错误。
6. **拒绝不影响后续**：一次被拒的载荷之后，同连接／新连接的合法载荷仍成功。

## 5. 未决问题

1. 档 3 是否应当**默认关闭**？默认关闭更安全，但会改变现网功能（互信目标写入会失败），所以我认为第一版必须默认开启，由部署方显式关闭；请评审确认。
2. 策略的粒度是否够用？如果实际运维需要"只允许某些操作"，方案 B 的清单维护成本要提前评估。
3. 策略文件放在 `node.json` 还是执行器自己的配置文件？A1 已把 `user_id` 放进 `node.json`（两个服务共读），A2 的策略只被执行器使用，放执行器自己的配置更清晰，但要多一个文件与安装步骤。
4. 拒绝时返回什么？统一 `forbidden` 并附档位名，还是细分错误码以便节点侧给出提示。
