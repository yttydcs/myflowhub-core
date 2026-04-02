# 2026-04-02_core-flow-permission-defaults

## 变更背景 / 目标

- `flow.run` / `flow.read` 已成为稳定权限常量，Core 默认角色配置需要同步补齐，避免开箱部署回退为 `403`。
- 本轮目标是把默认 `admin/node` 角色权限与新的 Flow 权限模型对齐。

## 具体变更内容

### 修改

- `config/config.go`
  - 在 `DefaultAuthRolePerms` 中为 `admin` 与 `node` 补齐 `flow.run` / `flow.read`
- `config/config_test.go`
  - 增加默认权限字符串包含 `flow.run` / `flow.read` 的断言

### 删除

- 无

## Requirements impact

- `none`

## Specs impact

- `none`

## Lessons impact

- `none`

## Related requirements

- `D:\project\MyFlowHub3\worktrees\server-run-control-phase1\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\worktrees\server-run-control-phase1\docs\specs\flow.md`
- `D:\project\MyFlowHub3\worktrees\server-run-control-phase1\docs\specs\auth.md`

## Related lessons

- 无

## 对应 plan.md 任务映射

- `RC-P0-3`
  - Core 默认角色权限更新
  - Core 默认值测试锁定

## 经验 / 教训摘要

- 新权限进入稳定契约后，如果 Core 默认值不跟进，所有依赖默认角色的下游模块都会出现连锁 `403`。
- 默认权限字符串属于基础配置真相，适合先在 Core 收敛，再由 Server runtime 直接复用。

## 可复用排查线索

- 症状：
  - 默认 `admin/node` 角色无法 `run` 或 `status`
  - `Server` 和 `Core` 的默认权限字符串不一致
- 触发条件：
  - 只更新了 SubProto / Server docs，没有更新 Core 默认角色值
- 关键词 / 错误文本：
  - `DefaultAuthRolePerms`
  - `flow.run`
  - `flow.read`
- 快速检查：
  1. 看 `config/config.go` 中 `DefaultAuthRolePerms` 是否包含 `flow.run` / `flow.read`
  2. 看 `config/config_test.go` 是否锁定这两个权限

## 关键设计决策与权衡

- 仅补齐默认角色，不扩展新的角色层级或继承规则
  - 好处：改动面最小，风险最小
  - 代价：更细粒度的默认权限策略仍需后续单独 workflow 设计

## 测试与验证方式 / 结果

- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase3\go.work go test github.com/yttydcs/myflowhub-core/... -count=1 -p 1`
- 结果：通过

## 潜在影响

- 默认角色的 Flow 能力从“无显式 run/read 权限”收口为具备稳定 run/read 权限。
- 显式自定义 `auth.role_perms` 的部署不受默认值变更影响。

## 回滚方案

1. 回退 `config/config.go`
2. 回退 `config/config_test.go`
3. 重新验证默认配置行为

## 子Agent执行轨迹

- 本轮未使用子Agent
