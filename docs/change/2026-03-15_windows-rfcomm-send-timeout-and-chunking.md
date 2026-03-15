# 变更背景 / 目标

Windows RFCOMM 场景下，出现“连接成功但请求无响应、调用长时间挂起”的问题。目标是让发送路径具备更稳定的行为，避免在底层 socket 写入阶段出现长时间卡死。

# 具体变更内容

- 修改 `listener/rfcomm_listener/native_windows.go`
  - RFCOMM 写分片上限从 `256` 调整为 `127`（更保守，降低 provider 分片异常概率）。
  - `WSASocket` 增加 `WSA_FLAG_OVERLAPPED`。
  - 为 dial 侧 socket 与 accept 后连接 socket 设置 `SO_SNDTIMEO=8000ms`（best-effort）。

# 对应任务映射

- Task: Windows RFCOMM 写路径稳定性修复
  - 目标：避免写阶段无限阻塞、提升跨设备兼容性。
  - 文件：`listener/rfcomm_listener/native_windows.go`
  - 验收：`go test ./listener/rfcomm_listener` 通过；`go test ./...` 通过。

# 关键设计决策与权衡

- 采用保守分片尺寸（127）换取稳定性，代价是峰值吞吐可能下降。
- 采用发送超时保护，避免调用链被底层写阻塞无限期卡住。
- 仅设置发送超时，不设置接收超时，避免空闲连接被误判超时断开。

# 测试与验证

- `go test ./listener/rfcomm_listener`
- `go test ./...`

# 潜在影响与回滚

- 影响：Windows RFCOMM 大负载发送行为变得更保守，吞吐可能略降。
- 回滚：恢复 `winRFCOMMWriteChunkBytes`、`WSA_FLAG_OVERLAPPED` 与 `SO_SNDTIMEO` 相关改动即可。
