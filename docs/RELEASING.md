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
