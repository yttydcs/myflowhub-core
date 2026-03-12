# Plan - Core：RFCOMM（Bluetooth Classic）Transport（字节流 Pipe）

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`feat/bluetooth-rfcomm-transport`
- Worktree：`d:\project\MyFlowHub3\worktrees\feat-bluetooth-rfcomm-transport\repo\MyFlowHub-Core`
- Base：`master`（当前发布 tag：`v0.3.0`）
- 关联仓库（同一特性分支名）：
  - `MyFlowHub-Server`：装配 RFCOMM listener + parent dial
  - `MyFlowHub-SDK`：支持 `bt+rfcomm://` 的 dial（可选）
  - `MyFlowHub-Android`：Java/Kotlin 提供 Android RFCOMM Provider

## 背景 / 问题陈述（事实，可审计）
- 项目目标：构建“虚拟网络”，可在多种承载（TCP / Bluetooth Classic RFCOMM 等）之上复用同一套 Header 编解码、路由与子协议。
- 现状：
  - Core 已完成 Pipe 抽象（`core.IPipe` + `IConnection.Pipe()`），Reader/SendDispatcher 已不强依赖 `net.Conn`。
  - Server 已预留 `RFCOMMEnable/RFCOMMUUID` 配置与 CLI flag，但运行时仍返回 “not implemented”。
- 需求：新增 RFCOMM（Bluetooth Classic）transport，要求：
  - v1 走 RFCOMM/SPP 风格**字节流**；
  - 支持 Windows / Linux / Android；
  - 支持 `listen` 与 `dial`；
  - 路由只要求解析 Header（按 header 路由），payload 仅按需解析；
  - UUID-first（默认 MyFlowHub UUID），可选 channel 覆盖/兜底；
  - 允许手工输入目标信息；未来需要预留“按设备名扫描/解析到 MAC”的扩展点；
  - Android 默认 `secure=true`（已确认）。

## 目标
1) 在 Core 提供 RFCOMM 的统一抽象与实现，使其在上层表现为 `core.IPipe` 字节流，并可包装成 `core.IConnection`。
2) 提供可复用的 dial：供 Server parent-link、SDK client 侧复用。
3) 提供 `core.IListener`：供 Server 装配为 listener（与 TCP 并存、可分别开关）。
4) 预留未来扩展：按设备名解析/扫描到 MAC（本轮不实现扫描）。

## 非目标
- 不实现 BLE（GATT）与配对/Pin UI（平台 UI/权限差异大）。
- 不改变 wire 协议：HeaderTcp 编解码、Major/SubProto 语义不改。
- 不要求所有节点解析 payload；仅在需要时才解包。

## 关键接口与约定（v1）
- ParentEndpoint / dial endpoint（跨仓统一）：
  - `bt+rfcomm://<bdaddr>?uuid=<uuid>&channel=<1-30>&adapter=hci0&secure=true&name=<reserved>`
  - 其中 `name` 为预留字段：v1 不实现扫描/解析，若传入则返回明确的 “not implemented” 错误。
- Android 默认：`secure=true`；仍允许通过参数显式指定 `secure=false`（不推荐）。

## 验收标准
- 代码侧（可自动化）：
  - 本仓 `go test ./...` 通过（主机平台）。
  - 端点解析/校验单测覆盖：UUID、bdaddr、channel、保留字段 name 等边界。
  - 至少完成跨平台**可编译**验证（不要求本机跑真机蓝牙）：
    - `GOOS=linux GOARCH=amd64 go test ./...`（编译通过即可）
    - `GOOS=android GOARCH=arm64 go test ./...`（编译通过即可）
- 行为侧（手工冒烟，依赖真实设备/系统环境）：
  - Linux/Windows 任一端 listen，另一端 dial，能完成 MyFlowHub 的基本帧收发（至少能完成一条 Cmd/Resp 链路）。
  - 失败时能输出可定位的错误信息（权限缺失/BlueZ 不可用/Provider 未注入等）。

## 开发/联调方式（避免污染 go.mod）
- 在 `d:\project\MyFlowHub3\worktrees\feat-bluetooth-rfcomm-transport\` 下维护 workflow-local `go.work`（后续任务创建），将 Core/Server/SDK/Android-hubmobile 同时加入，以便在不发布 tag 的情况下完成联调编译与测试。
- 在准备合并到各仓 `main/master` 前，必须确保各仓 `GOWORK=off go test ./...` 仍可编译（如需要发布/升级版本，另在对应仓 workflow 内完成）。

## 3.1) 计划拆分（Checklist）

### CORE-BT0 - 归档旧 plan（已执行）
- 已执行：`git mv plan.md docs/plan_archive/plan_archive_2026-03-12_bluetooth-rfcomm-transport-core-prev.md`

### CORE-BT1 - RFCOMM 端点与配置建模（含校验与扩展点）
- 目标：定义 endpoint 解析与 Options（UUID-first + channel override），并为未来 `name` 解析留接口。
- 涉及模块/文件（预期）：
  - `transport/rfcomm/*`（或同等目录，最终以实现为准）
  - `transport/rfcomm/endpoint_test.go`
- 验收条件：
  - `go test ./...` 通过；
  - `bt+rfcomm://` 的解析与参数校验覆盖边界。
- 回滚点：revert 本任务提交。

### CORE-BT2 - RFCOMM Pipe/Connection 适配（IConnection + IPipe）
- 目标：将平台连接统一包装为 `core.IPipe`，并提供 `core.IConnection` 实现（元数据/LocalAddr/RemoteAddr）。
- 关注点（性能）：
  - 避免无意义拷贝；优先复用缓冲（必要时在 Reader 侧引入 `bufio.Reader` 降低小读次数）。
- 涉及模块/文件（预期）：
  - `listener/rfcomm_listener/connection.go`（或 `transport/rfcomm/connection.go`）
  - `iface.go`（仅在确有必要时，尽量不动核心接口）
- 验收条件：最小可用 `IConnection` + `Pipe()` 可被现有 reader 解码链路复用。
- 回滚点：revert。

### CORE-BT3 - Linux 实现（BlueZ / D-Bus）
- 目标：在 Linux 下实现 RFCOMM listen/dial（依赖 BlueZ + D-Bus）。
- 涉及模块/文件（预期）：
  - `transport/rfcomm/rfcomm_linux.go`（build tag）
  - 可能新增依赖：`github.com/godbus/dbus/v5`（如采用自实现 D-Bus）
- 验收条件：
  - `GOOS=linux GOARCH=amd64 go test ./...` 编译通过；
  - 错误信息清晰可定位（BlueZ 不存在/权限不足等）。
- 回滚点：revert。

### CORE-BT4 - Windows 实现（AF_BTH / RFCOMM）
- 目标：在 Windows 下实现 RFCOMM listen/dial（UUID-first + channel override）。
- 涉及模块/文件（预期）：
  - `transport/rfcomm/rfcomm_windows.go`（build tag）
  - 可能新增依赖：`golang.org/x/sys/windows`
- 验收条件：Windows 下 `go test ./...` 通过；dial/listen 具备可用错误路径与可观测性（日志/错误）。
- 回滚点：revert。

### CORE-BT5 - Android 接入点（Provider 注入 + 默认 secure）
- 目标：Android 侧 Core 不直接依赖 Android Bluetooth API；通过“Java 实现 Go interface”的 Provider 注入实现 dial/listen。
- 涉及模块/文件（预期）：
  - `transport/rfcomm/rfcomm_android.go`（build tag）
  - `transport/rfcomm/android_provider.go`（接口定义，需满足 gomobile bind 可用类型）
- 验收条件：
  - `GOOS=android GOARCH=arm64 go test ./...` 编译通过；
  - 未注入 Provider 且启用 RFCOMM 时返回明确错误（可指导 Android 仓集成）。
- 回滚点：revert。

### CORE-BT6 - RFCOMM Listener（core.IListener）
- 目标：提供 `listener/rfcomm_listener`（或等价包），可在 Server 中与 TCP 组合（MultiListener）。
- 验收条件：接口满足 `core.IListener`；Close/ctx cancel 可可靠退出；Accept 异常处理不自旋。
- 回滚点：revert。

### CORE-BT7 - Code Review（强制）
- 逐项审查：需求覆盖/架构/性能风险/可读性一致性/可扩展性/稳定性与安全/测试覆盖。

### CORE-BT8 - 归档变更（强制）
- 输出：`docs/change/2026-03-12_bluetooth-rfcomm-transport-core.md`
- 必须标注：本特性为重大变更（跨平台新增 transport；涉及端点规范与连接抽象扩展）。

### CORE-BT9 - 合并 / push（需 workflow 结束后执行）
- 在 `repo/MyFlowHub-Core` 合并到 `master` 并 push（是否打 tag 由后续确认）。

