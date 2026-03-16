# 变更背景 / 目标

在保留现有 TCP / RFCOMM 字节流抽象与子协议栈的前提下，新增 QUIC 传输能力（UDP 家族），为后续跨平台低时延链路提供基础。

# 具体变更内容

- 新增 `listener/quic_listener/`：
  - `listener.go`：`core.IListener` 实现（listen + accept + stream 接入）；
  - `dial.go`：`Dial` / `DialEndpoint`；
  - `connection.go`：`IConnection` 与 `IPipe` 封装；
  - `endpoint.go`：`quic://` endpoint 解析与校验；
  - `tls_config.go`：TLS1.3、pin_sha256、mTLS 预留配置；
  - `endpoint_test.go`、`listener_test.go`：解析与回环连通测试。
- `go.mod` / `go.sum`：
  - 引入 `github.com/quic-go/quic-go` 及相关依赖。

# 对应任务映射

- QUIC-CORE-1：QUIC listener/dial/connection 骨架
- QUIC-CORE-2：安全策略（单向 TLS + pinning，预留 mTLS）

# 关键设计决策与权衡

- v1 采用“单连接单流”映射到现有 `IPipe`，优先兼容性和可维护性；
- 不改 `HeaderTcpCodec` 与上层路由流程，降低回归风险；
- 默认 TLS1.3，pin 校验为可选增强，`insecure` 仅用于调试。

# 测试与验证

- `GOWORK=off go test ./listener/quic_listener ./...`

# 潜在影响与回滚

- 影响：新增 QUIC 依赖与监听能力，不影响既有 TCP/RFCOMM 逻辑。
- 回滚：删除 `listener/quic_listener/*` 并回退 `go.mod/go.sum`。
