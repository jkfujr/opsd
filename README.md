# OPSD

对等节点管理。每台机器安装同一个 `opsd`，提供成员通信、本机管理、监控和可选控制台；修改由目标节点持久受理并执行。普通节点只保存本机运行资料，附近的 CouchDB 保存共享数据和历史。

当前为开发预发布的对等架构，不兼容旧 Hub／Agent 协议和数据库。

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

控制台默认关闭；节点通信默认 TCP 8444，控制台默认 65533。安装输出独立管理员账号、随机密码及登录入口。可使用 `--username`、`--entrance`、`OPSD_PASSWORD` 设置初始账号；`--listen` 和 `--console-listen` 设置监听地址，`--install-dir` 设置程序目录，`--config` 设置实例配置路径。重复安装恢复服务，不覆盖已有身份或账号，不隐式升级。

在节点页打开“添加节点”，可填写名称和地域，点击“生成安装命令”才创建接入码；接入码列表与撤销位于节点页“接入码”弹窗。安装命令支持 `--name` 和 `--region`，名称留空使用目标机器主机名，地域留空保持未设置。新成员主动连接签发节点取得目录；接入码 24 小时有效，成功加入即消费，之后不依赖签发节点在线。

未指定 `--address` 时，安装器优先发现公网 IPv4，再尝试 IPv6；显式地址可使用现有组网网络。防火墙和云安全组需开放节点通信端口。节点详情可分别设置 IPv4／IPv6 的自动或手工公网 IP；自动模式每五分钟及网络变化后更新，冲突或失败保留旧值。

防火墙工具栏进入“设置”，可开启默认关闭的集群互信，为当前及后续加入成员维护主机入站来源放行。规则页的“互信规则”按当前目标节点展示其他成员、精确 IP、共享 IP 关联及实际状态；关闭后持续展示未撤除规则。互信来源可访问主机全部入站端口，共享公网 IP 的其他来源也在此范围。Docker 互信随后续容器功能接入。

安装注册以下服务，均使用同一个程序：

- `opsd.service`：普通用户运行节点、通信、采集和可选控制台。
- `opsd-executor.service`：root 运行本机 Unix Socket 执行器，只接受有类型的操作。
- `opsd-operation@.service`、`opsd-recover.service`：独立执行与恢复防火墙操作。

实例配置默认 `/etc/opsd/instance.json`，节点资料在 `/var/lib/opsd/node`，root 执行与恢复资料在 `/var/lib/opsd/executor`。控制台关闭不停止后台工作。本机账号修改与控制台开关通过本机 Socket 受理：

```bash
sudo /opt/opsd/bin/opsd console --enabled=false
sudo /opt/opsd/bin/opsd account --entrance /new-login-entry
sudo env OPSD_PASSWORD='新密码' /opt/opsd/bin/opsd account
sudo systemctl start opsd-recover.service
```

## 地域存储

普通节点不需要 Docker。仅在存储服务器准备 Docker，并在节点详情启用数据库，填写组网绑定 IP 和端口。程序部署 CouchDB 容器，数据保留在执行器目录下；数据库凭据与控制台密码独立。

默认使用三个独立实例，各节点优先使用指定数据库、同地域数据库，再选择其他可用实例。CouchDB 持续复制任务在实例间异步复制；全部数据库不可达时，目标控制和本机恢复继续，集群历史明确显示不可用。地域断网不等待全局多数派。

配置、操作和账号密文由所属节点生成签名的不可变版本记录。批量上传逐项确认，切换旧副本不覆盖已知新版本。数据库内容不重放系统命令。私钥、浏览器会话和执行恢复资料只留本机。

监控默认每 5 分钟采集一次，保留 7 天；数据库按 UTC 日期分库，过期时先停止对应复制再删除。概览打开时每 3 秒直连获取读数，同一目标共享采集，实时读数不逐次写入数据库。

## 操作与账号

请求由目标节点本地落盘后编号，同一目标按受理顺序串行修改。重复操作 ID 或同一幂等键返回原记录；应答丢失时查询原操作。入口退出、数据库故障不取消目标已受理的操作。读取、取消、确认和恢复独立于后续修改。

防火墙首次在后台导入当前用户入站规则及默认策略，之后由所属节点 SQLite 保存统一配置。页面直接读取保存的数据，按端口规则和 IP 规则分类；保存配置与操作同事务提交，返回待应用，后台立即执行并每 30 秒检查外部改动。运行规则变更保留读回、120 秒确认与定向撤销，停用时只维护持久配置；自动修复只使用已确认版本，失败版本保留修改并手动重试。无法表达的内容、系统基础规则和第三方独立链保留。独立操作服务不依赖控制台存活。

每个节点保留独立账号和会话。密码验证信息与可查看密文分开保存，密文使用 AES-GCM，解密密钥使用 age 多接收方封装。新成员获得授权封装，不复制其他节点私钥。撤销成员拒绝其新请求和后续密码授权，并移除数据库访问；离线成员恢复通信后才能得知撤销，已经获得的旧密码无法收回。

## 文档

- [开发](CONTRIBUTING.md)
- [架构](docs/ARCHITECTURE.md)
- [UI 规范](docs/UI.md)
- [验收命令与最近结果](docs/VALIDATION.md)
- [发布](docs/RELEASING.md)
- [变更记录](CHANGELOG.md)
