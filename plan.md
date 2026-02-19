# Plan - Core：上移 subproto/kit 到 MyFlowHub-Core 并发布 v0.2.1（PR1）

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`refactor/subproto-kit-core`
- Worktree：`d:\project\MyFlowHub3\worktrees\pr1-kit-core\MyFlowHub-Core`
- Base：`origin/master`
- 参考：
  - `d:\project\MyFlowHub3\target.md`
  - `d:\project\MyFlowHub3\repos.md`
  - `d:\project\MyFlowHub3\guide.md`（commit 信息中文）

## 约束（边界）
- wire 不改：不改 HeaderTcp / Major 路由语义 / SubProto 值 / Action 名称 / JSON schema。
- 本 PR 只做“代码归属调整 + 轻量封装复用”：
  - 将 `MyFlowHub-Server/subproto/kit` 上移为 `MyFlowHub-Core/subproto/kit`；
  - 不引入 Core 对 Server 的依赖（避免循环依赖）。
- 验收必须使用 `GOWORK=off`（避免本地 `go.work` 干扰审计）。

## 当前状态（事实，可审计）
- `MyFlowHub-Server/subproto/kit` 已存在并被多个子协议引用（action 注册模板化、响应发送工具等）。
- `MyFlowHub-Core/subproto` 当前仅包含基础基类（`ActionBaseSubProcess` 等），尚无 `subproto/kit`。
- 上游（至少 Server）将需要切换 import 到 `github.com/yttydcs/myflowhub-core/subproto/kit`，因此本仓库需要发布新 tag：`v0.2.1`。

---

## 目标
1) 在 Core 中新增包 `github.com/yttydcs/myflowhub-core/subproto/kit`，承载：
   - `NewAction/FuncAction/ActionKind`（action 注册模板化）
   - `SendResponse/BuildResponse/Clone*`（响应与 header 克隆工具）
2) 发布 `myflowhub-core@v0.2.1`，供 Server（以及未来 subproto 独立 module）以 semver 依赖使用。

## 非目标
- 不做 broker 上移（本 PR 仅处理 kit；broker 将在拆 `flow/exec` 前单独处理）。
- 不做任何协议字段/语义变化。

---

## 3.1) 计划拆分（Checklist）

### COREKIT0 - 归档旧 plan
- 目标：避免覆盖上一轮 semver workflow 的 `plan.md`，保留可审计回放。
- 涉及文件：
  - `docs/plan_archive/plan_archive_2026-02-19_core-semver-v0.2.0.md`
- 验收条件：旧 plan 已归档且可阅读。
- 回滚点：撤销本次 `git mv`。

### COREKIT1 - 新增 `subproto/kit` 包（从 Server 上移）
- 目标：Core 提供可复用的 action 模板与响应发送工具，Server 不再自带 kit 实现。
- 涉及文件（预期）：
  - `subproto/kit/action.go`
  - `subproto/kit/action_test.go`
  - `subproto/kit/kit.go`
- 设计要点：
  - `ActionKind` 仅用于工程组织/可观测，不参与 wire/路由语义；
  - `SendResponse` 仍优先走 `core.ServerFromContext(ctx).Send`，无 server 时回退 `conn.SendWithHeader`。
- 验收条件：
  - `GOWORK=off go test ./... -count=1 -p 1` 通过。
- 回滚点：revert 提交。

### COREKIT2 - Code Review（阶段 3.3）
- 按 3.3 清单输出结论（通过/不通过）；不通过则回到 COREKIT1 修正。

### COREKIT3 - 归档变更（阶段 4）
- 新增文档：
  - `docs/change/2026-02-19_core-subproto-kit.md`
- 需包含：
  - 变更背景/目标、关键设计决策（为何上移 Core）、对外影响（新包路径）、验证方式/结果、回滚方案。

### COREKIT4 - 合并 / tag / push（需你确认 workflow 结束后执行）
- 在 `repo/MyFlowHub-Core` 执行：
  1) `git merge --ff-only origin/refactor/subproto-kit-core`
  2) `git push origin master`
  3) 创建并 push tag：`v0.2.1`（annotated）
- 回滚点：revert 合并提交；删除/回滚 tag（如需）。

---

## 依赖关系 / 风险 / 注意事项
- 依赖：Server 的 PR1（切换到 Core kit）依赖本仓库 `v0.2.1` tag 发布完成后，才能用 `GOWORK=off` 做可审计验收。
- 风险：
  - 若 `subproto/kit` 设计不小心引入对 Server 的引用，将导致循环依赖（必须禁止）。
- 注意：
  - commit 信息使用中文（允许 `refactor:`/`docs:` 等英文前缀）。

## 验证命令（统一）
```powershell
$env:GOTMPDIR='d:\\project\\MyFlowHub3\\.tmp\\gotmp'
New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null
GOWORK=off go test ./... -count=1 -p 1
```

