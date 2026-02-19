# 2026-02-19 Core：上移 subproto/kit 到 MyFlowHub-Core（为子协议拆分铺路）

## 变更背景 / 目标
此前 `MyFlowHub-Server` 仓库内存在 `subproto/kit`，用于承载：
- action 注册模板（`NewAction/FuncAction`）与 action 语义分类（`ActionKind`，仅用于工程组织/可观测）；
- 常用的响应构造/发送辅助（`BuildResponse/SendResponse/Clone*`）。

随着“子协议实现拆成独立 Go module（A2：单仓多 module）”的推进，子协议实现需要尽量只依赖：
- `myflowhub-core`（框架能力）
- `myflowhub-proto`（协议字典）

因此，将 `subproto/kit` 上移到 Core，避免未来子协议实现继续绑定 `myflowhub-server` 仓库。

本次目标：
1) 在 `MyFlowHub-Core` 新增 `github.com/yttydcs/myflowhub-core/subproto/kit`；
2) 发布 `myflowhub-core@v0.2.1`（新增包路径，供上游以 semver 依赖使用）；
3) 保持 wire 与行为不变（仅做代码归属调整）。

## 具体变更内容（新增 / 修改 / 删除）

### 新增
- `subproto/kit/action.go`：`ActionKind` + `NewAction/FuncAction`（action 注册模板化）
- `subproto/kit/kit.go`：`BuildResponse/SendResponse/Clone*`（响应与 header 克隆工具）
- `subproto/kit/action_test.go`：`KindFromName` 的最小单测

### 修改
- `plan.md`：本 workflow 的计划与验收记录
- `docs/plan_archive/*`：归档上一轮 plan（审计回放用）

### 删除
- 无（Server 侧切换与删除 `myflowhub-server/subproto/kit` 将在后续 workflow 进行）

## 对应计划任务映射（plan.md）
- COREKIT0：归档旧 plan
- COREKIT1：新增 `subproto/kit` 包
- COREKIT2：Code Review
- COREKIT3：归档变更（本文档）

## 关键设计决策与权衡
1) **kit 上移 Core，而不是放在 Proto**
   - Proto 的职责是“协议字典”（只依赖标准库）；`kit` 属于运行时辅助与模板，应随 Core 发布。

2) **`ActionKind` 仅作为工程元信息**
   - 不参与路由/转发/鉴权语义，避免造成“看似分类改变会影响 wire 行为”的误解。

3) **保持 `SendResponse` 的发送策略不变**
   - 优先走 `core.ServerFromContext(ctx).Send`（触发统一发送管线与钩子）；
   - 无 server context 时回退 `conn.SendWithHeader`，便于在测试/离线场景使用。

## 测试与验证
- `GOWORK=off go test ./... -count=1 -p 1`
- 结果：通过。

## 潜在影响与回滚方案

### 潜在影响
- 上游仓库若要使用 `kit`，应改为依赖 `github.com/yttydcs/myflowhub-core/subproto/kit`（本次仅新增，不会影响现有编译）。
- 后续删除 Server 内部 `subproto/kit` 时，需要同步切换 import（将另起 workflow 处理并归档）。

### 回滚方案
- `git revert` 本次引入 `subproto/kit` 的提交即可回滚（仅新增文件，不影响现有行为）。

