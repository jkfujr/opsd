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
| 容器 | 全部／节点／地域 | 启动、停止、重启 |
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

`GET /settings/navigation` 返回当前节点管理员的 `order` 和 `revision`，`PATCH /settings/navigation` 保存完整顺序及读取时的版本。菜单 ID 为 `overview`、`nodes`、`containers`、`databases`、`storage`、`firewall`、`settings`，须全部提供且无重复；旧版本返回 409，重新读取后再保存。接口使用管理员会话，写入检查 CSRF，Bearer 令牌继续按管理接口拒绝。
