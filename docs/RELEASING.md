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

## 共享数据库切换

本批新增节点迁移 `007_shared_storage.sql`、执行器迁移 `008_resource_claims.sql` 和共享管理契约版本 1；构建状态同时报告提交 SHA、节点协议与持久操作契约版本。先将全部有效成员更新到支持共享管理的程序，再通过数据库页面或本机 `opsd data init --cluster-id '集群 ID'` 启动持久迁移。

切换期间暂停新的共享管理写入和数据库流程推进，导入后核对目录、固定步骤、请求回执和待上传记录，全部完成后启用共享库。保留原 SQLite 与 CouchDB 历史，备份范围加入 `opsd_core`。迁移中断使用原操作及 `opsd data recover`，同 ID 内容冲突保留现场。旧程序、旧节点数据库及旧共享副本的恢复先核对共享版本和原操作结果，再恢复写入。
