# OPSD

多节点防火墙与容器管理。当前已完成工程基础，以及安装、账号、节点、独立首页和设置页的本地实现与验收。主机防火墙、容器、互信和部署迁移按后续批次实施。

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

本批使用本地渠道，已完成账号实际 HTTP 调用、真实 WebSocket／Noise 接入、Agent 心跳与重连、临时目录安装恢复、真实 API 浏览器操作及 Linux amd64／arm64 编译。浏览器使用专用临时数据与本机进程；这些结果不代表 Linux systemd、跨主机部署或防火墙流量已通过实机验收。
