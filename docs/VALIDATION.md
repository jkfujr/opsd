# 验收

## 命令与渠道

本批先在本机独立 WSL 2 发行版 `OPSD-CI` 完成验收，通过后再提交。发行版安装在 `F:\VM\WSL\OPSD-CI`，系统为 Ubuntu 24.04；启用 systemd，安装 Go 1.26.2、Node.js 24.14.1、Docker、Compose、nftables、iptables、firewalld、UFW 与中文字体，`ci` 用户可使用 Docker 和免密 sudo。Go 下载使用 `https://goproxy.cn,direct`，Docker Hub 使用 `https://docker.m.daocloud.io` 镜像。GitHub Actions 的 push、PR 和手动触发保持原值。

从仓库根目录执行：

```powershell
pwsh -NoProfile -File scripts/check-wsl.ps1
```

入口同步当前工作区到 WSL 的 `/home/ci/opsd`，在 Linux 文件系统构建，保留依赖缓存；产物和日志复制到本机被忽略的 `artifacts/wsl-ci/`。安装验收会修改独立发行版内的 OPSD 服务与防火墙，每次重建验收节点及执行器数据，脚本校验发行版名称。需要重跑失败阶段时使用 `-Stage build|shared|backend|monitoring|firewall|themes|api`；成功且输入未变的阶段直接复用。

本地与 GitHub Actions 使用相同的项目构建及验收命令，Linux 手动构建入口为：

```bash
npm ci
npm --prefix frontend ci
npm run build
OPSD_DOCKER_TEST=1 OPSD_DATABASE_TEST=1 npm test
```

构建包含接口生成、前端类型检查与生产构建、Linux amd64／arm64 编译。CI 使用 busybox:1.37.0 验证真实容器启停、重启与 TTY／非 TTY 日志，校对生成类型、严格适配、授权与能力定义、版本和安装脚本语法，在隔离网络空间执行原生互信规则的新连接验收，再安装生产程序，通过 v2 HTTP／WebSocket API 检查任务及 Compose 的 Bearer 授权、任务执行和取消、日志分页、容器更新、Compose 操作及 root 终端释放；结果输出为 `api-check` 工件。

数据库检查启动三成员 MariaDB Galera、两个 ProxySQL、Prometheus 与 Grafana，覆盖业务库、账号、代理实际登录、权限、部分恢复、受管排序、活动事务、故障切换、重启及多写冲突，结果输出为 `database-check` 工件。固定部署模板另覆盖单成员创建、扩为三成员、TLS 全量同步、已有数据保留、首成员重启入群及代理部署，结果为 `database-deployment-result.json`。四逻辑 OPSD 节点在同一 CI 主机运行，其中一台仅管理代理；Docker 缺失安装、已有安装和安装失败使用命令替身；协调节点及执行器恢复使用持久数据库重新打开检查。

共享迁移专项另启动三个运行节点，复用两个本机代理并自动安装第三个代理；新成员单独检查引导节点停止后的接入资料获取。覆盖迁移、共享策略、地址补传、原请求与步骤复用、协调接管、代理恢复和账号撤销，结果输出为 `shared-storage-result.json`。Linux 手动复现从仓库根目录执行：

```bash
go -C backend test -c -o ../artifacts/shared-storage.test ./internal/control
(cd backend/internal/control && sudo env OPSD_DATABASE_TEST=1 ../../../artifacts/shared-storage.test -test.run '^TestSharedStorageNativeMigrationAndRecovery$' -test.v -test.timeout=8m)
```

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

2026-11-16；源码：基于 `223aef0` 的工作区，验证后通信修复提交为 `79db0cc`。环境：Windows PowerShell、WSL 2.6.3 的独立 `OPSD-CI`、Ubuntu 24.04.5、Go 1.26.2、Node.js 24.14.1、Docker 29.1.3、systemd，Linux amd64。全部检查在本机执行，GitHub CI 触发配置保留。

接口生成一致性、前端类型检查与生产构建、Linux amd64／arm64 编译、版本和安装脚本语法通过。公网观测限制修复后只重编译受影响的双架构后端，前端成功结果复用此前结果。完整后端通过，包含双向满载查询、已确认握手的重复连接、二十节点连接总数、三条公网主备观测的加入目录，以及真实 Docker、三成员 Galera、双 ProxySQL、Prometheus／Grafana、单成员创建和扩为三成员、TLS 同步、业务数据保留与代理重启恢复。

共享迁移专项通过：三个运行节点与一个新加入成员、两个复用代理及一个自动安装代理，覆盖原 ID 与回执、共享策略、地址补传、引导节点停止后的接入资料、协调转交、已受理步骤复用、代理独立恢复、精确库权限及账号撤销。4／10／40 个全新代理节点的每后端连接预算为 24／60／240，管理连接另计，复用代理保留现场限制。

原生网络检查通过 UFW、nftables、iptables 的双栈新连接与互信恢复。React／Vue 整包主题、默认内置及内置恢复三模式交互通过，包含共享库原请求查询、主备入口、代理状态、协调权转交及窄屏；三模式窄屏截图的人工视觉检查复用此前结果，界面源码保持原值。

实际安装与 API 检查通过：默认父目录 `0755`，节点与执行器私有目录 `0700`；下载和版本失败路径、停止服务后改网络、重复安装保留账号、HTTP／WebSocket、任务幂等与取消、日志、容器更新、Compose 和 root 终端释放。安装阶段重建验收实例后再次通过，下一轮使用新的验收身份和请求回执；结束时停止本地验收服务。

结果位于 `artifacts/wsl-ci/`：`local-ci/` 保存各阶段日志，`shared-storage-result.json`、`database-result.json`、`database-deployment-result.json`、`theme-ui/result.json`、`frontend/gpt2/integration*/` 和 `api/result.json` 保存对应结果与截图。构建产物为同目录的双架构程序和 `install.sh`。

现有备份审查复用此前结果：C091 小时任务只备份 `Openlist`，每日任务采用全量物理备份；检查时两任务的最近结果均为退出码 1。原始结果保留在 `artifacts/shared-storage-20261116/existing-backup-scope.json` 和 `existing-backup-detail.json`。本批生产切换与线上 `opsd_core` 初始化尚未执行。

已验证：本地构建、完整后端、真实数据库与共享迁移、原生互信新连接、三模式浏览器、实际安装及 API，重复执行安装验收。未验证：生产切换与真实跨地域故障接续、生产备份恢复、arm64 实机及整机重启；本批未推送新提交、未公开发布安装包。

历史结果见 [变更记录](../CHANGELOG.md)。
