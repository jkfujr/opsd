# OPSD

多节点防火墙与容器管理。当前已实现安装、账号、节点、首页、主机防火墙和全局操作抽屉，并完成对应本地验证。容器、Docker 端口、互信和部署迁移按后续批次实施。

Linux 安装权限、systemd 停启与本机恢复的实机验收留在 C091 维护窗口；当前没有变更或部署 C091。

## 开发

需要 Go 1.26.2、Node.js 24.14.1 及 npm。

```powershell
npm ci
npm --prefix frontend ci
go mod download
npm run build
npm test
```

页面通过控制端的同源入口访问。`npm run dev` 只启动前端开发服务器，不转发后端请求；账号和节点验收使用 `opsd serve` 提供的入口。

## 安装与运行

在 Debian 或 Ubuntu 的 systemd 环境中使用同架构的两个已构建程序，保持 `opsd` 与 `opsd-agent` 位于同一目录：

```bash
sudo ./opsd install
```

向导支持安装目录、端口、安全入口、账号和隐藏密码输入。非交互安装使用 `--non-interactive`，其余参数为 `--install-dir`、`--port`、`--entrance`、`--username`、`--password`，对应 `OPSD_*` 环境变量；命令行优先。无终端时须显式启用非交互模式。

控制端安装后以专用普通用户运行，本机 Agent 以 root 运行。默认安装目录为 `/opt/opsd`，实例配置为 `/etc/opsd/instance.json`，Agent 数据为 `/var/lib/opsd-agent`。安装中断保留原意图及身份，重复安装不覆盖账号、密钥和数据，也不隐式升级。

服务入口为 `opsd serve --instance <实例配置>` 和 `opsd-agent run --data-dir <Agent 数据目录>`。安装完成分别报告服务就绪和本机 Agent 接入，外部可访问性须另行验收。

远程节点使用“设置 → 接入码”生成的命令，以 root 运行同架构的 `opsd-agent`。接入码仅创建时展示，24 小时有效，登记成功即消费；撤销节点保留历史及本机数据。

## 主机防火墙与操作恢复

在“防火墙”选择节点与配置范围，单项编辑后预览、应用。运行规则修改及需要重载的操作，从首次原生写入前开始计时，须在 120 秒内从顶部操作抽屉确认保留；未确认时由节点独立撤销。永久配置修改不伪称已经运行生效。

UFW 支持可无损表达的入站规则、顺序及默认入站策略，分别读取 IPv4 的 `ufw-*` 和 IPv6 的 `ufw6-*` 原生链。复杂用户规则、自定义初始化脚本，或运行与永久用户配置不一致时保持只读。firewalld 支持运行／永久两侧、区域端口、简单 rich rule、原生优先级及无损条件下的区域目标重载。nftables 只修改来源明确、能唯一映射的入站基础链简单片段；变量、通配 include、共行混合语句等复杂来源保持只读。

管理器全局启停目前保持只读，其影响超出本批入站恢复范围。Debian／Ubuntu 无管理器时可安装 UFW、firewalld 或 nftables；安装不自动启用或切换后端，只解除本次建立且归属明确的服务屏蔽。

同一预览重复应用追踪原操作；每节点只允许一个未决写操作。受理前发现冲突保留页面草稿，提交结果不明时查询原操作，不自动重发。失联只显示“结果待同步”；第三值或定位歧义保持“需处理”及写入限制，人工接受现状不计为成功。

Agent 安装会注册 `opsd-operation@.service` 和 `opsd-recover.service`。本机执行记录固定在 Agent 数据目录，操作服务独立于通信服务，使用 `Restart=on-failure` 和 `KillMode=control-group`。手工启动恢复使用：

```bash
sudo systemctl start opsd-recover.service
```

`opsd-agent execute --operation-id <ID>` 和 `opsd-agent recover` 由对应 systemd 服务调用；直接绕过服务执行会被拒绝。恢复先停止旧操作服务并确认其受管命令已退出，再取得本机执行锁。

本机账号恢复入口：

```bash
sudo /opt/opsd/bin/opsd recovery show-entrance
sudo /opt/opsd/bin/opsd recovery reset-password
sudo /opt/opsd/bin/opsd recovery reset-entrance --entrance /new-login-entry
```

查看入口只读；修改命令在部署互斥下停服，以控制端数据所属用户事务更新凭据并撤销旧会话，然后启动服务。密码重置使用隐藏输入。

## 构建与契约

`npm run build` 依次生成 Go／TypeScript 接口、检查并构建前端、嵌入界面资源，最后构建两个 Go 程序。产物位于 `dist/linux-amd64` 和 `dist/linux-arm64`；正式安装包在后续构建收口批次生成。

`api/openapi.json` 是唯一 HTTP 契约。单独更新生成文件使用 `npm run generate`，生成结果不手工修改；Go 服务仅挂载已实现接口。Agent 协议类型与编解码位于 `internal/protocol`，Noise 分片传输位于 `internal/transport`，控制端和 Agent 生命周期分别位于 `internal/control`、`internal/agent`。

控制端与 Agent 的数据库迁移分别位于 `internal/storage/migrations/control` 和 `internal/storage/migrations/agent`。迁移使用事务提交版本，数据库采用 WAL、FULL 同步和连接级外键设置。账号、会话、接入码和身份已持久化；在线状态来自当前有效连接，不从历史记录推断。

生产环境由控制端提供嵌入的静态资源，无需 Node.js 服务。运行时模型请求为零。

本批使用本地渠道。实际 HTTP／Noise／SQLite 覆盖持久受理、重复投递、应答丢失、封存与迟到投递、节点互斥、旧修订、确认与超时、主机启动标识变化及第三值冲突；原生格式夹具与受控执行替身覆盖三个后端、定向撤销和组件安装恢复。浏览器通过实际 HTTP／Noise 验证草稿、预览、操作抽屉、应答丢失、超时、离线和浅深主题窄屏交互。Go／TypeScript 生成、前端构建及 Linux amd64／arm64 两程序编译已通过。

浏览器原生执行替身明确标为测试数据。Linux 原生命令、systemd、重启恢复和实际放行／阻断仍待既定维护窗口；本批未操作 C091、WSL 或云端 CI。上述本地结果不能代替 Linux 实机验收。
