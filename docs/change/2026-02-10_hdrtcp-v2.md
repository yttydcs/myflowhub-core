# 2026-02-10 HeaderTcp v2（32B）+ Core 路由规则统一（Core）

## 背景 / 目标
现有 TCP wire 头部（HeaderTcp v1，24B）缺少 `magic/ver/hdr_len` 等自描述字段，且 Core 层路由存在协议特例，导致协议演进与跨端一致性维护成本较高。

本次 PR1（跨 Core/Server/Win 同步）目标：
1) 升级 wire 头为 **HeaderTcp v2（32B）**：增加 `magic/ver/hdr_len/hop_limit/route_flags/trace_id` 等字段，提升可扩展性与可观测性。
2) Core 层收敛路由框架规则：**MajorCmd 必须进入 handler（逐跳可见）；MajorMsg/OK/Err 走 Core 快速转发**。

## 具体变更内容
### 新增 / 修改
- `header/header.go`
  - 定义 HeaderTcp v2（32B）字段布局与 `HeaderTcpCodec` 编解码。
  - Decode 校验 `magic/ver/hdr_len`；允许 `hdr_len>32` 的扩展头（读取并忽略扩展区）。
  - 新增 `CloneToTCPForForward()`：用于“转发”场景的克隆 + `hop_limit` 递减。
- `iface.go`
  - 扩展 `core.IHeader`：新增 `HopLimit/RouteFlags/TraceID` 访问/修改接口（移除旧 `Reserved` 读写语义）。
- `process/prerouting.go`
  - 移除协议特例（如 file CTRL 特判），按 Major 统一：
    - `MajorCmd`：放行到 handler（Core 不做自动转发/广播）。
    - `MajorMsg/MajorOKResp/MajorErrResp`：按 `TargetID` 执行 Core 快速转发/广播，并在“发生转发”时递减 `hop_limit`。
- `process/senddispatcher.go`
  - 零拷贝写帧路径更新为写入 v2 头部（32B）。
- `server/server.go`
  - 发送链路默认补齐：`hop_limit==0 => DefaultHopLimit(16)`；`trace_id==0 => 自动生成`。
- `README.md`
  - 同步头部长度描述为 32B。

### 删除
- 无（big-bang 语义上弃用 v1，但本次 PR 未单独清理历史文件/兼容分支以降低噪音）。

## plan.md 任务映射
- C1：HeaderTcp v2（32B）编解码 + 单测 ✅
- C2：Core 路由规则按 Major 统一（去协议特例）✅
- C3：可观测性与安全默认（最小集）✅（发送侧补齐 `trace_id/hop_limit` + magic/ver/hdr_len 校验）

## 关键设计决策与权衡
- **兼容策略：S3 / big-bang**：切换后 v1 不再兼容（需要三端同步升级）。
- **头部大小：32B（+8B）**：在换取扩展与观测字段的同时，带宽开销为每帧增加 8 字节（不含 payload）。
- `magic=0x4D48`（"MH"）、`ver=2`、`hdr_len=32`：便于快速判帧与后续扩展。
- `hop_limit`：默认 16；仅在“发生一次转发”时递减；耗尽则丢弃并告警（用于防环）。
- `trace_id`：发送侧若为 0 自动填充随机 `uint32`；响应继承请求；转发不改（便于跨 hop 关联日志/观测）。
- Core 路由：将“控制面 vs 数据面”的分界前置到 Major，避免 SubProto/payload 级别的耦合特判。

## 测试与验证
- 单元测试：
  - `go test ./...`
  - HeaderTcp v2 编解码测试覆盖：roundtrip、扩展头、坏 magic/ver/hdr_len。

## 潜在影响
- 破坏性 wire 变更：任一端未升级将无法互通（magic/ver 校验直接失败）。
- 原先依赖 Core 自动转发 Cmd 的子协议，需要在 Server/端侧 handler 内补齐逐跳转发逻辑（已在本批次 Server 侧补齐关键路径）。

## 回滚方案
- 按提交粒度 `git revert` 回退本次 PR1（需三端同步回退，否则会出现 wire 不匹配导致互通失败）。

