# Plan - Core：修复 Windows RFCOMM Dial 前置 Bind

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`fix/windows-rfcomm-dial-bind`
- Worktree：`d:\project\MyFlowHub3\worktrees\windows-rfcomm-dial-bind`
- Base：`master`
- 范围：仅修复 Core 的 Windows RFCOMM client dial 建链逻辑；不改 Win UI、SDK API、Server 协议语义

## 项目目标与当前状态
- 目标：
  - 修复 Windows 作为 RFCOMM dial/client 时，连接已被服务端 accept 但客户端仍报 `Only one usage of each socket address...` 或后续 `socket is not connected` 的问题；
  - 保持 endpoint 规范不变：继续使用 `bt+rfcomm://<bdaddr>?uuid=...&channel=...`；
  - 为 Win 侧后续重新发布提供稳定 Core 修复基线。
- 当前状态：
  - Server 侧 RFCOMM listener 已能启动并 accept 到来自 Windows 客户端的连接；
  - Win 客户端在 `Session.Connect -> SDK Session.ConnectEndpoint -> Core rfcomm_listener.DialEndpoint -> native_windows.go:dialNative` 链路中失败；
  - 根据 Microsoft Bluetooth/Winsock 文档，RFCOMM 客户端在 `connect` 前应先 `bind` 本地 `SOCKADDR_BTH{btAddr=0, serviceClassId=GUID_NULL, port=0}`，当前实现缺失该步骤。

## 可执行任务清单（Checklist）

### WIN-DIAL-1 - 修正 Windows RFCOMM client dial 建链流程
- 目标：
  - 在 `dialNative` 中补齐本地 `bind(BT_PORT_ANY)`；
  - 明确区分“本地 bind 地址”和“远端 connect 地址”；
  - 回填本地地址信息，便于日志和诊断。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows.go`
- 验收条件：
  - `dialNative` 在 `connect` 前完成本地 `windows.Bind`；
  - channel-first/uuid-first 两种 remote sockaddr 构造仍正确；
  - 不改外部 endpoint/API。
- 测试点：
  - Windows 编译与单测通过；
  - 结构性测试覆盖本地/远端 sockaddr 构造逻辑。
- 回滚点：
  - 回退 `native_windows.go` 改动。

### WIN-DIAL-2 - 补充稳定单测
- 目标：
  - 把本轮修复抽成可稳定测试的 helper，避免依赖真实蓝牙硬件。
- 涉及模块 / 文件：
  - `listener/rfcomm_listener/native_windows.go`
  - `listener/rfcomm_listener/native_windows_test.go`
- 验收条件：
  - 单测覆盖 client local bind sockaddr = `btAddr=0 / port=0 / guid=zero`；
  - 单测覆盖 remote connect sockaddr 在 `channel>0` 与 `channel=0` 时的构造差异。
- 测试点：
  - `go test ./listener/rfcomm_listener -count=1`
- 回滚点：
  - 回退 helper 与测试变更。

### WIN-DIAL-3 - Code Review 与归档
- 目标：
  - 补充变更归档、验证记录与逐项审查结论。
- 涉及模块 / 文件：
  - `plan.md`
  - `docs/change/2026-03-14_windows-rfcomm-dial-bind.md`
- 验收条件：
  - 文档覆盖背景、根因、变更、验证、影响、回滚；
  - Review 结论完整。
- 测试点：
  - 审查项全部有明确结论。
- 回滚点：
  - 回退文档提交。

## 依赖关系
- `WIN-DIAL-1` 完成后进入 `WIN-DIAL-2`
- `WIN-DIAL-2` 完成后进入 `WIN-DIAL-3`

## 风险与注意事项
- 当前环境缺少真实蓝牙自动化链路，本轮必须把关键逻辑抽成可单测 helper；
- 只修建链流程，不扩大到 Session 生命周期与 UI 重连策略；
- 若修复后仍存在“连接后断开/注册卡住”，应另起 workflow 分析读写或子协议问题。
