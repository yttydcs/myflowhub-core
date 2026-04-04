# 2026-04-04_bootstrap-selfregister-endpoint-dialer

## 变更背景 / 目标
- `bootstrap.SelfRegister(...)` 之前在 helper 内部直接 `net.Dial("tcp", ParentAddr)`，导致启动前 bootstrap 只能走临时 TCP 连接。
- 本次目标是在不改变 register/login 同步语义的前提下，把 helper 改成“可注入通用 dialer + 通用 `core.IConnection` + 同步收响应”。

## 具体变更内容
- `bootstrap/selfregister.go`
  - `SelfRegisterOptions` 新增 `Dial func(context.Context) (core.IConnection, error)`。
  - `SelfRegister(...)` 改为优先使用注入 dialer；未提供时仍回退到原有 TCP 直连，并包装为 `tcp_listener.NewTCPConnection(...)`。
  - 发送 register/login 改为走 `conn.SendWithHeader(...)`。
  - 同步收响应改为从 `conn.Pipe()` 解码一帧。
  - 为通用连接路径补了 context-aware send/recv 包装：超时后关闭连接并返回 `ctx.Err()`，避免继续卡在读写上。
- `bootstrap/selfregister_test.go`
  - 保留原有 `parseRegisterResp(...)` 覆盖。
  - 新增 injected dialer 成功路径测试。
  - 新增 injected dialer 下 `pending` 返回 `RegisterStatusError` 的测试。

## Requirements impact
- none

## Specs impact
- none

## Lessons impact
- none

## Related requirements
- none

## Related specs
- `../plan.md` 对应 workflow 控制面
- `../../../MyFlowHub-Server/docs/specs/auth.md`
- `../../../MyFlowHub-Server/docs/specs/core.md`

## Related lessons
- none

## 对应 plan.md 任务映射
- `CORE-BOOT-1`：bootstrap helper 支持通用 dialer / connection
- `CORE-BOOT-2`：补充 bootstrap helper 测试
- `CORE-BOOT-3`：Core 测试验证

## 经验 / 教训摘要
- 不需要把 endpoint 解析逻辑搬进 Core；让调用方注入 `core.IConnection` dialer，耦合最小。
- 只抽象到 `IConnection` 即可复用现有 `SendWithHeader` / `Pipe()` 能力，没必要再为 bootstrap 单独发明一层 transport API。

## 可复用排查线索
- 症状：启动前 bootstrap 只支持 TCP，`quic://` / 未来其他 endpoint 在 helper 层被卡住。
- 触发条件：调用 `bootstrap.SelfRegister(...)` 时只能提供 `ParentAddr`，无法复用上层 endpoint dialer。
- 关键词：`SelfRegister`, `ParentAddr`, `Dial`, `Pipe()`, `SendWithHeader`
- 快速检查：
  - 看 `SelfRegisterOptions` 是否带 `Dial`
  - 看 helper 是否仍直接 `net.Dial("tcp", ...)`

## 关键设计决策与权衡
- 保留 `ParentAddr` fallback，避免破坏现有调用点。
- 不修改 `parseRegisterResp(...)` / `assertLoginOK(...)` 协议解析逻辑，降低回归风险。
- timeout 不通过扩展 `IConnection` 接口解决，而是通过 context-aware 读写包装在失败路径关闭连接；这样不需要对所有 transport 额外做 deadline 契约升级。

## 测试与验证方式 / 结果
- `cd D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/MyFlowHub-Core`
- `$env:GOWORK='off'; go test ./bootstrap -count=1`
- `$env:GOWORK='off'; go test ./... -count=1`
- 结果：通过

## 潜在影响
- 新调用方如果提供 `Dial`，必须返回可正常 `SendWithHeader` 和 `Pipe()` 读帧的 `core.IConnection`。
- 超时路径现在会主动关闭连接以打断阻塞读写；这只影响失败路径。

## 回滚方案
- 回退：
  - `bootstrap/selfregister.go`
  - `bootstrap/selfregister_test.go`
- 恢复到 helper 内部 TCP 直连实现即可。

## 子Agent执行轨迹
- 未使用子Agent。
