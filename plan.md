# Plan - Core：发布 v0.4.1

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`chore/core-v0.4.1-release`
- Worktree：`d:\project\MyFlowHub3\worktrees\core-v0.4.1-release`
- Base：`master`
- 范围：仅完成 `v0.4.1` 补丁版本发布归档、审计与 tag；不新增实现逻辑

## 项目目标与当前状态
- 目标：
  - 以已合并的 Windows RFCOMM listener 修复为基础发布 `v0.4.1`；
  - 补齐可审计的发布计划与归档文档；
  - 为下游 `MyFlowHub-Win` 升级依赖提供稳定版本锚点。
- 当前状态：
  - `master` 已包含 Windows RFCOMM listener 修复；
  - 自动化验证 `GOWORK=off go test ./... -count=1` 已通过；
  - 尚未创建 `v0.4.1` tag，也缺少单独的“版本发布”归档文档。

## 可执行任务清单（Checklist）

### CORE-REL-1 - 审计发布基线
- 目标：
  - 确认 `master` 上将被发布的提交、已有 tag、验证记录与发布范围。
- 涉及模块 / 文件：
  - `git tag`
  - `git log`
  - `docs/change/2026-03-14_windows-rfcomm-listener-fix.md`
- 验收条件：
  - 明确 `v0.4.1` 基于的提交；
  - 明确本次为补丁发布，不新增代码实现。
- 测试点：
  - `git tag --sort=version:refname`
  - `git log --oneline`
- 回滚点：
  - 无代码改动，可放弃本次发布流程。

### CORE-REL-2 - 维护发布计划与归档
- 目标：
  - 让本次版本发布具备独立 `plan.md` 与 `docs/change` 文档，便于交接和追踪。
- 涉及模块 / 文件：
  - `plan.md`
  - `docs/change/2026-03-14_core-v0.4.1-release.md`
- 验收条件：
  - 文档覆盖背景、内容、任务映射、验证、影响、回滚；
  - 文档明确 `v0.4.1` 是 Windows RFCOMM listener 修复补丁。
- 测试点：
  - 文档内容可独立理解并执行发布动作。
- 回滚点：
  - 回退新增文档提交。

### CORE-REL-3 - 执行 tag 与远端发布准备
- 目标：
  - 在审核通过后为 `master` 创建并推送 `v0.4.1` annotated tag。
- 涉及模块 / 文件：
  - Git tag / remote refs
- 验收条件：
  - `origin/master` 上存在 `v0.4.1`；
  - tag 指向已验证的修复提交。
- 测试点：
  - `git show v0.4.1`
  - `git ls-remote --tags origin`
- 回滚点：
  - `git tag -d v0.4.1`
  - `git push origin :refs/tags/v0.4.1`

### CORE-REL-4 - Code Review 与收敛
- 目标：
  - 逐项审查发布范围、可维护性、稳定性与验证证据；
  - 确认可供下游升级。
- 涉及模块 / 文件：
  - `plan.md`
  - `docs/change/2026-03-14_core-v0.4.1-release.md`
- 验收条件：
  - Review 结论完整；
  - 发布动作与回滚动作明确。
- 测试点：
  - Review 逐项结论明确。
- 回滚点：
  - 修订文档或取消发布。

## 依赖关系
- `CORE-REL-1` 完成后进入 `CORE-REL-2`
- `CORE-REL-2` 与 Review 完成后才能执行 `CORE-REL-3`
- `CORE-REL-3` 完成后进入 `CORE-REL-4`

## 风险与注意事项
- `v0.4.1` 是补丁发布，禁止夹带新的实现性修改；
- tag 一旦推送即成为下游版本锚点，必须先完成文档与验证记录；
- 本轮仅面向 `MyFlowHub-Win` 对齐，不同步调整其他下游仓库。
