# 控制台主题

默认内置资源来自 `frontend/gpt2`。内置模式使用绝对资源路径，外部 ZIP 仍使用下述相对路径契约；gpt2 普通与恢复模式、登录和切换行为见 [前端开发](../frontend/README.md#默认前端接入)。

“设置 → 外观”可以上传 ZIP，或填写公开 GitHub 仓库地址安装主题。安装后点击“启用”，再点击“刷新界面”。选择保存在当前节点，对访问该节点控制台的用户生效。其他节点独立管理自己的主题。

主题是独立构建的完整前端，可以使用 React、Vue 或其他技术，直接调用 OPSD HTTP API。主题包包含作者提供的控制台 JavaScript，使用当前管理员的浏览器会话；安装不修改账号、节点队列或后台任务。

## 打包

ZIP 根目录固定为：

```text
theme.json
dist/
  index.html
  assets/
    ...
```

不添加仓库名外层目录，只包含 `theme.json` 和 `dist/`。示例清单：

```json
{
  "format_version": 1,
  "id": "example",
  "name": "示例主题",
  "version": "1.0.0",
  "api_version": 2,
  "min_opsd_version": "0.1.0-dev",
  "login": "builtin",
  "author": "作者",
  "url": "https://github.com/example/opsd-theme",
  "preview": "preview.webp"
}
```

`id` 为 1～64 位小写字母、数字、下划线或短横线，以字母或数字开头，保留 `builtin`。名称不能为空，名称和作者最多 128 字符。主题版本和最低 OPSD 版本使用完整语义化版本；版本前缀 `v` 会被归一化。`login` 省略时为 `builtin`，作者、项目地址和预览图可省略。项目地址使用 HTTPS，预览图为 `dist/` 内存在的相对文件路径。

ZIP 最多 100 MiB，解压后总大小最多 256 MiB，单文件最多 64 MiB；入口 HTML 最多 1 MiB，清单最多 64 KiB，文件和目录合计最多 4096 项。路径不能越界、重复或发生文件与目录冲突，大小写不同的同名项也视为重复；拒绝符号链接、特殊文件和 Windows 保留路径。

发布到公开 GitHub Release 时，附件固定命名为 `opsd-theme.zip`。仓库安装默认读取最新正式 Release；填写标签可以安装指定公开 Release。只有源码压缩包的仓库须先构建并打包，OPSD 不运行仓库构建命令。

## 资源与路由

每次安装生成独立安装 ID，资源位于 `/__opsd/themes/{installation_id}/`，对应主题的 `dist/`。OPSD 在入口的 `<head>` 开头注入 `<base href="资源地址">`，原有脚本和 HTML 保留。入口必须有显式 `head` 元素，不能自行声明 `base`。

构建采用相对资源路径，例如 Vite 配置 `base: './'`；图片、字体和 CSS 内资源也使用相对路径。路由基址保持 `/`：React Router 使用默认根路径，Vue Router 使用 `createWebHistory('/')`。资源基址与业务路由基址分别设置，不用资源前缀作为路由前缀。

已登录时，主题自行处理业务页面及深层路由。`/api/`、`/metrics`、`/assets/` 和 `/__opsd/` 由 OPSD 保留。不存在的主题资源返回 404，不返回 SPA HTML。主题静态资源使用长期缓存，HTML 和管理 API 不缓存。

## 认证与业务调用

浏览器先读取 `GET /api/v2/auth/sessions/current`，使用现有 Cookie 会话。返回 401 时显示登录页；其余错误按实际状态显示。写请求使用会话返回的 `csrf_token` 作为 `X-CSRF-Token`，浏览器同源调用；业务请求按 OpenAPI 使用请求键、ETag 和分页，错误读取 Problem 的 `detail`，通用流程见 [API v2](API.md)。

- `login: builtin`：原登录地址显示内置登录页，登录成功进入主题首页。主题可以只实现登录后的业务页面。
- `login: theme`：原登录地址显示主题入口。主题从 `window.location.pathname` 取得登录入口，向 `POST /api/v2/auth/sessions` 发送 `entrance`、`username`、`password`，201 表示成功，随后进入 `/`。
- 退出使用 `DELETE /api/v2/auth/sessions/current`，随后跳转会话返回的 `login_address`。主题内跳转保留绝对业务路径，API 使用 `/api/v2/...`，避免受资源基址影响。

主题允许只实现部分业务功能，应提供指向 `/__opsd/builtin/` 的链接，让用户访问完整内置界面。资源、操作状态与错误字段以 [OpenAPI](../api/openapi.json) 为准；需要实时刷新时订阅 `/api/v2/events`。写入应答未知时按原键查询 `/api/v2/requests/{request_key}?node_id={所属节点}`，并读取原操作、安装记录或选择，不自动换键重发。

## 管理与恢复

以下接口位于 `/api/v2`，只接受管理员 Cookie 会话，写请求同时检查同源及 CSRF；API 令牌不能调用。

| 接口 | 内容 |
|---|---|
| `GET /console/theme-installations` | 已安装版本、当前选择、实际可用版本及内置入口；分页使用 `items`、`next_cursor` |
| `POST /console/theme-installations` | JSON：`repository`、可选 `tag`；或 `application/zip` 的 ZIP 原始字节。要求 `Idempotency-Key`，成功返回 `201 + Location` |
| `GET /console/theme-installations/{installation_id}` | 单项安装记录与强 ETag |
| `DELETE /console/theme-installations/{installation_id}` | 用单项 GET 的 `If-Match` 删除未启用版本 |
| `GET /console/theme-selection` | 读取配置选择、实际选择、revision 和强 ETag |
| `PUT /console/theme-selection` | JSON：`installation_id`；使用选择 GET 的 `If-Match`，值为 `builtin` 时切回内置 |

列表的 `active_installation_id` 表示配置中的选择，`effective_installation_id` 表示实际可用的主题；空字符串表示内置。文件缺失、大小变化、入口无效或版本不兼容时保留原选择并显示不可用，实际使用内置界面。脚本自身的运行错误通过独立入口恢复。

选择资源以 `installation_id`、`effective_installation_id` 返回 `builtin` 表示内置。API 主版本升级为 2，ZIP 格式版本仍为 1；旧 `api_version: 1` 安装记录和原选择保留，显示不兼容并使用内置界面，新上传的旧 API 包被拒绝。

已登录时打开 `/__opsd/builtin/settings/appearance`，即可切回内置或其他主题。登录页损坏时，在原登录地址添加 `?builtin=1`，例如 `/原安全入口?builtin=1`；内置登录成功后进入 `/__opsd/builtin/`，不加载所选主题。内置入口继续要求有效会话，未登录访问返回 404。

主题文件位于节点数据目录的 `themes/`，记录及当前选择保存在本机 SQLite。安装先校验临时目录，完整发布后登记；启用只切换本机选择。启动时清理未登记安装并恢复未完成删除，主题目录的文件错误不停止内置控制台。当前主题须先切换后再删除；后台任务继续按原契约执行。
