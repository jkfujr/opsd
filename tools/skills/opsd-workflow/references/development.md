# 开发参考

## 实际入口

- `backend/cmd/opsd/main.go` → `backend/internal/cli/node.go`：install、node、network、console、api、account、executor、operation、recover。
- `backend/internal/installation/node.go`：安装记录和 systemd 服务；重复安装先读取已有实例，不重新生成身份或账号。`address.go` 在新安装写入前识别加入节点的地址。
- `backend/internal/cluster`：节点身份、成员目录、Noise 通信、持久队列、配置及账号、签名记录、CouchDB 上传和查询。`node.go` 定义本机配置和默认值，`firewall.go` 连接统一防火墙配置与队列。
- `backend/internal/control`：浏览器会话、HTTP、SSE；`backend/internal/executor`：root Socket 和类型化服务；`backend/internal/operations`：独立执行、读回、确认与恢复；`backend/internal/firewall`：UFW、firewalld、nftables 适配。
- `backend/internal/monitoring` 与 `frontend/gpt2/src/monitoring.tsx`：本机历史采集、当前读数与趋势。当前缺值显示“—”，不能拿历史补当前数据。
- `backend/internal/dockerengine`：显式本机 Socket、官方 SDK、容器身份核对和有界日志；容器执行意图保存在执行器 SQLite，恢复入口只核对原操作。列表与日志分别合并五秒及两秒内读取，操作更新后清除缓存。
- `backend/internal/apiauth` 与 `backend/internal/control/api_authorization.go`：令牌摘要、生命周期、统一模块与资源权限；核验通过 `cluster/api_tokens.go`，受理身份进入原队列。
- `backend/internal/databaseengine`：本机数据库发现、Galera／ProxySQL 读写及只读备份状态；`cluster/database_workflow.go` 管理协调与远端持久步骤，`operations/database.go` 保存发送意图及恢复记录。

本机 SQLite 保存身份、配置、账号、队列及待上传记录。附近 CouchDB 保存共享数据和历史；其不可用不阻塞本机控制。普通节点不保存其他节点完整历史。

节点与执行器数据库统一通过 `storage.Open` 及 `backend/internal/storage/migrations` 迁移。既有节点的首次迁移登记原结构，后续连续编号；不能重新初始化身份或队列。`state_cleanup.go` 分批整理已上传的重复防火墙状态，保留实际变化、每对象最新状态和未上传资料，不删除 CouchDB 签名历史。

## 通信与读取

`peer.go` 管理拨号、连接复用、常驻提升及空闲回收，`peer_dispatch.go` 维护普通与控制队列；接收循环先分发应答。`transport.Channel` 的协议分片可以交错，协议版本升级不改变持久操作契约。`directory_exchange.go` 按对象摘要生成差异；完整目录仅用于加入或对端缺少摘要。未知写入仍查询原请求，不因连接替换自动重发。

`current_reads.go` 在入口共享当前资源，立即返回已有状态，目标独立刷新并从取得名额起计算期限；无页面请求后停止。`read.state` 区分排队、读取、已有数据、过期、超时、失败与离线。`database_reads.go` 按健康和页签分类，`databaseengine` 只负责本机 SQL 与有界连接池，写入前检查不复用展示缓存。`history.go` 复用请求内选库和公钥，批量分页后归并图表时间桶。采集、上传及 CouchDB 维护分别运行；承载节点采用双向环复制。

服务结果、读取故障与来源时间分别保存；操作元信息由 `operation_description.go` 在受理事务写入，历史缺值保持未知。`proxy_routing.go` 管理策略预览与持久工作流，`proxy_ranking.go` 在代理所属节点后台维护本机排序；节点迁移 `006_proxy_routing.sql` 只增加本机状态表。CouchDB 维护复用节点 HTTP Transport、变化唤醒及有上限退避，互信五秒维护保留、事件按实际变化发送。

## HTTP 与前端

只修改 `api/openapi.json`，执行 `npm run generate` 生成 backend 内的 HTTP Go 类型、`api/typescript/schema.gen.ts` 共享类型、严格接口、授权和节点／主题 API 能力版本，不手改生成文件或另写 DTO。`/api/v2/docs` 使用同份规范及内置 Swagger 资源；通用调用规则只维护在 [API](../../../../docs/API.md)。`frontend/gpt2/src/api/client.ts` 统一附加 CSRF、记录 GET 的 ETag、遍历集合分页并处理 Problem；写入禁止自动重试，未知结果查询原请求键。

先读 `frontend/gpt2/node_modules/@appica/ui-react/agent-rules.md`，组件从各自子路径导入；沿用角色颜色和现有样式，不修改组件库。

内置界面的明暗配色集中在 `frontend/gpt2/src/theme.ts`。Vite 使用 `getThemeScript()` 将同步脚本插入 HTML 头部，React `ThemeProvider` 使用同一配置；纯客户端首次配色由 HTML 脚本应用。配色修改使用生产构建验收首次打开、刷新和系统偏好，开发环境 StrictMode 会重复执行 Effect。

整套控制台主题由 `backend/internal/consoletheme` 安装和检查，`control/themes.go` 提供管理接口，`server.go` 选择主题及资源；节点迁移保存安装记录和选择，文件位于节点数据目录的 `themes/`。修改前读取 [主题格式与恢复契约](../../../../docs/THEMES.md)。内置恢复入口为 `/__opsd/builtin/`，前端路由的 basename 与 API 根路径分开；原登录地址保留 `?builtin=1`。gpt2 根据实际路径选择业务路由基址；恢复模式的退出及账号修改保留强制内置登录，主题切换后点击“刷新界面”进入普通根路径。完整交互以实际内置资源的浏览器结果为准。

导航定义与共享查询位于 `frontend/gpt2/src/navigation.ts`。账号菜单顺序经 v2 `GET/PUT /settings/navigation` 和 `If-Match` 保存到所属节点的 `account_navigation` 表，SSE 的 `navigation` 事件只刷新对应节点查询；编辑器保留未提交版本。账号偏好独立于凭据版本与其他节点账号，修改密码时保留偏好。“存储”的当前入口只展示未接入状态，Silo 功能按后续批次建设。

- `DialogContent` 的直接子项使用 Header、Body、Footer；长内容在 Body 滚动。表单放 Body，Footer 的提交按钮用 `form` 关联。
- 抽屉同样使用 Header、Body、Footer；用框架焦点管理及中文关闭标签。确认框复用 `ui.tsx` 的 `Confirm`。
- `BusyButton` 在提交或手动刷新时转圈，保留动作名称。后台轮询保留旧内容，不反复转圈。
- 防火墙与容器筛选字段使用显式网格及清零的子项外距，按实际控件边界核对 40px 高度与对齐。容器列以名称、状态开头，单节点筛选隐藏节点列；互信按来源节点分行并分别显示双栈状态，错误留在详情。
- 首次等待时立即显示字段标签、筛选器和表头，在数据处放 `Skeleton` 或 `LoadingRows`；错误与空集合分别显示。
- 节点详情和防火墙按目标重置编辑器、密码和请求状态；同一目标后台刷新保留脏表单，提交使用开始编辑时的版本。
- 组网 HTTP 使用 `newRequestKey()` 的 `crypto.getRandomValues`，不用 `randomUUID()`。命令复制保留 Clipboard API 及文本选择回退。

## 配置与操作

防火墙 GET 读取所属节点 SQLite 的保存值。首次后台导入后，修改配置与受理操作在同一事务提交，后台应用；保存不等于已生效。页面保持 pending、applying、awaiting_confirmation、failed、unknown、in_sync 的区别。

系统规则修改由独立操作服务执行，保留实际读回、确认期限和定向恢复。失败版本不自动反复应用；外部改动仅向已确认版本恢复。UFW 定向更新不调用会重启全局规则的 `ufw default`。不得清空其他系统、Docker 或第三方链。

防火墙后台检查在请求期限内等待本机操作锁；锁繁忙保留上次实际状态，实际读取失败单独记错。互信维护与检查的周期相交时，须继续等待读取，避免将短暂锁冲突保存成配置失败并阻塞自动迁移。

UFW 互信只改写 OPSD 所属链与带标记的引用，`INPUT` 首条引用决定运行优先级，持久文件的 before 引用负责加载。两类引用均纳入所属检查与撤除；任意外部引用或专用链中的外部条件保持冲突。生产 CI 同时检查真实 UFW 重载、节点停止后的执行器重启恢复及迁移，原生隔离检查验证外部拒绝链之前的双栈新连接。

## 按任务选择验收

后端 CI 设置 `OPSD_DATABASE_TEST=1` 启用三成员 Galera、两个 ProxySQL 和实际 Prometheus／Grafana，结果位于 `artifacts/database-result.json`。四逻辑 OPSD 节点和真实数据库容器运行在同一 CI 主机，其中一台仅管理代理；受管配置、应答丢失、事务保持、故障切换、代理重启与多写冲突复用该环境。生产页面检查创建令牌后的实际 JSON／指标请求与 Web/API 独立启停；数据库界面注入状态数据，数据库变更由后端真实集群检查。三模式受影响界面结果随 `theme-check` 保存，数据库结果位于 `database-check`。

本批使用 GitHub Actions，流程见 [验收文档](../../../../docs/VALIDATION.md)。`npm run build` 包含接口生成、前端类型检查、构建与双架构程序编译；同提交成功产物直接用于发布。当前成功结果输入未变就复用。

本批停止旧前端开发，CI 使用 `OPSD_API_TEST=1 go -C backend test ./internal/control -run '^TestProductionBackendAPI$'` 在实际安装程序上检查 HTTP、终端 WebSocket、任务取消和日志、容器更新与 Compose。结果为 `api-check`；`frontend/gpt2` 为默认业务与内置恢复界面，开发和检查入口见 [前端开发](../../../../frontend/README.md)，其他方案保留在 `frontend/variants`。以下旧界面流程仅在后续明确要求界面工作时使用。

`scripts/check-ui.mjs` 在实际安装的生产程序上运行 Chromium，覆盖页面、主题、窄屏、浮层和未提交修改，截图写入 `artifacts/ui/`。防火墙顶部按筛选组、状态与操作组分别核对位置，包含 639／640px 换行边界、1000px 中等宽度和长名称、状态换行、加载、错误；模拟加载结束后定向移除所属处理器，并等待已进入的拦截请求完成，保留其他页面处理器。通过结果后下载该产物，检查中等宽度与换行边界的实际视觉；交互与人工视觉结果分别记录。人工浏览器缺少连接时，继续使用已授权 CI，保留其尚未覆盖的实机范围。

界面检查失败时先读取工件中的 `failure-state.json`，核对防火墙配置、实际互信状态、待处理及最近操作，再处理 `failure.txt` 和截图；响应解析失败时保留原始响应正文。


## 终端、任务和 Compose

终端入口为 `control/terminal.go` → `cluster/terminal.go` → 执行器 Unix WebSocket → `backend/internal/terminal`。业务 RPC 和终端通道分别管理；绑定会话的单次票据、Origin、CSRF 及断连释放保持在相应模块。

`cluster/tasks.go` 保存和调度任务，保存完成通知调度器，未来计划使用稳定请求键；`operations/shell.go` 保存执行意图，CLI 按任务 ID 加锁，普通修改仍用原锁。取消持久登记后重启原 systemd 操作服务，释放整个进程组，未知脚本不补跑。执行日志限 16 MiB，HTTP 每批最多 64 KiB。

`dockerengine` 负责实际 Compose 标签与目录核对、CLI 和镜像更新；执行和恢复状态留在 `operations`。更新先拉取再停止、保留原配置与卷；已发送步骤应答未知时只读回。Compose 配置编辑继续使用 SSH，不添加另一套编辑状态。

同时拨号选择稳定连接时，原连接先保留已发送请求与应答；空闲后释放，两类连接共同受三十二条上限约束。节点关闭同时释放当前与待释放连接。
