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

数据库检查启动三成员 MariaDB Galera、两个 ProxySQL、Prometheus 与 Grafana，覆盖业务库、账号、代理实际登录、权限、部分恢复、受管排序、活动事务、故障切换、重启及多写冲突，结果输出为 `database-check` 工件。四逻辑 OPSD 节点在同一 CI 主机运行，其中一台仅管理代理；数据库读写与恢复使用真实集群；既有浏览器结果作为此前界面验收记录保留。

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

2026-11-13；程序提交：`86cbd7b6b8e150b3e563c1a6243bfd8f024781d3`；环境：GitHub Actions Linux、四台生产 Linux amd64 节点、Windows Edge；覆盖项：通信、公网发现、防火墙接纳、六项状态契约、受管代理排序及后台维护开销。

[CI 43312055499](https://github.com/jkfujr/opsd/actions/runs/43312055499) 完整通过：接口生成一致性、前端类型与生产构建、Linux amd64／arm64 编译、完整后端、真实 Docker／Galera／双代理、原生互信新连接、主题与内置恢复、实际安装与生产 API。真实数据库检查包含代理专用节点、未知应答恢复、活动事务、故障切换、重启持久化及多写冲突；独立主题、默认内置、内置恢复各八组受影响界面检查通过。输入未变的此前三模式各 33 项结果继续复用。

C001、C041、C052、C091 已使用同次成功工件更新；运行程序与工件一致，节点和执行器均 active、`NRestarts=0`，node.db 为 6、executor.db 为 7，未结束操作均为 0。四台各 15 项保留检查通过，包含身份、账号、入口、配置、任务、原操作、40 个容器及外部规则。C041 已通过停服网络流程改为 `:8444`，保留自动公网模式，其余三节点到公网及组网入口的新 TCP 连接均成功。实际 iptables 配置已接纳，四节点双栈互信为 `in_sync`，C041 IPv6 在主源超时后由 icanhazip 确认。

两台代理已预览接管并启用自动排序，分别使用本机 C041、C052 为写节点；C071 从受管路由移除，原服务器与数据库保留。四个生产 Galera 成员均 Primary／Ready／Synced，成员数为 4。线上登录、六个页面、恢复入口与代理页可访问，1440／320px 无整体横向溢出、脚本错误为 0；读取 40 个容器、3 个 Compose 项目及 1 项任务。C091 时钟约快 15 秒、C041 约慢 4 秒，两台缺少时间同步服务，跨节点读取持续出现 `stale`，时间同步修复待用户确认。

服务器审查：C041 图床 Node 进程约占 2.75 GiB RSS 和 3.91 GiB swap，整机可用内存约 161 MiB，4 GiB swap 接近用满；部署后 OPSD 两服务合计约 36 MiB。图床上传整文件缓冲、分块读取整块聚合及缓存无字节上限已从运行容器源码确认，图床保持原运行状态。C041 旧日志刷新失败、C091 小时及日备份失败保留现场。

结果位于 `artifacts/optimization-20261113/`（成功工件、更新及保留记录、接口和浏览器结果）与 `artifacts/optimization/`（服务器内存与运行容器源码）。已验证：上述 CI、生产服务、网络、规则、代理及只读服务器检查。未验证：arm64 实机、整机重启、生产业务故障注入、真实负载性能及图床具体保留对象；未公开发布安装包。

历史结果见 [变更记录](../CHANGELOG.md)。
