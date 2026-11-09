# 验收

## 命令与渠道

本批使用 GitHub Actions CI，从仓库根目录运行：

```bash
npm ci
npm --prefix frontend ci
npm run build
OPSD_DOCKER_TEST=1 OPSD_DATABASE_TEST=1 npm test
```

构建包含接口生成、前端类型检查与生产构建、Linux amd64／arm64 编译。CI 使用 busybox:1.37.0 验证真实容器启停、重启与 TTY／非 TTY 日志，校对生成文件、版本和安装脚本语法，在隔离网络空间执行原生互信规则的新连接验收，再安装生产程序，通过 Chromium 检查页面与交互；截图输出为 `ui-check` 工件。

数据库检查启动三成员 MariaDB Galera、两个 ProxySQL、Prometheus 与 Grafana，覆盖业务库、账号、代理实际登录、权限和部分恢复，结果输出为 `database-check` 工件。三逻辑 OPSD 节点在同一 CI 主机运行；生产页面覆盖 API、数据库六页签与操作反馈，数据库页面状态通过请求注入，后端执行使用真实集群。

```bash
go test -c -o artifacts/firewall-native.test ./internal/firewall
sudo env OPSD_NATIVE_TRUST=1 artifacts/firewall-native.test -test.run '^TestPeerTrustNativeNetwork$' -test.v
```

Release 复用同提交成功工件，通过公开地址下载，检查实际安装、两个 systemd 服务、版本与重复安装保留配置。

## 最近结果

2026-11-10：第一批 `e4602af` 已通过 [CI 42709280364](https://github.com/jkfujr/opsd/actions/runs/42709280364)，包括生成一致、双架构构建、真实容器和数据库、原生互信新连接、实际安装及生产界面。四台 C001／C041／C052／C091 已使用同一成功工件原位更新，`node.db` 版本为 2，服务 active，未结束队列与操作均为 0。

已验证：C001、C052 已上传的本机重复防火墙记录整理到 17／5 条；C041、C091 继续按每批最多 200 条整理。C041 原生 iptables 双栈互信运行与永久结果均为 in_sync；旧 nftables 普通确认配置与当前只读后端的差异保持 failed，未覆盖外部规则。C001 无公网 IPv6 状态和互信正常汇总通过线上 Chromium 检查，1440／320px 页面无横向溢出，脚本错误为 0；四张截图及 API 读回保存在 `artifacts/optimization/batch1-browser.json` 和 PNG。

第二批通信、共享读取、数据库分类和历史批量查询源码已更新，CI 与四节点升级尚未完成。

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
