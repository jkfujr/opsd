# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

React 19 + TypeScript + Vite + Tailwind CSS v4 + TanStack Query + openapi-fetch（在 `frontend-cc/` 下构建完全定制的设计系统与原生组件，摆脱第三方模板库束缚）

## Users

多服务器运维工程师、系统管理员与基础设施负责人。常在桌面宽屏或笔记本应急终端下同时巡检与操作多台跨地域 Linux 主机（Debian/Ubuntu systemd 宿主机），执行节点接入、防火墙策略维护、Docker 容器与 Compose 编排、MariaDB Galera + ProxySQL 集群管理、计划任务调度及远程终端排障。

## Product Purpose

OPSD 是对等节点（Peer-to-Peer）多服务器管理后台。每台机器运行同一 `opsd` 二进制，无需中心 Hub 即可完成成员通信、本机管理、实时监控、容器与数据库编排、防火墙策略同步与可选 Web 控制台。目标是让多机运维具备高密度态势感知、可验证的持久操作执行与防失联安全保障。

## Positioning

去中心化认证 Noise 对等通信 + 普通用户进程与本机 Unix Socket root 执行器权限隔离 + 目标节点本地 SQLite 持久串行操作队列（含 120 秒防火墙变更确认自动回滚与未知写入幂等核对）+ 地域 CouchDB 异步签名历史复制。任意控制台或数据库离线均不中断已受理的本机操作与自动恢复。

## Operating Context

- 集群由多台跨地域或组网（如公网 IPv4/IPv6、EasyTier）Linux 服务器组成。
- 管理员通过自定义安全入口（`entrance`）登录任一开启控制台的节点，即可管理全网有效成员。
- 高频工作流：
  - 集群概览实时盯盘（3 秒共享刷新 CPU/内存/磁盘/网络与 24h/7d 趋势、待确认与需人工介入的操作队列）。
  - 节点生命周期（生成一次性 `curl`/`wget` 接入命令、24h 接入码管理、双栈公网地址自动发现/手工指定、OPSD 自用 CouchDB 存储配置、节点独立账号与密码查看）。
  - 防火墙与集群互信（UFW / firewalld / nftables / iptables 端口与 IP 规则、默认策略、变更预览、120 秒倒计时确认、集群成员公网 IP 自动互信放行与规则转交）。
  - 容器与 Compose 管理（跨节点容器状态筛选、启停重启、单容器镜像更新、实时流式日志、Compose 项目登记与拉取/重建/更新）。
  - 数据库集群（MariaDB Galera + ProxySQL 实例发现、业务库与账号授权、主机组连接池监控、多代理同步步骤核对、备份计划产物检查）。
  - 运维工具（WebSocket 安全票据远程 root PTY 终端、五字段 Cron + IANA 时区 Shell 定时任务与 16 MiB 分页执行日志）。
  - 全局持久操作抽屉（追踪 `accepted`、`applying`、`awaiting_confirmation`、`attention_required` 等状态，支持取消排队、确认保留、重新处理/核对状态、接受现状）。

## Capabilities and Constraints

- 严格遵守 `../api/openapi.json` 定义的全部接口契约、CSRF 头（`X-CSRF-Token`）、幂等键（`Idempotency-Key`）与 `/api/v1/events` SSE 实时失效通知机制。
- 写操作遇到网络超时或未知应答时，严禁盲目重发命令，必须提供“查询原操作”或“核对状态”入口。
- 界面全中文呈现，只保留功能名称、字段、单位、选项、按钮、业务数据、必要状态、操作结果与风险确认，剔除一切营销口号、教学段落与解释性废话。
- 导航支持概览（`overview`）、节点（`nodes`）、容器（`containers`）、数据库（`databases`）、存储（`storage`）、防火墙（`firewall`）、工具（`tools`：终端、定时任务、Compose）、设置（`settings`：账号、会话、API、外观、版本），且菜单顺序与当前节点管理员账号持久化同步。
- 支持浅色、深色与跟随系统主题切换（存储键 `opsd.theme`）。

## Evidence on Hand

- 完整 OpenAPI 规范：`../api/openapi.json` 及生成的 TypeScript 类型 `../api/typescript/schema.gen.ts`。
- 架构与交互规范：`../README.md`、`../docs/ARCHITECTURE.md`、`../docs/API.md`、`../docs/UI.md`。
- 旧版前端实现参考（作为业务逻辑与 API 调用参考，视觉作为反面教材）：`legacy/src/`。

## Product Principles

1. **态势一目了然，操作零歧义**：核心指标、节点在线/离线、防火墙互信状态与待确认倒计时必须在第一视口直接呈现，无需层层点击。
2. **持久操作与幂等安全优先**：任何跨节点或系统级变更都围绕持久操作队列展开，清晰区分“已受理”“应用中”“待确认（含倒计时）”“需人工处理”与“未知结果待核对”。
3. **纯粹工具属性，零冗余文案**：界面即仪器，删除一切自我介绍与装饰性说明，用精准的排版密度、等宽数字（`tabular-nums`）与状态语义服务高频运维。
