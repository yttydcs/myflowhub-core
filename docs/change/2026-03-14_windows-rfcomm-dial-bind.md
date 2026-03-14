# Core：修复 Windows RFCOMM Dial 前置 Bind

## 变更背景 / 目标
- 背景：
  - Windows 侧 RFCOMM listener 已可启动并 accept 到来自 Win 客户端的连接；
  - 但 Win 客户端在发起 `bt+rfcomm://...` 连接后，会出现：
    - `Only one usage of each socket address (protocol/network address/port) is normally permitted.`
    - 或连接后首个 `send/recv` 报 `socket is not connected`
- 目标：
  - 修复 Windows RFCOMM client dial 的底层建链流程；
  - 保持 `Win / SDK / Server` 的外部接口和 endpoint 规范不变。

## 具体变更内容
### 修改
- `listener/rfcomm_listener/native_windows.go`
  - 在 `dialNative` 中补齐 Windows RFCOMM client 的本地 `Bind`：`BT_PORT_ANY` / `GUID_NULL` / `btAddr=0`
  - 将 RFCOMM dial 侧 sockaddr 构造拆为：
    - `newDialLocalSockaddrBth`
    - `newDialRemoteSockaddrBth`
    - `newDialAddrFromSockaddr`
  - `connect` 成功后读取本地 `getsockname`，用于生成更准确的本地诊断地址

### 新增
- `listener/rfcomm_listener/native_windows_test.go`
  - 覆盖 local bind sockaddr 必须为零地址/零端口
  - 覆盖 remote connect sockaddr 在 channel-first 与 uuid-first 两种模式下的构造差异

## 对应 plan.md 任务映射
- `WIN-DIAL-1`：修正 Windows RFCOMM client dial 建链流程
- `WIN-DIAL-2`：补充稳定单测
- `WIN-DIAL-3`：Code Review 与归档

## 关键设计决策与权衡
- **只修 transport 根因**
  - 问题根因位于 Core 的 Windows RFCOMM dial 建链，不扩散到 Win UI、SDK await 或 Auth 子协议。
- **先 bind 再 connect**
  - 这是 Windows Bluetooth/Winsock 的平台要求；遗漏该步骤会导致 `WSAEADDRINUSE / WSAENOTCONN` 一类错误。
- **helper 化 sockaddr 构造**
  - 将“本地 bind 地址”和“远端 connect 地址”分离，后续若扩展 scan-by-name 或 SDP 路径，维护成本更低。
- **不增加运行期帧成本**
  - 新增系统调用只发生在建链阶段，不影响 steady-state 收发性能。

## 测试与验证方式 / 结果
- 自动化：
  - `GOWORK=off go test ./listener/rfcomm_listener -count=1`：通过
  - `GOWORK=off go test ./... -count=1`：通过
- 手工验证：
  - 当前仓内已修复到可编译、可测试状态；
  - 真机蓝牙双机验证需在用户环境继续确认 Win 客户端连接、注册、登录是否恢复正常。

## 潜在影响与回滚方案
- 潜在影响：
  - Windows RFCOMM dial 建链路径增加一次显式 `Bind`；
  - 本地诊断地址可能显示系统分配的本地 RFCOMM port/channel。
- 回滚方案：
  - 回退 `listener/rfcomm_listener/native_windows.go`
  - 回退 `listener/rfcomm_listener/native_windows_test.go`

## Code Review（结论）
- 需求覆盖：通过（已覆盖 Win 客户端 RFCOMM 连接失败的已知根因）
- 架构合理性：通过（修复点收敛在 Core transport 层，未污染上层）
- 性能风险：通过（仅增加建链阶段固定成本）
- 可读性与一致性：通过（sockaddr 构造职责更清晰）
- 可扩展性与配置化：通过（channel-first / uuid-first 仍保持统一入口）
- 稳定性与安全：通过（遵循平台建链要求，降低伪连接与未连接写入风险）
- 测试覆盖情况：通过（新增 Windows 单测并完成全仓 Go 测试）
