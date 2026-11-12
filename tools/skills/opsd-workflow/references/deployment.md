# 已授权部署参考

## 准备与读取

先确认用户已授权本次目标实例更新。`npm run build` 依次生成接口、构建 `frontend/gpt2`、写入 `backend/internal/webassets/artifacts/version.txt`、将资源嵌入 Go，并产生 `artifacts/opsd-linux-amd64` 和 `artifacts/opsd-linux-arm64`；使用目标架构的完整工件，不修改二进制中的资源字符串。无需在服务器安装 Node.js 或编译。

从实际实例配置读取 `binary`、`data_dir`、`executor_dir` 等路径，不把默认路径当成现场事实。检查节点队列和 root 操作中 `terminal=0` 的记录；有未结束操作时停止更新。普通用户与 root 服务使用同一程序，停止和恢复两者，不触碰 CouchDB 或其他服务。

受管代理策略会进入签名目录。逐台更新全部有效成员后，再通过能力接口确认 `proxy_routing_version=1`，最后逐台预览并接管代理。节点程序更新与实际监听修复、防火墙接纳、代理策略应用分别读取结果；C041 使用既有停服 `network` 流程，C071 只从已预览的受管代理路由移除，服务器与数据库保持原值。

前端已有成功构建且输入未变化时，复用 `backend/internal/webassets/artifacts`，运行 `npm run build:backend` 编译双架构。只编译目标架构时从根目录使用 `go -C backend build`，输出位置按根目录产物路径指定。前端或根版本变化后先运行 `npm run build:frontend`。产物写入被忽略的 `artifacts/`，使用 `-ldflags='-s -w'` 去掉调试符号可缩小传输文件；完整程序可先用 XZ 压缩，远端解压到独立候选路径再执行 update。普通 SFTP 已出现持续卡顿时，复用该连接结果启用小包方式。传输中断后先读取远端文件状态，续传同一工件。

`scripts/opsd_remote.py` 使用本机 OpenSSH 和远端 Python 3。相对路径均从项目根目录解释；实例配置通过 `--config` 指定。默认动作为只读 inspect：

```powershell
py tools/skills/opsd-workflow/scripts/opsd_remote.py inspect --host root@服务器 --identity F:/keys/服务器 --config /etc/opsd/instance.json
```

它通过 SSH 标准输入发送 UTF-8 Python 源码，避免 PowerShell 文本管道转码或补丁 CRLF。输出仅含实例路径、节点名称及地址、未结束数量、服务状态和程序版本，不读取或打印账号密码。

## 续传与更新

```powershell
py tools/skills/opsd-workflow/scripts/opsd_remote.py upload --host root@服务器 --identity F:/keys/服务器 --local-file artifacts/opsd-linux-amd64 --remote-file /opt/opsd/bin/opsd.next
py tools/skills/opsd-workflow/scripts/opsd_remote.py update --host root@服务器 --identity F:/keys/服务器 --config /etc/opsd/instance.json --candidate /opt/opsd/bin/opsd.next
```

upload 先读取候选路径，首次使用 SFTP `put` 创建文件，已有普通文件使用 `reput` 续传，不碰正在运行的程序。只对同一来源工件和已确定归属的候选路径续传；工件变化时使用新的候选路径。传输失败不自动重发，读取错误后再决定是否续传。

update 在停服前及停服后检查未结束操作，设置候选文件可执行权限并读取版本，再原位替换程序，最后恢复 `opsd-executor.service` 和 `opsd.service` 并读回运行状态。不重新安装，不覆盖配置、身份、账号或数据库。

SSH 中断或应答丢失后先运行 inspect，核对程序版本、服务 PID／启动时间与操作状态；不能因没看到成功文字就再次 update。失败时脚本非零退出，必须按实际结果处理，不声称已回滚。需要还原时另行使用明确已知的旧工件，不自动新建备份或清理历史工件。

普通 SSH／SFTP 确实失败并已定位为传输问题时，可在上述命令增加 `--small-packets`。此模式通过随附 Node.js TCP 代理分块发送，需本机 Node.js；不作为默认连接方式。不要用长文本补丁规避完整构建，也不要把 Windows 换行直接送给远端 shell。

成功后复用任务要求的实际入口／功能检查，区分程序与服务状态、登录和页面交互、规则修改与恢复结果。辅助脚本本身的验证使用临时文件和替代命令，不为验证 skill 操作线上服务。

浏览器登录成功后会替换整页；检查脚本应等待新文档加载并出现主页面，再导航到业务页。链路缓慢时，从实例实际网卡与监听读取同一服务的其他已开放入口，确认资源与节点身份后继续检查。

API 检查先读回原开关，使用临时的限定资源令牌核验 JSON、指标与越权结果，结束后撤销令牌并恢复原开关；截图遮盖令牌及含令牌的接入配置。数据库实例发现与集群登记、业务管理写入分别记录验收结果。

## 现有数据库集群接入

从当前服务器清单取得宿主机、组网地址、节点名称与 SSH 密钥位置，先读取实际实例配置、服务、端口及节点身份；保留同名历史退出容器。各 Galera 成员的 UUID、Primary、Ready、Synced 状态一致后，再登记同一集群。

每个数据库及代理宿主机运行所属 OPSD 节点，连接只使用本机回环地址或 Socket。缺少 OPSD 时使用同提交成功工件加入现有集群，显式配置已有组网地址；网页、API 及 CouchDB 承载保持关闭。现有实例使用 update，保留原入口、账号及身份。安装器会输出新账号凭据，自动化须捕获输出并只报告安装状态。

通过现有管理员接口发现实例，选择运行中的实际容器 ID；MariaDB 凭据复用 Docker 配置及已有私有配置，ProxySQL 复用其本机管理配置。补齐实际管理、备份、SST、健康检查和监控账号的保护名单，连接密码保持原值并加密保存在所属节点。

先查询协调节点及 Galera UUID 对应的已有集群，再使用固定请求键登记或按当前版本补齐成员与代理。接入只修改 OPSD 登记资料，读取实际库、账号、代理主机组和备份状态；既有备份失败、配置中的缺失成员及历史退出容器分别保留并报告。接入或安装应答未知时，先读取成员目录、目标身份、实例及接入码记录，按原身份继续处理。

按成员补齐同一登记，已完成的成员和代理直接复用；某个成员读取失败时，先保存已确认可用的本机成员，再按原集群 ID 和当前版本继续。数据库发现读取包含元数据查询，超时时分别核对本机执行器、实际 SQL 和节点通信；记录真实错误，不反复提交登记。线上页签检查复用现有 CI 的等待方式：跳过已选页签，切换后等待对应数据出现。

跨节点结果持续为 `stale` 时，先比较来源采集时间、请求时间、各节点时钟与时间同步服务；服务健康和读取状态分别核对。时钟修复按本次服务器维护授权执行，保留实际采集时间。

规则比较忽略计数器，分别核对外部原生链、普通规则内容和普通规则的相对顺序；按策略已转交的精确 IP 规则单独记录。发现范围或复合端口规则在导入后移动时，如实记录具体规则与前后位置。

切换公网前，通过现有防火墙入口配置 IPv4／IPv6 的实际通信端口，并确认原操作成功。随后在队列空闲时停止节点和执行器，用本机 `network` 设置通配监听及自动地址模式，再恢复两个服务；核对成员目录公布的 URL、实际监听、外网 TCP、`/peer` WebSocket 和添加节点弹窗中的接入地址。新节点实际安装加入与服务端入口连通分别记录，公开安装包继续沿用独立发布流程。

## 1Panel 替代后的维护

C001／C041／C052／C091 已移除面板程序和服务，应用仍由原 Docker 容器运行。维护前从当前实例读取容器、挂载与规则；`/1panel` 下的应用、数据库、配置、日志及 swap 继续保留。停用面板须先完成对应任务、证书和规则的替代，确认没有运行中的业务任务，再停止调度；实际访问正常后，仅移除已核对的系统服务、面板二进制和命令软链。

C091 的任务 ID 为 `recording-move`，时区 Asia/Shanghai、每天 02:30、目录 `/`；脚本和 `/root/.config/rclone/rclone.conf` 保留原内容。再次迁移时先读回已有任务和旧调度状态，按原版本保存为停用任务，确认旧任务未运行后切换，再读取新计划和运行记录。原移动脚本只按业务计划执行，检查配置时不手动触发。

C011 Certimate 工作流 `c001renewal0001` 每天 01:25 检查 `kagura-mea.com` 及通配证书，复用已有 DNS 凭据和专用 SSH 账号 `certimate-deploy`。C001 的 `/usr/local/libexec/certimate-c001-deploy` 源码为 [certimate_c001_deploy.py](../scripts/certimate_c001_deploy.py)，由 root 管理；sudoers 只允许该入口。脚本从 `/var/lib/certimate-deploy/incoming` 读取交付文件，核对密钥、有效期和域名后替换 OpenResty 与 PMail 的原证书，保留权限和所有者；nginx 配置检查失败时恢复原文件。实际服务已加载同一证书时跳过重载，否则分别读回 443／465 后确认结果。维护后核对工作流原运行与实际 TLS，不因应答未知重复签发或重启。

C041 的普通规则配置为 `/etc/opsd/firewall/inherited.json`，按命令保存 `chains` 与 `references`；恢复入口为 `/usr/local/libexec/opsd-inherited-firewall`，源码为 [restore_panel_rules.py](../scripts/restore_panel_rules.py)。`opsd-inherited-firewall.service` 在 Docker、Galera 防火墙后、OPSD 执行器前启动并保持启用。只恢复配置中的 `1PANEL_*` 固定链及引用，使用 `--noflush`，读回一致时不修改；已有内容不一致时停止并报告。OPSD 互信、Docker 与 CODEX_GALERA 留在各自所属入口，普通 iptables 规则继续只读。重启恢复的整机验收与隔离网络中的规则恢复分别记录。
