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

2026-11-07；提交：`f0cfc7d9d14dacb9fd1649911472239a61df0dd9`；环境：GitHub Ubuntu 24.04、Go 1.26.2、Node.js 24.14.1、Chromium、Docker、启用的 UFW；三个逻辑 OPSD 节点、三个 MariaDB Galera 成员和两个 ProxySQL 运行在同一 CI 主机，另运行真实 Prometheus 与 Grafana；覆盖项：构建、后端、原生互信、实际安装、API、数据库与页面交互。

已验证：OpenAPI 生成一致、前端类型与生产构建、Linux amd64／arm64 编译、程序版本及安装脚本语法、实际安装与重复安装保留配置。三节点令牌通用、节点及动态地域范围、权限编辑、停用、重新生成、撤销与所属节点失联；Bearer 优先于浏览器会话，模块权限与操作列表先过滤再分页；同一令牌跨入口重复提交返回原操作，参数变化冲突，已受理操作在令牌停用后继续。当前 JSON 与指标授权范围、采集合并、部分历史、按节点返回及七天边界通过。

已验证：真实三成员 Galera 业务库创建与复制读回、业务账号创建、改密、库级权限与撤权、受保护账号拒绝；两个 ProxySQL 三层配置应用及实际业务账号登录、其他待应用差异保留、部分完成后的原操作继续、已完成步骤保持、删除账号前停用代理及代理记录清理。执行器应答丢失、入口退出及身份重开、跨入口幂等和执行记录持久重开通过；单元检查覆盖发送中断、未知结果停止自动重发与外部修改。真实 Prometheus 授权抓取、Grafana 数据源及查询通过。

已验证：生产浏览器创建令牌后直接核验、读取 JSON 与 `/metrics`，生成结果提供地址、验证命令及 15 秒 Prometheus 配置；网页／API 独立启停、两者关闭时端口释放、恢复后保留会话与令牌撤销通过。13 页深浅主题及 320／768／1440px、数据库六页签、连接与账号表单、部分操作及恢复反馈、读取错误、浮层和未提交修改检查通过。数据库页面状态使用请求注入，数据库实际变更由后端真实集群检查。浏览器脚本错误为 0。

已验证：既有真实 Docker 启停、重启及日志，未知应答的重启请求数为 1；原生互信双栈新连接、UFW 重载、节点停止后的独立执行器恢复及迁移；防火墙七种宽度、五种状态、互信分组、共享地址和 200% CSS 缩放回归通过。已人工检查本轮宽屏成员表、中屏令牌表单、窄屏数据库账号与代理选择、操作恢复抽屉截图；窄屏代理名称保持完整，主机组单独排列。

未验证：跨地域真实多主机、生产 MariaDB／ProxySQL 接入、arm64 实机、rootless Docker、整机重启及 firewalld 运行配置的新连接。本批未发布版本或更新 C091；备份页的既有计划及产物读取未在生产实例验收，Silo 页面与部署留待存储功能批次。

- [CI 42378352702](https://github.com/jkfujr/opsd/actions/runs/42378352702)
- `ui-check` 工件下载到 `artifacts/ui-42378352702/`，`result.json` 记录 API、页面、容器和互信结果，PNG 保存实际截图。
- `database-check` 工件下载到 `artifacts/database-42378352702/`，`database-result.json` 记录三个逻辑节点、三成员 Galera、两个 ProxySQL、幂等、部分恢复及看板结果。
- `opsd-linux` 工件包含同提交 amd64／arm64 程序和固定版本安装脚本。

历史 CI 与 C091 部署结果见 [变更记录](../CHANGELOG.md)。
