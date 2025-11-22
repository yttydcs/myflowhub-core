# Login/Register 协议标准（SubProto = 2）

## 目标
- 为设备分配唯一的 `node_id`，与 `device_id` 绑定，避免重复。
- 支持注册（首次绑定）与登录（取回绑定）。未注册的登录返回失败。
- 支持多级 Hub 协作：最近 Hub 可上送协助请求到父级；父级处理后原路返回。
- 未登录设备使用 `SourceID=0`；仅子协议 2 在未登录状态放行，其余 `SourceID=0` 帧直接丢弃。
- 响应使用 `TargetID=0`，由最近 Hub 按 `device_id`/连接索引送达。
- 新增上报：最近 Hub 在获得 `node_id` 后向父链路发送 `upload_msg`，逐级更新所有父/祖先的索引。

## 报文格式
- 子协议：固定 `2`
- 编码：JSON

### 请求字段
| 字段      | 类型   | 说明                                                         |
|-----------|--------|--------------------------------------------------------------|
| action    | string | `register` \| `login` \| `assist_register` \| `assist_login` |
| device_id | string | 设备唯一标识（如 MAC/序列号），必填                          |

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
   - 若需上送父级协助：将 `action` 改为 `assist_register`/`assist_login`，`SourceID` 置为 Hub 的 `node_id`，`TargetID` 为父 Hub，发送给父。
3) 上级/祖先 Hub 对 `assist_*`：
   - 若能处理：完成注册/登录并返回响应（code/msg/node_id/device_id）。
   - 否则继续向更上级转发同样的 `assist_*` 请求。
4) 响应回到最近 Hub：
   - 转换为设备可识别的响应（`code/msg/node_id/device_id`，`TargetID=0`），按 `device_id` 或连接索引送达设备。
   - 在本地为该设备连接写入 `node_id/device_id` 元数据，更新本地路由索引。
   - 发送 `upload_msg`（SubProto=2，action=`upload_msg`，`SourceID` 为最近 Hub 的 node_id，`TargetID` 为父 Hub），逐级向上告知父/祖先节点该绑定；父/祖先更新自身索引后继续上送，直到链路顶端。
5) 登录成功后，后续子协议发送即可使用分配的 `node_id`。

## 发送与过滤规则
- 预路由/入口过滤：`SourceID=0` 仅当 `SubProto=2` 时放行；其他 `SourceID=0` 帧直接丢弃。
- 响应的 `TargetID=0`，由最近 Hub 的 login handler 根据 `device_id`/连接索引直接投递到原连接。
- upload_msg 使用父链路的 `TargetID`，沿父链逐级上传，不需要设备侧响应。

## 错误码建议
- `1`：成功
- 其他值：失败，`msg` 说明原因（如 “unregistered device”, “invalid request”, “upstream unavailable” 等）。

## 示例
### 注册请求
Header（TCP HeaderTcp 示例）：
- `SubProto=2`
- `Major=MajorCmd` 或 `MajorMsg`
- `SourceID=0`（未登录）
- `TargetID=0`（或直连 Hub 的 node_id）

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
- `SourceID` 为响应节点（处理请求的 Hub/父 Hub）
- `TargetID=0`

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

### upload_msg（最近 Hub 上报到父，逐级上传）
Header：
- `SubProto=2`
- `Major=MajorMsg`（或 Cmd）
- `SourceID` = 最近 Hub 的 `node_id`
- `TargetID` = 父 Hub 的 `node_id`

Payload：
```json
{
  "action": "upload_msg",
  "device_id": "mac-001122334455",
  "node_id": 5
}
```

父/祖先处理：更新自身 node/device 索引后，继续向上转发同样的 upload_msg；如需确认，可在未来扩展 ACK。无需返回设备。
