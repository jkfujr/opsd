# OPSD

对等节点管理。每台机器安装同一个 `opsd`，提供成员通信、本机管理、监控、数据库管理、API 和可选控制台；修改由目标节点持久受理并执行。普通节点只保存本机运行资料，附近的 CouchDB 保存共享数据和历史。

当前为开发预发布的对等架构，不兼容旧 Hub／Agent 协议和数据库。

控制台默认按概览、节点、容器、数据库、存储、防火墙、设置排列。“设置 → 外观”可调整菜单顺序，保存到当前节点管理员账号，各设备共用；“存储”当前显示未接入，后续接入 Silo。

## 开发

需要 Go 1.26.2、Node.js 24.14.1 及 npm。

```powershell
npm ci
npm --prefix frontend ci
go mod download
npm run build
npm test
```

`api/openapi.json` 是 HTTP 类型来源，`npm run generate` 生成 Go 和 TypeScript 接口。前端资源嵌入程序，生产环境无需 Node.js。构建产物位于 `artifacts/`，包含 `install.sh` 与 `opsd-linux-amd64`、`opsd-linux-arm64`。

## 安装与运行

Debian／Ubuntu 的 systemd 主机使用 [Releases](https://github.com/jkfujr/opsd/releases) 中固定版本的 `install.sh`。以 root 下载脚本，完整下载后执行：

```bash
mkdir -p artifacts
curl -fsSL https://github.com/jkfujr/opsd/releases/download/v$(node -p 'require("./package.json").version')/install.sh -o artifacts/install.sh
sudo sh artifacts/install.sh
sudo /opt/opsd/bin/opsd console --enabled=true
```

控制台和 API 默认关闭，共用 Web 监听地址，分别启停，两者均关闭时释放端口；节点通信默认 TCP 8444，Web 默认 65533。安装输出独立管理员账号、随机密码及登录入口。可使用 `--username`、`--entrance`、`OPSD_PASSWORD` 设置初始账号；`--listen` 和 `--console-listen` 设置监听地址，`--install-dir` 设置程序目录，`--config` 设置实例配置路径。重复安装恢复服务，不覆盖已有身份或账号，不隐式升级。

在节点页打开“添加节点”，可填写名称和地域，点击“生成安装命令”才创建接入码；弹窗显示实际接入地址，可选择 curl 或 wget 并复制短命令。`-e/-k/-t` 分别对应接入地址、节点公钥、接入码，`-n/-r` 对应名称和地域；已有长参数继续可用。名称留空使用目标机器主机名，地域留空保持未设置。接入码列表与撤销位于“接入码”弹窗，24 小时有效，成功加入即消费。新成员取得成员目录后独立通信。

未指定 `--address` 时，安装器优先发现公网 IPv4，再尝试 IPv6；显式地址可使用现有组网网络。防火墙和云安全组需开放节点通信端口。节点详情可分别设置 IPv4／IPv6 的自动或手工公网 IP；自动模式每五分钟及网络变化后更新，冲突或失败保留旧值。

公网地址、成员通信 URL 与监听地址分别展示。既有组网节点切换公网时，先通过现有防火墙入口配置节点通信端口；队列空闲后在本机停止节点服务，再保存网络配置：

```bash
sudo systemctl stop opsd.service
sudo /opt/opsd/bin/opsd network --listen ':8444' --address-mode auto
sudo systemctl start opsd.service
```

`network` 保留节点身份、账号和其他配置，检查节点及执行器的未结束操作、监听地址是否可用。手工通信地址使用 `--address-mode manual --address 'http://组网地址:8444'`。

防火墙工具栏进入“设置”，可开启默认关闭的集群互信，为全部有效成员维护主机入站来源放行，包含本机及后续加入成员。UFW 对互信公网 IP 优先放行全部主机入站端口，包含数据库端口；其他来源继续按现有规则处理，共享公网 IP 的其他来源也在互信范围。互信规则应用后，已有相同精确 IP、任意目标、全协议及全端口的普通允许规则会自动转交互信管理；关闭互信、成员撤销或地址替换时随互信撤除。网段、限定端口或目标、拒绝规则继续由普通规则管理。“互信规则”按来源节点展示 IPv4、IPv6 地址与实际状态，包含共享 IP 关联和迁移状态；关闭后持续展示未撤除规则。Docker 发布端口策略及互信留待后续。

安装注册以下服务，均使用同一个程序：

- `opsd.service`：普通用户运行节点、通信、采集和可选控制台。
- `opsd-executor.service`：root 运行本机 Unix Socket 执行器，只接受有类型的操作。
- `opsd-operation@.service`、`opsd-recover.service`：独立执行与恢复防火墙、容器及数据库操作。

实例配置默认 `/etc/opsd/instance.json`，节点资料在 `/var/lib/opsd/node`，root 执行与恢复资料在 `/var/lib/opsd/executor`。控制台关闭不停止后台工作。本机账号修改与控制台开关通过本机 Socket 受理：

```bash
sudo /opt/opsd/bin/opsd console --enabled=false
sudo /opt/opsd/bin/opsd account --entrance /new-login-entry
sudo env OPSD_PASSWORD='新密码' /opt/opsd/bin/opsd account
sudo systemctl start opsd-recover.service
```

## API 与看板

在“设置 → API”开启 API，选择监控看板、全部只读、容器管理、数据库管理或自定义模板，设置资源范围后生成令牌。完整令牌仅显示一次；同一窗口提供地址、验证命令及 Prometheus 配置。令牌支持编辑权限、停用、重新生成和删除。节点、地域与数据库集群范围直接附着在令牌上。

```bash
sudo /opt/opsd/bin/opsd api --enabled=true
```

任意启用 API 的成员均可接受 `Authorization: Bearer`，每次请求向令牌所属节点核验一次。当前指标使用 JSON API 或 `/metrics`；历史继续由 CouchDB 保存，也可用 Prometheus 每 15 秒采集后接入 Grafana。详见 [API 接入](docs/API.md)。

## 数据库

“数据库”管理 MariaDB Galera 集群、业务库和账号、ProxySQL 与既有备份状态。接入时选择节点，发现本机实例，补充连接信息并确认成员与代理关联；每个宿主机通过本机地址或 Unix Socket 连接实例。现有私有配置和 Docker 中可用的连接信息直接复用，连接密码保存在所属节点的加密配置中。

业务变更由选定协调节点在其可写、已同步的合格成员执行一次，再同步所选 ProxySQL。代理依次应用运行配置、保存磁盘并读回；存在其他待应用差异时报告冲突。每个步骤保存独立结果；部分完成或应答未知时，从原操作核对和继续。删除账号先停用所选代理，再删除数据库账号及代理记录。系统库、系统账号及登记的管理、监控、备份、SST 账号保持只读。

详情提供概览、成员、库、账号、代理、备份六个页签；SQL 查询与表数据编辑通过登记的外部工作台。扩缩容、升级、备份整改与故障恢复留待后续批次。

## OPSD 数据

普通节点按需准备 Docker。在节点详情“OPSD 数据”启用自用 CouchDB，填写组网绑定 IP 和端口。程序部署 CouchDB 容器，数据保留在执行器目录下；数据库凭据与控制台密码独立。

默认使用三个独立实例，各节点优先使用指定数据库、同地域数据库，再选择其他可用实例。CouchDB 持续复制任务在实例间异步复制；全部数据库不可达时，目标控制和本机恢复继续，集群历史明确显示不可用。地域断网不等待全局多数派。

配置、操作和账号密文由所属节点生成签名的不可变版本记录。批量上传逐项确认，切换旧副本不覆盖已知新版本。数据库内容不重放系统命令。私钥、浏览器会话和执行恢复资料只留本机。

监控默认每 5 分钟采集一次，保留 7 天；数据库按 UTC 日期分库，过期时先停止对应复制再删除。概览打开时每 3 秒直连获取读数，同一目标共享采集，实时读数不逐次写入数据库。

后续“存储”采用 Silo：选定服务器承载存储池，全部节点通过 S3 访问，同地域优先，跨地域按桶异步复制。数据库备份可存入 Silo，MariaDB 运行数据和本机恢复记录保持原存放方式。本批保留方向，页面与部署随存储功能建设。

## 容器

“容器”汇总各有效节点的 Docker 容器，支持节点、状态和名称筛选，查看详情、启动、停止、重启及最近日志。列表每五秒刷新，日志默认最近 200 行，可选 100／500／1000 行及每两秒自动刷新；关闭日志或隐藏页面后停止请求。每批日志最多读取 256 KiB，超出时显示截断状态。

首批支持 Linux rootful Docker，通过目标节点本机 Unix Socket 管理。Compose 容器显示项目和服务；Swarm 任务只读，OPSD 数据库组件通过存储设置管理。容器状态、日志读取与 Docker 可用状态分别报告，操作沿用目标节点持久队列。重启应答未知时查询原操作并核对状态；“核对状态”只读取当前结果，人工接受现状后可另行提交新操作。

管理员 API 支持单容器镜像更新、Compose 项目登记和拉取／启停／重建，以及 root 终端和 Shell 定时任务。接口、认证、日志和未知结果处理见 [API 接入](docs/API.md#终端shell-任务与-compose)；这些新增操作暂未接入旧控制台页面。

## 操作与账号

持久修改请求由目标节点本地落盘后编号，普通系统修改按受理顺序串行执行；Shell 任务按任务 ID 独立加锁，同一任务禁止重入。重复操作 ID 或同一幂等键返回原记录；应答丢失时查询原操作。入口退出、数据库故障不取消目标已受理的操作。读取、取消、确认和恢复独立于后续修改。

防火墙首次在后台导入当前用户入站规则及默认策略，之后由所属节点 SQLite 保存统一配置。页面直接读取保存的数据，按端口规则和 IP 规则分类；保存配置与操作同事务提交，返回待应用，后台立即执行并每 30 秒检查外部改动。运行规则变更保留读回、120 秒确认与定向撤销，停用时只维护持久配置；自动修复只使用已确认版本，失败版本保留修改并手动重试。无法表达的内容、系统基础规则和第三方独立链保留。独立操作服务不依赖控制台存活。

每个节点保留独立账号和会话。密码验证信息与可查看密文分开保存，密文使用 AES-GCM，解密密钥使用 age 多接收方封装。新成员获得授权封装，不复制其他节点私钥。撤销成员拒绝其新请求和后续密码授权，并移除数据库访问；离线成员恢复通信后才能得知撤销，已经获得的旧密码无法收回。

## 文档

- [开发](CONTRIBUTING.md)
- [架构](docs/ARCHITECTURE.md)
- [API 接入](docs/API.md)
- [UI 规范](docs/UI.md)
- [验收命令与最近结果](docs/VALIDATION.md)
- [发布](docs/RELEASING.md)
- [变更记录](CHANGELOG.md)
