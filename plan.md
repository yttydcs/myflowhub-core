# Plan - Core：修复 RFCOMM 字节流短写导致的半帧阻塞

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`fix/rfcomm-write-contract`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-rfcomm-write-contract\repo\MyFlowHub-Core`
- Base：`master`
- 关联仓库：
  - `MyFlowHub-SDK`
  - `MyFlowHub-Win`

## 项目目标与当前状态
- 目标：
  - 修复 RFCOMM 连接上发送帧时未保证“写满整帧”导致的半帧阻塞问题；
  - 将“整帧写出契约”下沉到传输/帧写出层，而不是业务子协议层；
  - 为后续串口、USB、命名管道等非 TCP 字节流承载提供统一可靠的写出语义。
- 当前状态：
  - `HeaderTcpCodec.Decode` 按完整头和完整 payload 阻塞读取；
  - Core 发送路径对 `io.Writer` / `IPipe` 默认只写一次；
  - Windows RFCOMM `WSASend` 可能短写，导致接收端卡在半帧读取；
  - 现象已在真实 Win RFCOMM 注册流程中复现：连接成功、发送 register 后长时间无回包。

## 范围
- 必须：
  - 为 Core 引入统一的“写满字节流”能力；
  - 修复 Core 内所有帧写出路径的短写风险；
  - 补充短写场景测试。
- 可选：
  - 对零拷贝路径保留现有优化，只在必要时补写；
  - 增加更明确的短写错误语义与日志。
- 不做：
  - 不修改 auth 子协议语义；
  - 不修改路由决策与 header 结构；
  - 不引入 payload 解析增强。

## 可执行任务清单（Checklist）

### CORE-RFCOMM-1 - 收敛统一写出契约
- 目标：
  - 在 Core 的传输/帧写出层提供统一的 `write-full` 能力，保证完整帧发送完成后才返回。
- 涉及模块 / 文件：
  - `process/frame_writer.go`
  - `frame.go`
  - 新增或调整共享写出辅助文件
- 验收条件：
  - 帧写出逻辑对普通 `io.Writer` 与 `IPipe` 都能正确处理短写；
  - 不要求 transport 层自行保证一次写满。
- 测试点：
  - 使用故意短写的 fake writer 验证完整帧最终被写完；
  - 保留现有 TCP header 编码兼容性。
- 回滚点：
  - 回退新增写出辅助实现与调用点。

### CORE-RFCOMM-2 - 修复 RFCOMM 连接直接发送路径
- 目标：
  - 让 `rfcommConnection.Send` / `SendWithHeader` 与发送调度器使用同一写出契约，避免 listener / direct send 分叉。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/connection.go`
  - `process/senddispatcher.go`
- 验收条件：
  - RFCOMM 连接上的原始发送与带 header 发送都不再依赖单次 `Write` 成功；
  - 不引入新的并发写风险。
- 测试点：
  - fake pipe 短写测试；
  - 回归现有发送调度器测试。
- 回滚点：
  - 回退 RFCOMM connection 与 send dispatcher 的写出调用调整。

### CORE-RFCOMM-3 - 补充回归测试
- 目标：
  - 增加能稳定复现“短写 + 半帧阻塞”风险的单测，防止后续回归。
- 涉及模块 / 文件：
  - `process/*_test.go`
  - `listener/rfcomm_listener/*_test.go`（如有必要）
- 验收条件：
  - 覆盖至少：
    - 普通编码路径短写；
    - HeaderTcp 零拷贝路径短写；
    - RFCOMM connection 直接发送短写。
- 测试点：
  - `go test ./process ./listener/rfcomm_listener -count=1`
- 回滚点：
  - 删除新增测试文件/用例。

### CORE-RFCOMM-4 - Code Review（强制）
- 目标：
  - 审查需求覆盖、分层位置、性能影响、错误处理与测试充分性。
- 涉及模块 / 文件：
  - 本 workflow 全部改动文件
- 验收条件：
  - 明确逐项通过/不通过结论；
  - 若发现问题，返回实现阶段修正。
- 测试点：
  - Review 结论完整可审计。
- 回滚点：
  - 修订实现或取消发布。

### CORE-RFCOMM-5 - 归档与发版准备
- 目标：
  - 形成变更归档，并为新 patch tag 做准备。
- 涉及模块 / 文件：
  - `docs/change/2026-03-15_rfcomm-write-contract-fix.md`
- 验收条件：
  - 文档覆盖背景、设计权衡、任务映射、验证结果、影响与回滚；
  - 明确本次为重大问题修复，但版本建议走 patch。
- 测试点：
  - 归档文档可脱离对话独立理解。
- 回滚点：
  - 回退新增文档。

## 依赖关系
- `CORE-RFCOMM-1` 完成后进入 `CORE-RFCOMM-2`
- `CORE-RFCOMM-2` 完成后进入 `CORE-RFCOMM-3`
- `CORE-RFCOMM-3` 完成后进入 `CORE-RFCOMM-4`
- `CORE-RFCOMM-4` 通过后进入 `CORE-RFCOMM-5`

## 风险与注意事项
- 写满循环必须正确处理中途返回 `n>0, err!=nil` 的场景，避免重复发送或丢字节；
- 不能把修复散落到 auth / UI / routing 层，否则会破坏分层并放大维护成本；
- 需要保留现有 `HeaderTcp` 快路径，避免无意义的额外拷贝；
- 下游 SDK 仍存在同类单次写问题，本仓修完后仍需继续修复 SDK 才能闭环。
