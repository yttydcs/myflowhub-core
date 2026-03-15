# Core：修复 RFCOMM / 字节流短写导致的半帧阻塞

## 变更背景 / 目标
- 背景：
  - Windows RFCOMM 实测中，连接建立成功后，Win 客户端发送 `register` 帧，服务端偶发或稳定卡在收包阶段；
  - 根因不是 auth 协议，也不是 payload 长度，而是发送侧默认“一次 `Write` 就等于整帧发完”；
  - RFCOMM 这类字节流承载可能出现短写，接收端 `HeaderTcpCodec.Decode` 又按完整帧阻塞读取，最终表现为半帧阻塞。
- 目标：
  - 将“整帧写出契约”下沉到 Core 的传输/帧写出层；
  - 保持 TCP 现有行为不变，同时让 RFCOMM 与未来其他非 TCP 字节流承载稳定工作。

## 具体变更内容
- 新增：
  - `writeutil.go`
    - 增加 `WriteAll`
    - 增加 `WriteAllBuffers`
    - 统一处理短写、零进度写和 `nil writer`
  - `writeutil_test.go`
    - 覆盖短写重试、多段写入、错误返回、零进度写等路径
  - `process/frame_writer_test.go`
    - 覆盖普通 codec 路径短写
    - 覆盖 `HeaderTcp` 帧写出路径短写
- 修改：
  - `process/frame_writer.go`
    - 普通编码路径改为使用 `WriteAll`
    - `HeaderTcp` 快路径由 `net.Buffers.WriteTo` 改为显式分段写满，避免在短写时造成头/载荷错位
  - `process/senddispatcher.go`
    - 非 writer-encode 分支改为使用 `WriteAll`
  - `listener/rfcomm_listener/connection.go`
    - `Send` / `SendWithHeader` 改为使用 `WriteAll`
  - `listener/rfcomm_listener/connection_test.go`
    - 增加 RFCOMM 直接发送短写回归测试

## 对应 plan.md 任务映射
- `CORE-RFCOMM-1`：完成统一写出契约
- `CORE-RFCOMM-2`：完成 RFCOMM connection 直接发送修复
- `CORE-RFCOMM-3`：完成短写回归测试
- `CORE-RFCOMM-4`：已执行代码审查
- `CORE-RFCOMM-5`：本文档

## 关键设计决策与权衡
- 分层位置：
  - 修复放在 Core 传输/帧写出层，而不是 auth、路由、UI 层；
  - 这样可以一次修正所有依赖 `IPipe` 的承载，避免业务层补丁扩散。
- 性能：
  - 保留 `HeaderTcp` 的“头和 payload 分段发送”思路，不退化成“每帧强制拼接大 buffer”；
  - 正常一次写满时只有极小循环判断成本；
  - 仅在短写发生时才多做补写，属于正确性优先且成本低的补偿。
- 可扩展性：
  - 统一写出契约后，未来串口、USB、命名管道等字节流承载可直接复用；
  - transport 不再需要假设自己一定满足“单次写满”。

## 测试与验证方式 / 结果
- 已执行：
  - `GOWORK=off go test ./... -count=1`
- 结果：
  - 通过
- 重点覆盖：
  - Core 通用短写重试
  - `HeaderTcp` 快路径短写
  - RFCOMM connection 直接发送短写

## 潜在影响与回滚方案
- 潜在影响：
  - TCP 不应有行为变化，仅发送实现更稳健；
  - RFCOMM 稳定性提升，注册 / 登录 / 普通发包都受益；
  - 若上层依赖“写一部分也算成功”的错误行为，会被修正为“必须写完整帧”。
- 回滚方案：
  - 回退以下文件：
    - `writeutil.go`
    - `writeutil_test.go`
    - `process/frame_writer.go`
    - `process/frame_writer_test.go`
    - `process/senddispatcher.go`
    - `listener/rfcomm_listener/connection.go`
    - `listener/rfcomm_listener/connection_test.go`
