# Core：Pipe 抽象 + MultiListener + ParentDialer（为 RFCOMM 等多承载铺路）

## 变更背景 / 目标
现状（变更前）：
- Core 连接链路在多个关键路径上强依赖 `net.Conn`（典型：`core.IConnection.RawConn()`、Reader 解包、SendDispatcher 发送优化）。
- 当需要接入 Bluetooth Classic RFCOMM（SPP 风格字节流）等非 TCP 承载时，上层被迫“直接持有/感知底层连接类型”，不利于扩展与审计。

目标（本次变更后）：
- Core 内部以“字节流管道（Pipe）”作为最低公共能力，Reader/SendDispatcher/父链路拨号不再硬编码 TCP。
- 为 Server 侧“同时启用多个 listener（TCP + 未来 RFCOMM）”提供可复用的 `MultiListener`。
- 直连 `nodeID` 仅保留一条连接（后绑定覆盖并关闭旧连接），避免路由歧义；后代路由索引仅覆盖映射不误杀连接。

## 具体变更内容
### 新增
- `core.IPipe`：仅包含 `io.ReadWriteCloser` 语义，作为底层承载的最小抽象（便于接入 RFCOMM/串口等）。
- `listener/multi_listener`：组合多个 `core.IListener` 并统一收敛关闭（ctx cancel 视为正常退出）。
- ConnMgr 直连冲突策略单测：
  - 直连冲突会关闭旧连接；
  - 后代路由覆盖不关闭旧连接。

### 修改
- `core.IConnection`：
  - 新增 `Pipe() core.IPipe`；
  - 移除 `RawConn() net.Conn`（破坏性变更）。
- `listener/tcp_listener`：
  - `tcpConnection` 内部持有 `pipe`（基于 net.Conn 包装）并实现 `Pipe()`。
- `reader/tcp_reader.go`：
  - 从 `conn.Pipe()` 解码帧（Header+Payload），不再依赖 `net.Conn`。
- `process/senddispatcher.go`：
  - 改为写入 `io.Writer`（来自 `conn.Pipe()`），并保留 HeaderTcp v2 的低拷贝路径（`net.Buffers.WriteTo`）。
- `server/server.go`：
  - 父链路拨号改为可注入 `ParentDialer`（默认 TCP Dialer，便于未来替换为 RFCOMM）。
- `connmgr/manager.go`：
  - `UpdateNodeIndex` 增加“直连冲突关闭旧连接”策略（锁外关闭，避免死锁/锁竞争）。

## plan.md 任务映射
- CORE1：Pipe 抽象 + `IConnection` 改造
- CORE2：Reader 基于 Pipe 解包
- CORE3：SendDispatcher 写 Pipe + 保留 HeaderTcp 性能路径
- CORE4：MultiListener
- CORE5：ParentDialer 注入
- CORE6：ConnMgr 直连冲突策略 + 单测

## 关键设计决策与权衡
- **最小抽象**：`IPipe` 只要求读/写/关闭，不强依赖 deadline/flush 等能力；承载差异通过“可选接口”再扩展。
- **性能**：HeaderTcp v2 仍走 `net.Buffers{hdrBytes, payload}.WriteTo(writer)`，避免拼接与额外拷贝。
- **并发与稳定性**：
  - per-conn writer 串行写入仍保留（避免帧交错）；
  - 直连冲突的关闭在锁外执行，降低锁持有时间并规避潜在死锁。
- **扩展点**：ParentDialer 注入使“dial = protocol-specific”可插拔，避免 Core 继续硬编码 `net.Dial("tcp")`。

## 测试与验证
在 workflow-local `go.work`（`worktrees/refactor-transport-pipe/go.work`）下验证：
- `cd MyFlowHub-Core; go test ./... -count=1 -p 1` ✅

## 潜在影响
- **破坏性 API 变更**：`core.IConnection` 形态变化会影响所有实现/Mock/依赖仓库（本 workflow 已同步适配 Server/SubProto）。
- 未来如需支持更多承载能力（deadline、半关闭、窥探地址等），建议通过可选接口扩展，避免把承载特性强灌进最小抽象。

## 回滚方案
- 回滚本次提交（或整体 revert）：
  - 恢复 `RawConn()` 并回退 Reader/SendDispatcher/ParentDialer 改造；
  - 移除 `MultiListener` 与 ConnMgr 冲突策略变更；
  - Server/SubProto 同步回滚对应适配提交。

