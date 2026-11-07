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

2026-11-07；提交：`45f5a1b645f5deee3d5dd986acf232580dcaa37e`；环境：GitHub Ubuntu 24.04、Go 1.26.2、Node.js 24.14.1、Chromium、真实 Docker 与启用的 UFW，以及 C091 实例；覆盖项：构建、后端、原生互信、实际安装、部署和控制台。C091 直接使用同一 CI 的 amd64 工件，复用此前 CI 成功结果。

已验证：接口生成一致、前端类型与生产构建、Linux amd64／arm64 编译、安装与重复安装保留配置；原生 UFW 双栈互信优先于外部拒绝链，非互信新连接保持拒绝，所属跳转去重与位置恢复、持久文件重载、地址替换及停用时撤除运行规则并保留持久配置，外部规则内容与相对顺序保持原状；生产程序中的实际 `ufw reload`、节点服务停止后执行器重启并从持久目标恢复双栈首条跳转、节点重连后的状态同步，以及普通精确 IP 规则自动转交与关闭撤除，网段和端口规则保留。nftables 双栈新连接与 firewalld 永久配置沿用现有 CI 检查。

已验证：真实容器启停重启、未知应答只查询原操作且重启请求数为 1、日志暂停及页面隐藏和关闭后的轮询停止；10 页深浅主题及 320／768／1440px，防火墙顶部、容器筛选和互信列表另覆盖 639／640px、1000px、1920px，控件实际边界对齐；容器列顺序、单节点隐藏节点列、空列表、部分离线、长名称、加载和错误；互信来源节点双栈合并、分地址族筛选、部分生效、详情中的运行与永久结果、设置页双栈状态、共享 IP、待发现和待撤除条目，以及 200% CSS 缩放。浏览器脚本错误为 0。

已验证：C091 原位更新完成，账号、节点身份与配置保留，两个服务 active 且 NRestarts=0，未结束操作为 0；本机 IPv4／IPv6 互信运行及永久配置全部 in_sync，所属跳转各一条并位于双栈 INPUT 首条，外部规则内容及相对顺序保持一致。普通精确 IP 放行规则自动转交成功，保留 17 条普通规则；10 个既有容器的 ID、状态和启动时间保持一致。线上 Chromium 登录、互信双栈合并列表与详情、设置页、容器列表与详情、选择节点后隐藏节点列通过；1440／768／320px 容器筛选控件实际位置检查通过，页面脚本错误为 0。已人工检查线上互信宽屏及容器中窄屏截图。

未验证：C091 跨节点来源的新连接及整机重启、arm64 实机、rootless Docker、跨地域真实多节点、firewalld 运行配置的新连接、浏览器原生缩放。本批未发布版本或公开安装包。

已人工检查宽屏互信与容器列表、窄屏长名称、部分生效设置页，以及共享 IP、待发现与待撤除截图；中屏刷新对齐、部分生效详情和 200% CSS 缩放的视觉检查复用此前相同界面输入的截图。

- [CI 42267960773](https://github.com/jkfujr/opsd/actions/runs/42267960773)
- 界面工件：该运行的 `ui-check`，下载到 `artifacts/ui-42267960773/`；`result.json` 记录实际检查结果。
- 程序工件：同一运行的 `opsd-linux`，包含 amd64／arm64 程序及固定版本安装脚本。
- C091 部署与线上检查：`artifacts/deploy-42267960773/`，`readback.json` 保存双栈规则与服务读回，`browser-result.json` 保存互信迁移、普通规则及页面检查结果。

本次部署复用上述 CI 工件，程序与验收脚本输入保持一致。历史 CI 与 C091 部署结果见 [变更记录](../CHANGELOG.md)。
