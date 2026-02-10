# Plan - HeaderTcp v2（32B）+ Core 路由统一（Core）

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`refactor/hdrtcp-v2`
- Worktree：`d:\\project\\MyFlowHub3\\worktrees\\hdrtcp-v2\\MyFlowHub-Core`
- 目标 PR：PR1（跨 3 个 repo 同步提交/合并）

## 项目目标
1) 将 TCP wire 头从 HeaderTcp v1（24B）升级为 **HeaderTcp v2（32B）**，支持：`ver/hdr_len/magic`、`hop_limit/route_flags/trace_id` 等扩展字段。  
2) 在 Core 层收敛路由框架规则：**MajorCmd 必须进入 handler（逐跳可见）；MajorMsg/OK/Err 走 Core 快速转发**，消除 Core 内的“协议特例”。  
3) 为后续“子协议解耦为独立库 + Server 组装调用 + UI 通过 SDK/hook 调用”打底（本 PR 只做必要地基，不一次性做完拆分）。

## 范围
### 必须（PR1）
- HeaderTcp v2：32B 固定头 + 编解码 + 单测
- 升级 Core 接口（`IHeader/IHeaderCodec` 等）以承载 v2 字段
- Core 路由规则框架化（基于 Major 区分控制面/数据面）
- 移除/替换现有 PreRouting 中的协议特例（例如 file CTRL 特判）

### 可选（若风险可控）
- 基于 `trace_id` 的最小可观测性：在关键日志里带上 trace_id（避免日志洪水）
- `hop_limit` 的最小保护：只在“发生转发”时递减并做 0 丢弃（防环）

### 不做（本 PR）
- 子协议彻底拆 repo/go module（另起 PR2+）
- Linux 构建/验收（用户已允许忽略）
- 大规模重命名/全局格式化（避免噪音 diff）

## 已确认的关键决策（来自阶段 2）
- 兼容策略：**S3 / big-bang**；切换后 **v1 不再兼容**（允许临时双栈但最终移除）。
- HeaderTcp v2：**32B（+8B）**，字段集合“如前讨论版本”。
- 路由框架规则：**MajorCmd 不由 Core 自动转发，必须进 handler；MajorMsg/OK/Err 走 Core 快速转发**。
- 语义基线：`TargetID=0` 仅表示“下行广播不回父”，不能表示上送父节点。

## 问题清单（阻塞：是）
> 以下项目若不确认，将导致 wire 语义不一致或实现需要拍脑袋，禁止进入阶段 3.2 写代码。

1) HeaderTcp v2 的 `magic` 取值是否确认？
   - 建议：`0x4D48`（ASCII "MH"），BigEndian 写入。
2) `hop_limit` 的默认值与语义是否确认？
   - 建议：默认 `16`；每发生一次“转发”递减 1；递减后为 0 则丢弃并记录告警日志（不自动回 Err，避免放大风暴）。
3) `trace_id` 的生成策略是否确认？
   - 建议：发送侧若 `trace_id==0` 则在 Core 发送链路自动填充随机 uint32；响应帧复用请求的 trace_id；转发不改 trace_id。
4) `timestamp` 单位是否确认？
   - 建议：保持 v1 语义：`uint32` Unix 秒（UTC）；`0` 表示未填。

## 任务清单（Checklist）

### C1 - 设计并落地 HeaderTcp v2（32B）
- 目标：定义 v2 头部结构、编解码、校验策略（magic/ver/hdr_len），并让 Core 内所有使用方编译通过。
- 涉及模块/文件（预期）：
  - `iface.go`（扩展 `IHeader` 字段访问/修改接口）
  - `header/header.go`（新增 v2：结构体 + codec；v1 代码按 big-bang 可删除或保留为历史文件但不再使用）
  - `tests`（新增/更新：编解码一致性、长度、字段 roundtrip）
- 验收条件：
  - `HeaderTcp v2` 编解码 roundtrip：字段一致、payload 一致、长度恒为 32B+payload。
  - `go test ./...` 通过。
- 测试点：
  - 编解码：空 payload、超长 payload、边界字段（0/最大值）、错误 magic/ver/hdr_len。
- 回滚点：
  - 单独提交：仅包含 HeaderTcp v2 + 测试；可 `git revert`。

### C2 - Core 路由规则按 Major 统一（去协议特例）
- 目标：实现统一框架规则：
  - `MajorCmd`：不做自动转发，必须进入 handler（逐跳可见）。
  - `MajorMsg/MajorOKResp/MajorErrResp`：保持快速转发策略（target==0 广播到子；target!=local 转发到子或父；target==local 才进入 handler）。
- 涉及模块/文件（预期）：
  - `process/prerouting.go`（移除 file CTRL 特判等协议耦合；按 Major 分流）
  - `process/dispatcher.go`（如需：把“数据面快速转发”尽量前置/减少不必要排队；仅在必要时改动）
  - `config/*`（如需新增/调整路由相关开关；默认安全）
- 验收条件：
  - Core 中不再出现“特定 SubProto + payload[0]”之类的协议特判（用 Major 规则覆盖）。
  - 现有 server/win 行为在冒烟场景不回退（由 PR1 的跨 repo 联调验证）。
- 测试点：
  - 单元：构造 Cmd/Msg 帧，验证 PreRoute 返回值与转发路径选择。
  - 集成（联调）：file CTRL 从子节点上送能逐跳进入 handler；file DATA 仍可快速转发。
- 回滚点：
  - 将路由变更独立提交；可单独 revert。

### C3 - 可观测性与安全默认（最小集）
- 目标：在不制造日志噪音的前提下，关键路径日志携带 `trace_id`，并保留“来源一致性校验”等安全默认。
- 涉及模块/文件（预期）：
  - `process/*`（日志字段、必要的校验错误路径）
- 验收条件：
  - 关键错误日志包含 `trace_id/source/target/subproto/major` 等定位信息。
  - 不引入高频 Info 日志（避免大流量 file data 打爆日志）。
- 回滚点：
  - 日志变更独立提交；可 revert。

## 依赖关系
- 本 repo（Core）的 C1 是 Server/Win 编译通过的前置条件；但由于 big-bang，本 PR 需要 3 个 repo 同步修改后才能联调通过。

## 风险与注意事项
- 这是 wire 破坏性变更：任一端未升级都会导致无法互通；必须同步发布/部署。
- `IHeader` 接口扩展会触发大量编译错误：需按计划逐个修复，避免临时兼容层长期存在。
- 性能关键点：Header 编解码是热路径，避免多余分配；必要时引入 `sync.Pool` 但先以正确性为主，后续再 perf PR。

