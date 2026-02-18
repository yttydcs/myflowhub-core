# Plan - 协议仓库拆分（Proto）+ Win 上移解耦（Core）

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`refactor/proto-extract`
- Worktree：`d:\project\MyFlowHub3\worktrees\proto-extract\MyFlowHub-Core`
- 目标 PR：PR1（跨多个 repo 同步提交/合并）

## 本 PR 在 Core 的定位
> 本 PR 目标是“**不动 Core**”，仅在跨仓解耦改动期间做回归验证与兜底。

## 范围（PR1）
### 必须
- 不引入实现性改动（除非发现阻塞性问题：例如上层解耦后暴露出 Core API 缺口）
- 回归验证：`go test ./...`

### 不做
- Core 内部再次大重构（另起专门 workflow/PR）

## 任务清单（Checklist）

### C1 - 回归验证
- 目标：确认在 Server/Win/Proto 解耦改动后，Core 仍保持稳定。
- 涉及模块/文件：无（预期不改动）。
- 验收条件：
  - `go test ./...` 通过。
- 测试点：
  - `go test ./... -count=1`
- 回滚点：
  - 若出现误伤，优先回滚上层改动；必要时对 Core 做最小修复（需回到阶段 3.1 更新计划并确认）。

## 风险与注意事项
- 若必须修改 Core 才能完成解耦，需要新增任务并再次确认（禁止计划外改动）。
