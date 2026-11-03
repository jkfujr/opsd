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
