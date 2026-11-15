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

2026-11-15；源码与四节点生产程序提交：`955f7e5cfe9e650caf2bd4e3901f6ae541391d08`。环境：Windows PowerShell、Go、Python 3、GitHub Actions Ubuntu 24.04，以及 C001／C041／C052／C091 Linux amd64；此前清理、校时、改名与凭据证据继续复用；覆盖项：两种锁入口的错误分类、RPC 应答、节点终止与恢复、四节点更新及 API／数据库／代理读回。

```powershell
go -C backend test ./internal/executor ./internal/cluster -count=1
go -C backend test ./internal/cluster ./internal/executor ./internal/dockerengine ./internal/protocol ./internal/databaseengine ./internal/operations -count=1
py -B -m unittest discover -s tools/skills/opsd-workflow/scripts -p test_container_names.py -v
```

本批两包测试及六个后端包回归通过。执行器覆盖 `OperationLock`／`TaskLock` 的直接繁忙、包装繁忙和普通错误，以及 RPC 错误码和消息、未持久化、未启动与任务 ID；节点覆盖 `dependency_unavailable` 保留消息并以 `finished/terminal=1` 终止，后续调度与重新打开数据库保持结果。既有首次繁忙后原 ID 执行一次、传输错误封存、明确拒绝保留原码和恢复用例通过；扫描覆盖运行、停止、移除、已有登记、非容器、错误凭据和扫描后停止拒绝接入。容器命名工具两个用例复用此前结果，覆盖完整 ID、旧名迁移、重复执行、回退、凭据保留、名称冲突和手工接入排除。

源码提交 `955f7e5` 的 [CI 43709156533](https://github.com/jkfujr/opsd/actions/runs/43709156533) 全部通过：接口生成一致性、前端类型与生产构建、Linux amd64／arm64 编译、完整后端及真实数据库与 Docker、原生互信新连接、主题与内置恢复、CI 环境实际安装及生产 API／root 执行器验收。

本次复用该 CI 的 `opsd-linux` 工件（ID `14160221334`），按 C052 → C091 → C041 → C001 更新。每台停服前及停服后确认队列空闲，再替换程序并恢复两个服务；最终八个服务 active，节点及执行器队列均为 0，四节点文件及运行中程序的 SHA-256 均为 `8cdebc26193a8e685831ddc95c954f3d689c7ec40fa94269bdf8c1f79255dab3`。节点配置、实例路径、数据目录及现有容器的完整 ID、名称、状态和挂载保留。C052 首次检查遇到 Docker `Mounts` 数组顺序变化，按挂载目标核对后内容一致，已通过只读读回确认更新结果，程序更新只执行一次。

最终 Galera 四成员均 Primary／Ready／Synced、集群大小为 4；管理员登录及 API 读取通过，四节点 connected，四成员的读取状态和两个代理均为 `ready`。C052／C041 本机代理读回均为 `PendingChanges=false`，排序仍以本机为首。证据位于 [deployment-result.json](../artifacts/deploy-lock-errors-20261115/deployment-result.json)，同目录保存 CI 工件来源、逐节点更新与最终读取、代理路由及 `api-final.json`。

此前程序 `87448c7` 复用此前 [CI 43690361179](https://github.com/jkfujr/opsd/actions/runs/43690361179) 成功结果：接口生成一致性、前端类型与生产构建、Linux amd64／arm64 编译、完整后端、真实数据库与 Docker、原生互信新连接、主题与内置恢复、实际安装及生产 API。真实部署测试覆盖新旧容器名识别、单成员创建、扩为三成员、TLS 同步、业务库保留、首成员重启和代理部署。OpenAPI 与表结构保持原值。

复用此前 `87448c7` 部署结果：工件按 C052 → C091 → C041 → C001 原位更新。每台停服前及停服后确认节点、执行器均无未结束操作；启动后核对版本、服务、运行工件及 Galera 同步。最终八个服务 active，四成员均 Primary／Ready／Synced、集群大小为 4，节点及执行器队列均为 0；实例路径、节点配置及数据目录保留。amd64 工件与实际运行文件 SHA-256 均为 `7e51db99764e731af5b7cf324e412635a4adb13ee145edcdda627a5ef3e7f25c`。C091 中断续传后的校验不符在停服前被拒绝，完整重传校验通过后继续部署。

C052 此前清理前核对旧容器 `125109d193b27ceea1555f99e3ba719b02b80b7aafbeaf39b71e3ce60498fed0` 的名称、停止状态及挂载。停服窗口 checkpoint 为 `[0,0,0]`，备份为 `/var/lib/opsd/executor/maintenance/87448c738db456c066b2bba4d5b59283cba888c7/node-before-cleanup.db`；普通 `docker rm` 后 inspect 返回退出码 1、`No such object`。事务删除孤立登记后，启动按该 ID 读回 `managed_database_instances` 与 `managed_database_versions`，计数均为 0；清理前后队列历史均为 5 条，原排序操作仍可通过 API 查询为 `rejected/not_accepted`。现用容器 `09320cd39762e2a1cf86ba390a41d44239e379b633194660a207b8fe702b8b34`、共享数据和配置目录保留。集群引用采用删除后只读核对无引用及扫描 4 成员结果：`2026-11-15T10:17:45.851114+00:00` 查询 `managed_database_clusters.body`、`proxy_routing_state`、`database_workflows`，旧 ID 匹配数均为 0，instances／versions 计数均为 0；匹配条件及复用的扫描结果见 [cleanup-references.json](../artifacts/optimization-20261115/cleanup-references.json)。

复用此前校时结果：C041／C091 安装前 mask chrony／chronyd 阻止自启，移除活动 `makestep` 后启用渐进校时，保留发行版时间源和 Asia/Shanghai。两台均通过 `chronyc waitsync 120 0.1`，tracking System time 分别为 0.000154818／0.000082103 秒。已建立 SSH 通道的三次时间交换取最小往返时间估计，与 C001 的偏差分别为 -0.045972／+0.011731 秒；当时已有可用源，未触发备用源。最后连续三轮健康读取中，四节点服务与读取状态均为 `ready`，来源采集时间持续更新。

复用此前排序和扫描结果：C052 使用原代理路由预览及提交恢复排序，`Enabled/Automatic/Backends` 保留。四个 30 秒周期均为 `ready`、本机第一、`PendingChanges=false`，配置指纹保持一致；顺序为 C052 → C091 → C001 → C041。线上扫描为一集群、四成员、双代理，`unresolved=0`、`problems=0`，旧容器 ID 消失。

复用此前改名结果：节点验证通过后，最后将 C091 自用 CouchDB 改为 `opsd-data-couchdb-e62e7ae0a45b302748c7f16a65b5b623`，完整 ID 仍为 `440bf25ce1ff0462dc155417d7a14b83bb29e6d63d567e6a778a0a70066e2f90`。`couchdb.json` 保存 `storage_id/full_id/old/new`，挂载、启动时间保留。凭据验收采用当前哈希及健康检查：[C091-couchdb.json](../artifacts/optimization-20261115/C091-couchdb.json) 的 `current_verification` 保存 `2026-11-15T10:17:47.883085+00:00` 采集的 credentials／cookie SHA-256、输入编码和 `/_up=ok`；改名前哈希未留存，无法回溯。原 `credentials_preserved` 保留为当时脚本报告。现有手工接入 MariaDB／ProxySQL 保留原名。

结果位于 `artifacts/optimization-20261115/`：`production-result.json` 汇总此前部署结果，`C052-update.json` 保存原清理证据，`cleanup-references.json` 补充删除后只读引用核对，`C091-couchdb.json` 保存实际改名读回及当前凭据哈希；`C041-chrony.log`／`C091-chrony.log`、`clock-comparison.json`、`routing-four-rounds.json`、`scan-final.json` 和 `health-rounds.json` 保存此前对应原始结果。证据文件沿用 artifacts 的本地忽略规则。

已验证：本批两包测试、六包回归及完整 CI（复用此前结果），`955f7e5` 四节点程序与运行文件、服务及队列、配置和容器保留、四成员同步、管理员 API 及两代理状态；此前清理、改名、校时、路由四周期、扫描、连续健康读取和当前凭据哈希复用此前结果。未验证：全新 CouchDB 初始化、生产程序回退演练、arm64 实机及整机重启；本批未公开发布安装包。

历史结果见 [变更记录](../CHANGELOG.md)。
