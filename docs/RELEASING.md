# 发布

版本仅维护在根 `package.json`。构建注入程序与前端，输出 `artifacts/install.sh`、`artifacts/opsd-linux-amd64`、`artifacts/opsd-linux-arm64`。

PR 与 main 推送触发 CI；成功后推送同提交 `v<version>` 标签触发 Release。Release 复用同 SHA 成功的 CI 工件，先建立草稿、上传资产，再公开发布。带连字符的版本标为预发布。

```bash
git push origin main
git tag v0.1.0-dev
git push origin v0.1.0-dev
```

实际发布须有本次任务授权。失败后先读取运行、草稿和资产；已有同名资产下载比较后复用。固定标签与资产保持原内容。修复需要新版本或在尚未发布时修正提交，避免替换已公开版本。

Release 作业验证公开安装与重复安装；生产部署另按明确指令执行。源码与发布验证结果写入 VALIDATION.md。

## 节点通信切换

先核对控制台接入命令对应的发布工件，确保安装器能下载支持私有 TCP 的版本。逐台更新到兼容版本后，读取节点和执行器的启动状态，确认原地址及备用端点继续公告。兼容版本默认同时接收 WebSocket 与私有 TCP。全部有效成员都具备两种接收能力后，再逐台更改已公布地址。先确认原端点对应的主机及端口也接收私有 TCP；端口经 HTTP 代理的端点须换成可直连地址。

```bash
sudo systemctl stop opsd.service
sudo /opt/opsd/bin/opsd network --address-scheme tcp
sudo systemctl start opsd.service
```

切换后读取签名成员目录和实时概览。生产顺序为 C091、C041，随后 C001、C052，最后 C054。检查 C091 读取 C041 的实时结果，再扩大范围。五台通过 TCP 互读、接入和访问授权终端后，逐台关闭旧接收入口。

```bash
sudo systemctl stop opsd.service
sudo /opt/opsd/bin/opsd network --peer-transport tcp
sudo systemctl start opsd.service
```

从 C091 浏览器检查概览请求的目标，只应看到入口 C091。通过节点通信核对四个远端数据来源。入口使用标准 HTTP 时，会话及 Bearer 流量仍有告警 #21、#22 记录的明文传输风险。

单节点恢复兼容监听时，在停止节点服务后使用 `opsd network --peer-transport dual`。需要回退旧程序时，先用兼容程序恢复全部原 WebSocket 主地址和备用端点，在各有效节点读回公告，再更换程序工件。整组旧地址使用同一协议时可用 `--address-scheme http` 或 `--address-scheme https` 恢复。写入结果未知时，先查原请求或操作 ID。

## I7 生产工件重建

五节点工件 `44bc74bcf69a` 的源码提交为 `ffaccfd3913127170aea002ee72491867a088240`。2026-11-28 在独立 WSL `OPSD-CI` 干净检出该提交，按锁文件重新生成内置前端和 Swagger UI。生成的追踪文件无差异，二进制及 xz 压缩件均与发布时的字节一致。

工具版本为 Go 1.26.2、Node.js 24.14.1、npm 11.11.0 和 XZ Utils 5.4.5。构建注入的 `buildinfo.Commit` 是原发布时的标记，重建时使用下列原值。内置资源的输入摘要为各自目录内按相对路径排序的 `sha256sum` 清单摘要。

| 项目 | SHA-256 |
| --- | --- |
| 内置前端清单 | `7c9a5d828e37081685172e78908234d0064e962af5fa5d2dc7f071ac16d41208` |
| Swagger UI 清单 | `ead3fe34bff8843b9f3484e81c3c084994b0e70883a738c1da3653000bdde20d` |
| Linux amd64 工件 | `44bc74bcf69a9c7ac8df731f66e7ab9ba86424e1ef7ffd3709cae8dbf6d6afae` |
| xz 传输工件 | `1a7796e7ab35ee8230bb8a3edd077e99bff713b29710a3230b39cbbfb2c826e2` |

在该提交的独立检出根目录执行：

```bash
npm ci
npm --prefix frontend ci
npm run generate
npm run build:frontend
mkdir -p artifacts
version=$(node -p 'require("./package.json").version')
stamp=5c215eda15de4c9cc4b4c6c655f490ac31dc155d+historyfix.b1801d8470fa+dockertimeout.68ce1cdd14f2
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C backend build -trimpath -buildvcs=false \
  -ldflags "-s -w -X github.com/jkfujr/opsd/internal/buildinfo.Version=$version -X github.com/jkfujr/opsd/internal/buildinfo.Commit=$stamp" \
  -o ../artifacts/opsd-linux-amd64 ./cmd/opsd
xz -T2 -3 -c artifacts/opsd-linux-amd64 > artifacts/opsd-linux-amd64.xz
sha256sum artifacts/opsd-linux-amd64 artifacts/opsd-linux-amd64.xz
```

## 共享数据库切换

本批新增节点迁移 `007_shared_storage.sql`、执行器迁移 `008_resource_claims.sql` 和共享管理契约版本 1；构建状态同时报告提交 SHA、节点协议与持久操作契约版本。先将全部有效成员更新到支持共享管理的程序，再通过数据库页面或本机 `opsd data init --cluster-id '集群 ID'` 启动持久迁移。

切换期间暂停新的共享管理写入和数据库流程推进，导入后核对目录、固定步骤、请求回执和待上传记录，全部完成后启用共享库。保留原 SQLite 与 CouchDB 历史，备份范围加入 `opsd_core`。迁移中断使用原操作及 `opsd data recover`，同 ID 内容冲突保留现场。旧程序、旧节点数据库及旧共享副本的恢复先核对共享版本和原操作结果，再恢复写入。
