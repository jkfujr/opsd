# 0001：单一监控链路与 telemetry 边界

日期：2026-11-16。状态：决策已接受；本批实施 P0、P1a、P1b。

## 目标

监控采用 best-effort+：单 Receive，持久缓冲、原批次补传和缺口展示；Query 与 Store 的独立部署留待后续。暂不建设 RF3 或 SLA。

采集负责生成带原始时间的样本；归档负责将接收数据保存到 S3；查询负责授权范围内的时序读取；监控服务宿主承载监控组件，页面的“查看节点”只筛选被观察的节点。

目标保留策略为 raw 30 天、5m／1h 365 天。统一历史入口为 `/monitoring/series`，P3 删除旧五分钟采样、七天历史及 `/monitoring/history`。SLO 只做展示，`/monitoring/slos` 为 P4 计划的只读接口。

RPO 24h 是恢复目标，当前尚未验收。后续须分别验证来源节点故障、监控服务宿主及磁盘丢失、S3 不可用等故障下的恢复来源与实际数据缺口；持续中断期间的采集缺口另行记录。

## 当前实现与本批保持项

- Receive、Query、Store Gateway、Compactor 同宿主运行。本地保留 30 天；S3 的 raw、5m、1h 均保留 365 天。P0/P1 保持 retention 参数及原生测试中的 31／364 天 raw 查询断言。
- 来源节点待传缓冲上限为 1h／64MiB；回执保留 2h5m。容量淘汰、超时和永久拒绝会记录缺口。这些限制保留原值，RPO 24h 尚未成为当前能力。
- `/monitoring/history` 及其页面调用继续运行；P0 只记录废弃计划。`/monitoring/series` 保持现有 API，`/monitoring/slos` 尚未实现。
- 每秒采样、五秒上传、批次 ID、序号、重试参数及确认顺序保持原值：查原回执，写入 Receive，持久保存回执，返回确认，来源确认原批次。

## P0/P1 执行边界

P0 统一两个页面的角色名称和文档术语。P1a 迁移配置、状态、查询、角色流程；P1b 迁移采集、上传、接收及循环。各阶段检查通过后进入下一阶段。

`telemetry.Service` 唯一拥有原八个 archive 字段及配置锁，`Node` 保留一个 Service 和 facade。`monitoring_config`、`monitoring_workflows` 的业务读写由 telemetry 管理，既有 schema 和迁移执行入口保持原值。

telemetry 通过 cluster adapter 获取 Peer、Queue、Storage、Shared、Executor、Clock、Events、采集输入、本机数据库及身份；telemetry 不导入 cluster。Queue 复用父操作保存、子操作与 stepID；Storage 保留本地目标和凭据读取。S3 修改与角色流程继续共用同一配置锁。

Noise 握手、成员身份与端点选择留在 cluster；payload、资源归属、回执、补传及运行状态归 telemetry。保留每来源一条流、最多 128 条流、四并发写入和两并发查询，以及先停旧宿主再启动新宿主的恢复顺序。

以当前未提交工作区为修改起点，保留三个 `cluster/monitoring_*.go` 文件作为 facade／流适配。protocol、SQL schema、OpenAPI、操作与批次 ID 保持原值；队列、database／proxy、旧历史和 SLO 保持原行为。P2 及以后、提交、推送与部署均在本批范围外。
