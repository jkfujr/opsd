---
name: OPSD 新前端
description: 中性背景、清楚读数与放大尺度的服务器管理界面
colors:
  primary: "rgb(245 245 245)"
  primary-ink: "rgb(18 18 18)"
  workspace-dark: "rgb(10 10 10)"
  rail-dark: "rgb(10 10 10)"
  panel-dark: "rgb(18 18 18)"
  overlay-dark: "rgb(24 24 24)"
  selected-dark: "rgb(38 38 38)"
  border-dark: "rgb(255 255 255 / .1)"
  input-border-dark: "rgb(255 255 255 / .15)"
  text-dark: "rgb(245 245 245)"
  secondary-text-dark: "rgb(163 163 163)"
  terminal: "rgb(0 0 0)"
  workspace-light: "rgb(247 247 248)"
  panel-light: "rgb(255 255 255)"
  border-light: "rgb(221 221 224)"
  text-light: "rgb(27 27 30)"
  secondary-text-light: "rgb(86 86 92)"
  success-dark: "rgb(90 211 163)"
  warning-dark: "rgb(238 188 92)"
  error-dark: "rgb(248 129 136)"
  network-dark: "rgb(77 206 164)"
  cpu-dark: "rgb(123 163 241)"
  memory-dark: "rgb(194 148 237)"
typography:
  display:
    fontFamily: "Inter, Source Han Sans SC, sans-serif"
    fontSize: "2rem"
    fontWeight: 650
    lineHeight: 1.25
    letterSpacing: "-0.03em"
  headline:
    fontFamily: "Inter, Source Han Sans SC, sans-serif"
    fontSize: "1.75rem"
    fontWeight: 650
    lineHeight: 1.35
    letterSpacing: "-0.025em"
  title:
    fontFamily: "Inter, Source Han Sans SC, sans-serif"
    fontSize: "1.25rem"
    fontWeight: 600
    lineHeight: 1.4
  body:
    fontFamily: "Inter, Source Han Sans SC, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "Inter, Source Han Sans SC, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 500
    lineHeight: 1.5
  meta:
    fontFamily: "Inter, Source Han Sans SC, sans-serif"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1.5
  terminal:
    fontFamily: "JetBrains Mono, Sarasa Fixed SC, monospace"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.25
  terminal-sarasa:
    fontFamily: "Sarasa Fixed SC, monospace"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.25
rounded:
  xs: "0.25rem"
  sm: "0.375rem"
  md: "0.5rem"
  lg: "0.75rem"
spacing:
  xs: "0.5rem"
  sm: "0.75rem"
  md: "1rem"
  lg: "1.5rem"
  xl: "2rem"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-ink}"
    rounded: "{rounded.sm}"
    padding: "0 1rem"
    height: "2.75rem"
  button-outline:
    backgroundColor: "{colors.panel-dark}"
    textColor: "{colors.primary}"
    rounded: "{rounded.sm}"
    padding: "0 1rem"
    height: "2.75rem"
  button-icon:
    textColor: "{colors.secondary-text-dark}"
    rounded: "{rounded.sm}"
    width: "2.75rem"
    height: "2.75rem"
  panel:
    backgroundColor: "{colors.panel-dark}"
    textColor: "{colors.text-dark}"
    rounded: "{rounded.md}"
  input:
    backgroundColor: "{colors.panel-dark}"
    textColor: "{colors.primary}"
    rounded: "{rounded.sm}"
    height: "2.75rem"
---

# Design System: OPSD 新前端

## Overview

**Creative North Star: "清晰的服务器工作区"**

用户指定 Mosona Manager 为主要参考。界面采用中性黑白灰、明确的文字层级和面板边界；信息通过名称、读数、单位与状态建立关系。正文和主要控件具有适合桌面操作的尺度，窄屏通过重排保留内容。

**Key Characteristics:**

- 文字、数值和分组标题具有清楚的字重与尺度差异。
- 主操作使用黑白反差，状态与图表分别承担颜色含义。
- 名称与相邻属性形成阅读组，宽表保留字段间的比较关系。
- 深浅模式使用同一套布局与交互。

## Colors

主色是用于操作的高反差中性色。深色工作区、侧栏与顶栏共用近黑色，面板、浮层与选中项依次变亮，所有背景均为无偏色的中性色；浅色以白色面板和浅灰工作区区分区域。常规边框使用白色 10%，输入边框使用白色 15%。具体色值见前置 token。

终端输出区在深浅模式中均为纯黑，终端菜单与设置共用独立的深色角色。状态与图表序列保留各自语义颜色。

绿色用于在线、成功和网络接收；蓝色用于 CPU 与网络发送；紫色用于内存。警告与错误分别使用暖黄和红色，并配合文字状态。

**The 中性背景 Rule.** 工作区、侧栏和通用面板直接使用中性色角色；状态色用于状态，图表颜色用于对应序列。

## Typography

界面使用本地 Inter 与思源黑体，名称与关键数字加重。表格和指标采用等宽数字排布。终端使用 JetBrains Mono 与更纱 Fixed SC，允许切换为完整更纱字体。

前置 token 记录实际的指标、页名、分组标题、正文、标签和辅助文字层级。控件文字沿用正文尺寸，选择器与密集状态使用标签尺寸。终端默认字号与正文一致，行高系数为 1.25，由 xterm.js 按字体测量结果计算字符单元。

**The 读数与单位 Rule.** 数值通过尺寸和字重突出；单位留在读数附近，颜色和尺寸足以阅读。

## Layout

桌面侧栏宽 256px，可收起至 80px；顶栏高 64px。内容区含两侧留白最大 1624px，内部有效内容最大 1560px；面板通常相隔 24px。短表单和设置页进一步限宽。

900px 以下使用可打开的导航侧栏。639px 以下主内容采用 16px 横向留白，工具栏和表单按可用宽度重排。密集表格在自身容器内横向滚动；长地址允许换行，窄屏地址字段占满所在行。

独立终端占用侧栏和顶栏以外的剩余视口。会话集中在左侧导航，收起侧栏或手机上通过当前会话名称切换；相同节点的多个会话使用递增序号区分。顶部只有节点信息与操作，窄屏按可用宽度换行，终端输出保持独立滚动。

## Elevation & Depth

普通面板通过底色和细边框区分区域。阴影用于弹窗、选择菜单、提示和侧栏等浮层；焦点使用独立的清楚轮廓。背景遮罩与模糊用于表达弹窗对当前任务的中断。

## Shapes

控件使用较小圆角，面板使用中等圆角，弹窗略大；具体取值见前置 token。边框通常为 1px，进度条和状态点使用相应的圆形或小圆角。图标来自同一套 Lucide 描边。

## Components

### Buttons

按钮和输入框的常规高度为 44px；主要按钮采用中性反差，普通操作使用轮廓或透明按钮。链接形式的主要操作沿用同一按钮样式。悬停、按下、焦点和禁用状态保留明确反馈。

刷新、编辑、日志、打开终端、操作记录、全屏和关闭使用图标及可访问名称；创建、保存、取消和危险确认保留文字。图标按钮共用 Tooltip，次要操作使用 DropdownMenu。字体和字号放在设置中，字段、单位、筛选值与状态文字保留。

### Cards / Containers

使用 Appica Card 承载单层内容。外层边框定义边界，内部内容不再叠加一圈边框。标题与操作位于同一行，空间不足时换行。

### Inputs / Fields

字段有可见标签。错误位于相应字段下方，提交后将焦点移到首个错误字段。编辑中的关闭操作需要确认放弃修改；弹窗遵循组件库的焦点约束和 Escape 行为。

### Navigation

主导航使用图标、文字与可选计数。当前页面用中性填色和较强文字表示；手机通过导航按钮打开相同页面入口。收起的桌面侧栏保留图标名称。

主导航采用后端八类定义与账号保存的顺序。工具和设置保留二级分类；手机使用相同定义。终端会话组独立于业务导航，包含节点名、当前选择与关闭操作。顶栏账号菜单提供账号设置、登录会话、内置界面和退出。恢复模式的普通导航与终端会话链接保留恢复前缀、查询参数和哈希；切换主题后的“刷新界面”明确进入所选主题的普通根路径。

### Terminal

每个会话拥有独立的 xterm.js 实例和传输状态。切换会话、离开业务页面、改变窗口或字体后保留内容并重新测量尺寸。断开状态申请新票据连接并保留旧输出，不重放输入。搜索支持方向操作、Enter、Escape 和 Ctrl+Shift+F。

节点名、地域和真实连接状态组成信息行；常驻操作为新建、搜索、全屏与更多。新建打开可搜索的在线节点菜单，更多包含复制、粘贴、清屏、终端设置、断开与关闭。终端设置使用 Popover，字体和字号共用全局偏好。菜单和设置关闭后恢复入口焦点；搜索关闭后返回终端输入。服务端输出按字节原样交给终端；票据过期、连接限额、断线和进程退出显示实际原因。

### Status

状态使用圆点和文字表达，保留成功、警告、错误和中性颜色；背景透明，与相邻内容处于同一层。

减少动态效果偏好下关闭界面过渡。其余变化用于控件反馈与状态切换。

## Do's and Don'ts

### Do:

- 保持主要名称、关键读数与辅助信息的层级。
- 为数值保留单位，为状态保留文字，为操作保留可访问名称。
- 使用已有控件与语义颜色角色。
- 在深浅模式和窄屏实际检查长名称、地址与动态内容。

### Don't:

- 不将低对比辅助文字颜色用于主要数据。
- 不将主色混入整页背景和全部边框。
- 不将终端重新放回节点详情中的有限弹窗。

### 业务状态与编辑

数据通过真实 API 读取。概览每三秒更新，历史按服务端采集间隔更新；SSE 按资源刷新。后台读取保留已有内容与未提交表单。分页中断保留有效内容和原游标，显示“继续读取”；Problem 展示字段错误、请求 ID，429 显示等待秒数。已打开的确认框与登记表单持续跟随所属节点可写状态，断连时保留内容并禁止写入。操作抽屉属于应用，跨业务页面保留所选记录、筛选及请求恢复引用。危险确认包含目标和操作摘要；防火墙确认期限采用服务端值。任务列表不读取脚本，编辑时读取定义；受保护数据库账号和备份状态保持只读。
