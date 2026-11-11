---
name: "OPSD Monitor"
description: "节点对照与趋势联动的多服务器管理界面"
colors:
  blue-accent-light: "rgb(39 94 174)"
  blue-accent-dark: "rgb(140 183 244)"
  teal-accent-light: "rgb(20 112 92)"
  teal-accent-dark: "rgb(105 205 179)"
  violet-accent-light: "rgb(108 73 171)"
  violet-accent-dark: "rgb(190 162 240)"
  amber-accent-light: "rgb(143 91 17)"
  amber-accent-dark: "rgb(232 184 104)"
  light-primary-foreground: "rgb(255 255 255)"
  light-background-inverse: "rgb(28 36 48)"
  light-foreground: "rgb(51 62 76)"
  light-foreground-subtle: "rgb(86 100 118)"
  light-foreground-strong: "rgb(39 50 65)"
  light-foreground-emphasis: "rgb(25 36 52)"
  light-foreground-inverse: "rgb(248 250 252)"
  light-success: "rgb(21 114 88)"
  light-success-subtle: "rgb(231 244 237)"
  light-success-soft: "rgb(211 236 222)"
  light-warning: "rgb(141 87 16)"
  light-warning-subtle: "rgb(255 245 224)"
  light-warning-soft: "rgb(248 233 200)"
  light-error: "rgb(181 54 57)"
  light-error-subtle: "rgb(255 239 238)"
  light-error-soft: "rgb(249 215 215)"
  light-error-strong: "rgb(156 40 44)"
  light-info-subtle: "rgb(232 241 253)"
  light-chart-cpu: "rgb(52 108 182)"
  light-chart-memory: "rgb(143 87 170)"
  light-chart-fill: "rgb(52 108 182 / 0.05)"
  light-overlay: "rgb(15 22 34 / 0.48)"
  light-terminal: "rgb(20 27 37)"
  light-terminal-text: "rgb(219 229 241)"
  light-terminal-muted: "rgb(165 181 201)"
  light-shadow-modal-base: "rgb(15 22 34 / 0.2)"
  light-preview-light-ground: "rgb(244 247 251)"
  light-preview-light-surface: "rgb(210 220 234)"
  light-preview-dark-ground: "rgb(23 30 41)"
  light-preview-dark-surface: "rgb(56 70 89)"
  dark-primary-foreground: "rgb(19 30 44)"
  dark-background-base: "rgb(24 29 38)"
  dark-background-subtle-base: "rgb(29 35 45)"
  dark-background-muted-base: "rgb(34 41 52)"
  dark-background-strong-base: "rgb(42 51 64)"
  dark-background-inverse: "rgb(231 237 245)"
  dark-workspace-base: "rgb(18 23 31)"
  dark-rail-base: "rgb(23 29 39)"
  dark-foreground: "rgb(202 213 228)"
  dark-foreground-subtle: "rgb(159 175 196)"
  dark-foreground-strong: "rgb(221 230 241)"
  dark-foreground-emphasis: "rgb(236 242 250)"
  dark-foreground-inverse: "rgb(22 29 39)"
  dark-border-base: "rgb(47 57 72)"
  dark-border-muted-base: "rgb(39 48 61)"
  dark-border-strong-base: "rgb(66 81 100)"
  dark-border-emphasis: "rgb(105 124 146)"
  dark-border-intense: "rgb(138 157 181)"
  dark-primary-subtle-base: "rgb(24 31 42)"
  dark-success: "rgb(111 207 173)"
  dark-success-subtle: "rgb(29 49 43)"
  dark-success-soft: "rgb(35 66 52)"
  dark-success-foreground: "rgb(18 41 31)"
  dark-warning: "rgb(235 187 111)"
  dark-warning-subtle: "rgb(55 44 31)"
  dark-warning-soft: "rgb(73 57 35)"
  dark-warning-foreground: "rgb(43 30 13)"
  dark-error: "rgb(243 151 151)"
  dark-error-subtle: "rgb(58 37 43)"
  dark-error-soft: "rgb(78 43 48)"
  dark-error-strong: "rgb(255 177 176)"
  dark-error-foreground: "rgb(49 22 25)"
  dark-info-subtle: "rgb(32 47 68)"
  dark-chart-memory: "rgb(201 161 224)"
  dark-chart-fill: "rgb(140 183 244 / 0.065)"
  dark-shadow-modal-base: "rgb(0 0 0 / 0.4)"
typography:
  headline:
    fontFamily: "Segoe UI, PingFang SC, Microsoft YaHei UI, system-ui, sans-serif"
    fontSize: "1.25rem"
    fontWeight: 600
  brand:
    fontFamily: "Segoe UI, PingFang SC, Microsoft YaHei UI, system-ui, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 650
  section:
    fontFamily: "Segoe UI, PingFang SC, Microsoft YaHei UI, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 600
  title:
    fontFamily: "Segoe UI, PingFang SC, Microsoft YaHei UI, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 600
  body:
    fontFamily: "Segoe UI, PingFang SC, Microsoft YaHei UI, system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 400
  label:
    fontFamily: "Segoe UI, PingFang SC, Microsoft YaHei UI, system-ui, sans-serif"
    fontSize: "0.75rem"
    fontWeight: 400
  reading:
    fontFamily: "Segoe UI, PingFang SC, Microsoft YaHei UI, system-ui, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 600
  code:
    fontFamily: "Cascadia Code, SFMono-Regular, Consolas, monospace"
    fontSize: "0.75rem"
    fontWeight: 400
rounded:
  sm: "calc(0.375rem * 6 / 7)"
  md: "0.375rem"
  lg: "calc(0.375rem * 8 / 7)"
spacing:
  xs: "0.5rem"
  sm: "0.75rem"
  md: "1rem"
  lg: "1.25rem"
  page: "1.5rem"
components:
  button-primary-dark:
    backgroundColor: "{colors.blue-accent-dark}"
    textColor: "{colors.dark-primary-foreground}"
    rounded: "{rounded.sm}"
    height: "2.5rem"
  button-primary-light:
    backgroundColor: "{colors.blue-accent-light}"
    textColor: "{colors.light-primary-foreground}"
    rounded: "{rounded.sm}"
    height: "2.5rem"
  panel:
    rounded: "{rounded.md}"
    padding: "{spacing.md}"
---

# Design System: OPSD Monitor

## Overview

**Creative North Star: "节点对照工作面"**

为长期管理多台服务器的管理员提供紧凑的资源对照界面。通过稳定的列、清晰的对象名称和相邻的趋势读数组织工作，颜色承担操作、选择与状态的区分。

**Key Characteristics:**

- 节点资源、趋势与操作目标关系明确。
- 四套配色共享布局、字体和组件；深浅模式完整对应。
- 状态与数据有效范围保留，说明文案仅在影响操作判断时出现。

## Colors

实际颜色来源为 `src/palettes.css`，控件只消费角色变量。前置颜色记录该文件使用的实色和混色基底；背景、导航、选中区域和边框通过角色变量及 color-mix 生成，不在组件中重复定义颜色。

雾蓝为默认配色，另有青绿、鸢紫、琥珀；每套均有深浅模式。主色承担主要操作、焦点和选中状态。正常为绿、警告为黄、失败为红；CPU 和网络序列使用蓝色，内存序列使用紫色，跨配色保持稳定。

## Typography

正文和表格为 14px，辅助值为 12px，页面标题为 20px，资源汇总主值为 24px。品牌名为 18px，浮层标题和集群名可使用 16px。数字等宽；命令、日志和地址采用等宽字体，其他界面使用中文系统无衬线字体。

## Layout

桌面侧栏 208px、顶栏 56px，页面外距 24px。间距主要使用 8／12／16／20／24px。七个导航入口及排序功能共用桌面和手机实现；详情采用对象路径，每页保留一组主标题与工具栏。

首页分为监控区和节点表两层，间距 16px。1440px 起资源趋势与需关注共用一个外框，以竖分隔线区分，需关注宽 320px，趋势使用剩余宽度；异常列表最大高 272px，更多事项在区域内滚动。节点表独占下一行，保留正常行高，筛选控件在空间充足时与标题同行。

768–1439px 的趋势占满宽度，需关注位于其下方，事项使用可换行的三列列表。首页图表、加载占位及空状态至少高 240px，768px 以下为 224px；手机保留“节点／趋势”切换，趋势视图中先显示曲线，再显示自然展开的需关注列表。节点详情图表保持 164px。1024px 以下导航使用抽屉；复杂表格和页签仅在自身区域滚动。

## Elevation & Depth

静态工作区域通过背景和细分隔线区分；弹窗、菜单和提示使用有偏移的柔和阴影。浮层背景遮罩不使用装饰性模糊。状态过渡为 160／220ms，尊重减少动态效果偏好，页面直接进入内容。

## Shapes

基础半径为 0.375rem；控件、面板和浮层使用 Appica 的半径角色。页签工作区采用平面分隔，减少嵌套容器。图标沿用统一 SVG 线条体系。

## Components

- 按钮与输入：桌面主要控件高 40px，窄屏主要操作目标高 44px；主按钮表达当前动作，错误与对应字段建立无障碍关联。
- 节点对照：名称进入详情，独立趋势操作在同页选择对象，并将资源趋势标题滚动到可见位置、移入键盘焦点；减少动态效果模式使用即时定位。普通筛选和清除选择不触发定位，筛选变化移除范围外的预览。地址支持复制，长名称可读取完整值。
- 图表：24 小时／7 天、资源／网络切换；显示当前节点范围，支持悬停和方向键读数。首页宽屏工具栏将标题、节点范围和切换操作组织为一行，长名称可省略并提供完整名称；有效节点数与采集频率位于图例行。首页使用独立布局参数，节点详情继续采用默认布局。
- 状态：语义文字与颜色共同表达，未知结果通过查询原操作核对。
- 浮层：短表单和风险确认使用对话框，详情与操作记录使用右侧抽屉；正文滚动，保留关闭和提交入口。
- 外观：颜色模式与配色分别选择并即时生效，偏好保存在当前浏览器；其他标签页可同步配色。菜单顺序维持原有保存操作。
- 加载与异常：首次读取使用骨架；部分失败保留最近数据；离线保持只读；空数据、筛选无结果分别呈现。

## Do's and Don'ts

### Do:

- 使用 Appica 控件和角色变量，颜色定义集中在 src/palettes.css。
- 保留字段、单位、采集时间、操作目标与恢复入口。
- 节点缺值显示“—”，状态同时使用文字和颜色。
- 保留键盘焦点，浮层正文滚动且提交入口始终可访问。

### Don't:

- 给标题添加重复介绍，或在弹窗和反馈里重复解释演示实现。
- 以重复面板和空容器增加层级。
- 随配色改变正常、警告、失败和图表序列的语义。
- 为原型发送后端请求或执行系统命令。
