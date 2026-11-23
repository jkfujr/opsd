# OPSD 前端

`frontend/` 是仓库中唯一的前端,也是默认内置界面,主题名 **OPSD**,直接对接同源 `/api/v2`。它同时可以打包成独立主题安装。设计理念、交互与视觉规范、动效参数、关键取舍和开发约定见 [DESIGN.md](DESIGN.md);修改界面前请先阅读。

目录已铺平到 `frontend/` 根:源码在 `src/`,`theme.json` 为主题清单,`index.html` 与 `vite.config.ts` 为构建入口。

## 设计要点

| 方面 | 做法 |
|---|---|
| 导航 | 68px 图标侧栏,悬停浮出完整标签,可固定;`⌘/Ctrl + K` 命令面板搜索节点、容器、数据库、页面和命令;`G` + 字母快速跳转 |
| 操作 | 所有写入统一走请求键;202 受理的操作进入右下角“操作坞”实时追踪,防火墙待确认带倒计时环;结果未知时保留原键,只能“核对原请求”,绝不换键重发 |
| 实时 | 订阅 `/api/v2/events` 精确失效查询,断线指数退避重连后整体重读;概览每 3 秒采样并在前端保留环形缓冲,驱动火花线与实时曲线 |
| 动画 | Motion 弹簧:共享布局指示器(导航、页签、分段控件、命令面板选中项)、列表交错入场、侧板/对话框弹性进出、数字缓动、主题切换圆形扩散(View Transitions) |
| 视觉 | OKLCH 色板,单一强调色可在设置里调色相;深浅色感知亮度一致;Inter + JetBrains Mono,中文回退系统字体(不打包大体积 CJK 字体) |
| 可达性 | 尊重 `prefers-reduced-motion`;键盘可达;焦点环;`aria` 角色 |

## 功能范围

概览 · 节点(列表、详情、配置、公网地址、节点账号、改名、撤销、接入命令) · 监控(五分钟趋势、秒级归档、采集状态) · 容器(启停、重启、更新镜像、详情、实时日志) · Compose(登记、拉取、启停、重建、更新) · 数据库(集群健康、新建集群向导、库、账号与授权、代理连接池、备份) · 存储(S3 目标、凭据、连接检查) · 防火墙(规则增删改、拖拽排序、默认策略、组件安装、接纳现场配置、集群互信) · 终端(多会话 xterm,切页不断开) · 定时任务(编辑器、cron 预设与描述、立即执行、执行记录与日志) · 操作记录 · 设置(账号、会话、API 令牌、外观、控制台主题、版本)。

接入已有数据库集群、为集群添加成员与代理、代理路由、集中监控部署、OPSD 数据(共享存储)管理暂无界面,需通过 `/api/v2` 完成,见 [DESIGN.md](DESIGN.md) 3.3 节。

## 从仓库根目录运行

```powershell
npm ci
npm --prefix frontend ci
npm run generate
npm run build:frontend
npm run dev:backend
```

隔离 HTTP 服务为 `http://127.0.0.1:5183`,登录入口 `/signin`,账号 `admin`,密码 `test-password`。服务使用嵌入的生产资源,数据只写入根 `artifacts/frontend/`;不运行 Linux 执行器,终端、Docker、防火墙与数据库在此环境下不可用。

另开终端启动开发界面:

```powershell
npm run dev
```

访问 `http://127.0.0.1:5184/signin`。后端地址可用 `OPSD_API_URL` 覆盖,内置资源基址可用 `OPSD_BASE` 覆盖。

## 构建与检查

| 命令 | 说明 |
|---|---|
| `npm --prefix frontend run build` | 生产构建到 `frontend/dist`(相对资源基址) |
| `npm --prefix frontend run typecheck` | TypeScript 严格类型检查 |
| `npm --prefix frontend run check` | 类型检查 + 构建 |
| `npm --prefix frontend run package` | 构建并打包主题 ZIP 到 `artifacts/frontend/opsd-theme.zip` |
| `npm run build:frontend` | 类型检查后以 `/` 资源基址输出到 `backend/internal/webassets/artifacts`,并复制字体许可证 |
| `npm run build:backend` | 复用上述资源编译双架构程序和安装脚本 |
| `npm run build` | 依次执行接口生成、内置前端构建、Linux amd64/arm64 编译 |

接口有改动时先运行 `npm run generate`;前端或根版本有改动时先运行 `npm run build:frontend`。只修改根 `api/openapi.json` 后运行 `npm run generate`,本前端通过 `src/api/types.ts` 引用 `api/typescript/schema.gen.ts`,不手写 DTO。

## 内置界面与恢复

| 流程 | 行为 |
|---|---|
| 普通页面 | 路由基址 `/`;工具类页面位于 `/tools/terminal`、`/tools/tasks`、`/tools/operations`,与服务端页面白名单一致;旧地址 `/terminal`、`/tasks`、`/operations` 保留查询参数与锚点跳转 |
| 内置恢复 | `/__opsd/builtin/` 为路由基址,导航、刷新与深层路径保留恢复前缀;API 与终端票据地址不带前缀 |
| 登录 | 提交原安全入口为 `entrance`;成功后整页跳转,由服务端决定提供主题还是内置界面;带 `?builtin=1` 时进入恢复首页或 `return_to` 指定的恢复页 |
| 退出、失效与账号修改 | 跳转服务端返回的登录地址,恢复模式追加 `?builtin=1`;失效时 `return_to` 保留路径、查询与锚点 |
| 主题 | “设置 → 外观 → 控制台主题”上传 ZIP 或从 GitHub Release 安装、启用、切回内置与删除;上传应答未知时只允许“重新读取”,不自动重传;切换后点击“刷新界面”进入普通根路径 |

## 作为主题安装

主题包根目录只有 `theme.json` 与 `dist/`,`login: theme` 表示安全入口直接显示本界面的登录页。在任意界面“设置 → 外观 → 控制台主题”上传 ZIP 并启用;出问题时访问 `/__opsd/builtin/` 或在登录地址后加 `?builtin=1` 回到内置界面。

## 验证

`npm --prefix frontend run check` 完成类型检查与生产构建。主题、内置恢复、双登录模式、账号修改与会话失效由 `OPSD_THEME_UI=1 go -C backend test ./internal/control -run '^TestConsoleThemeBrowser$'` 在生产资源上验证(需 Playwright,经 WSL 运行)。页面级检查由仓库根 `node scripts/check-frontend.mjs` 在隔离本地服务上覆盖登录页字段可访问名称、整页登录落地、页面白名单路由、旧地址带参数与锚点重定向、白名单外 404、外观主题管理与内置恢复,结果写入 `artifacts/frontend-check/result.json`;容器、数据库、防火墙、终端等仍需在 Linux 实际安装上验收。
