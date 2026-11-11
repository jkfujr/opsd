# 验收

## 命令与渠道

本批使用 GitHub Actions CI，从仓库根目录运行：

```bash
npm ci
npm --prefix frontend/gpt2 ci
npm run build
OPSD_DOCKER_TEST=1 OPSD_DATABASE_TEST=1 npm test
```

构建包含接口生成、前端类型检查与生产构建、Linux amd64／arm64 编译。CI 使用 busybox:1.37.0 验证真实容器启停、重启与 TTY／非 TTY 日志，校对生成类型、严格适配、授权与能力定义、版本和安装脚本语法，在隔离网络空间执行原生互信规则的新连接验收，再安装生产程序，通过 v2 HTTP／WebSocket API 检查任务及 Compose 的 Bearer 授权、任务执行和取消、日志分页、容器更新、Compose 操作及 root 终端释放；结果输出为 `api-check` 工件。

数据库检查启动三成员 MariaDB Galera、两个 ProxySQL、Prometheus 与 Grafana，覆盖业务库、账号、代理实际登录、权限和部分恢复，结果输出为 `database-check` 工件。三逻辑 OPSD 节点在同一 CI 主机运行；数据库读写与恢复使用真实集群；既有浏览器结果作为此前界面验收记录保留。

```bash
go -C backend test -c -o ../artifacts/firewall-native.test ./internal/firewall
sudo env OPSD_NATIVE_TRUST=1 artifacts/firewall-native.test -test.run '^TestPeerTrustNativeNetwork$' -test.v
```

Release 复用同提交成功工件，通过公开地址下载，检查实际安装、两个 systemd 服务、版本与重复安装保留配置。

主题检查沿用 Go 测试和 Playwright，CI 的“整套主题与内置恢复界面”步骤构建 React、Vue 成品包，在内置生产资源上检查安装、连续条件切换、双登录模式、深层路由、未知应答后的读取和损坏主题恢复，同时验证 Swagger 会话／Bearer、CSRF、条件写入、原键查询与根路径指标。结果为 `theme-check` 工件。本地从仓库根目录运行：

```powershell
npm install --prefix artifacts/theme-ui/vue-runtime --no-save --package-lock=false vue@3.5.45
# 使用已安装的 Edge；CI 使用 Playwright Chromium。
$env:OPSD_THEME_UI='1'
$env:OPSD_THEME_BROWSER='msedge'
go -C backend test ./internal/control -run '^TestConsoleThemeBrowser$' -v -count=1 -timeout=4m
```

## 最近结果

2026-11-12：登录布局及后续共享布局、导航、数据库展示和操作摘要已提交推送，最终程序源码为 `e39bbdde72749024e73cb7cbd3518ca9d1958541`；[CI 43106255224](https://github.com/jkfujr/opsd/actions/runs/43106255224) 完整通过，C091 已使用同次成功的 amd64 工件原位更新。版本保持 `0.1.0-dev`。本次只有前端与文档变更；其余三节点网页关闭，继续使用此前程序。

已验证：本次 CI 的生成一致性、前端类型与生产构建、Linux 双架构编译、完整后端与真实 Docker／Galera／ProxySQL、原生互信新连接、整套主题与内置恢复、实际安装与重复安装，以及生产 API 与 root 执行器检查均通过。此前本地三模式各 29 项及登录布局结果在输入未变范围内复用，详见下方本地记录。

C091 实际运行程序与成功工件一致，节点和执行器均为 active、`NRestarts=0`，数据库版本为 node 5／executor 7，未结束操作均为 0。更新前后 15 项比较全部通过：实例路径、节点身份、账号、入口、菜单、配置、任务、令牌、数据库登记、Compose 登记及旧操作保留；10 个容器的 ID、状态、启动时间与双栈 filter 规则内容、顺序保持一致。

线上 Chromium 检查通过：普通与恢复登录在 1440／320px 下的字段 20px、标题与按钮 24px 间距、44px 高度和等宽均符合实际样式；四节点在线且监控数据可读取。节点编辑弹窗在定义加载完成、按钮可用后核对 1440／320px 单行 Footer、44px 操作区及透明红字撤销，随后取消。数据库六页签均为 48px，概览、采集时间、成员、库及代理读取正常，四个 Galera 成员 Primary／Ready／Synced、成员数 4，两个 ProxySQL 为 ready。中文操作摘要、设置与工具二级导航、恢复页深层刷新通过；40 个容器、3 个 Compose 项目与 1 项定时任务均完成四节点 ready 读取。15 张线上截图已保存并查看代表页面，无整体横向溢出，脚本错误为 0。编辑弹窗首次截图处于读取中，仅定向补查加载完成状态，其余成功场景复用。

结果位于 `artifacts/deploy-ui-layout/43106255224/` 的成功工件、CI 及更新／运行记录，以及 `artifacts/deploy-ui-layout/` 的 `preservation-C091.json`、`browser-result.json` 和 PNG。未验证：arm64 实机与整机重启；生产业务写入未执行，相关系统执行由本次 CI 验证。本批未公开发布安装包；[后端交接](UI.md#后端交接数据库读取与操作摘要)四项未实施。后续文档提交复用上述源码和工件结果。

2026-11-12：gpt2 全界面布局、导航与信息展示优化，本地记录；后续提交与部署见本节最新结果。共享页头、页签、面板、筛选、菜单和编辑器采用明确的间距容器；二级导航取消竖线，分类、子页面与终端会话使用不同反馈，长会话关闭区保持 44px。数据库区分健康、读取与上次结果，健康与分类查询独立，已有列表及详情健康缓存按成员身份和时间更新；操作记录显示中文业务摘要，完整标识与请求键保留在技术详情。HTTP API、OpenAPI、后端业务、协议和持久数据未改。

已验证：最终源码的类型检查、内置生产构建和独立 ZIP 打包通过；六组 Node.js 展示检查通过。Edge 的默认内置、独立主题及内置恢复各 29 项检查全部通过，未预期脚本错误均为 0。受影响的布局与交互重新运行，未受影响及同批输入未变的成功结果复用；HTTP 读取失败提示仅定向复核健康和列表错误。三模式各保留 196 张业务页面与浮层截图，覆盖深浅色 320／768／1440px、数据库六页签、节点配置与公网地址、节点及任务编辑、Compose 登记、容器详情及日志；代表截图已查看。另核对 639／640、900／901、1279／1280px、200% 等效视口、24／20／16／8px 实际间距、48px 页签、资源菜单、长名称、IPv6、公钥、键盘焦点和正文滚动。布局扫描无发现。

编辑节点按后续反馈调整为名称正文与单行 Footer，左侧红色文字撤销，右侧取消与保存。重新构建内置资源和独立 ZIP，三模式各重新执行主题安装登录、节点改名未知应答恢复和编辑器布局三项，其余 26 项复用此前结果。新截图以各模式的 `layout-node-editor-{dark,light}-{320,768,1440}.png` 为准；已核对单行、44px 区域、保存表单关联、撤销确认取消后保留草稿及正文滚动。

数据库覆盖首次排队、读取、旧数据、部分待确认、真实不可用、关联不一致、离线、超时、读取故障、分类失败及后到旧响应。列表与详情已有缓存互相更新；HTTP 失败保留数值并标记上次结果。快速重查在五秒内结束，随后恢复普通间隔，手动刷新可重新开始；隐藏后停止周期刷新，分类页十秒内仅附加一次健康 GET，无逐成员请求。操作摘要覆盖全部 13 类、目标缺失、当前名称缓存、完整长请求键、技术详情和分页。终端跨页、多会话、关闭、重连及输入不重放重新通过，响应式切换未创建新票据或实例。

主题切换与登录流程复用此前内置资源的 `TestConsoleThemeBrowser` 成功结果，覆盖实际主题安装、连续切换、普通与恢复登录、自定义入口、账号修改、深层刷新、会话失效、丢失应答及损坏主题恢复。登录布局复用此前三种模式的 24 张深浅色尺寸截图、实际间距、等宽按钮、错误、等待、键盘、短窗口和真实跳转结果，错误为 0。身份、导航、令牌、请求回执和主题安装使用真实本地 HTTP；容器、数据库、防火墙和任务状态由浏览器注入，终端字节由 WebSocket 路由提供。

结果位于 `artifacts/frontend/gpt2/integration/`、`integration-builtin/` 和 `integration-recovery/` 的 `result.json`、截图及 `layout-*-final.log`、`layout-*-http-error.log`；构建、打包、展示检查、主题恢复与登录结果分别为 `layout-builtin-build.log`、`layout-theme-build.log`、`layout-package.log`、`layout-presentation-check.log`、`layout-theme-browser.log`、`layout-login-check.log` 和 `login-layout/result.json`。节点编辑末次构建、打包和三模式定向检查为 `node-editor-*-build.log`、`node-editor-package.log` 与 `node-editor-*-check.log`。后端交接见 [UI 规范](UI.md#后端交接数据库读取与操作摘要)。本地验收服务检查后已停止；本批 CI、Linux 系统执行及生产部署见本节最新结果。

2026-11-12：登录页布局调整，本地记录；已随 `0303087` 提交，后续提交与部署见本节最新结果。登录表单使用专用 `auth-form`，字段间距 20px、密码框到等宽按钮 24px、标题到表单 24px；输入框和按钮保持 44px。卡片最大宽度 448px、桌面内距 32px，639px 以下内距 24px、页面两侧留白 16px；错误提示按内容展开，无错误时不留空位。

已验证：类型检查、内置生产构建及独立 ZIP 打包通过。既有三模式检查重新运行真实主题安装与登录，未受影响的业务检查复用此前结果。定向 Edge 检查在默认内置、独立主题及恢复模式下核对上述实际间距、等宽、自动填充属性、Tab 顺序、短窗口滚动、错误、提交中、429 等待和真实登录跳转，脚本错误为 0；24 张深浅色 320／768／1440px 与 200% 等效视口截图及三张错误截图已保存，代表截图已查看。布局扫描无发现。结果位于 `artifacts/frontend/gpt2/login-layout/result.json`、同目录 PNG、`login-*-check.log`、`login-builtin-build.log` 和 `login-package.log`。本批 CI 与生产部署见本节最新结果。

2026-11-12：API v2、主题管理、前后端目录整理及 gpt2 默认／内置恢复接入已提交推送。程序源码为 `f2a8171242f40893dcf76c851cb19dcd671f8791`，[CI 43064890948](https://github.com/jkfujr/opsd/actions/runs/43064890948) 完整通过，C001／C041／C052／C091 已使用该次成功的 amd64 工件原位更新；程序与界面版本保持 `0.1.0-dev`，API 契约为 `2.0.0`。

已验证：CI 的接口生成一致性、默认前端类型与生产构建、Linux amd64／arm64 编译、完整后端、真实 Docker、三成员 Galera／两个 ProxySQL 的权限、幂等和部分恢复、原生互信新连接、整套主题与内置恢复、实际安装与重复安装，以及生产 API 的任务、容器更新、Compose 和 root 终端交互／释放均通过。CI 发现后修复了跨节点未找到回执的错误转换、领域故障与 HTTP 错误的契约区别、步骤 `intent` 枚举；验收脚本补齐清理上下文和路由跳转等待，软件源保留 Ubuntu 官方候选地址。

四节点的节点与执行器服务均为 active、`NRestarts=0`，实际进程使用的程序与成功工件一致；数据库版本为 node 5／executor 7，检查时未结束操作均为 0。原实例路径、节点身份、账号及入口／版本、菜单顺序、配置、任务脚本与计划、数据库登记和全部旧操作 ID 保留；40 个容器的 ID、状态、启动时间及双栈 filter 规则内容与顺序保留。只有 C091 开启网页，四节点的外部 API 开关均保持关闭。C041 旧 nftables 确认配置与当前只读 iptables 的既有差异仍为 failed，未覆盖现场规则；录播任务未手动触发。

C091 线上 Chromium 检查通过：API 返回 `2.0.0`、v1 返回 404、四节点在线；40 个容器、3 个 Compose 项目和 1 项定时任务均完成四节点 ready 读取。实际数据库状态为四个 Galera 成员 Primary／Ready／Synced、成员数 4，两个 ProxySQL 均为 ready。gpt2 登录、容器、数据库、防火墙、定时任务、外观及恢复页刷新通过；实时监控、289 个五分钟历史点和内置主题选择正常读取，1440／320px 概览及恢复页截图已查看，无整体横向溢出，脚本错误为 0。首次读取状态的截图在数据返回后重新获取，其余成功场景直接复用。

结果位于 `artifacts/deploy-api-v2/43064890948/` 的成功工件、逐节点更新／运行记录与 CI 报告，以及 `artifacts/deploy-api-v2/` 的 `preservation.json`、`browser-result.json`、`monitoring-result.json` 和三张线上截图。未验证：arm64 实机与整机重启；生产容器／SQL／任务写入未执行，相关系统执行由本次 CI 验证。本批未公开发布安装包；后续文档提交直接复用上述源码与工件结果。

2026-11-11：gpt2 完成 API v2 调用差异、默认内置与内置恢复适配，未提交。恢复路由保留业务基址，登录、退出、账号修改和会话失效使用实际安全入口；主题选择以选择资源 ETag 为准，“刷新界面”进入所选主题的普通根路径。未知写入按原目标与原键查询，分页中断保留内容和游标，已打开弹窗随节点状态禁止写入，防火墙版本冲突保留草稿并显式重新读取。

已验证：`npm run build:frontend` 的类型检查与生产构建、独立主题 ZIP 打包及 `npm run build:backend` 的 Linux amd64／arm64 编译通过。Windows Edge 的独立主题、默认内置、内置恢复三种模式各 22 项交互检查通过，未预期脚本错误均为 0；三种模式各有 80 张深浅色、320／768／1440px 和 200% 等效视口截图，代表页面已查看。复用此前输入未变的成功结果，受防火墙最后改动影响的场景在独立主题和默认内置模式重新运行；恢复模式的终端菜单检查增加前一菜单关闭等待后，重跑终端及后续场景通过。

身份、菜单、接入码、令牌、主题和请求回执使用真实本地 HTTP；容器、数据库、防火墙、任务和终端字节通过浏览器注入 API 状态及 WebSocket 路由检查。覆盖原游标继续读取、弹窗内断连、SSE 所属节点过滤、跨节点未知结果及暂时 404、秘密值丢失恢复、Problem 字段与请求 ID、429 等待、防火墙冲突草稿，以及终端输入、多会话、重连和退出释放。

`TestConsoleThemeBrowser` 使用最终 gpt2 内置资源通过，覆盖 React／Vue、连续主题切换、双登录、自定义安全入口、账号修改、恢复页深层刷新和终端参数、会话失效、上传应答丢失、损坏主题恢复及刷新后进入所选主题。结果位于 `artifacts/frontend/gpt2/integration/`、`integration-builtin/`、`integration-recovery/` 的 `result.json` 和截图，以及 `artifacts/theme-ui/result.json`；构建与主题检查日志位于 `artifacts/frontend/gpt2/`。本次隔离本地服务检查后已停止。

未验证：本批云端 CI、Linux 实机的容器／数据库／防火墙／任务／PTY 系统执行和生产环境。构建保留既有主包体积提示；未执行提交、推送、发布或部署。

2026-11-11：前后端目录整理，未提交。Go 模块迁入 `backend/`，默认前端改为 `frontend/gpt2`；原内置前端保留在 `frontend/legacy`，其余方案保留在 `frontend/variants`。模块名、HTTP API、节点协议、SQLite 结构和根版本值保持不变。共享 TypeScript 类型生成到 `api/typescript/schema.gen.ts`。

已验证：本地完整 `npm run build` 的接口生成、gpt2 类型检查和生产构建、Linux amd64／arm64 编译通过；完整 `npm test` 通过，新增实际内置资源检查覆盖页面认证、工具深层路径、HTML 引用资源读取、缺失资源 404、HEAD 及控制台关闭／重新启用。legacy 的共享类型引用及 frontend-gpt 的版本引用通过各自类型检查。本地 HTTP 辅助程序已实际启动并完成登录、五个深层页面和四项入口资源读取，随后停止。独立主题 ZIP 打包通过，保留相对资源地址和四份字体许可证。原有三个 Vite 服务已按 5173／5290／5292 端口恢复并读回页面。迁移后 255 个原跟踪文件的对应路径均存在，十个脚本语法检查及受影响文档链接核对通过。结果位于 `artifacts/layout/` 的 `build.log`、`tests.log`、`local-console-result.json`、`theme-package-result.json` 和 `vite-restored.json`。

目录整理时尚未验证 gpt2 内置恢复的完整浏览器交互；后续接入方式见 [默认前端接入](../frontend/README.md#默认前端接入)，交互结果见本节最新记录。该次构建保留了原有主包体积提示，未执行提交、推送、发布或部署。

2026-11-11：API v2 工作区实施，未提交。契约为 `2.0.0`、87 个操作；v1 返回 JSON 404，普通 JSON 接口使用生成的严格适配，授权和节点能力由同一 OpenAPI 生成。内置调用方、实际 API 测试、主题包和验证脚本已迁移，演示前端保持原工作区内容。

已验证：本地完整 `npm test`，后续受影响的 control 测试，以及 `npm run build` 的接口生成、前端类型／生产构建、Linux amd64／arm64 编译通过；Linux 生产 API 测试已跨编译。HTTP 用例覆盖错误媒体与字段、超限正文、428／412、并发同键创建、同键异参、跨入口复用、秘密值丢应答、重新登录及重启查询、受理后版本冲突、任务读取不泄露脚本、目标 Compose 登记条件、权限变化后的游标拒绝、创建时间分页、离线位置保留和离线成员撤销。迁移用例从原节点版本升级后核对身份配置、账号、原令牌授权、任务、未结束操作、请求键及旧主题选择保留。

Windows Edge 的 Playwright 已使用最终生产资源验证 Swagger 会话／Bearer、CSRF 写入、节点条件更新、原键查询、权限拒绝、根路径 `/metrics` 与令牌不写入浏览器存储；React／Vue ZIP 的安装、切换、连续切换、双登录、深层路由、未知应答后读取、内置恢复和删除通过，深浅色 320／768／1440px 无整体横向溢出，未预期脚本错误为零。旧 API v1 主题保留选择并停止加载由管理模块用例验证。`scripts/check-ui.mjs` 完成 v2 调用迁移及语法检查，既有整套旧业务页面流程未重跑。

结果位于 `artifacts/api-v2-tests.log`、`api-v2-control-final.log`、`api-v2-build.log`、`api-v2-theme.log` 与 `artifacts/theme-ui/result.json`。未验证：本批云端 CI、真实 Docker／Galera／ProxySQL／原生防火墙与实际安装程序的 v2 运行、公开主题 Release 和生产部署；本机没有 Docker，未执行提交、推送、发布或部署，历史 CI 与线上结果不计为本批验收。

2026-11-10；整套前端主题工作区改动，未提交。已验证：本地 `npm test`、OpenAPI 生成、前端类型与生产构建、Linux amd64／arm64 编译；GitHub Release 下载由本机 HTTP 测试服务覆盖默认正式版与指定标签，异常 ZIP、兼容版本、重复安装、并发冲突、中断清理、数据库重新打开后的选择保留与文件缺失处理通过。Windows Edge 的 Playwright 检查使用实际构建的 React、Vue ZIP，覆盖安装、切换、内置与主题登录、深层路由刷新、资源隔离、未知应答后只读回、脚本错误恢复及删除；深浅色 320／768／1440px 页面无整体横向溢出，截图已查看，未预期脚本错误为零。结果位于 `artifacts/theme-ui/result.json` 和 `screenshots/`。未验证：本批云端 CI、实际公网 GitHub 主题 Release 安装及生产部署；既有业务容器、数据库与原生防火墙的实机结果不计为本批验证。

2026-11-10：第一批 `e4602af` 已通过 [CI 42709280364](https://github.com/jkfujr/opsd/actions/runs/42709280364)，包括生成一致、双架构构建、真实容器和数据库、原生互信新连接、实际安装及生产界面。四台 C001／C041／C052／C091 已使用同一成功工件原位更新，`node.db` 版本为 2，服务 active，未结束队列与操作均为 0。

已验证：C001、C052 已上传的本机重复防火墙记录整理到 17／5 条；C041、C091 继续按每批最多 200 条整理。C041 原生 iptables 双栈互信运行与永久结果均为 in_sync；旧 nftables 普通确认配置与当前只读后端的差异保持 failed，未覆盖外部规则。C001 无公网 IPv6 状态和互信正常汇总通过线上 Chromium 检查，1440／320px 页面无横向溢出，脚本错误为 0；四张截图及 API 读回保存在 `artifacts/optimization/batch1-browser.json` 和 PNG。

第二批 `66a2a64` 已通过 [CI 42759277088](https://github.com/jkfujr/opsd/actions/runs/42759277088)，四节点均已使用同一成功工件更新。四节点 online、40 个容器可读；五轮概览响应为 149～1076 ms（含当前网络传输）。四个 Galera 健康摘要不附带库和账号，分类读取分别返回各 7 个库、13 个账号；两个 ProxySQL 均可读。历史返回 289 个五分钟点，无不可用来源。线上脚本错误为 0；账号版本、身份、原容器及外部规则内容与相对顺序保留，队列与执行器未结束操作均为 0。结果保存在 `artifacts/optimization/batch2-browser.json` 与 `batch2-preservation.json`。

第三批 `eceabeb` 已通过 [CI 42777079237](https://github.com/jkfujr/opsd/actions/runs/42777079237)，四节点已使用同一成功工件更新。CI 覆盖实际安装程序的任务执行、计划触发、幂等、禁止重入、取消和分页日志，容器镜像更新及挂载／网络保留，Compose 登记、拉取、启停、重建和更新，以及 root 终端的尺寸、Ctrl+C、断连与退出登录释放。匿名卷复用显式保留原读写模式，具名只读卷和通过 Mount API 创建的卷均已验证。旧前端停止新增功能，第三批验收使用 HTTP／WebSocket API。

线上 API 已完成四节点 root 身份、37×101 终端尺寸、Ctrl+C 及进程释放；任务、Compose 和容器读取均为 ready，读到 3 个 Compose 项目、40 个容器。四节点当前程序与成功工件逐字节一致，节点和执行器实际进程均使用该程序，数据库版本为 node 3／executor 7，服务 active、NRestarts=0，检查时未结束操作均为 0。原节点身份、配置、账号版本、全部容器的 ID／状态／启动时间，以及外部原生规则内容和相对顺序保留；双栈互信的运行及永久结果全部为 in_sync。

C091 的“录播移动”已先停用登记，再停止旧面板调度并启用 OPSD 任务 `recording-move`；3650 字节原脚本、`/` 工作目录、rclone 配置、Asia/Shanghai 和每天 02:30 均保留，下次执行为 2026-11-11 02:30。本次没有手动执行移动脚本，新任务运行记录为 0。C011 Certimate 的 `c001renewal0001` 工作流已启用，每天 01:25 检查 C001 的 `kagura-mea.com` 及通配证书；签发和 SSH 交付已成功，第二次运行复用了证书。C001 的 443／465 实际证书均匹配，CA 与主机名验证通过，有效期到 2027-02-08；两个原有 DSM 失败工作流保持原样。

C041 的 20 条固定普通规则和 4 条入站引用已交给 `opsd-inherited-firewall.service`，隔离网络从空状态恢复成功，重复执行改动为 0，Docker／CODEX_GALERA 保留。旧 nftables 普通确认配置与当前只读 iptables 的差异仍报告 failed；固定普通规则由独立服务恢复。四节点的 1Panel 程序、命令软链和系统服务均已移除，应用、证书、配置目录及现有 swap 保留。三个 OpenResty 容器及挂载、启动时间保留，本机访问均为 200；C001 邮件站点与 C052 的外部访问为 200，C091 公网 80 在停用前后均为既有超时。

结果保存在 `artifacts/optimization/batch3-api-ci.json`、`batch3-api.json`、`batch3-final-api.json`、`batch3-preservation.json`、`eceabeb-*-runtime.json`、`*-final-state.json` 和相关站点、证书及任务记录。未验证：录播移动下一次计划的实际运行、arm64 实机与整机重启。生产业务容器的镜像／Compose 变更未执行，相关写入由 CI 真实 Docker 验证；本批未公开发布安装包。上述成功结果直接复用，后续文档与已部署脚本归档不改变程序工件。

2026-11-08；提交：`da421e16ad333c7e56f25b21eac2ae83dd8d3344`；环境：GitHub Ubuntu 24.04、Go 1.26.2、Node.js 24.14.1、Chromium、Docker，以及 C001／C041／C052／C091 四台 amd64 宿主机；覆盖项：账号菜单排序、存储入口、现有 CI、四个节点安装更新、生产数据库集群登记及线上读取。CI 结果复用此前同提交成功结果。

已验证：OpenAPI 生成一致、前端类型与生产构建、Linux amd64／arm64 编译、后端、原生互信规则、实际安装及完整生产界面检查通过。账号菜单默认顺序、保存与恢复默认、跨会话持久化、节点账号隔离、改名及改密后保留、非法顺序与版本冲突、CSRF／权限、SSE 更新及未提交编辑保留通过；14 页深浅主题和 320／768／1440px 检查通过。既有真实 Docker、Galera／ProxySQL 管理与恢复、API／令牌和 Prometheus／Grafana 场景仍由同次 CI 覆盖。

已验证：使用 CI 42393969504 的同一 amd64 工件原位更新 C091，并为 C001、C041、C052 安装节点，通过现有 EasyTier 网络加入。四台节点与执行器均 active、NRestarts=0，节点队列及执行器未结束操作均为 0。三个新增节点的网页、API、CouchDB 承载关闭；C091 原账号登录、节点身份、入口和配置保留。原有 7／9／14／10 个容器的 ID、状态与启动时间，数据库账号和代理配置均保留。

已验证：由 C091 协调登记 `jk_galera_cluster`，复用实际容器 ID，包含 C001、C041、C052、C091 四个 MariaDB 与 C041、C052 两个 ProxySQL。四个 Galera 实例的实际 UUID 一致，SQL 读回 Primary／Ready／Synced、成员数 4；OPSD 成功读取每个成员的 7 个库、13 个账号及两个代理的实际主机组和连接池。管理、SST、备份、健康检查与监控账号加入保护名单，凭据加密留在所属节点。配置中的 C071 保留，实际成员列表为四个节点。

已验证：线上 Chromium 完成默认菜单、跨浏览器保存与同步、刷新保持、恢复默认、320px 菜单抽屉、数据库列表和六页签、受保护账号、两个代理与备份状态，以及独立存储入口；页面脚本错误为 0，12 张截图已保存。已人工检查窄屏排序与菜单、宽屏成员表、窄屏代理表。成功读回时四个成员和两个代理均为 ready；C041 在部分轮询中出现读取超时，成员页显示不可用及集群部分可用，本机执行器及 SQL 另有成功结果。

已验证：双栈外部原生链的规则内容与相对顺序保留；普通规则内容保留，匹配的精确 IP 全入站允许规则按现有策略转交互信。C001、C052、C091 已发现地址的互信运行及永久配置均为 in_sync。C041 缺少可维护的持久入站基础链，互信结果为 failed；C001、C041 公网 IPv6 发现失败。C001 两条 IPv4 `61010:61012` 允许规则在普通规则导入后移到 443／25 规则之后，其他普通规则的相对顺序保持；本批未追加防火墙修复。

已验证：备份页读回 C091 每小时、每日任务的既有失败状态，每周任务未配置；其余三台未配置对应备份服务。现有备份计划与产物保持原样。

未验证：生产数据库或代理管理写入、arm64 实机、rootless Docker、整机重启及 firewalld 运行配置的新连接。本批未发布公开安装包；Silo 功能留待存储批次。

- [CI 42393969504](https://github.com/jkfujr/opsd/actions/runs/42393969504)；同提交成功结果直接复用，未重复本地构建或测试。
- `artifacts/ui-check-42393969504/result.json` 与 PNG 保存 CI 页面结果，`artifacts/database-check-42393969504/database-result.json` 保存真实数据库场景结果，`artifacts/opsd-linux-42393969504/` 保存安装工件。
- `artifacts/database-onboarding/` 保存各节点安装前后读取、登记 ID、`cluster-state.json`、`peer-trust.json`、`preservation.json`、`browser-result.json` 与线上截图。`preservation.json` 分别记录外部原生链、普通规则内容和相对顺序。

历史 CI 与 C091 部署结果见 [变更记录](../CHANGELOG.md)。
