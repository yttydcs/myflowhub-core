# 2026-03-15 - Core：修复 Windows RFCOMM 连接后中止（10053）

## 变更背景 / 目标
- 背景：
  - Win 客户端可通过 RFCOMM 建链到 Server，但在注册/登录首帧交互阶段出现连接中止（10053）；
  - 现网日志显示可连接但会话生命周期可观测性不足，且 Windows socket 读写边界语义存在风险。
- 目标：
  - 在不改业务协议和路由语义的前提下，修复 Windows RFCOMM transport 的边界行为；
  - 提供可回归测试，降低后续回归风险。

## 具体变更内容
### 修改
- `listener/rfcomm_listener/native_windows.go`
  - `Read`：当 `WSARecv` 成功但返回 0 字节时，按 `io.Reader` 语义返回 `io.EOF`；
  - `Read`：当 `WSARecv` 返回 `WSAEMSGSIZE` 且已读取部分字节时，优先返回已读取字节，避免无谓中断；
  - `Write`：增加 `WSAEMSGSIZE` 兼容路径，首写遇到消息尺寸错误时自动降级为分块写出；
  - 引入 `wsaRecvFn` / `wsaSendFn` 可替换函数变量，便于在单测中稳定模拟 Winsock 错误路径。

### 新增/调整测试
- `listener/rfcomm_listener/native_windows_test.go`
  - 新增 `TestWinSockPipeReadZeroReturnsEOF`：覆盖 0 字节读返回 EOF；
  - 新增 `TestWinSockPipeWriteFallbackOnMsgSize`：覆盖 `WSAEMSGSIZE` 下分块写出降级路径。

## 对应 plan.md 任务映射
- `WIN-RFCOMM-ABORT-1`：修正 Windows RFCOMM 读写边界语义
- `WIN-RFCOMM-ABORT-2`：补充可回归测试
- `WIN-RFCOMM-ABORT-3`：Code Review（本文件末尾）
- `WIN-RFCOMM-ABORT-4`：归档与发布准备（本文件）

## 关键设计决策与权衡
- 仅在 transport 层修复，不将问题上抬到 Auth/SDK/UI：
  - 优点：保持分层稳定，不引入业务侧特殊分支；
  - 代价：需要更细致处理 WinSock 边界语义。
- 对 `WSAEMSGSIZE` 使用“按需降级分块”而非常态分块：
  - 优点：常态路径维持低 syscall 开销；
  - 代价：错误路径逻辑稍复杂，但通过单测覆盖。
- 严格遵循 `io.Reader` EOF 契约：
  - 优点：可避免 read loop 在连接关闭后的无进展循环，提升连接生命周期可观测性；
  - 代价：无额外性能成本。

## 测试与验证方式 / 结果
- 包级验证：
  - `GOWORK=off go test ./listener/rfcomm_listener -count=1`：通过
- 全仓验证：
  - `GOWORK=off go test ./... -count=1`：通过

## 潜在影响
- 正向影响：
  - 连接关闭语义更准确，读循环不会因 0 字节读而卡死；
  - 遇到 WinSock 消息尺寸限制时具备自动降级能力，提高 RFCOMM 大帧稳健性。
- 行为变化：
  - 某些此前“无错误但无进展”的场景会变为显式 EOF，便于上层感知断连并清理状态。

## 回滚方案
- 回滚以下文件即可恢复旧行为：
  - `listener/rfcomm_listener/native_windows.go`
  - `listener/rfcomm_listener/native_windows_test.go`

## Code Review（3.3 强制）
- 需求覆盖：通过  
  结论：修复点直接对应“连接后交互中止 + 生命周期不可观测”问题，且不改业务协议。
- 架构合理性：通过  
  结论：改动收敛在 Core Windows RFCOMM transport，不污染 SDK/Win/SubProto。
- 性能风险：通过  
  结论：常态写路径不增加额外循环；降级分块仅在 `WSAEMSGSIZE` 错误路径触发。
- 可读性与一致性：通过  
  结论：读写语义和错误处理明确，测试覆盖关键分支。
- 可扩展性与配置化：通过  
  结论：为后续更多 WinSock 特殊错误处理保留了可测试扩展点（函数变量注入）。
- 稳定性与安全：通过  
  结论：EOF/错误语义更准确，避免无进展循环导致的连接泄漏风险。
- 测试覆盖情况：通过  
  结论：新增边界回归测试 + 全量测试通过。
