# Core：Windows RFCOMM Listener 缺陷修复

## 变更背景 / 目标
- 背景：
  - RFCOMM（Bluetooth Classic）transport 已作为重大变更接入 Core。
  - 后续在 Windows 上作为 RFCOMM listener/server 进行手工测试时，出现了明显异常：
    - 蓝牙关闭、无客户端时仍持续出现 `new connection`
    - 大量 `conn exists`
    - `An operation was attempted on something that is not a socket.`
- 目标：
  - 修复 Windows RFCOMM listener 的伪连接与错误路径；
  - 修复连接标识冲突；
  - 保持 Pipe / Header / 路由抽象不变。

## 具体变更内容
### 修改
- `listener/rfcomm_listener/native_windows.go`
  - 改用正确的 `windows.RawSockaddrBth` 原始布局进行 RFCOMM 地址编码/解码；
  - `bind/connect/listen` 改为调用 `golang.org/x/sys/windows` 提供的封装；
  - `accept/getsockname/WSASetService` 的错误改为读取真实调用错误，不再依赖失真的 `WSAGetLastError`；
  - 增加 `svcRegDone`，仅在服务注册成功时执行 delete。
- `listener/rfcomm_listener/connection.go`
  - RFCOMM 连接 ID 改为进程内唯一序列 `rfcomm#<seq>`，不再依赖地址字符串拼接。

### 新增
- `listener/rfcomm_listener/connection_test.go`
  - 覆盖 RFCOMM 连接 ID 唯一性与 nil pipe 边界。
- `listener/rfcomm_listener/native_windows_test.go`
  - 覆盖 Windows `RawSockaddrBth` 编解码回归。

## 对应 plan.md 任务映射
- `WIN-RFCOMM-1`：修正 Windows 原生 RFCOMM socket/address 层
- `WIN-RFCOMM-2`：修正 accept/连接包装与唯一连接 ID
- `WIN-RFCOMM-3`：补充验证与文档
- `WIN-RFCOMM-4`：Code Review 与归档

## 关键设计决策与权衡
- **优先修根因，不屏蔽日志**
  - 伪连接根因是 Winsock 错误读取错误，导致 `INVALID_SOCKET` 被当成成功 accept，而不是日志策略问题。
- **地址编码/解码集中化**
  - 将 Windows `SOCKADDR_BTH` 的原始布局收敛到 helper，避免后续继续使用错误结构体布局。
- **连接 ID 与地址解耦**
  - 地址是诊断信息，不应承担唯一身份职责；唯一 ID 使用序列更稳定，也避免地址缺失时出现 `conn exists`。
- **最小化变更面**
  - 只修改 Core 的 Windows RFCOMM 路径，不扩散到 Server / SDK / Android。

## 测试与验证方式 / 结果
- 自动化：
  - `GOWORK=off go test ./listener/rfcomm_listener -count=1`：通过
  - `GOWORK=off go test ./... -count=1`：通过
- 本机冒烟（当前 Windows 环境）：
  - 使用临时最小程序调用 `rfcomm_listener.Listen(...)`
  - 当蓝牙适配器不可用时，返回明确错误：
    - `A socket operation encountered a dead network.`
  - 不再出现伪连接日志风暴。

## 潜在影响
- Windows RFCOMM 真实错误会更早暴露：
  - 例如蓝牙关闭、适配器不可用时，listener 会明确启动失败，而不是“假启动 + 后台刷错误”。
- 连接 ID 由可读地址串改为唯一序列：
  - 日志可读性略降，但稳定性显著提升；地址仍可从 `LocalAddr/RemoteAddr` 获取。

## 回滚方案
- revert 本次提交，恢复：
  - `listener/rfcomm_listener/native_windows.go`
  - `listener/rfcomm_listener/connection.go`
  - 新增测试文件

## Code Review（结论）
- 需求覆盖：通过
- 架构合理性：通过
- 性能风险：通过（仅修建链与连接标识；未增加帧处理开销）
- 可读性与一致性：通过
- 可扩展性与配置化：通过
- 稳定性与安全：通过（真实错误显式暴露，避免假成功）
- 测试覆盖情况：通过
