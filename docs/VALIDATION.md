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

数据库检查启动三成员 MariaDB Galera、两个 ProxySQL、Prometheus 与 Grafana，覆盖业务库、账号、代理实际登录、权限、部分恢复、受管排序、活动事务、故障切换、重启及多写冲突，结果输出为 `database-check` 工件。固定部署模板另覆盖单成员创建、扩为三成员、TLS 全量同步、已有数据保留、首成员重启入群及代理部署，结果为 `database-deployment-result.json`。四逻辑 OPSD 节点在同一 CI 主机运行，其中一台仅管理代理；Docker 缺失安装、已有安装和安装失败使用命令替身；协调节点及执行器恢复使用持久数据库重新打开检查。

```bash
go -C backend test -c -o ../artifacts/firewall-native.test ./internal/firewall
sudo env OPSD_NATIVE_TRUST=1 artifacts/firewall-native.test -test.run '^TestPeerTrustNativeNetwork$' -test.v
```

Release 复用同提交成功工件，通过公开地址下载，检查实际安装、两个 systemd 服务、版本与重复安装保留配置。

主题检查沿用 Go 测试和 Playwright，CI 的“整套主题与内置恢复界面”步骤构建 React、Vue 成品包，在内置生产资源上检查安装、连续条件切换、双登录模式、深层路由、未知应答后的读取和损坏主题恢复，同时验证 Swagger 会话／Bearer、CSRF、条件写入、原键查询与根路径指标。另在独立主题、默认内置与内置恢复三模式执行受影响状态、日志、受管路由和防火墙接纳检查，截图随 `theme-check` 工件保存。手动复现从仓库根目录运行：

```powershell
npm install --prefix artifacts/theme-ui/vue-runtime --no-save --package-lock=false vue@3.5.45
# 使用已安装的 Edge；CI 使用 Playwright Chromium。
$env:OPSD_THEME_UI='1'
$env:OPSD_THEME_BROWSER='msedge'
go -C backend test ./internal/control -run '^TestConsoleThemeBrowser$' -v -count=1 -timeout=8m
```

## 最近结果

2026-11-15；程序提交：`d3bc165eaf147be035549750fe7eecbef413f5aa`；环境：GitHub Actions Ubuntu 24.04、真实 Docker／MariaDB Galera／ProxySQL、Chromium，以及 C001／C041／C052／C091 的 Linux amd64 与线上控制台；覆盖项：数据库创建、只读接入、成员扩容、独立代理部署、原操作恢复和四节点程序更新。

[CI 43683087418](https://github.com/jkfujr/opsd/actions/runs/43683087418) 完整通过：接口生成一致性、前端类型与生产构建、Linux amd64／arm64 编译、完整后端、真实数据库与 Docker、原生互信新连接、主题与内置恢复、实际安装及生产程序 API。固定模板完成单节点起步、扩为三成员、TLS 全量同步、原业务库保留、首成员重启入群和 ProxySQL 部署；原有真实数据库业务、受管代理、活动事务、故障切换和多写冲突检查同时通过。新增停止容器、已移除容器及旧登记的检查，确认发现与接入阶段阻止它们通过本机端口读取当前实例。

节点与执行器用例覆盖只读扫描及预览、重复接入、集群身份变化、预览失效、端口／目录／磁盘阻断、版本及健康变化、未知应答、长时间同步、协调节点和执行器重新打开持久数据库、排队取消，以及后续成员失败后保留首成员并继续原任务。Docker 缺失、已有安装和安装失败分支使用命令替身。

独立主题、默认内置、内置恢复各九组受影响交互检查通过，脚本错误均为 0；包含创建预览、补填凭据、取消接入、添加成员／代理、原请求查询、键盘选择和 320px 窄屏。每种模式保存 34 张截图，已人工检查创建确认页与窄屏成员预览。数据库页面使用请求注入，数据库部署和同步由后端真实容器检查。

复用同次成功工件原位更新四节点。八个服务均为 active、NRestarts=0，运行程序与工件一致；node.db／executor.db 版本分别为 6／7，未结束操作均为 0。实例路径、节点名称、地址、地域、配置版本及控制台开关与更新前一致。

线上 Playwright 检查通过：四节点连接，四个 Galera 成员均 Primary／Ready／Synced、集群大小为 4，两个 SQL 代理可读；只读发现得到四成员与双代理，C052 的 `mariadb-galera-pre-recovery` 停止容器进入待处理列表。关闭接入弹窗后集群登记版本仍为 5。新建预览正确显示 Docker、空间及端口冲突并阻止提交；添加节点和添加代理入口、320px 弹窗已检查，页面脚本错误为 0。修复仅影响发现逻辑，复用此前新建预览与两个新增入口的通过结果；已人工检查线上截图。

C052 代理排序保留 2026-11-14 的 `conflict` 记录（原代理操作未完成），当前写节点为本机；C041 排序为 ready。部分跨节点读取仍显示 `stale`，服务健康与读取状态分别记录。

结果保存在 `artifacts/database-deployment-20261115/`：`43683087418/` 为本次部署工件和逐节点读回，`production-deployment-result.json`、`production-browser-result.json` 为线上结果；`database-check/`、`theme-check/`、`api-check/` 保留此前 `7c022d2` 的检查工件。已验证：上述 CI、真实容器、恢复用例、四节点程序更新及线上发现、预览和页面。未验证：生产跨主机实际创建与扩容、空白主机实际安装 Docker、arm64 实机及整机重启；未公开发布安装包。

历史结果见 [变更记录](../CHANGELOG.md)。
