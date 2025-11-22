# Login/Register 协议标准（SubProto = 2）

## 目标
- 为设备分配唯一的 `node_id`，与 `device_id` 绑定，避免重复。
- 支持注册（首次绑定）与登录（取回绑定）。未注册的登录返回失败。
- 允许多级 Hub 协作：最近 Hub 包装“协助”请求向父级上送，父级处理后原路返回。
- 未登录设备使用 `SourceID=0`；只有子协议 2 的帧在未登录状态放行，其余 `SourceID=0` 的帧直接丢弃。
- 响应的 `TargetID=0`，由最近 Hub 按 `device_id`/连接索引送达。

## 报文格式
- 子协议：固定 `2`。
- 编码：JSON。

### 请求字段
| 字段      | 类型   | 说明                                         |
|-----------|--------|----------------------------------------------|
| action    | string | `register` \| `login` \| `assist_register` \| `assist_login` |
| device_id | string | 设备唯一标识（如 MAC/序列号），必填          |

### 响应字段
| 字段      | 类型   | 说明                                      |
|-----------|--------|-------------------------------------------|
| code      | int    | 1 表示成功，其他表示失败                  |
| msg       | string | 失败描述或提示                            |
| node_id   | uint32 | 成功时返回分配/绑定的 node_id             |
| device_id | string | 成功时回传请求中的 device_id              |

## 状态与约束
- `device_id` 与 `node_id` 一一绑定；相同 `device_id` 重复注册返回已绑定的 `node_id`（成功）。
- `node_id` 全局不重复，默认自增分配，起始值保留给系统节点（示例：1 为默认 Hub）。
- 未注册的登录请求直接失败（code ≠ 1）。
- 连接管理器维护 `device_id -> connection` 索引，未登录阶段也可用。

## 路由与协作流程
1) 设备直连最近 Hub，使用 `SourceID=0` 发送 `register`/`login`。
2) 最近 Hub 处理：
   - 若本地可分配/查找绑定：直接完成，响应 `TargetID=0`，通过 `device_id` 索引送回原连接。
   - 若需上送父级协助：将 `action` 改为 `assist_register`/`assist_login`，`SourceID` 置为 Hub 的 `node_id`，发送给父 Hub。
3) 上级 Hub 对 `assist_*`：
   - 若能处理：完成注册/登录并返回响应（code/msg/node_id/device_id）。
   - 否则继续向更上级转发同样的 `assist_*` 请求。
4) 响应回到最近 Hub：
   - 转换为设备可识别的响应（`code/msg/node_id/device_id`，`TargetID=0`），按 `device_id` 或连接索引送达设备。
5) 登录成功后，Hub 将 `node_id` 写入连接元数据并更新路由索引；可选择保留 `device_id` 索引用于后续按设备发送。

## 发送与过滤规则
- 预路由/入口过滤：`SourceID=0` 仅当 `SubProto=2` 时放行；其他 `SourceID=0` 帧直接丢弃。
- 响应的 `TargetID=0`，由最近 Hub 的 login handler 根据 `device_id`/连接索引直接投递到原连接。
- 其他子协议与跨节点路由不受影响。

## 错误码建议
- `1`：成功
- 其他值：失败，`msg` 说明原因（如 “unregistered device”, “invalid request”, “upstream unavailable” 等）。

## 示例
### 注册请求
Header（示例，TCP HeaderTcp）：
- `SubProto=2`
- `Major=MajorCmd` 或 `MajorMsg`（自选）
- `SourceID=0`（未登录）
- `TargetID` 可为 0 或直连 Hub 的 node_id（未登录阶段通常为 0）
- 其他字段可按默认

Payload：
```json
{
  "action": "register",
  "device_id": "mac-001122334455"
}
```

### 注册成功响应
Header：
- `SubProto=2`
- `Major=MajorOKResp`
- `SourceID` 为响应节点（处理该请求的 Hub/父 Hub）
- `TargetID=0`（由最近 Hub 根据 device_id 投递回设备）

Payload：
```json
{
  "code": 1,
  "msg": "ok",
  "node_id": 5,
  "device_id": "mac-001122334455"
}
```

### 未注册登录失败响应
Header：
- `SubProto=2`
- `Major=MajorErrResp`
- `SourceID` 为响应节点
- `TargetID=0`

Payload：
```json
{
  "code": 4001,
  "msg": "unregistered device"
}
```

### 协助注册请求（Hub 上送父级）
Header：
- `SubProto=2`
- `Major=MajorCmd`/`MajorMsg`
- `SourceID` = 下级 Hub 的 `node_id`
- `TargetID` = 上级 Hub 的 `node_id`

Payload：
```json
{
  "action": "assist_register",
  "device_id": "mac-001122334455"
}
```

### 协助注册响应
Header：
- `SubProto=2`
- `Major=MajorOKResp`/`MajorErrResp`
- `SourceID` = 上级 Hub
- `TargetID` = 下级 Hub

Payload（成功例）：
```json
{
  "code": 1,
  "msg": "ok",
  "node_id": 5,
  "device_id": "mac-001122334455"
}
```
