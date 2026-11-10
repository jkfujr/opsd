# 验收

## 命令与渠道

本批使用 GitHub Actions CI，从仓库根目录运行：

```bash
npm ci
npm --prefix frontend ci
npm run build
OPSD_DOCKER_TEST=1 OPSD_DATABASE_TEST=1 npm test
```

构建包含接口生成、前端类型检查与生产构建、Linux amd64／arm64 编译。CI 使用 busybox:1.37.0 验证真实容器启停、重启与 TTY／非 TTY 日志，校对生成文件、版本和安装脚本语法，在隔离网络空间执行原生互信规则的新连接验收，再安装生产程序，通过实际 HTTP／WebSocket API 检查任务执行和取消、日志分页、容器更新、Compose 操作及 root 终端释放；结果输出为 `api-check` 工件。

数据库检查启动三成员 MariaDB Galera、两个 ProxySQL、Prometheus 与 Grafana，覆盖业务库、账号、代理实际登录、权限和部分恢复，结果输出为 `database-check` 工件。三逻辑 OPSD 节点在同一 CI 主机运行；数据库读写与恢复使用真实集群；既有浏览器结果作为此前界面验收记录保留。

```bash
go test -c -o artifacts/firewall-native.test ./internal/firewall
sudo env OPSD_NATIVE_TRUST=1 artifacts/firewall-native.test -test.run '^TestPeerTrustNativeNetwork$' -test.v
```

Release 复用同提交成功工件，通过公开地址下载，检查实际安装、两个 systemd 服务、版本与重复安装保留配置。

## 最近结果

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
