核心框架详解
============

接收链路（RX Pipeline）
----------------------
1) **Listener 接入**：`IListener.Listen` 接受新连接，包装为 `IConnection`，加入 `ConnManager`，触发 OnListen。
2) **Reader 解帧**：`IReader.ReadLoop` 从底层连接读取，使用 `HeaderCodec` 解出 `hdr/payload`，调用 `conn.DispatchReceive`，最终触发 `Process.OnReceive`。
3) **Process 组合**：常用组合为 `PreRoutingProcess`（base）+ `DispatcherProcess`：
   - Dispatcher 将事件放入内部 channel，worker 取出执行。
   - worker 内先调用 `preRoute`（如果 base 实现了 `PreRoute`，如 PreRoutingProcess），返回 false 则终止，不再分发到子协议。
   - 返回 true 时，按 SubProto 选择已注册的 handler 调用 `OnReceive`。
4) **PreRouting 核心规则（现实现）**：
   - SourceID=0 且 SubProto!=2：丢弃（未登录非登录协议）。
   - SubProto=2：直接放行 dispatcher。
   - target==0：广播给子节点（不回父），返回 false。
   - target!=local：按节点索引/父链转发，返回 false（子协议 handler 不会执行）。
   - target==local：返回 true，交给 Dispatcher 调用子协议。

发送链路（TX Pipeline）
----------------------
1) 入口：`server.Send(ctx, connID, hdr, payload)`。
2) 调用 `Process.OnSend` 钩子（仅一次），若返回错误则终止。
3) 若启用 `SendDispatcher`：
   - 事件放入 dispatcher channel，按连接映射到 per-connection writer，串行写出。
   - 支持 shard/worker 并行、入队超时、连接级缓冲。
4) 未启用 SendDispatcher 时，直接调用 `SendWithHeader` 发送（同步，阻塞至写完）。

上下文与 Server 注入
-------------------
- `core.WithServerContext(ctx, srv)` 将 server 放入 context；handler 可用 `core.ServerFromContext(ctx)` 获取 srv/ConnManager/NodeID。
- Server.Start 包装了全局 ctx，Reader/Process 调用时 ctx 中有 server。手动调用 handler 时需自行注入，否则会 fallback 到直接写连接。

路由与默认转发
--------------
- DefaultForwardHandler 仅在 Dispatcher 找不到子协议 handler 时触发，用于按配置将未知子协议转发到父/指定节点。
- PreRouting 的跨节点转发发生在 Dispatcher 之前，返回 false 短路子协议处理。

Major 与子协议处理
------------------
- 框架未对 `MajorCmd/MajorMsg` 做额外分支，路由决策主要依赖 target/subproto。Cmd 帧 target!=local 时默认也会被 PreRouting 转发，不会进 handler。
- 若需要 Cmd 帧逐跳解析，需在 PreRouting/Dispatcher 特殊处理（如指定 SubProto 的 Cmd 先进 handler 再转发），或在 handler 内自行构造转发逻辑（保留 SourceID、调整 Target）。

注册与分发
----------
- 通过 `DispatcherProcess.RegisterHandler(ISubProcess)` 按 SubProto 注册；每个 SubProto 仅可注册一个 handler。
- Default handler 可注册一次，用于兜底未知子协议。
- Dispatcher 使用 worker+channel 解耦收包与处理，避免阻塞 Reader。

连接管理
--------
- `ConnManager` 维护连接表，支持按 connID/nodeID/deviceID 索引；提供 Range/Broadcast/CloseAll 等。
- 登录流程会在连接元数据中写入 `nodeID/deviceID`，用于路由与索引。

父链与自动重连
--------------
- Server 根据配置 `parent.enable/parent.addr` 维护父连接，断线自动重连（`parent.reconnect_sec`）。父连接的 meta 标记为 `role=parent`。

关键默认值/约束
---------------
- SourceID=0 的非登录协议默认被 PreRouting 丢弃。
- target!=local 的帧默认不会进入子协议 handler。
- 发送时 OnSend 仅执行一次（即便广播）。

扩展点提示
----------
- 若需要“target!=local 也进 handler 再转发”，需调整 PreRouting/Dispatcher 或为特定 SubProto 增加 Cmd 拦截标记。
- 若需要更复杂的路由（例如 Cmd 强制逐跳），可以在 PreRouting 增加 allowlist 或在 handler 内先处理再手动转发。

快速参考：典型收发路径
---------------------
1) 收：Listener→Reader→Process.OnReceive→Dispatcher.worker→PreRoute（可能转发/丢弃）→子协议 handler。
2) 发：server.Send→Process.OnSend→SendDispatcher（可选）→net.Conn.Write / SendWithHeader。***
