# 设计研究与已确认的选择

## 问题与方向

原型的 14px 正文与分组标题过于接近，12px 辅助字被用于大量业务信息，弱化文字角色覆盖面过广。主色同时参与工作区、侧栏和边框，导致整页泛色。宽表自动铺开后，字段之间距离大而文字偏小。

新原型采用中性背景、真实字重与较大的尺度；首页观察全局趋势，节点与容器保持可直接进入的对象关系。终端从详情中的有限区域改为独立工作区。用户已确认本方案，按其指定参考执行。

## 项目源码

| 项目 | 技术与相关实现 | 借鉴 |
|---|---|---|
| [Mosona Web](https://github.com/mosona-labs/mosona-manager-web/tree/a0ef78d7f656f79c4ad020dd8d52bef26796bf97) | React、TypeScript、Vite、Tailwind、shadcn/Radix、Recharts、xterm；src/index.css、page/dashboard/components/card.tsx、page/session/index.tsx | 中性背景、名称与读数、面板边界、独立会话页面 |
| [Komari Web](https://github.com/komari-monitor/komari-web/tree/05c3bad5dedf42796aae3cc0a528f474a0860fc0) | React、Radix Themes、Recharts、xterm；main.tsx、admin/dashboard、terminal | 间距角色、排行、终端标签、尺寸适配 |
| [AstrBot v4.25.5](https://github.com/AstrBotDevs/AstrBot/tree/v4.25.5/dashboard) | Vue、Vuetify、Pinia、Sass、ApexCharts；views/stats/StatsPage.vue | 标签、主值和分组清晰，统计页独立的排版规则 |

参考截图与检索时的当前源码版本可能不同。实现吸收已确认的视觉和交互规律，不声称逐像素复制。

## 技能取舍

- Impeccable：Operate 模式，任务、可读性、常用交互优先；用户指定的 Mosona 方向优先于随机方向。
- UI/UX Pro Max：用于可读性、焦点与具体组件查询。两次自动设计系统结果混入营销结构，相关结构未采用。
- [interface-design](https://github.com/Dammyjay93/interface-design)：后台层级、密度、尺寸与一致性。
- [Anthropic frontend-design](https://github.com/anthropics/skills/tree/main/skills/frontend-design)：根据实际产品内容作出视觉选择。
- [Vercel web-design-guidelines](https://github.com/vercel-labs/agent-skills/tree/main/skills/web-design-guidelines)：键盘、焦点、长内容、主题与反馈。
- [baseline-ui](https://github.com/ibelick/ui-skills/tree/main/skills/baseline-ui)：组件复用、数字排版与状态。未照搬其技术栈约束。

## 实施边界

第一批是独立视觉与交互原型，只覆盖 PRODUCT.md 列出的样本。未列入的数据库、存储、防火墙业务页面继续保留在旧原型。新目录不修改其代码、API 或生产打包方式。真实服务器会话、原会话断线恢复、文件管理与多窗格不属于本批。
