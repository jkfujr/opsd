# 开发

需要 Go 1.26.2、Node.js 24.14.1 和 npm。依赖安装与检查命令见 [验收](docs/VALIDATION.md)。本批通过云端 CI 验证，同提交成功结果复用。

Go 单模块位于 `backend/`，根目录通过 `go -C backend` 调用。代码边界见 [架构](docs/ARCHITECTURE.md)，前端入口、分工和命令见 [前端开发](frontend/README.md)。HTTP 只修改 OpenAPI 后生成，共享 TypeScript 类型位于 `api/typescript/`，产物写入被忽略的 artifacts。版本只修改根 package.json。

提交按实际变更边界组织，提交说明使用简体中文。PR 说明具体行为变化和实际验证范围。凭据、运行资料及本机 AGENTS.md 保留在仓库外。
