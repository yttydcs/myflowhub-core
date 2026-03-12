# Plan - Core：引入 Pipe 抽象 + MultiListener + Parent Dialer（为 RFCOMM 等多传输铺路）

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`refactor/transport-pipe`
- Worktree：`d:\project\MyFlowHub3\worktrees\refactor-transport-pipe\MyFlowHub-Core`
- Base：`origin/master`
- 关联仓库（同一 workflow）：
  - `MyFlowHub-Server`：装配多 listener、父链路 endpoint
  - `MyFlowHub-SubProto`：适配 `core.IConnection` 变更（主要是测试 stub/mock）
- 参考：
  - `d:\project\MyFlowHub3\target.md`
  - `d:\project\MyFlowHub3\repos.md`
  - `d:\project\MyFlowHub3\guide.md`（commit 信息中文）

## 背景 / 问题陈述（事实，可审计）
- 目前 Core 的连接读写链路强依赖 `net.Conn`（例如 `IConnection.RawConn()`、`TCPReader`、`SendDispatcher` 写入优化路径）。
- 该设计在“新增非 TCP 承载（例如 Bluetooth Classic RFCOMM）”时会遇到两类问题：
  1) 某些平台/承载不易稳定地暴露为 `net.Conn`（或不支持 deadline 等能力）。
  2) 上层期望“管理器持有管道（Pipe）”，内部按需解包（Header）并路由转发，避免把底层连接类型泄漏到业务层。

## 目标
1) **Pipe 抽象**：Core 以 `Pipe`（`io.ReadWriteCloser` 语义）作为连接底座，Reader/SendDispatcher 不再依赖 `net.Conn`。
2) **多 Listener**：在 Core 提供可复用的 `MultiListener`（实现 `core.IListener`），支持同时启动多个协议 listener。
3) **Parent Dialer 抽象**：Server 父链路拨号从硬编码 `net.Dial("tcp")` 改为可注入 dialer（为未来 BT dial 做准备）。
4) **路由索引策略**：同一 `nodeID` 的“直连绑定”仅保留一条（后绑定覆盖；旧连接可被移除/关闭）；但“后代路由索引”仅覆盖映射，不主动关闭旧连接（避免误杀承载其他后代的 childConn）。

## 非目标
- 本仓不实现 RFCOMM 具体 listener/dialer（平台差异大，另起 workflow 在 Win/Android 落地）。
- 不修改 wire：HeaderTcp v2 / Major 路由语义 / SubProto 值 / Action 名称 / JSON schema 均保持不变。
- 不引入 payload 的通用业务解析（payload 仍以 `[]byte` 透传给 handler；仅 Header 用于路由/安全门禁）。

## 约束（边界）
- 允许破坏性改动（为未来多协议与性能），但必须：
  - 提供清晰的迁移路径与回滚点；
  - 更新同 workflow 的 Server/SubProto 以通过集成测试。
- listener 开关变更需重启 Hub 生效（运行期动态开关不在本轮）。

## 验收标准
- 本 workflow 的集成联调（通过 workflow-local `go.work`）满足：
  - `MyFlowHub-Core`：`go test ./... -count=1 -p 1` 通过；
  - `MyFlowHub-Server`：在同一 workspace 下可编译、可启动、最小冒烟链路不回退；
  - `MyFlowHub-SubProto`：受影响的测试 stub/mock 已适配并可通过单测。
- 不引入循环依赖（Core 仍不依赖 Server/Win/SDK）。

## 3.1) 计划拆分（Checklist）

### CORE0 - 归档旧 plan（已执行）
- 目标：避免覆盖历史 plan，保留可审计回放。
- 已执行：`git mv plan.md docs/plan_archive/plan_archive_2026-03-11_transport-pipe-prev.md`
- 验收条件：归档文件存在且可阅读。
- 回滚点：撤销该 `git mv`。

### CORE1 - 引入 Pipe 抽象并改造 `IConnection`
**目标**
- 在 Core 定义 `Pipe`（读写关闭）语义，并使 `IConnection` 以 Pipe 作为底座能力。

**涉及模块 / 文件（预期）**
- `iface.go`（`IConnection`：新增 `Pipe()`；移除或降级 `RawConn()`）
- `listener/tcp_listener/connection.go`（TCPConnection 适配 Pipe）

**设计要点**
- `Pipe` 只要求 `io.ReadWriteCloser`；不强依赖 deadline（不同承载能力不同）。
- `IConnection` 的 `Send/SendWithHeader` 仍作为“框架统一发送入口”；上层不直接写 Pipe。

**验收条件**
- Core 编译通过；同 workflow 的 Server/SubProto 可适配通过编译。

**测试点**
- `go test ./... -count=1 -p 1`

**回滚点**
- revert 该提交。

### CORE2 - Reader 改造：从 Pipe 解码帧（Header+Payload）
**目标**
- 将当前 `reader/tcp_reader.go` 改造为通用 stream reader（从 `conn.Pipe()` 读取并调用 `codec.Decode`）。

**涉及模块 / 文件（预期）**
- `reader/tcp_reader.go`（重命名或保留文件名但语义改为 stream）

**验收条件**
- Server 侧收包处理不回退（OnReceive 正常触发）。

**测试点**
- Core 单测 + Server 集成最小测试（后续在 Server 仓计划中列）。

**回滚点**
- revert 该提交。

### CORE3 - SendDispatcher 改造：写入 Pipe 并保留 HeaderTcp 低拷贝路径
**目标**
- 发送调度器不再依赖 `net.Conn`，改为写 `io.Writer`（来自 `conn.Pipe()`）。

**涉及模块 / 文件（预期）**
- `process/senddispatcher.go`

**性能关键点**
- HeaderTcp v2：继续使用 `net.Buffers{hdrBytes, payload}.WriteTo(writer)` 以减少拼接与拷贝。

**验收条件**
- Server 侧发送/转发仍可工作；并发写不出现帧交错（仍由 per-conn writer 串行保障）。

**测试点**
- `go test ./... -count=1 -p 1`

**回滚点**
- revert 该提交。

### CORE4 - `MultiListener`：支持同时启动多个 listener
**目标**
- 提供一个 Core 可复用的多 listener 组合器，便于 Server 同时开启 TCP + 未来 RFCOMM。

**涉及模块 / 文件（预期）**
- `listener/multi_listener/listener.go`（新）

**验收条件**
- MultiListener 在任一子 listener 返回错误时能触发 Close 并尽快退出；
- ctx cancel 时能关闭全部 listener 并退出。

**测试点**
- 最小单测：模拟两个 listener，一个返回错误，一个阻塞；断言 MultiListener 返回并关闭另一个。

**回滚点**
- revert 该提交。

### CORE5 - Parent Dialer：父链路拨号可注入（为 BT dial 做准备）
**目标**
- 将 `server.Server` 的父链路拨号从硬编码 `net.Dial("tcp")` 改为可注入 dialer / dial 函数。

**涉及模块 / 文件（预期）**
- `server/server.go`
- （可选）新增 `dialer/*` 包：默认 TCP dialer 实现

**验收条件**
- 默认行为不变：不配置时仍按 TCP 拨号父节点；
- 配置 dialer 后可替换拨号实现（本轮不实现 BT dialer）。

**测试点**
- Server 单测或最小集成：注入一个 fake dialer，断言被调用。

**回滚点**
- revert 该提交。

### CORE6 - ConnMgr：nodeIndex 冲突策略（直连仅保留一条）
**目标**
- 当 `nodeID` 的“直连绑定”（`nodeID == conn.meta(nodeID)`）发生冲突时：
  - 采用“后绑定覆盖”；
  - 旧直连连接从 manager 移除并关闭（避免同一 node 两条连接导致路由歧义）。
- 当更新的是“后代路由索引”（`nodeID != conn.meta(nodeID)`）时：仅覆盖映射，不主动关闭旧连接。

**涉及模块 / 文件（预期）**
- `connmgr/manager.go`

**验收条件**
- 不影响现有多 hop 路由（后代映射仍可覆盖更新）。

**测试点**
- 新增单测覆盖：
  - 直连冲突会关闭旧连接；
  - 后代路由更新仅覆盖不关闭旧连接。

**回滚点**
- revert 该提交。

### CORE7 - Code Review（阶段 3.3）+ 归档变更（阶段 4）
**目标**
- 输出 Code Review 结论与 `docs/change/2026-03-11_transport-pipe-core.md` 归档文档（背景、变更、权衡、测试、回滚）。

### CORE8 - 合并 / tag / push（需你确认 workflow 结束后执行）
- 由于 `core.IConnection`/传输抽象可能属于破坏性变更，建议发布新 tag（例如 `v0.3.0`）。
- 在 `repo/MyFlowHub-Core` 执行（待 workflow 结束确认后）：
  1) 合并到 `master`
  2) push
  3) 创建并 push tag

---

## 验证命令（建议）
> 本 workflow 会在 `d:\project\MyFlowHub3\worktrees\refactor-transport-pipe\` 下创建一个 **仅本地使用** 的 `go.work`，用于把 Core/Server/SubProto 的 worktree 串起来联调（不提交到任何仓库）。

```powershell
$env:GOTMPDIR='d:\\project\\MyFlowHub3\\.tmp\\gotmp'
New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null
go test ./... -count=1 -p 1
```

