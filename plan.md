# Plan - Core：修复 Windows RFCOMM“只发不回”与无提示

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`fix/rfcomm-win-stream-read`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-rfcomm-win-stream\repo\MyFlowHub-Core`
- Base：`master`
- 关联仓库（后续发版可能涉及）：`MyFlowHub-SDK`、`MyFlowHub-Win`

## 项目目标与当前状态
- 目标：
  - 修复 Win RFCOMM 场景中“客户端可连接但 register/login 无返回”的问题；
  - 避免前端按钮无反馈（本质是后端调用被阻塞/长时间无返回）；
  - 保持 Pipe/Header/SubProto 语义不变。
- 当前状态：
  - Win 日志出现连续 `[TX] register`，但无 `[RX]`；
  - Server 仅看到 `new connection`，无后续业务处理日志；
  - 现象符合“底层读写语义与流式解码不匹配”，导致请求帧未被正确重组/消费。

## 范围
- 必须：
  - 在 Windows RFCOMM Pipe 层引入稳定的“消息到流”读取缓冲；
  - 调整写出策略，降低消息尺寸边界导致的兼容风险；
  - 增加可回归单测覆盖“分段读取”和“分块写出”路径。
- 可选：
  - 提升错误可观测性（截断/异常路径错误语义）。
- 不做：
  - 不修改 Auth 业务协议；
  - 不修改 SDK/Win 业务 API；
  - 不引入计划外跨仓实现改动。

## 可执行任务清单（Checklist）

### WIN-RFCOMM-STREAM-1 - 修复 Windows RFCOMM 流式读取语义
- 目标：
  - 将底层接收的数据先进入内部缓冲，再按上层 `Read` 请求尺寸返回，避免小 buffer 直接读 socket 导致帧重组失败。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows.go`
- 验收条件：
  - 多次小 `Read` 可持续消费同一批接收数据；
  - 不再出现“发送后无返回且无显式错误”的卡住行为。
- 测试点：
  - 新增单测验证“先大包接收，再小块 Read”。
- 回滚点：
  - 回退 `native_windows.go` 对读取路径改动。

### WIN-RFCOMM-STREAM-2 - 修复/强化写出分块策略
- 目标：
  - 在 Windows RFCOMM 写路径中采用受控分块写出，减少消息尺寸边界差异导致的不确定行为。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows.go`
- 验收条件：
  - `Write` 在正常路径返回完整写入字节数；
  - `WSAEMSGSIZE` 场景可降级继续写出，不出现静默挂起。
- 测试点：
  - 单测验证分块次数与总写出字节。
- 回滚点：
  - 回退写出策略改动。

### WIN-RFCOMM-STREAM-3 - 回归测试与验证
- 目标：
  - 用稳定单测覆盖本次修复的关键边界行为。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows_test.go`
- 验收条件：
  - `GOWORK=off go test ./listener/rfcomm_listener -count=1` 通过；
  - `GOWORK=off go test ./... -count=1` 通过。
- 回滚点：
  - 回退新增测试。

### WIN-RFCOMM-STREAM-4 - Code Review 与归档
- 目标：
  - 输出逐项评审结论并归档到 `docs/change`。
- 涉及模块 / 文件：
  - `docs/change/2026-03-15_windows-rfcomm-stream-fix.md`（日期按实际）
- 验收条件：
  - 评审项齐全（需求/架构/性能/可维护性/测试）；
  - 文档可独立交接。

## 依赖关系
- `WIN-RFCOMM-STREAM-1` -> `WIN-RFCOMM-STREAM-2` -> `WIN-RFCOMM-STREAM-3` -> `WIN-RFCOMM-STREAM-4`

## 风险与注意事项
- 读取缓冲必须避免并发数据竞争与无界增长；
- 分块写出不能破坏上层帧字节顺序；
- 出错时优先返回明确错误，避免“静默无响应”。

---

# Plan - Core：QUIC 传输接入（已完成归档）

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`feat/quic-transport-core`
- Worktree：`d:\project\MyFlowHub3\repo\MyFlowHub-Core\worktrees\feat-quic-transport-core`
- Base：`master`
- 关联仓库：`MyFlowHub-SDK`、`MyFlowHub-Server`、`MyFlowHub-Win`

## 目标与结果
- 目标：新增 QUIC（UDP 家族）传输能力，保持现有 `HeaderTcpCodec + Router + SubProto` 主链路不变。
- 结果：Core/SDK/Server/Win 已完成接入、依赖收敛、测试验证与发布标签。

## Checklist（完成态）
- [x] `QUIC-CORE-1` Core 新增 `listener/quic_listener`（listen/dial/connection/endpoint）
- [x] `QUIC-CORE-2` TLS1.3 + pin_sha256 + mTLS 预留
- [x] `QUIC-SDK-1` SDK `ConnectEndpoint` 支持 `quic://`
- [x] `QUIC-SERVER-1` Server Runtime 支持 QUIC listener 与 `quic://` parent endpoint
- [x] `QUIC-WIN-1` Win 依赖链对齐并验证
- [x] `QUIC-REL-1` 发布链路：Core `v0.4.7` -> SDK `v0.1.10` -> Server `v0.0.11` -> Win `v0.0.10`
