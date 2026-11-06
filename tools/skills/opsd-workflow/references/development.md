# 开发参考

## 实际入口

- `cmd/opsd/main.go` → `internal/cli/node.go`：install、node、network、console、account、executor、operation、recover。
- `internal/installation/node.go`：安装记录和 systemd 服务；重复安装先读取已有实例，不重新生成身份或账号。`address.go` 在新安装写入前识别加入节点的地址。
- `internal/cluster`：节点身份、成员目录、Noise 通信、持久队列、配置及账号、签名记录、CouchDB 上传和查询。`node.go` 定义本机配置和默认值，`firewall.go` 连接统一防火墙配置与队列。
- `internal/control`：浏览器会话、HTTP、SSE；`internal/executor`：root Socket 和类型化服务；`internal/operations`：独立执行、读回、确认与恢复；`internal/firewall`：UFW、firewalld、nftables 适配。
- `internal/monitoring` 与 `frontend/src/monitoring.tsx`：本机历史采集、当前读数与趋势。当前缺值显示“—”，不能拿历史补当前数据。
- `internal/dockerengine`：显式本机 Socket、官方 SDK、容器身份核对和有界日志；容器执行意图保存在执行器 SQLite，恢复入口只核对原操作。列表与日志分别合并五秒及两秒内读取，操作更新后清除缓存。

本机 SQLite 保存身份、配置、账号、队列及待上传记录。附近 CouchDB 保存共享数据和历史；其不可用不阻塞本机控制。普通节点不保存其他节点完整历史。

## HTTP 与前端

只修改 `api/openapi.json`，执行 `npm run generate` 生成 `internal/httpapi/api.gen.go` 和 `frontend/src/api/schema.gen.ts`，不手改生成类型或另写 DTO。`frontend/src/api/client.ts` 统一附加 CSRF，并禁止自动重试写入。

先读 `frontend/node_modules/@appica/ui-react/agent-rules.md`，组件从各自子路径导入；沿用角色颜色和现有样式，不修改组件库。

主题配置集中在 `frontend/src/theme.ts`。Vite 使用 `getThemeScript()` 将同步脚本插入 HTML 头部，React `ThemeProvider` 使用同一配置；纯客户端首次主题由 HTML 脚本应用。主题修改使用生产构建验收首次打开、刷新和系统偏好，开发环境 StrictMode 会重复执行 Effect。

- `DialogContent` 的直接子项使用 Header、Body、Footer；长内容在 Body 滚动。表单放 Body，Footer 的提交按钮用 `form` 关联。
- 抽屉同样使用 Header、Body、Footer；用框架焦点管理及中文关闭标签。确认框复用 `ui.tsx` 的 `Confirm`。
- `BusyButton` 在提交或手动刷新时转圈，保留动作名称。后台轮询保留旧内容，不反复转圈。
- 首次等待时立即显示字段标签、筛选器和表头，在数据处放 `Skeleton` 或 `LoadingRows`；错误与空集合分别显示。
- 节点详情和防火墙按目标重置编辑器、密码和请求状态；同一目标后台刷新保留脏表单，提交使用开始编辑时的版本。
- 组网 HTTP 使用 `newRequestKey()` 的 `crypto.getRandomValues`，不用 `randomUUID()`。命令复制保留 Clipboard API 及文本选择回退。

## 配置与操作

防火墙 GET 读取所属节点 SQLite 的保存值。首次后台导入后，修改配置与受理操作在同一事务提交，后台应用；保存不等于已生效。页面保持 pending、applying、awaiting_confirmation、failed、unknown、in_sync 的区别。

系统规则修改由独立操作服务执行，保留实际读回、确认期限和定向恢复。失败版本不自动反复应用；外部改动仅向已确认版本恢复。UFW 定向更新不调用会重启全局规则的 `ufw default`。不得清空其他系统、Docker 或第三方链。

防火墙后台检查在请求期限内等待本机操作锁；锁繁忙保留上次实际状态，实际读取失败单独记错。互信维护与检查的周期相交时，须继续等待读取，避免将短暂锁冲突保存成配置失败并阻塞自动迁移。

## 按任务选择验收

本批使用 GitHub Actions，流程见 [验收文档](../../../../docs/VALIDATION.md)。`npm run build` 包含接口生成、前端类型检查、构建与双架构程序编译；同提交成功产物直接用于发布。当前成功结果输入未变就复用。

`scripts/check-ui.mjs` 在 CI 实际安装的生产程序上运行 Chromium，覆盖页面、主题、窄屏、浮层和未提交修改，截图写入 `artifacts/ui/`。防火墙顶部按筛选组、状态与操作组分别核对位置，包含 639／640px 换行边界、1000px 中等宽度和长名称、状态换行、加载、错误；模拟加载结束后等待拦截请求完成，再清理处理器。通过结果后下载该产物，检查中等宽度与换行边界的实际视觉；交互与人工视觉结果分别记录。人工浏览器缺少连接时，继续使用已授权 CI，保留其尚未覆盖的实机范围。
