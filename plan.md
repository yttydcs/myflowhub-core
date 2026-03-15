# Plan - Core：修复 Windows RFCOMM 连接后中止（10053）

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`fix/rfcomm-win-abort`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-rfcomm-win-abort\repo\MyFlowHub-Core`
- Base：`master`
- 关联仓库（后续可能需要发版对齐）：`MyFlowHub-SDK`、`MyFlowHub-Win`

## 项目目标与当前状态
- 目标：
  - 修复 Windows RFCOMM 在“已连接后首次业务交互（register/login）”阶段出现的连接中止问题；
  - 保持现有 Pipe/Router/SubProto 抽象和协议语义不变；
  - 将修复收敛在 transport 层，避免业务层分叉补丁。
- 当前状态：
  - Win 客户端可完成 RFCOMM 建链，Server 侧能看到 `new connection`；
  - 但注册/登录阶段 Win 报 `An established connection was aborted by the software in your host machine`（典型 10053）；
  - 现网证据显示连接关闭链路可观测性不足，且 Windows socket 读写边界语义存在风险点。

## 范围
- 必须：
  - 修复 Windows RFCOMM `Read` 在 0 字节返回时的 EOF 语义；
  - 增强 Windows RFCOMM `Write` 对 `WSAEMSGSIZE` 的兼容写出策略（不改上层协议）；
  - 补充单测覆盖上述边界路径；
  - 提升错误语义可诊断性（至少可区分 EOF/消息尺寸错误）。
- 可选：
  - 在不增加业务层耦合前提下，补充最小必要的 transport 日志或错误包装信息。
- 不做：
  - 不改 Auth/VarStore 等子协议处理逻辑；
  - 不改 HeaderTcp 格式与路由规则；
  - 不引入按 payload 深解析的全链路开销。

## 可执行任务清单（Checklist）

### WIN-RFCOMM-ABORT-1 - 修正 Windows RFCOMM 读写边界语义
- 目标：
  - 修复 `Read` 0 字节返回导致的“非 EOF”行为；
  - 为 `Write` 增加 `WSAEMSGSIZE` 兼容写出策略，保障大帧可持续发送。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows.go`
- 验收条件：
  - `Read` 在连接正常关闭时返回 `io.EOF`；
  - `Write` 在遇到消息尺寸限制时可降级分片发送（或返回明确错误，不再静默失败）；
  - 不影响 TCP / Linux / Android 路径。
- 测试点：
  - Windows 单测：EOF 语义；
  - Windows 单测：`WSAEMSGSIZE` 触发下的降级写出。
- 回滚点：
  - 回退 `native_windows.go` 改动。

### WIN-RFCOMM-ABORT-2 - 补充可回归测试
- 目标：
  - 用可重复测试覆盖这次缺陷路径，防止回归。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows_test.go`
- 验收条件：
  - 新增用例稳定通过，且不依赖真实蓝牙硬件；
  - 现有 RFCOMM listener 单测不退化。
- 测试点：
  - `go test ./listener/rfcomm_listener -count=1`
- 回滚点：
  - 回退新增测试用例。

### WIN-RFCOMM-ABORT-3 - Code Review（强制）
- 目标：
  - 对需求覆盖、分层合理性、性能影响、错误处理与测试充分性做逐项评审。
- 涉及模块 / 文件：
  - 本 workflow 全部改动文件
- 验收条件：
  - 输出逐项“通过/不通过”；
  - 不通过项必须回到实现阶段修正。
- 测试点：
  - 评审结论完整可审计。
- 回滚点：
  - 修订实现或取消发布。

### WIN-RFCOMM-ABORT-4 - 归档与发布准备
- 目标：
  - 形成 docs/change 归档，供后续 Core/SDK/Win 发版对齐。
- 涉及模块 / 文件：
  - `docs/change/2026-03-15_windows-rfcomm-abort-fix.md`（文件名以实际日期为准）
- 验收条件：
  - 文档覆盖背景、任务映射、关键权衡、验证结果、影响与回滚；
  - 明确本次属于 transport 稳定性修复（patch）。
- 测试点：
  - 文档可脱离对话独立交接。
- 回滚点：
  - 回退归档文档。

## 依赖关系
- `WIN-RFCOMM-ABORT-1` 完成后进入 `WIN-RFCOMM-ABORT-2`
- `WIN-RFCOMM-ABORT-2` 完成后进入 `WIN-RFCOMM-ABORT-3`
- `WIN-RFCOMM-ABORT-3` 通过后进入 `WIN-RFCOMM-ABORT-4`

## 风险与注意事项
- 读写语义修复必须遵循 `io.Reader/io.Writer` 约定，避免引入 busy-loop 或重复发送；
- 分片发送策略不能破坏帧边界语义（Header + Payload 仍由上层编码后作为字节流连续发送）；
- 不引入 plan 外跨仓改动；若需 SDK/Win 跟进，仅在本仓完成后另建 workflow；
- 所有行为变化必须可通过日志/错误信息定位。
