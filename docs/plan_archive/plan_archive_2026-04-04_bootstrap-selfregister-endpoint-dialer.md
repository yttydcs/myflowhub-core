# Plan - Bootstrap Endpoint Dialer Helper

## Workflow Information
- Repo: `MyFlowHub-Core`
- Branch: `feat/bootstrap-endpoint-dialer`
- Base: `origin/master`
- Worktree: `D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/MyFlowHub-Core`
- Current Stage: `completed`

## Stage Records

### Initialization
- guide.md: `D:/project/MyFlowHub3/guide.md` reviewed; commit messages must use Chinese wording when committing later.
- base/worktree confirmation: dedicated worktree under `D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/`; implementation stays out of the main repo path.

### Stage 1 - Requirements Analysis
#### Goal
- Keep startup bootstrap semantics unchanged while removing the bootstrap helper's hard-coded temporary TCP dial path.

#### Scope
- Must:
  - change `bootstrap.SelfRegister(...)` so it can dial via an injected generic connection factory and exchange frames over `core.IConnection` / `Pipe()`
  - preserve synchronous request/reply behavior and current register/login response parsing
  - keep backward compatibility for callers that still only provide a TCP address
- Optional:
  - add small internal helpers for generic frame send/recv if that reduces duplication
- Not in scope:
  - serial transport implementation
  - pre-start bootstrap removal
  - auth protocol payload/schema changes
  - runtime policy changes around register/rebind ordering

#### Use Cases
- Server runtime wants pre-start bootstrap to reuse an already-supported parent endpoint dial path instead of opening a separate TCP-only connection.
- Integration tests or future transports need bootstrap over any byte-stream transport that already implements `core.IConnection`.

#### Functional Requirements
- `SelfRegister` must still validate required inputs and return the assigned `node_id`.
- Caller must be able to inject a dialer that returns `core.IConnection`.
- If no injected dialer is provided, existing `ParentAddr` TCP behavior must keep working.
- Synchronous response handling must decode exactly one response frame from the generic connection pipe.

#### Non-functional Requirements
- Minimum safe API surface change.
- No new dependency from Core to Server.
- No transport-specific branching beyond the existing default TCP fallback.

#### Inputs / Outputs
- Inputs:
  - context
  - `SelfRegisterOptions` with `SelfID`, optional `JoinPermit`, timeout settings, and either TCP address or injected dialer
- Outputs:
  - assigned `node_id`
  - legacy credential string placeholder
  - explicit error on dial/send/decode/register-status failure

#### Edge Cases
- dialer missing and `ParentAddr` empty
- dialer returns nil connection
- response frame decode fails or returns non-approved status
- login path still disabled by caller (`DoLogin=false`)

#### Acceptance Criteria
- Existing TCP callers remain compatible.
- Generic dialer callers can complete bootstrap without using `net.Dial("tcp", ...)` inside the helper.
- New/updated tests cover approved and non-approved reply handling through the generic path.

#### Risks
- API change in `SelfRegisterOptions` may affect downstream callers.
- Test doubles for `core.IConnection` must satisfy the current pipe/send contract.

#### Issue List
- None.

### Stage 2 - Architecture Design
#### Overall Solution
- Add an injectable bootstrap dialer to `bootstrap.SelfRegisterOptions` with signature `func(context.Context) (core.IConnection, error)`.
- Keep the current `ParentAddr` + `DialTimeout` TCP path as the default fallback when no dialer is supplied.
- Move frame exchange inside `SelfRegister` onto `core.IConnection`: send via `SendWithHeader(...)` and synchronously read one frame from `conn.Pipe()` using `header.HeaderTcpCodec`.

#### Alternatives Considered
- Add endpoint parsing to Core:
  - rejected because endpoint scheme knowledge already lives in Server runtime and would create unnecessary cross-module coupling.
- Remove TCP fallback entirely:
  - rejected for compatibility; current tests and callers still use `ParentAddr`.

#### Module Responsibilities
- `bootstrap/selfregister.go`:
  - validate options
  - dial a generic connection
  - perform sync register/login frame exchange
  - keep register/login response parsing unchanged

#### Data / Call Flow
- caller builds `SelfRegisterOptions`
- helper resolves dial strategy:
  - injected dialer
  - otherwise default TCP dial -> wrap as `tcp_listener.NewTCPConnection`
- helper sends auth register frame through `core.IConnection`
- helper decodes one response frame from `conn.Pipe()`
- optional login follows the same connection and response path

#### Interface Drafts
- `SelfRegisterOptions.Dial func(context.Context) (core.IConnection, error)`
- internal helper:
  - send header/payload through `conn.SendWithHeader(...)`
  - receive header/payload through `codec.Decode(conn.Pipe())`

#### Error Handling and Safety
- reject nil dialer result explicitly
- keep existing timeout handling on context and TCP fallback connection deadlines
- always close the connection in helper scope

#### Performance and Testing Strategy
- no extra network round trip; only transport abstraction changes
- add bootstrap unit tests with stub `core.IConnection` / in-memory pipe

#### Extensibility Design Points
- future transports only need to provide a `core.IConnection` dialer
- Server remains the endpoint-routing owner

#### Issue List
- None.

### Stage 3.1 - Planning
#### Project Goal and Current State
- Goal: make Core bootstrap transport-agnostic at the connection level so Server can reuse its generic parent endpoint dialer for pre-start bootstrap.
- Current state: `bootstrap.SelfRegister` directly uses `net.Dial("tcp", opts.ParentAddr)` and sync-decodes on `net.Conn`.

#### Docs Governance Routing Decision
- Using `$m-docs` for routing.
- Requirements impact: `none`
- Specs impact: `none`
- Lessons impact: `none`
- Stable truth remains in Server `docs/specs/auth.md` and `docs/specs/core.md`; this Core worktree root `plan.md` is workflow control only.
- Completed results will be archived in `docs/change/`.

#### Related Requirements / Specs / Lessons
- Related requirements: `none`
- Related specs:
  - `../MyFlowHub-Server/docs/specs/auth.md`
  - `../MyFlowHub-Server/docs/specs/core.md`
- Related lessons: `none`

#### Executable Task List
- [x] `CORE-BOOT-1` introduce generic bootstrap dial path in `bootstrap.SelfRegister`
- [x] `CORE-BOOT-2` add/update bootstrap tests for generic connection request/reply behavior
- [x] `CORE-BOOT-3` run Core test suite relevant to bootstrap

#### Task Details
##### `CORE-BOOT-1` - Generic bootstrap dial path
- Owner: main agent
- Worktree: `D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/MyFlowHub-Core`
- Plan Path: `D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/MyFlowHub-Core/plan.md`
- Goal: replace TCP-only bootstrap dial internals with injected `core.IConnection` support while keeping fallback compatibility.
- Files / Modules:
  - `bootstrap/selfregister.go`
- Write Set:
  - `bootstrap/selfregister.go`
- Acceptance:
  - helper can bootstrap over injected generic connection
  - TCP fallback remains available
- Test Points:
  - dialer path success
  - nil connection / non-approved response failure
- Rollback:
  - revert Core helper changes in this workflow branch

##### `CORE-BOOT-2` - Bootstrap helper tests
- Owner: main agent
- Worktree: `D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/MyFlowHub-Core`
- Plan Path: `D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/MyFlowHub-Core/plan.md`
- Goal: lock the helper contract with generic connection tests.
- Files / Modules:
  - `bootstrap/selfregister_test.go`
- Write Set:
  - `bootstrap/selfregister_test.go`
- Acceptance:
  - tests prove request/reply over `core.IConnection` pipe
- Test Points:
  - approved register returns node id
  - pending/rejected response surfaces `RegisterStatusError`
- Rollback:
  - remove new bootstrap tests with the helper revert

##### `CORE-BOOT-3` - Validation
- Owner: main agent
- Worktree: `D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/MyFlowHub-Core`
- Plan Path: `D:/project/MyFlowHub3/worktrees/feat-bootstrap-endpoint-dialer/MyFlowHub-Core/plan.md`
- Goal: verify bootstrap refactor does not break Core package tests.
- Files / Modules:
  - `bootstrap/*`
- Write Set:
  - none beyond test execution
- Acceptance:
  - targeted or full `go test ./...` passes in Core worktree
- Test Points:
  - bootstrap package
  - repo regression as needed
- Rollback:
  - do not merge failing test state

#### Dependencies
- Server worktree will consume the new injected dialer field after Core helper lands.

#### Risks and Notes
- Keep the change minimal: do not move endpoint parsing into Core.

#### Parallelism Assessment
- No sub-agent use.
- Work is intentionally serialized because the Core API choice is a direct dependency for Server wiring.

#### Issue List
- None.
