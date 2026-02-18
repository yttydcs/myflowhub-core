# Plan - Core：发布 v0.2.0（semver 依赖基线）（PR19-CORE-SemVer）

## Workflow 信息
- Repo：`MyFlowHub-Core`
- 分支：`chore/core-semver`
- Worktree：`d:\project\MyFlowHub3\worktrees\pr19-semver-deps\MyFlowHub-Core`
- Base：`master`
- 参考：
  - `d:\project\MyFlowHub3\target.md`
  - `d:\project\MyFlowHub3\repos.md`
  - `d:\project\MyFlowHub3\guide.md`（commit 信息中文）
- 目标：发布并推送 tag `v0.2.0`

## 约束（边界）
- 本 workflow 在 Core 的范围以“**发布版本/依赖可拉取**”为主：
  - 允许：文档归档（`docs/change`）、必要的极小修复（仅当阻塞上层 `GOWORK=off go test` 验收）。
  - 禁止：计划外重构、协议 wire 变更、破坏性 API 变更（若必须，需回到 3.1 更新计划并重新确认）。
- 验收必须使用 `GOWORK=off`（避免本地 go.work 影响审计）。

## 当前状态（事实，可审计）
- Core 已存在 tag：`v0.1.0`（对应旧版本 HeaderTcp v1）。
- 当前 `master` 已包含 HeaderTcp v2（32B）与统一路由规则（Major 分流），需要通过新 tag 向上游发布。
- 上游（Server/SDK/Win）当前仍通过 `replace ../MyFlowHub-Core` 联调；本 workflow 要移除该依赖方式。

---

## 1) 需求分析

### 目标
1) 发布 `github.com/yttydcs/myflowhub-core@v0.2.0`，使上游在不使用 `replace`/`go.work` 的情况下可拉取并构建。
2) 提供可审计的发布记录（变更说明、验证方式、回滚策略）。

### 范围（必须 / 不做）
#### 必须
- 新增归档文档：`docs/change/2026-02-18_core-v0.2.0.md`
- 回归验证：`GOWORK=off go test ./... -count=1 -p 1`
- 结束 workflow 且你确认后：
  - 在 `repo/` 合并分支到 `master` 并 push
  - 创建 annotated tag `v0.2.0` 并 push tag

#### 不做
- 不额外引入依赖
- 不进行大规模目录/包重构

### 验收标准
- `GOWORK=off go test ./... -count=1 -p 1` 通过
- 远端存在并可拉取：`github.com/yttydcs/myflowhub-core@v0.2.0`

### 风险
- tag 一旦 push 不应删除/改写；若发现错误，需追加发布更高版本（例如 `v0.2.1`）修复。

---

## 2) 架构设计（分析）

### 总体方案（采用）
- 使用 **semver tag** 发布 Core：
  - `v0.1.0` → `v0.2.0`（breaking bump：HeaderTcp v2 / 路由规则为上层基线）
- 在上游仓库通过 `require ...@v0.2.0` 引用，彻底移除 `replace ../MyFlowHub-Core`。
- 本地多仓联调使用 `d:\project\MyFlowHub3\go.work`（不提交），并用 `GOWORK=off` 做“真实可拉取”验收。

### 测试策略
- 以编译/单测回归为主：`go test ./...`

---

## 3.1) 计划拆分（Checklist）

## 问题清单（阻塞：否）
- 已确认版本：Core 发布 `v0.2.0`；上游将移除 `replace` 并使用 tag 依赖；验收使用 `GOWORK=off`。

### CORESEM1 - 归档发布文档
- 目标：写清楚 `v0.2.0` 发布内容、与 `v0.1.0` 的关键差异、验证命令、回滚策略。
- 涉及文件：
  - `docs/change/2026-02-18_core-v0.2.0.md`
- 验收条件：
  - 文档包含 tag、关键变更点、`GOWORK=off` 验收命令、风险与回滚方案。
- 测试点：
  - 无（文档）
- 回滚点：
  - revert 文档提交（不影响代码）。

### CORESEM2 - 回归测试（GOWORK=off）
- 目标：确保 Core 在发布点可通过回归（不依赖 go.work）。
- 命令：
  - `$env:GOTMPDIR='d:\\project\\MyFlowHub3\\.tmp\\gotmp'`
  - `New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null`
  - `$env:GOWORK='off'`
  - `go test ./... -count=1 -p 1`
- 验收条件：通过。
- 回滚点：revert 本分支改动（若有）。

### CORESEM3 - Code Review（阶段 3.3）+ 归档（阶段 4）
- 目标：Review 覆盖需求/风险/测试，并在 docs/change 归档。
- 验收条件：Review 结论为“通过”。

### CORESEM4 - 合并与打 tag（你确认结束 workflow 后执行）
- 目标：将本分支合并到 `master`，并发布 `v0.2.0`。
- 步骤（在 `repo/MyFlowHub-Core` 执行）：
  1) `git merge --ff-only origin/chore/core-semver`（或等价方式）
  2) `git push origin master`
  3) `git tag -a v0.2.0 -m \"chore: 发布 v0.2.0\"`
  4) `git push origin v0.2.0`
- 验收条件：
  - tag 可在远端查看并被 `go` 拉取。
- 回滚方案：
  - 不删除 tag；如需修复，追加发布 `v0.2.1`。
