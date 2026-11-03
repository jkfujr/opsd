# 开发

需要 Go 1.26.2、Node.js 24.14.1 和 npm。依赖安装与检查命令见 [验收](docs/VALIDATION.md)。本批通过云端 CI 验证，同提交成功结果复用。

代码边界见 [架构](docs/ARCHITECTURE.md)，界面遵循 [UI 规范](docs/UI.md)。HTTP 只修改 OpenAPI 后生成，产物写入被忽略的 artifacts。版本只修改根 package.json。

提交按实际变更边界组织，提交说明使用简体中文。PR 说明具体行为变化和实际验证范围。凭据、运行资料及本机 AGENTS.md 保留在仓库外。
