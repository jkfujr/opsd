# 已授权部署参考

## 准备与读取

先确认用户已授权本次目标实例更新。`npm run build` 依次生成接口、构建前端、写入 `frontend/dist/version.txt`、将资源嵌入 Go，并产生 `artifacts/opsd-linux-amd64` 和 `artifacts/opsd-linux-arm64`；使用目标架构的完整工件，不修改二进制中的资源字符串。无需在服务器安装 Node.js 或编译。

从实际实例配置读取 `binary`、`data_dir`、`executor_dir` 等路径，不把默认路径当成现场事实。检查节点队列和 root 操作中 `terminal=0` 的记录；有未结束操作时停止更新。普通用户与 root 服务使用同一程序，停止和恢复两者，不触碰 CouchDB 或其他服务。

前端已有成功构建且输入未变化时，复用 `frontend/dist`，从根 `package.json` 写入 `frontend/dist/version.txt` 后只编译目标架构。产物写入被忽略的 `artifacts/`，使用 `-ldflags='-s -w'` 去掉调试符号可缩小传输文件；完整程序可先用 XZ 压缩，远端解压到独立候选路径再执行 update。普通 SFTP 已出现持续卡顿时，复用该连接结果启用小包方式。传输中断后先读取远端文件状态，续传同一工件。

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
