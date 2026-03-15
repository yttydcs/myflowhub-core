# 2026-03-15 - Core：修复 Windows RFCOMM“只发不回”流式兼容问题

## 变更背景 / 目标
- 背景：
  - Win 客户端可连接 RFCOMM，但 register/login 仅见 `[TX]`，无 `[RX]`；
  - Server 只出现 `new connection`，无后续处理日志。
- 目标：
  - 修复 Windows RFCOMM Pipe 在“消息语义与流式解码”组合下的兼容问题；
  - 保证上层 `HeaderTcp` 解码可以持续获得完整字节流。

## 具体变更内容
### 修改
- `listener/rfcomm_listener/native_windows.go`
  - 为 `winSockPipe` 增加内部读取缓存（`readCache/readScratch`）；
  - `Read` 改为“先收包入缓存，再按调用方请求长度切片返回”，避免小 buffer 直读 socket 导致帧重组失败；
  - `Write` 改为受控分块发送（`winRFCOMMWriteChunkBytes=256`），并保留 `WSAEMSGSIZE` 降级处理。

### 新增/调整测试
- `listener/rfcomm_listener/native_windows_test.go`
  - `TestWinSockPipeReadCachesPacketForSmallReads`：验证单次接收可被多次小 `Read` 连续消费；
  - `TestWinSockPipeWriteUsesBoundedChunks`：验证写入分块上限与总写入完整性。

## 对应 plan.md 任务映射
- `WIN-RFCOMM-STREAM-1`：流式读取语义修复
- `WIN-RFCOMM-STREAM-2`：写出分块策略修复
- `WIN-RFCOMM-STREAM-3`：回归测试与验证
- `WIN-RFCOMM-STREAM-4`：评审与归档

## 关键设计决策与权衡
- 在 transport 层做“消息到流”适配，而不是在协议层兜底：
  - 优点：修复点收敛、上层协议不变；
  - 代价：Pipe 实现复杂度略升。
- 采用固定上限分块发送而非一次性大发送：
  - 优点：降低不同蓝牙栈消息尺寸边界带来的不确定行为；
  - 代价：系统调用次数增加，但对 auth 小帧影响可接受。

## 测试与验证方式 / 结果
- `GOWORK=off go test ./listener/rfcomm_listener -count=1`：通过
- `GOWORK=off go test ./... -count=1`：通过

## 潜在影响与回滚方案
- 潜在影响：
  - Windows RFCOMM 写路径变为分块发送，极端高吞吐场景 syscall 略增；
  - 读取路径引入内存缓冲，常驻额外小规模内存。
- 回滚方案：
  - 回退 `listener/rfcomm_listener/native_windows.go`
  - 回退 `listener/rfcomm_listener/native_windows_test.go`

## Code Review（3.3 强制）
- 需求覆盖：通过（直指“只发不回”）
- 架构合理性：通过（修复收敛在 Core transport）
- 性能风险：通过（小帧场景可接受；高吞吐可后续再调 chunk）
- 可读性与一致性：通过（读写职责明确）
- 可扩展性与配置化：通过（后续可按平台调 chunk）
- 稳定性与安全：通过（避免静默卡住）
- 测试覆盖情况：通过（新增关键边界测试 + 全量回归）
