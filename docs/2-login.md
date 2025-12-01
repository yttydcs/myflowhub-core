Login/Register 协议（SubProto=2，P2P 统一 action+data）
==================================

基本约定
--------
- 所有消息（请求/响应）统一格式：`{"action": "<name>", "data": {...}}`，状态码放在 data 内；响应 action = `<request_action>_resp`。登录/认证成功时响应附带 `role`（单值）与 `perms`（字符串数组）；未找到节点则返回空 `role/perms`。
- 未登录设备 `SourceID=0`；仅子协议 2 在未登录状态放行，其余 `SourceID=0` 帧直接丢弃。
- 注册必须由直连 Hub 代发（携带 device_id）为 assist_register 上送权威。
- 权威节点选择：配置指定权威 nodeID 优先；否则有父则默认父为权威（逐级可达祖先）；无父则本级处理。
- 凭证只下发给设备和直连 Hub；父/祖先不缓存凭证。
- 撤销（revoke）采用广播：仅当找到并删除凭证时返回 revoke_resp，未找到静默不回；凭证不匹配可回错误。

凭证
----
- 生成：32 字节随机数，base64url 无填充（约 43 字符）。
- 绑定：`device_id` + `node_id` + `credential` 保存在直连 Hub 白名单；设备端保存 credential。
- 无过期；通过 revoke 主动失效，可未来扩展序列号/版本。

消息格式（JSON）
----------------
```json
{
  "action": "<action_name>",
  "data": { ... }
}
```

### 请求 / data 字段
- `register` / `assist_register`: `{ "device_id": "..." }`
- `login` / `assist_login`: `{ "device_id": "...", "credential": "..." }`
- `revoke`: `{ "device_id": "...", "node_id": N, "credential": "..." }`
- `assist_query_credential`（可选）: `{ "device_id": "...", "node_id": N }`
- `offline` / `assist_offline`: `{ "device_id": "...", "node_id": N, "reason": "optional" }`
- `get_perms`（新）: `{ "node_id": N }` 查询指定节点角色/权限
- `list_roles`（新）: 无 data，查询已知节点角色/权限列表
- `perms_invalidate`（新）: `{ "node_ids": [N1, N2], "reason": "optional" }` 权限失效通知（node_ids 为空表示全量）

### 响应 / data 字段（action = `<req>_resp`）
- `register_resp` / `assist_register_resp`: `{ "code": 1|err, "msg": "...", "device_id": "...", "node_id": N, "credential": "...", "role": "...", "perms": ["..."] }`
- `login_resp` / `assist_login_resp`: `{ "code": 1|err, "msg": "...", "device_id": "...", "node_id": N, "credential": "...", "role": "...", "perms": ["..."] }`
- `revoke_resp`: `{ "code": 1|err, "msg": "...", "device_id": "...", "node_id": N }`
- `assist_query_credential_resp`: `{ "code": 1|err, "msg": "...", "device_id": "...", "node_id": N, "credential": "..." }`
- `get_perms_resp`（新）: `{ "code": 1|err, "msg": "...", "node_id": N, "role": "...", "perms": ["..."] }`
- `list_roles_resp`（新）: `{ "code": 1|err, "msg": "...", "roles": [ { "node_id": N, "role": "...", "perms": ["..."] }, ... ] }`
- `offline_resp` / `assist_offline_resp`: `{ "code": 1|err, "msg": "...", "device_id": "...", "node_id": N }`

流程
----
1) 注册：设备→直连 Hub 发 `register`；直连 Hub 依据权威规则上送 `assist_register`。权威分配 node_id、生成 credential，回 `assist_register_resp`。直连 Hub 保存白名单，回设备 `register_resp`，更新索引。
2) 登录：设备提交 device_id+credential；直连 Hub 本地白名单验证通过才回 `login_resp`。若本地无凭证，可向上发 `assist_query_credential`（若实现）获取并缓存后完成登录。
3) 撤销：管理/权威发 `revoke`（含 device_id，建议带 node_id/credential）。广播传播；直连 Hub 找到并删除时回 `revoke_resp`（code=1），未找到静默；凭证不匹配可回 4402。可选断开连接。
4) 离线登录：直连 Hub 缓存白名单后，在失去父节点时仍可登录；注册仍需按权威规则上送。
5) 设备离线：设备或直连 Hub 发送 `offline`，直连 Hub 删除本地 node/device 索引，向父发送 `assist_offline` 逐级移除；成功移除时回 `offline_resp`/`assist_offline_resp`。

发送与过滤
----------
- `SourceID=0` 仅当 SubProto=2 放行；其他直接丢弃。
- 响应 `TargetID=0` 由最近 Hub 投递设备。
- 凭证仅本地验证；父链主要路由 assist*/revoke/offline/query。
- 权威选择：配置优先；否则默认父；无父则本级。

权限失效通知（perms_invalidate）
------------------------------
- 动作：`perms_invalidate`，data: `{ "node_ids": [N1, N2], "reason": "optional" }`；`node_ids` 为空表示全量失效。
- 处理：各 Hub 清空对应节点的 role/perms 缓存；如需，可向子节点继续广播（target=0，不回父）。

错误码建议（data.code/msg）
--------------------------
- 1：成功
- 登录/注册失败：4001 未注册/凭证不匹配；4002 无法访问权威/协助失败；4500 内部错误。
- 撤销失败：4401 未找到白名单；4402 凭证不匹配/已更新；4500 内部错误。
- 下线失败：4701 未找到索引；4700 内部错误。
- 权限相关：4404 未找到节点/权限；权限不足建议返回 403。

配置键（角色/权限）
------------------
- `auth.default_role`：默认角色名，默认 `node`。
- `auth.default_perms`：默认权限列表（逗号分隔），默认空。
- `auth.node_roles`：静态 node_id→role 映射，如 `1:admin;2:node`。
- `auth.role_perms`：角色→权限列表映射，如 `admin:p1,p2;node:p3`。

示例
----
注册请求  
```json
{"action":"register","data":{"device_id":"mac-001122334455"}}
```
注册成功响应  
```json
{"action":"register_resp","data":{"code":1,"msg":"ok","node_id":5,"device_id":"mac-001122334455","credential":"base64url_random_token"}}
```
登录请求  
```json
{"action":"login","data":{"device_id":"mac-001122334455","credential":"base64url_random_token"}}
```
登录失败响应  
```json
{"action":"login_resp","data":{"code":4001,"msg":"invalid credential"}}
```
撤销请求（广播）  
```json
{"action":"revoke","data":{"device_id":"mac-001122334455","node_id":5,"credential":"base64url_random_token"}}
```
撤销响应（仅找到并取消时返回）  
```json
{"action":"revoke_resp","data":{"code":1,"msg":"ok","device_id":"mac-001122334455","node_id":5}}
```
下线请求,不用响应
```json
{"action":"offline","data":{"device_id":"mac-001122334455","node_id":5,"reason":"client disconnect"}}
```
