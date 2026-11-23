# 安全边界与数据归属

| 项目 | 内容 |
|---|---|
| 文档性质 | 现状记录 + 未决问题。**"现行行为"一节描述已发布的实现，"目标"一节描述尚未决定或尚未实施的方向**，两者不得混读。 |
| 对应审查条目 | `docs/ARCHITECTURE-REVIEW.md` 方案 A（安全边界重建）、方案 B（设计文档治理）、方案 D（数据权威） |
| 最后核对 | 2026-11-23，对照 `0.1.0-dev` 源码 |

---

## 1. 现行行为

### 1.1 进程与身份

安装器生成的两个常驻单元权限差异是整套边界的基础（`backend/internal/installation/node.go:176-177`）：

| 单元 | 运行身份 | 组 | 运行目录与掩码 |
|---|---|---|---|
| `opsd.service`（对等节点） | `User=opsd` | `Group=opsd` | `RuntimeDirectory=opsd-node`、`RuntimeDirectoryMode=0700`、`UMask=0077` |
| `opsd-executor.service`（本机执行器） | **`User=root`** | `Group=opsd` | `RuntimeDirectory=opsd`、`RuntimeDirectoryMode=0750`、`UMask=0007` |

节点单元另外声明 `NoNewPrivileges=true`、`ProtectSystem=strict`，并以 `ReadWritePaths` 限定可写目录。

### 1.2 执行器的准入条件

执行器**只经本地 Unix Socket 接受有类型的请求**（`backend/internal/executor/local.go:1`），不监听网络端口。路径与权限：

- socket 路径 `/run/opsd/executor.sock`（`installation/node.go:84`）；
- 监听后立即 `os.Chmod(socket, 0660)`（`executor/local.go:207`）。

socket 所在目录由单元声明：执行器单元的 `RuntimeDirectory=opsd`、`RuntimeDirectoryMode=0750`（`installation/node.go:177`），所以 `/run/opsd` 本身只允许 root 与 `opsd` 组进入。

因此在现行实现中，**执行器的准入等价于"该进程能否打开这个 socket"**：socket 属主是 root、组是 `opsd`、权限 0660，组内任何进程都可写；进入目录又需要 `opsd` 组身份。**执行器不另做调用方身份校验**——没有令牌、没有按操作的授权判断。

**A1 保留这组目录与 Socket 权限，并在其后叠加本机调用方校验**——权限位决定谁能连上，A1 决定连上来的是谁。**A1 已实施**：执行器在 `Accept` 后读取 `SO_PEERCRED`，只放行 `node.json` 里 `user_id` 指定的服务账号（旧配置缺该字段时按 `service_account` 推导，推导失败或解析到 UID 0 都拒绝启动），被拒连接随即关闭且不影响后续连接。细节见 [A1 设计](A1-LOCAL-CALLER.md)。四步的分工是：

- **A1**：在默认安装下识别本机调用方，使 `opsd` 组内其他进程不能直接驱动 root 执行器；
- **A2**：限制已获准的调用方能执行哪些操作；
- **A3**：远端请求的操作授权由目标节点判定；
- **A4**：远端高风险能力由目标节点显式开启，而不是默认可用。

> 风险含义：`opsd` 组内的任何进程都能驱动以 root 身份运行的执行器。节点进程失陷、或任何以 `opsd` 组身份运行的程序被利用，都等价于获得执行器的能力。这正是方案 A2–A4 要处理的范围。

### 1.3 节点之间的终端通道

两个方向各校验一次，职责不同：

**发起节点校验目标**（`cluster/terminal.go`）。先取目标节点的成员记录（`:28` 的 `n.Member(ctx, target)`），建立隧道后校验对端自报的成员数据（`:64`）：

- `remote.ID` 必须等于请求的目标节点；
- `remote.Public` 必须与已知成员公钥一致；
- `remote.Cluster` 必须与本集群一致；
- 通过 `validateMember` 的结构校验。

**目标节点校验发起方**（`cluster/peer.go:62` 起）。目标侧收到 hello 后自行判定：

- `validateMember(m)` 结构与字段校验；
- `m.Public` 必须等于通道对端公钥（`identity.Encode(channel.PeerPublic)`），防止冒名；
- 非入群连接（普通远端终端、监控通道都走这条路径）要求 `n.Active(ctx, m.ID)` 为真，并校验 `m.Cluster` 与本节点一致（`cluster/peer.go:82`）——`Active` 会双向检查撤销状态并确认成员存在（`cluster/node.go:415`）；
- 携带入群令牌的连接不走上面这条，而是交给 `acceptMember` 处理。

即**目标节点按自己的成员名单与撤销状态授权**，不采信发起方的自我声明；发起方同时也在校验目标，避免接到错误节点。

### 1.4 管理员会话

管理员登录、会话 cookie 与 CSRF 校验都发生在**入口控制台**。终端连接另有一张一次性票据：入口签发（`control/terminal.go:46` 存入 `terminalTickets`，30 秒过期），随后在连接时消费（`AttachTerminal` 再校验来源、会话与访问权）。

本地终端与远端终端都走这条路径。目标节点收到的是**已验证的成员身份**，不接收管理员口令、会话 cookie 或票据，也不再重复判定管理员权限。

### 1.5 数据归属

各类数据的权威来源、写入方与副本用途统一记录在
[架构](ARCHITECTURE.md#数据类型与权威来源)，本文档不重复列举，避免两处漂移。

---

## 2. A0 待决问题

本节**不是承诺**，而是 A0 起需要先定方向的问题。任何一项落地前都需要设计文档与验收。

1. **A1：本机调用方校验。** 1.2 那组权限只回答"谁能连上"，A1 要回答"连上的是谁"，使 `opsd` 组内其他进程无法直接驱动 root 执行器。这一步可以在单机验收：节点服务账号仍能完成合法操作，其他同组 UID 被拒。
2. **A2：本机操作策略。** 在 A1 的身份之上限定允许的操作类型与资源范围，缩小节点进程失陷后可造成的后果。
3. **A3：目标侧授权。** 远端请求的**操作**授权由目标节点判定；1.3 现在只到成员身份与撤销状态，不包含按操作的授权。
4. **A4：远端能力开关。** 高风险能力（如 root 终端）改为由目标节点显式开启，而不是默认可用；默认值改变会动到现有功能，因此必须连带定义新旧节点共存时的行为。
5. **数据写入路径枚举（方案 D）。** [架构](ARCHITECTURE.md#数据类型与权威来源) 只记录"谁是权威"，尚未枚举同一事实的所有可写路径。
6. **审计。** 现行实现有 HTTP 请求日志与操作记录，但没有独立的"谁在何时授权了什么"审计流。是否需要单列，待定。

## 3. 维护约定

- 改动 1.1–1.4 描述的行为时，**必须同步更新本文档**；只改 1.1 的单元权限而不更新这里，视为缺陷。
- 新增数据类型时，必须在 [架构](ARCHITECTURE.md#数据类型与权威来源) 的表格里登记权威来源与写入方。
- 依赖方向的固化由测试承担（`backend/internal/architecture/layers_test.go`），不在本文档重复列举。
