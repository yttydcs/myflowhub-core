# Login Server 设计与接入指南

## 目标
- 为整个 Hub 树提供唯一权威的登录服务：负责 register（分配 node_id）、credential 生成与持久化。
- 普通 Hub 只保留 login 校验缓存、revoke/assist_offline 转发能力；register 统一转发到 Login Server。
- Root 节点通过特权凭证选择并启用 Login Server，注册成功后 Root 的本地 login handler 失效，SubProto=2 全部转发到 Login Server。

## 角色与拓扑
- **Login Server**：运行在独立节点，接入网络时作为 Root 的子节点；配置 `login.mode=authority`，启用 Postgres。
- **Root**：无父节点的 Hub。持有特权凭证，唯一接受 `root_server_register` 请求；注册成功后将 SubProto=2 默认转发给 Login Server。
- **子 Hub**：有父节点的 Hub，不处理 `root_server_register`（直接丢弃），login/register 都经父链路到 Root，再到 Login Server。

## 协议扩展
- 新 action：`root_server_register`（SubProto=2，MajorCmd）。
  - 请求 data：`{ "token": "<root_priv_token>", "node_id": <login_server_node_id_optional> }`
  - 响应（仅 Root 返回）：`{ "action": "root_server_register_resp", "data": { "code": 1|err, "msg": "...", "node_id": <login_server_node_id> } }`
- 处理规则：
  - 只有 Root 处理；有父节点或未配置特权凭证时，静默丢弃。
  - Token 常量时间比较，不匹配丢弃或返回 403。
  - 成功：记录 login_server_node_id（来自连接元数据或请求 data），更新 default forward 映射（SubProto=2 → login_server_node_id），标记本地 login handler 禁用。
  - Login Server 断线时的策略：可选择保持映射等待重连，或断线即恢复本地 handler（需实现可配置策略）。

## 数据持久化（Postgres）
- 表：
  - `devices(device_id text primary key, credential text not null, node_id int not null, created_at timestamptz default now())`
  - `CREATE SEQUENCE node_seq START WITH <初始值>;` 用于全局 node_id 递增（设备使用）。
- 典型 SQL：
  - 注册（幂等）：`INSERT ... ON CONFLICT (device_id) DO UPDATE SET ... RETURNING node_id, credential;`
  - 分配 node_id：`SELECT nextval('node_seq');`
  - 登录校验：`SELECT node_id, credential FROM devices WHERE device_id=$1;`
  - 撤销：`DELETE FROM devices WHERE device_id=$1 AND ($2='' OR credential=$2);`
  - 下线：`DELETE FROM devices WHERE device_id=$1;`
- 连接：`database/sql` + `pgx` driver，配置池大小、超时。

## 流程总结
- **root_server_register**（Login Server → Root）：
  1) Login Server 通过 parent 链接到 Root。
  2) 发送 `root_server_register` 携带 Root 的特权 token（Root 预先生成并保存）。
  3) Root 校验通过后，记录 login_server_node_id，更新 default forward，禁用本地 login handler。
- **register**（设备 → 子 Hub）：
  1) 子 Hub 接收 `register`，直接转发 `assist_register` 到权威（Root → Login Server）。
  2) Login Server 事务：`nextval(node_seq)`、生成 credential、INSERT/UPSERT，返回 `assist_register_resp`。
  3) 子 Hub 缓存 whitelist（内存）并回设备 `register_resp`。
- **login**：
  - 子 Hub 先查本地缓存；未命中/不匹配 → `assist_query_credential` 到 Login Server；命中则回 `login_resp`，并缓存。
- **revoke/offline**：
  - 子 Hub 删除本地缓存并转发；Login Server 删除 DB 记录并广播 `revoke` 下行；`offline` 可删除索引/记录，不需要响应。

## 配置建议
- Root：
  - `parent.enable=false`
  - `root.priv_token=<生成后持久化>`
  - 注册成功后动态设置：`routing.default_forward_map=2=<login_server_node_id>` 或 `routing.default_forward_target=<id>`
  - `login.mode=client|disabled`（注册成功后禁用本地 login handler）
- Login Server：
  - `parent.addr=<root_addr>`，`parent.enable=true`
  - `root.priv_token=<与 Root 一致>`
  - `login.mode=authority`
  - Postgres DSN：`db.dsn=postgres://user:pass@host:port/dbname?sslmode=disable`
  - 角色/权限配置（可选，默认为 role=node、perms 空）：`auth.default_role`、`auth.default_perms`（逗号分隔）、`auth.node_roles`（如 `1:admin;2:node`）、`auth.role_perms`（如 `admin:p1,p2;node:p3`）
- 子 Hub：
  - 只需配置 parent 链路，其他跟随 Root 的 default forward 路由。

## 运维要点
- 特权凭证只在 Root 保存，子 Hub 不验证、不转发；登录服务器必须通过 Root 链路注册。
- Token 轮换：同时更新 Root 的 token 和 Login Server 配置，重发 `root_server_register`。
- 容错：断线后重连需重新注册；可配置 Root 是否在 Login Server 不可用时回退本地 login（默认建议等待恢复）。

## 最小上线步骤
1) 在 Root 生成并持久化 `root.priv_token`，启动 Root（无父）。
2) 部署 Postgres，建表与序列。
3) 启动 Login Server：配置 DSN、`root.priv_token`、parent 指向 Root，`login.mode=authority`。
4) Login Server 连上后发送 `root_server_register`，Root 确认成功并更新路由。
5) 其他 Hub 正常启动（有父），register/login 自动经 Root → Login Server 流转。***
