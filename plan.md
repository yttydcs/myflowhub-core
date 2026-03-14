# Plan - Core：Windows RFCOMM Listener 缺陷修复

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`fix/windows-rfcomm-listener`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-windows-rfcomm-listener\repo\MyFlowHub-Core`
- Base：`master`
- 范围：仅修复 Core 的 Windows RFCOMM listener；下游仓库不做实现性改动

## 项目目标与当前状态
- 目标：
  - 修复 Windows 作为 RFCOMM listener/server 时的启动异常与伪连接问题；
  - 保持现有 Pipe / Header / 路由抽象不变，让 Windows 端至少具备可稳定使用的 channel-first RFCOMM listen 能力。
- 当前状态（已复现，可审计）：
  - 在 Windows 上启动 `hub_server -tcp-enable=false -rfcomm-enable=true -rfcomm-channel 12` 后，即使蓝牙关闭、无客户端，也会持续出现：
    - `new connection`
    - `conn exists`
    - `An operation was attempted on something that is not a socket.`
  - 根因方向已确认：
    1. Windows `accept` 返回 `INVALID_SOCKET` 时，当前实现可能把它当成成功连接；
    2. Windows `SOCKADDR_BTH` 使用了错误的原始内存布局；
    3. RFCOMM 连接 ID 依赖地址字符串，导致地址缺失/重复时发生 `conn exists`；
    4. Windows listener 的 `WSASetService` 注册路径会触发持续异常 accept，本轮需要收敛为更稳定的 channel-first 策略。

## 约束与验收口径
- 不修改公开 endpoint 格式；
- 不改动 Linux / Android RFCOMM 行为；
- Windows listen 若不支持 `channel=0`，必须返回明确错误，不能假启动；
- 验收口径：
  - A 端（Windows）单机启动，不再出现伪连接日志风暴；
  - A 端 listener 能稳定运行；
  - 双机手工测试时，B 端可通过 `bt+rfcomm://...&channel=<n>` 连接 A，并继续做 `Register/Login`。

## 可执行任务清单（Checklist）

### WIN-RFCOMM-1 - 修正 Windows 原生 RFCOMM socket/address 层
- 目标：
  - 使用正确的 `SOCKADDR_BTH` 原始布局；
  - 收敛 Windows listen 为显式 channel 模式；
  - 明确禁止 Windows listen 的 `channel=0` 假启动路径。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows.go`
- 验收条件：
  - 不再使用错误的自定义 `sockaddrBth` 作为原始布局；
  - Windows listen 在 `channel<=0` 时返回清晰错误；
  - Windows listen 的显式 channel 启动不再依赖 `WSASetService`。
- 测试点：
  - Windows 本机 `go test ./... -count=1`
  - Windows 手工启动 server，观察启动日志
- 回滚点：
  - revert `native_windows.go` 本任务提交

### WIN-RFCOMM-2 - 修正 accept/连接包装与唯一连接 ID
- 目标：
  - 让 `INVALID_SOCKET` 严格走错误路径；
  - 避免把无效 accept 包装成 `core.IConnection`；
  - 连接 ID 改为唯一序列，不再依赖地址字符串。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows.go`
  - `listener/rfcomm_listener/connection.go`
  - `listener/rfcomm_listener/listener.go`
- 验收条件：
  - 单机启动时不再出现 `new connection` / `conn exists` 风暴；
  - RFCOMM 新连接的 ID 全局唯一；
  - 无效 accept 不会进入 read loop。
- 测试点：
  - Windows 本机启动 `hub_server`（蓝牙关闭 / 无客户端）
  - 观察日志中不再出现伪连接
- 回滚点：
  - revert 本任务涉及文件的提交

### WIN-RFCOMM-3 - 补充验证与文档
- 目标：
  - 补充能稳定覆盖本轮变更的最小测试；
  - 记录 Windows listener 的行为约束与手工验证方法。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/*_test.go`（如需要）
  - `docs/change/2026-03-14_windows-rfcomm-listener-fix.md`
- 验收条件：
  - 自动化测试覆盖至少一项稳定逻辑（如地址编码/解码、连接 ID、channel 校验）；
  - 变更文档明确写出 Windows listen 当前采用 channel-first 策略。
- 测试点：
  - `go test ./... -count=1`
  - Windows 手工启动与双机连接说明
- 回滚点：
  - revert 文档与测试提交

### WIN-RFCOMM-4 - Code Review 与归档
- 目标：
  - 按要求完成需求覆盖、架构、性能、可读性、扩展性、稳定性、安全、测试覆盖逐项审查；
  - 归档本次缺陷修复。
- 涉及模块 / 文件：
  - `plan.md`
  - `docs/change/2026-03-14_windows-rfcomm-listener-fix.md`
- 验收条件：
  - Review 结论完整；
  - docs/change 文档齐全；
  - 可交接、可审计。
- 测试点：
  - Review 逐项结论明确
- 回滚点：
  - 保留代码，重新修订 review / docs

## 依赖关系
- `WIN-RFCOMM-1` 完成后才能做 `WIN-RFCOMM-2`
- `WIN-RFCOMM-2` 完成后才能进入 `WIN-RFCOMM-3`
- 全部实现完成后必须进入 `WIN-RFCOMM-4`

## 风险与注意事项
- Windows 蓝牙栈行为依赖系统环境；本轮优先修复“单机伪连接/假启动”这一确定性问题；
- 若 Windows listener 的 UUID-first 服务发现能力仍需恢复，应作为后续独立任务，不与本轮稳定性修复耦合；
- 本轮不扩展下游仓库能力，避免放大变更面；
- 所有实现性修改必须只发生在本 worktree 中。
