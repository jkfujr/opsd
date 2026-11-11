# frontend-ds2 · OPSD 工作台

第二套完整设计：同一份 `/api/v1` 契约与 `openapi.json` 生成的类型，另一个视觉世界。
方向由 `ui-ux-pro-max` 的检索结果确定：**Developer Tool 产品 → Minimalism & Swiss + 数据密集控制台**，
配色取「冷白画布 + 深蓝权威 + 单一强调色」，字体配对取 **IBM Plex Sans + JetBrains Mono**。

```powershell
npm install
npm run dev          # http://127.0.0.1:5292 ，默认内置演示数据
npm run typecheck
npm run build        # dist/ 相对路径资源，可直接交给节点程序托管
```

## 与 frontend-ds（三联单）的区别

| | frontend-ds | frontend-ds2 |
| --- | --- | --- |
| 世界 | 三联单 / 货运单据：纸面、印章、骑缝线 | 冷光控制室：瑞士网格、卡片、胶囊状态 |
| 外壳 | 220px 墨色侧轨 + 顶栏 | 64px 顶部导航 + 命令面板（⌘K） |
| 主题 | 暖白 `#f1eee4` + 朱红印章 | 冷白 `#f4f6fa` + 深蓝 `#16325c` |
| 字体 | Archivo + IBM Plex Mono | IBM Plex Sans + JetBrains Mono |
| 圆角 | 2–3px，无投影 | 8–12px，克制投影 |
| 状态 | 旋转印章（`-1.4deg`） | 带点的胶囊徽标 |
| 签名动作 | 单据落章、存根齿孔 | 实时状态条、⌘K 命令面板 |

两套都是真实接口、真实交互、深浅色与 320px–1680px 响应式；`frontend-ds` 保留为打印气质的选项，
本目录是产品化控制台的选项。

## 结构

```
src/
  api/          openapi-fetch + api/openapi.json 生成的类型（与 frontend-ds 同源）
  components/   Icon、Shell（顶栏 + 命令面板）、Operations（操作队列）、ui 原子件
  dev/          演示数据（仅开发环境，构建不包含）
  pages/        概览、节点、容器、数据库、防火墙、存储、设置
  styles/       base（token 与字体）/ shell / parts / pages
scripts/        截图脚本（使用本机 Edge）
```

## 约定

- 颜色只来自 token；绿=正常、琥珀=进行中、红=失败/危险、蓝=交互与链接。
- 数字用 `font-variant-numeric: tabular-nums`；ID、地址、时间用等宽。
- 交互都有 120–200ms 过渡与可见焦点环；`prefers-reduced-motion` 全部关闭。
- 写请求由 `api/client.ts` 统一补 `X-CSRF-Token` 与 `Idempotency-Key`。
