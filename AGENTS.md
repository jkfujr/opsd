# OPSD 开发约定

- 当前设计来源为 `.data/全面架构设计`，按六个实施批次推进；尚未实现的契约不能计为功能完成。
- Go 单模块；控制端不执行宿主机命令，节点协议不依赖控制端或 Agent 服务实现。
- HTTP 类型只修改 `api/openapi.json`，通过 `npm run generate` 生成；禁止手改生成文件或再写前端 DTO。
- 前端遵循 [Appica 组件规则](frontend/node_modules/@appica/ui-react/agent-rules.md)，安装依赖后读取；使用已有组件、角色变量和简短中文文案。
- 本批采用本地验证，不运行云端 CI；成功且输入未变的验证直接复用，完整功能稳定后统一验收与文档收口。
- 运行时模型请求为零；确定性工作由程序执行。
