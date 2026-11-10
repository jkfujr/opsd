# API 接入

在“设置 → API”开启 API，选择用途模板与资源范围后生成令牌。完整值仅在生成时展示。API 和网页分别启停，共用 Web 地址；本机可以执行：

```bash
sudo /opt/opsd/bin/opsd api --enabled=true
```

## JSON

从仓库根目录执行，将地址和令牌替换为生成结果：

```bash
export OPSD_API_URL='http://127.0.0.1:65533/api/v1'
export OPSD_API_TOKEN='生成的令牌'
curl -fsS "$OPSD_API_URL/api-tokens/verify" -H "Authorization: Bearer $OPSD_API_TOKEN"
curl -fsS "$OPSD_API_URL/monitoring/current" -H "Authorization: Bearer $OPSD_API_TOKEN"
curl -fsS "$OPSD_API_URL/monitoring/history?range=7d&group_by=node" -H "Authorization: Bearer $OPSD_API_TOKEN"
```

`monitoring/current` 提供节点 ID、名称、地域、可达状态、检查时间及读数；汇总只计算有效读数。指标缺值保留为空，离线节点保留最后采集时间。历史增加 `node_ids`（逗号分隔）、`from`／`to`（RFC3339）、`group_by=node`，范围为最近七天；原有 `node_id` 与 `range=24h|7d` 保持可用。CouchDB 故障时历史返回 503。

Grafana 的 Infinity 数据源可配置固定请求头 `Authorization: Bearer <令牌>`，查询当前指标或历史 JSON。其他客户端直接使用相同接口。

## Prometheus 与 Grafana

生成结果提供完整配置片段。下面示例按 15 秒采集当前指标，地址填写 OPSD 的 Web 监听入口：

```yaml
scrape_configs:
  - job_name: opsd
    scrape_interval: 15s
    scheme: http
    metrics_path: /metrics
    static_configs:
      - targets: ['127.0.0.1:65533']
    authorization:
      type: Bearer
      credentials: '生成的令牌'
```

Grafana 添加 Prometheus 数据源后，可查询 `opsd_node_up` 及其他 `opsd_` 指标。Prometheus 保存连续趋势；OPSD 的资源历史继续每五分钟采集、保留七天。监控、容器及数据库指标分别遵循对应模块的读取权限。请求 Accept 为 `application/openmetrics-text` 时返回 OpenMetrics，其余返回 Prometheus 文本格式。

## 权限与故障

| 模块 | 范围 | 管理 |
|---|---|---|
| 监控 | 全部／节点／地域 | 读取 |
| 节点基础信息 | 全部／节点／地域 | 修改名称 |
| 容器 | 全部／节点／地域 | 启动、停止、重启、镜像更新 |
| 防火墙 | 全部／节点／地域 | 既有规则操作、确认及恢复 |
| 数据库 | 全部／指定集群 | 业务库、账号和代理同步 |

管理包含读取。地域范围随节点归属变化，指定节点使用稳定 ID。操作查询与后续处理继承原操作的模块及资源权限。管理员账号、密码明文、节点加入撤销、全局互信、连接登记和令牌管理使用管理员控制台。

| 状态 | 处理 |
|---|---|
| 401 | 核对令牌是否无效、过期、停用、撤销或已重新生成 |
| 403 | 编辑令牌模块权限或资源范围 |
| 409 | 查询原操作及目标版本；保持原请求键和参数 |
| 422 | 按错误修正字段 |
| 503 | 检查 API 开关、令牌所属节点或相关依赖 |

每个外部请求向令牌所属节点核验一次。所属节点不可达时可恢复该节点，或在健康节点创建替代令牌。完整令牌在存储中只保留摘要；任意启用 API 的成员都可接收令牌并内部路由核验。

写请求使用 `Idempotency-Key`。相同令牌、目标和请求键跨入口返回原操作，参数变化返回冲突。应答未知时查询 `/operations?node_id=<目标>&idempotency_key=<原请求键>`，随后读取 `/operations/<原操作 ID>`；已受理操作在入口退出或令牌撤销后继续，可通过原操作取消待执行项。

数据库接口位于 `/databases/clusters`，变更使用 `POST /databases/clusters/{id}/changes`。库与账号在协调节点的合格成员执行一次，各代理分别保存步骤；部分完成后从原操作读取、重新处理或接受现状。代理存在其他待应用差异时保留现场并返回冲突。

接口字段和响应以 [OpenAPI](../api/openapi.json) 为准。

## 管理员菜单偏好

`GET /settings/navigation` 返回当前节点管理员的 `order` 和 `revision`，`PATCH /settings/navigation` 保存完整顺序及读取时的版本。菜单 ID 为 `overview`、`nodes`、`containers`、`databases`、`storage`、`firewall`、`tools`、`settings`，须全部提供且无重复；旧版本返回 409，重新读取后再保存。接口使用管理员会话，写入检查 CSRF，Bearer 令牌继续按管理接口拒绝。


## 终端、Shell 任务与 Compose

以下新增接口只接受管理员 Cookie 会话，写请求同时发送 `Origin`、`X-CSRF-Token`；任务保存、执行及 Compose 操作还须提供 `Idempotency-Key`。路径均位于 `/api/v1`。前端可直接从 OpenAPI 生成类型。

| 功能 | 接口 |
|---|---|
| 创建终端票据 | `POST /nodes/{id}/terminal` |
| 连接终端 | `GET /terminals/{ticket}`，升级为 WebSocket |
| 任务列表 | `GET /tasks?node_id={id}` |
| 新增、编辑、启停任务 | `POST /nodes/{id}/tasks`，提交完整任务与当前 `revision`；新任务为 0 |
| 手动执行与运行记录 | `POST /nodes/{id}/tasks/{task_id}/run`、`GET /nodes/{id}/tasks/{task_id}/runs` |
| 取消任务 | `POST /operations/{operation_id}/cancel` |
| 执行日志 | `GET /nodes/{id}/executions/{operation_id}/logs?offset=0&limit=65536` |
| Compose 发现与状态 | `GET /compose-projects?node_id={id}` |
| 登记实际项目 | `POST /nodes/{id}/compose-projects/{project_id}/register`，正文为 `engine_id` |
| 项目操作 | `POST /nodes/{id}/compose-projects/{project_id}/actions`，正文为 `engine_id`、`action` |

终端票据有效三十秒、单次使用、绑定创建时的管理员会话。使用返回的 `url` 及同一 Cookie 连接，WebSocket 须附带同源 Origin。帧为 JSON：`input`／`output` 的 `data` 使用 Base64，每帧解码后最多 32 KiB；`resize` 提供 `cols`（2～500）和 `rows`（2～300）；Ctrl+C 发送字节 `0x03`。发送 `exit`、断连或退出登录都会关闭 root PTY 及其会话作业。终端输入输出不写入共享历史。

Shell 任务保存 `id`、`revision`、`name`、五字段 `schedule`、IANA `timezone`、`script`、绝对路径 `directory` 和 `enabled`。脚本经 `/bin/bash -lc` 执行。启用任务返回 `next_at`，重启只注册未来计划，错过的计划不补跑。同一任务禁止重入，每节点最多八个未结束任务；重复触发留下 `task_busy` 拒绝记录。运行状态包含 `cancelled`；取消通过原操作处理，日志保留在目标节点，最多 16 MiB。日志 `data` 为 Base64，按 `next_offset` 继续读取，`has_more` 与 `truncated` 分别表示后续内容及达到保存上限。

任务、Compose 列表携带各节点的 `read` 状态，先展示已有结果，再按状态轮询。`queued`／`reading` 表示尚未完成首次读取，`stale` 保留带时间的旧值；`timeout`、`error`、`offline` 保留对应错误。查询任务及项目操作可使用 `GET /operations?resource_id={id}`，筛选发生在分页之前。

Compose 由实际标签、定义目录和配置文件登记，配置编辑继续使用 SSH。`action` 为 `pull`、`start`、`stop`、`recreate` 或 `update`；更新依次拉取、重建，日志使用同一执行日志接口。缺少定义、插件或不一致标签时只读。单容器原操作接口增加 `update`：拉取后保留配置、挂载及网络创建替换容器，读回成功后移除原容器且保留卷；Compose 容器须通过项目更新。

所有执行接口先返回持久操作。应答未知时查询原 ID 或原请求键；镜像更新和 Compose 的未知步骤只核对实际资源，重新处理不会重放已发送命令。需人工处理时核对目标节点后，通过原操作接受现状。
