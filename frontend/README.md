# 前端开发

`gpt2/` 为默认内置界面，原 `frontend-gpt2` 的包名、主题 ID 和浏览器存储键保持不变。`legacy/` 保存原内置前端，`variants/` 保存其余独立方案；原源码、依赖锁文件和设计资料均保留。各目录保留自己的依赖，不引入 npm workspace。

## 从仓库根目录运行

```powershell
npm ci
npm --prefix frontend/gpt2 ci
npm run generate
npm run build:frontend
npm run dev:backend
```

隔离 HTTP 服务为 `http://127.0.0.1:5183`，登录入口 `/signin`，账号 `admin`，密码 `test-password`。服务使用嵌入的 gpt2 生产资源，数据只写入根 `artifacts/frontend/gpt2/`；不运行 Linux 执行器。

另开终端启动默认开发界面：

```powershell
$env:OPSD_GPT2_API_URL='http://127.0.0.1:5183'
npm run dev
```

访问 `http://127.0.0.1:5182/signin`。其他方案从根目录按下表独立启动，缺少依赖时先执行 `npm --prefix <目录> ci`：

| 前端 | 开发命令 |
|---|---|
| 默认 gpt2 | `npm run dev` |
| 原内置前端 | `npm --prefix frontend/legacy run dev` |
| frontend2 | `npm --prefix frontend/variants/frontend2 run dev -- --host 127.0.0.1 --port 5173 --strictPort` |
| frontend-cc | `npm --prefix frontend/variants/frontend-cc run dev -- --host 127.0.0.1 --port 5181 --strictPort` |
| frontend-ds | `npm --prefix frontend/variants/frontend-ds run dev` |
| frontend-ds2 | `npm --prefix frontend/variants/frontend-ds2 run dev` |
| frontend-ds3 | `npm --prefix frontend/variants/frontend-ds3 run dev` |
| frontend-gpt | `npm --prefix frontend/variants/frontend-gpt run dev` |

各目录使用 `npm --prefix <目录> run build` 独立构建。演示方案保留原有演示数据和旧 API 契约；它们不是当前 v2 业务接入结果。迁移前运行的 frontend2、frontend-ds 和 frontend-ds2 已按原端口恢复。

## 构建与类型

- `npm run build`：生成接口、构建默认前端、编译 Linux amd64／arm64。
- `npm run build:frontend`：gpt2 类型检查及生产构建，使用 `/` 资源基址，直接写入被忽略的 `backend/internal/webassets/artifacts/`，包含版本和字体许可证。
- `npm run build:backend`：复用上述资源，仅编译双架构程序和安装脚本。接口有改动时先运行 `npm run generate`；前端或根版本有改动时先运行 `npm run build:frontend`。
- `npm --prefix frontend/gpt2 run package`：保持相对资源基址，主题 ZIP 写入根 `artifacts/frontend/gpt2/`。
- 只修改根 `api/openapi.json` 后运行 `npm run generate`。共享类型为 `api/typescript/schema.gen.ts`；gpt2 和 legacy 通过本地类型入口引用，其他演示前端保留各自原有生成类型。

## 默认前端接入

默认内置、独立 ZIP 和内置恢复共用 gpt2 源码。API 始终为同源 `/api/v2`，共享类型来自 `api/typescript/schema.gen.ts`。

| 流程 | 行为 |
|---|---|
| 普通页面 | 业务路由基址 `/`，内置构建资源来自 `/assets/`；独立主题资源使用安装 ID 下的相对路径。 |
| 内置恢复 | `/__opsd/builtin/` 是业务路由基址，普通导航、详情、终端别名和刷新保留恢复前缀。API 与终端票据 URL 不带恢复前缀。 |
| 登录 | 原自定义安全入口提交为 `entrance`；加 `?builtin=1` 时登录后进入恢复首页。恢复深层路径不作为登录入口。 |
| 退出、失效与账号修改 | 使用服务端返回的登录地址，恢复模式保留 `?builtin=1`；清理查询、临时请求引用并释放终端。 |
| 切换主题 | 使用选择资源 ETag，区分配置选择和实际可用主题。启用后留在当前页；点击“刷新界面”进入普通根路径并加载所选主题。 |
| 写入与部分结果 | 使用原版本和请求键；应答未知查询原目标回执或资源。分页保留有效内容、读取状态与游标，可继续读取。离线及读取失败时禁止资源写入。 |

## 交互检查

先运行 `npm run build:frontend`、独立主题打包及 `npm run dev:backend`，再在 gpt2 目录执行：

```powershell
npm run check:ui -- --replace
npm run check:ui -- --builtin --replace
npm run check:ui -- --recovery --replace
```

三种模式分别使用根 `artifacts/frontend/gpt2/integration/`、`integration-builtin/` 和 `integration-recovery/` 保存报告与截图。`--resume` 只复用输入未变的成功场景，受影响场景用 `--rerun=场景名称片段` 强制重跑；新主题包使用 `--replace` 替换本地同版本安装。检查只接受本机地址。React／Vue 切换、自定义入口、账号修改和损坏主题恢复继续使用 [主题浏览器检查](../docs/VALIDATION.md#命令与渠道)。

构建另有主脚本超过 500 kB 的提示，可后续按业务路由拆分加载；本轮保留原打包行为。

## 验证范围

后端现有测试负责页面认证、资源提供和控制台启停。前端检查使用最终 gpt2 内置资源及独立 ZIP；身份、账号、菜单、主题和请求回执经真实本地 HTTP 检查，容器、数据库、防火墙、任务及终端字节场景由浏览器注入状态验证。Linux 系统执行、云端 CI 和生产环境分别记录，旧主题结果不替代新内置恢复交互结果。
