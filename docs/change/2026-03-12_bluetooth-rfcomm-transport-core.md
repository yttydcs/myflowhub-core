# Core：RFCOMM（Bluetooth Classic）Transport（重大变更）

## 背景 / 目标
- 背景：在 Core 已完成 `core.IPipe` 抽象后，需要新增非 TCP 承载（Bluetooth Classic RFCOMM），以复用现有 Header 编解码、路由与子协议处理。
- 目标：
  - v1 使用 RFCOMM/SPP 风格**字节流**（Read/Write/Close）承载 MyFlowHub 帧；
  - 支持 `listen` 与 `dial`；
  - Windows / Linux / Android 均可编译；并提供真实实现路径；
  - 按 Header 路由，payload 仅透传（避免无意义的 payload 全量解析）。

## 变更内容
### 新增
- `listener/rfcomm_listener/*`
  - Endpoint：
    - `bt+rfcomm://<bdaddr>?uuid=<uuid>&channel=<1-30>&adapter=hci0&secure=true&name=<reserved>`
    - `name` 预留：v1 传入直接返回明确错误（为未来“按设备名扫描/解析到 MAC”留扩展点）。
  - `DialEndpoint(ctx, endpoint)` 与 `Dial(ctx, opts)`：建立 RFCOMM 连接并包装为 `core.IConnection`。
  - `RFCOMMListener`（实现 `core.IListener`）：Accept 到新连接后包装成 `core.IConnection` 并加入 `IConnectionManager`。
  - 平台实现：
    - Windows：Winsock `AF_BTH` + `BTHPROTO_RFCOMM`；支持 UUID-first（ServiceClass GUID）与 channel-first；listener 侧注册服务记录（WSASetService）。
    - Linux：
      - listen：BlueZ D-Bus Profile（`ProfileManager1.RegisterProfile` + `Profile1.NewConnection`）获取连接 FD；
      - dial：`channel>0` 使用内核 RFCOMM socket（兜底）；`channel=0` 使用 BlueZ `Device1.ConnectProfile`（UUID-first）。
    - Android：通过 `AndroidRFCOMMProvider` 注入（gomobile-friendly），由 Java/Kotlin 实现真实蓝牙逻辑。

### 修改
- `go.mod` / `go.sum`
  - 新增依赖：`github.com/godbus/dbus/v5`（Linux BlueZ D-Bus）。

## 关键设计决策与权衡
- **抽象边界**：上层仅依赖 `core.IPipe`（字节流），避免暴露 `net.Conn`，便于扩展到 RFCOMM/串口等承载。
- **UUID-first + channel override**：
  - 默认以 UUID 作为“服务标识”（更一致、跨平台更易用）；
  - 允许 `channel` 覆盖，作为环境不具备 UUID-first 能力时的兜底（尤其在 Linux 下可避免依赖 BlueZ device object 的存在性）。
- **安全默认**：
  - endpoint 默认 `secure=true`；
  - Android 由平台 API 区分 secure/insecure；
  - Linux/Windows 受 OS 能力限制，Linux 通过 BlueZ `RequireAuthentication` 尽量贴近 secure 语义。

## 测试与验证
- `go test ./... -count=1`（主机平台）
- 交叉编译（编译通过即可）：
  - Linux / Android 的关键测试包 `go test -c` 编译通过

## 潜在影响
- Linux 运行期要求 BlueZ + system D-Bus；UUID-first dial 可能要求设备已被 BlueZ 发现/已配对（否则建议使用 channel-first 兜底）。
- Android 运行期需先注入 Provider，否则返回明确错误（避免静默失败）。

## 回滚方案
- revert 本次 RFCOMM 相关提交（主要位于 `listener/rfcomm_listener/*` 与 `go.mod/go.sum`）。

