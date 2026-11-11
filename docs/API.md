# API v2 接入

业务入口为 `/api/v2`，契约版本为 `2.0.0`。程序发行版本由根 `package.json` 单独维护。字段、类型、权限、参数上限和状态码以 [OpenAPI 3.0.4](../api/openapi.json) 为准；本页说明通用调用与恢复流程。

## 入口与在线文档

在“设置 → API”开启外部 API，或在节点本机执行：

```bash
sudo /opt/opsd/bin/opsd api --enabled=true
export OPSD_URL='http://127.0.0.1:65533'
export OPSD_API_URL="$OPSD_URL/api/v2"
export OPSD_API_TOKEN='生成的令牌'
curl -fsS "$OPSD_API_URL/" -H "Authorization: Bearer $OPSD_API_TOKEN"
```

`GET /` 返回 API／程序／前端版本、入口节点 ID、当前调用身份、授权及文档地址。API 和网页共用 Web 地址，分别启停；两者均关闭时释放监听。成员通信、本机 Socket 和已受理操作独立继续。

`/api/v2/docs` 提供 Swagger UI；完整规范 `/api/v2/openapi.json` 和业务接口要求有效会话或 Bearer。页面使用随程序分发的 `swagger-ui-dist@5.34.1`，关闭外部校验和持久授权。已有管理员会话自动使用同源 Cookie，写入自动附加 CSRF；也可在页面填写 Bearer 令牌。令牌保存在当前页面内存，刷新后重新填写。请求键与版本条件由调用者明确填写，文档不会自动换键重发。

## 认证与权限

请求包含 `Authorization` 时始终使用 Bearer；无效 Bearer 返回错误，不切换为同一浏览器的 Cookie。每个外部请求向令牌所属节点核验一次，请求内复用核验结果。失效、停用、过期、撤销或重新生成的旧令牌返回 401，附适用的 `WWW-Authenticate`；权限不足为 403，所属节点不可达为 503。

| 模块 | read | manage |
|---|---|---|
| `monitoring` | 当前指标、历史与指标导出 | — |
| `nodes` | 基础信息 | 修改名称 |
| `containers` | 详情、状态、日志 | 启停、重启、镜像更新 |
| `firewall` | 保存配置、运行状态 | 变更、安装、确认与恢复 |
| `databases` | 已登记集群、业务资源 | 业务库、账号、授权与代理同步 |
| `tasks` | 元数据、状态与运行记录 | 脚本定义、保存、启停、执行及处置 |
| `compose` | 项目、状态与执行记录 | 登记、拉取、启停、重建与更新 |

manage 包含 read。节点／地域权限按当前有效成员归属核验，数据库使用稳定集群 ID。`tasks.manage` 可以执行任意 root Shell，创建令牌时须明确选择，普通模板默认不包含。已有令牌保留原授权，新增模块不自动授予。

账号、令牌管理、连接与实例登记、接入码、成员撤销、全局互信、主题和 root 终端要求管理员会话。每个操作的具体身份与权限声明在 OpenAPI 的 `security`、`x-opsd-access` 中；缺少规则默认拒绝。操作详情、日志和后续处置继承原操作模块及资源权限。

### 管理员会话

下面命令使用 `jq`。将登录地址、用户名和密码换成当前节点配置：

```bash
curl -fsS -c cookies.txt "$OPSD_API_URL/auth/sessions" \
  -H 'Content-Type: application/json' \
  --data '{"entrance":"/signin","username":"admin","password":"当前密码"}'
curl -fsS -b cookies.txt "$OPSD_API_URL/auth/sessions/current" > session.json
export OPSD_CSRF=$(jq -r .csrf_token session.json)
curl -fsS -b cookies.txt "$OPSD_API_URL/auth/sessions"
```

登录成功为 `201 + Set-Cookie`。Cookie 名包含所属节点 ID，客户端保存服务端返回的完整 Cookie；浏览器使用 `credentials: 'same-origin'`，不读取 HttpOnly 值。会话写入同时要求同源 `Origin` 和 `X-CSRF-Token`。退出使用 `DELETE /auth/sessions/current`；撤销其他会话使用 `DELETE /auth/sessions/{session_id}`。账号修改保留当前密码检查及原有会话撤销行为。

## HTTP 类型、错误与并发

GET 保持只读，可采集当前状态。配置与运行状态使用独立资源；PUT 提交完整可写定义，PATCH 使用 `application/merge-patch+json`。省略字段保持原值，数组整体替换，`null` 只允许移除声明为可空或可省略的字段；删除必填字段返回 422。ID、观测时间和运行字段不进入写入类型，未知字段被拒绝。时间使用 RFC3339 UTC，ID 为不透明字符串。

成功状态：同步完成为 200，创建为 `201 + Location`，持久受理异步执行为 `202 + Operation + Location`，无正文完成为 204。202 只证明已持久受理，最终结果通过原 Operation 的 `state` 读取。确认、恢复、取消等决定持久保存后返回 `200 + Operation`。

错误媒体类型为 `application/problem+json`，例如：

```json
{
  "type": "urn:opsd:problem:validation_failed",
  "title": "请求字段无效",
  "status": 422,
  "detail": "请修改标出的字段",
  "instance": "urn:opsd:request:请求ID",
  "code": "validation_failed",
  "request_id": "请求ID",
  "errors": [{"location":"body","pointer":"/name","code":"required","message":"名称不能为空"}]
}
```

| 状态码 | 含义及处理 |
|---|---|
| 400 | 语法、JSON、参数编码或游标错误 |
| 401／403 | 核对身份、权限及会话 CSRF |
| 404／405／406 | 资源不存在、方法不支持或响应格式不支持；405 附 `Allow` |
| 409 | 业务冲突、同键异参或当前状态不允许；读取原记录后处理 |
| 412／428 | 版本已变化／缺少并发条件；重新读取，人工合并后提交 |
| 413／415／422 | 正文过大、请求媒体类型错误或字段／业务约束错误 |
| 429 | 已有容量或频率限制；遵守适用的 `Retry-After` |
| 500／503／504 | 内部错误、节点或依赖不可用、处理结果未知或上游超时 |

失败操作本身仍是可读取资源，查询返回 200，其 `error` 为领域故障；故障代码由执行模块定义，`request_id` 可省略。HTTP 错误使用 Problem 中的请求 ID。所有响应携带 `X-Request-ID`，与 Problem 和 HTTP 日志关联。日志记录方法、路由模板、身份、目标、状态和耗时，不记录秘密、正文、原始脚本或终端票据 URL。

### 条件更新

可修改资源的 GET 返回强 `ETag`。更新或删除发送原 GET 的 `If-Match`；创建已知 ID 的定义发送 `If-None-Match: *`。ETag 是不透明配置标签，不能根据运行检查时间或本地递增数字自行拼接。

下面先读取节点配置标签，再受理改名：

```bash
export OPSD_NODE_ID='目标节点ID'
curl -fsS -D headers.txt "$OPSD_API_URL/nodes/$OPSD_NODE_ID" \
  -H "Authorization: Bearer $OPSD_API_TOKEN" > node.json
etag=$(awk 'tolower($1)=="etag:" {sub(/\r$/, "", $2); print $2}' headers.txt)
curl -fsS -X PATCH "$OPSD_API_URL/nodes/$OPSD_NODE_ID" \
  -H "Authorization: Bearer $OPSD_API_TOKEN" -H "If-Match: $etag" \
  -H 'Content-Type: application/merge-patch+json' \
  -H 'Idempotency-Key: rename-20261110-1' --data '{"name":"东京节点"}'
```

条件在所属节点核验。异步受理固定预期版本，执行时再次核对；受理后发生冲突写入原操作，不改变此前 202 的含义。`/actions`、`/changes` 使用请求模型中的 `expected_revision` 指向操作对象，资源 CRUD 使用 HTTP 条件头。

## 分页与部分结果

资源集合使用 `items`、可选 `next_cursor`，默认 50、最多 100 条。先授权、筛选，再排序、分页及汇总；空集合为 `[]`。普通资源按稳定 ID，操作按 `created_at`、ID 降序；`queue_sequence` 单独表示目标受理顺序。

```bash
curl -fsS "$OPSD_API_URL/operations?node_id=$OPSD_NODE_ID&limit=50" \
  -H "Authorization: Bearer $OPSD_API_TOKEN" > page.json
cursor=$(jq -r '.next_cursor // empty' page.json)
curl -fsS --get "$OPSD_API_URL/operations" \
  -H "Authorization: Bearer $OPSD_API_TOKEN" \
  --data-urlencode "node_id=$OPSD_NODE_ID" --data-urlencode 'limit=50' \
  --data-urlencode "cursor=$cursor"
```

只有存在 `next_cursor` 才继续。后续页保持路径、筛选、页大小和身份不变。游标绑定查询与授权范围；令牌版本或地域范围变化后重新查询。跨节点游标仅推进已返回项目，不可用节点保留位置。部分失败时有效数据与 `read`／`unavailable` 一并返回；没有可用结果且无法完成请求返回 503。读取缓存、离线历史和待同步结果以各响应的状态及时间区分。

规则数组等有业务顺序的完整定义不分页。监控历史继续按时间范围和点数限制：最近七天，`node_ids`、`from`／`to`、`group_by=node` 或 `range=24h|7d`；CouchDB 故障返回 503。当前监控的缺值保留为空，汇总只计算有效读数。

## 幂等、异步操作与未知结果

进入持久队列及同步创建的写入要求 `Idempotency-Key`，格式为 1～128 位字母、数字、`.`、`_`、`:` 或 `-`。作用范围是稳定调用身份、所属目标和请求键。Bearer 身份由令牌所属节点与 ID 派生；管理员身份由账号所属节点与账号 ID 派生，重新登录保持不变，各节点的独立管理员是不同身份。

同键同参读取原记录，同键异参返回 409。重复请求先查询原回执，再判断当前版本；同步创建与回执在所属 SQLite 同一事务提交，异步写入复用原队列。调用方保存请求键、目标、原参数和版本条件。首次响应中的 `Location` 指向原资源或 Operation。

```bash
export OPSD_REQUEST_KEY='rename-20261110-1'
curl -fsS --get "$OPSD_API_URL/requests/$OPSD_REQUEST_KEY" \
  -H "Authorization: Bearer $OPSD_API_TOKEN" --data-urlencode "node_id=$OPSD_NODE_ID"
curl -fsS "$OPSD_API_URL/operations/原操作ID" -H "Authorization: Bearer $OPSD_API_TOKEN"
```

回执返回 `state`、`resource_url`，异步写入另有 `operation_id`；按返回地址继续读取。秘密只在首次生成响应展示，持久化仍只保存摘要。丢失令牌或接入码首次响应后，同键请求返回 `409 secret_unavailable` 及资源地址；查询回执含 `secret_unavailable: true`，由管理员显式重新生成或撤销后重新创建。

错误可包含 `operation_id` 和 `outcome: not_accepted|unknown`。网络中断、超时或 `unknown` 时先查询原请求、原操作或目标现状。暂时 404 不能证明原请求永远不会受理；保持原键，不自动换键再执行。离线目标不排队等待未来上线；已经受理的操作在入口退出、令牌撤销或共享数据库不可用时继续。

待处理操作使用 `GET /operations?pending=true`，详情与日志位于 `/operations/{id}`、`/operations/{id}/logs`。步骤状态 `intent` 表示已保存发送意图，实际结果继续读取原步骤。确认、重新处理、接受现状、取消统一为：

```bash
curl -fsS -X POST "$OPSD_API_URL/operations/原操作ID/actions" \
  -H "Authorization: Bearer $OPSD_API_TOKEN" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: cancel-20261110-1' \
  --data '{"action":"cancel","expected_revision":1}'
```

revision 使用刚读取的原操作版本；`resolve` 按模型提供 `accept_current`。未知 Shell、容器或 Compose 步骤保持原恢复契约，不因请求重试重放。防火墙确认期限和定向恢复继续由原操作管理。

## 任务、Compose 与数据库

`GET /tasks`、`GET /nodes/{node_id}/tasks/{task_id}` 只提供元数据、状态和记录，不返回脚本。`GET/PUT …/{task_id}/definition` 要求 `tasks.manage`；PUT 提交完整 `name`、`schedule`、`timezone`、`script`、`directory`、`enabled`，并发送条件头和请求键。

```bash
curl -fsS -X PUT "$OPSD_API_URL/nodes/$OPSD_NODE_ID/tasks/check/definition" \
  -H "Authorization: Bearer $OPSD_API_TOKEN" -H 'Content-Type: application/json' \
  -H 'If-None-Match: *' -H 'Idempotency-Key: task-definition-1' \
  --data '{"name":"检查","schedule":"30 2 * * *","timezone":"Asia/Shanghai","script":"printf ok","directory":"/tmp","enabled":false}'
```

任务使用五字段 cron 和 IANA 时区；脚本经 `/bin/bash -lc` 执行。重启只注册未来计划，错过不补跑。同任务禁止重入，每节点最多八个未结束任务。`POST/GET …/{task_id}/executions` 执行或读取记录，执行正文发送当前任务 `expected_revision`。取消通过原 Operation；日志 `data` 为 Base64，使用 `next_offset`、`has_more` 继续，每批最多 64 KiB、总保存最多 16 MiB，`truncated` 表示达到上限。

Compose 集合为 `/compose-projects`，详情位于 `/nodes/{node_id}/engines/{engine_id}/compose-projects/{project_id}`。单项 `GET /registration` 读取已登记配置和 ETag，`PUT /registration` 首次使用 `If-None-Match: *`、替换使用原登记的 `If-Match`，并提供请求键。`POST /actions` 发送 `action`、`expected_revision` 和请求键。定义文件内容参与动作版本核验；执行前再次检查实际项目，配置编辑继续使用 SSH。容器详情、日志与动作采用相同节点／Engine／完整容器 ID 归属。

数据库定义位于 `/database-clusters`，配置详情与 `/{id}/status` 分类运行读取分开；`POST /{id}/changes` 提交业务变更，`/{id}/backups` 读取备份。连接凭据留在所属节点，Galera 变更执行一次，各代理保留独立持久结果。部分完成读取原操作后显式处理；其他待应用差异保留。系统及已登记服务账号继续受保护。

## 主题上传、SSE 与终端

主题管理要求管理员会话。JSON 仓库安装与 ZIP 原始字节上传共用 `POST /console/theme-installations`：

```bash
curl -fsS -b cookies.txt "$OPSD_API_URL/console/theme-installations" \
  -H "Origin: $OPSD_URL" -H "X-CSRF-Token: $OPSD_CSRF" \
  -H 'Content-Type: application/zip' -H 'Idempotency-Key: theme-upload-1' \
  --data-binary @opsd-theme.zip
```

主题 ZIP 格式版本为 1，API 版本为 2；旧 API 版本主题保留安装与选择记录，显示不兼容并进入内置界面。选择资源 `/console/theme-selection`、安装单项 GET 提供 ETag，选择与删除使用条件头。打包限制、GitHub 安装和恢复路径见 [主题](THEMES.md)。

`GET /events` 为管理员 SSE，同源浏览器直接使用 `new EventSource('/api/v2/events')`。非浏览器示例：

```bash
curl -N -b cookies.txt "$OPSD_API_URL/events"
```

SSE 是刷新通知，不保存命令结果；断线重连后重新 GET 对应资源。事件数据可能合并，不据此重放写入。`navigation` 事件只刷新所属节点的菜单偏好；编辑中的草稿保留原版本。

root 终端先以管理员会话 `POST /nodes/{node_id}/terminal` 创建票据，再使用返回的 `url`、同一 Cookie 和同源 Origin 建立 WebSocket。票据三十秒有效且单次使用。JSON 帧使用 `type: input|output|resize|exit`；`input`／`output` 的 `data` 为 Base64，每帧解码最多 32 KiB。`resize` 提供 `cols` 2～500、`rows` 2～300；Ctrl+C 发送 `{"type":"input","data":"Aw=="}`。发送 exit、断连或退出登录释放 root PTY 与其作业。原始终端输入输出不进入共享历史。

## Prometheus 与 Grafana

指标保留根路径 `/metrics`，使用 Bearer，并按监控、容器及数据库读取授权过滤：

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

Accept 为 `application/openmetrics-text` 时返回 OpenMetrics，否则为 Prometheus 文本。Grafana 使用 Prometheus 数据源查询 `opsd_node_up` 及其他 `opsd_` 指标；Infinity 数据源可向 v2 JSON 地址附加 Bearer。Prometheus 保存连续趋势，OPSD 自身历史继续五分钟采集、保留七天。

## v1 迁移

v1 路由已移除，返回 JSON 404。客户端同时升级路径、请求媒体类型、版本条件和结果处理：

| 原接口 | v2 |
|---|---|
| `/auth/login`、`/auth/session` | `/auth/sessions`、`/auth/sessions/current` |
| `/settings/version`、`/api-tokens/verify` | `GET /` |
| `/nodes/{id}/settings` | `/nodes/{node_id}/configuration` |
| `/nodes/{id}/firewall` | `/firewall/configuration` 与 `/firewall/status`；写入 `/changes`、`/actions` |
| `/databases/clusters` | `/database-clusters`；定义与 `/status` 分开 |
| 节点下容器／Compose | 增加 `/engines/{engine_id}/` |
| 任务保存、`/run`、`/runs` | PUT `/definition`、POST／GET `/executions` |
| 操作 `/confirm`、`/recover`、`/resolve`、`/cancel` | POST `/actions`，动作名进入正文 |
| 节点下 `/executions/{id}/logs` | `/operations/{id}/logs` |
| `/api-tokens` | `/nodes/{node_id}/api-tokens` |
| `/themes` 及安装、选择子路由 | `/console/theme-installations`、`/console/theme-selection` |
| PATCH `/settings/navigation` | PUT `/settings/navigation`，完整顺序及 `If-Match` |

从每个配置 GET 获取 ETag，资源写模型移除只读字段及正文 revision；actions／changes 保留模型要求的 `expected_revision`。错误读取 `detail`，列表遍历 `next_cursor`，未知结果通过原请求键查询。HTTP 类型与持久领域类型分开，升级不重写历史操作 JSON，不回放旧请求。

节点能力 `api_authorization_version: 2` 声明新增身份及授权语义；目标缺少能力时拒绝对应写入。升级保留原身份、配置、账号、令牌权限、任务、操作、请求键与恢复记录。接口生成、授权定义、内置控制台和 React／Vue 主题验证使用同一 OpenAPI。
