# model.go God Class Refactoring Discussion Record

> Date: 2026-04-03
> Related Issue: [#24](https://github.com/zac15987/zpit/issues/24) model.go large file code split evaluation
> Method: 3-agent expert discussion (3 rounds)
> **Status: Complete** — Phase 1 via [PR #59](https://github.com/zac15987/zpit/pull/59) merged (2026-04-03); Phase 2 evaluated and decided against.

---

## Participants

| Role | Handle | Focus Area |
|------|--------|------------|
| Senior Technical Architect | **Alex** | Design principles, abstraction boundaries, information hiding, Code Construction Principles |
| Senior Go Engineer | **Bo** | Go idioms, interface design, testability, concurrency safety |
| Systems Terminal Engineer | **Carmen** | Bubble Tea framework constraints, TUI lifecycle, real-time behavior, value copy semantics |

---

## Problem Background

`internal/tui/model.go` has **2433 lines, 50+ methods, and 6 distinct responsibilities** mixed on a single `Model` struct, violating multiple Code Construction Principles.

### 6 Major Responsibility Blocks

| Block | Lines (approx) | Function Count | Model UI Dependency |
|-------|---------------|----------------|---------------------|
| Core (types/Init/Update routing/View) | ~650 | 12 | Core skeleton |
| Key Handlers | ~300 | 6 | Deeply coupled (cursor, currentView, viewport) |
| Session Lifecycle | ~500 | 12 | **Zero dependency** — only accesses `m.state` (AppState) |
| Launch & Deploy | ~450 | 17 | `m.cursor` can be parameterized |
| Tracker & Label Ops | ~350 | 11 | Mixed: cmd factory needs only AppState; confirm needs Model UI |
| Channel | ~30 | 2 | Minimal |

### Existing Successful Split Patterns

The project already has precedent for splitting files by responsibility:
- `loop_cmds.go` (856 lines) — loop-related tea.Cmd factories
- `loop_handler.go` (483 lines) — loop message handlers
- `view_projects.go` (458 lines) — project list rendering
- `view_channel.go` (229 lines) — channel event timeline

---

## Round 1: Problem Diagnosis and Initial Proposals

### Alex's Analysis

**Core diagnosis**: model.go violates three Code Construction Principles:
1. **§3 God Class** — Model carries 6 distinct responsibilities, each with completely different information hiding boundaries
2. **§2 Inconsistent abstraction levels** — `update()` switch mixes high-level one-line dispatches with low-level 30+ line inline handlers
3. **§2 Core belief** — modifying permission detection requires jumping across three methods in 2434 lines; implicit coupling

**Three-tier dependency classification**:
- **Tier 1 (zero UI dependency)**: Session methods ~430 lines; only accesses `m.state`, never touches any Model UI field
- **Tier 2 (parameterizable)**: Launch methods ~245 lines; sole dependency `m.cursor` can be converted to a parameter
- **Tier 3 (deeply UI-bound)**: Key handlers + confirm ~630 lines; not recommended for extraction in the first step

**Proposal**: Phase 1 does a same-package file split; Phase 2 extracts a `SessionManager` struct once test coverage exists.

### Bo's Response

- **Agrees file split matches Go idioms**, citing the `net/http` precedent of `server.go`/`request.go`/`response.go`
- **Adds field concern**: Model has 24 fields; §3 recommends 7±2. Fields form clear clusters (Core, StatusView, Confirm, LoopFocus); Phase 2 can use embedded struct composition
- **Challenges session.go scope**: internally mixes Discovery (one-shot cmd chain) and Monitoring (tick-driven), but does not insist on splitting into two files
- **Recommends adding a lock protocol doc comment to the top of each new file** (§10: Comments explain why, not what)

### Carmen's Response

**Three categories of session methods from a Bubble Tea framework perspective**:
1. **Cmd factories** (scanExistingSessionsCmd, etc.) — return tea.Cmd, goroutine-executed; fully safe to move
2. **Msg handlers** (existingSessionsMsg, etc.) — mutate activeTerminals; follow action-defer pattern
3. **Tick-driven** (checkSessionLiveness, etc.) — pointer receiver; side-effect + cmd collector

**Critical warnings about Phase 2 SessionManager struct**:
1. Bubble Tea Model is a value type; a `*SessionManager` pointer would be shared across multiple Model copies
2. If SessionManager has its own mutex → two-level lock (Go has no reentrant locks), increasing deadlock risk
3. `handleLaunchResult` operates on both `activeTerminals` (session) and `channelSubs` (channel) **within the same Lock/Unlock region** → splitting the struct breaks atomicity
4. If sharing `AppState.mu` → it becomes just a facade with no real isolation

**Carmen's position**: Full support for Phase 1 file split (extending to session.go + launch.go + tracker_ops.go); wait-and-see on Phase 2 struct extraction (§2 No speculative generality).

---

## Round 2: Core Debate

### Central Dispute: Is File Split Enough to Resolve the God Class?

**Alex revises position**:

Accepts Carmen's atomicity argument for `handleLaunchResult`, **drops Phase 2 SessionManager struct**.

New perspective: "Under Bubble Tea constraints, the solution to a God Class is not struct splitting, but file splitting to reduce cognitive load + field clustering to reduce field count. Bubble Tea's Elm architecture is fundamentally designed to centralize all mutable state in a single Model — this is an architectural decision, not a code smell."

**Bo's `database/sql.DB` analogy**:

Go's `database/sql.DB` is also a "large struct" (25+ fields), carrying multiple responsibilities including connection pool, statement cache, and stats. Go's approach: don't split the struct (API contract requires it), scatter methods across multiple files by responsibility. Bubble Tea's `tea.Model` interface is like `database/sql`'s public API — it cannot be split.

**Bo distinguishes two Phase 2 paths**:
- **Path A (recommended)**: Embedded structs for pure UI state (StatusViewState, ConfirmState) — no new locks, no change to concurrency semantics
- **Path B (rejected)**: SessionManager struct managing shared mutable state — Carmen's argument is decisive

**Carmen's analysis of `handleLaunchResult` ownership**:

```
handleLaunchResult performs the following within a single Lock/Unlock region:
1. nextTrackingKey()         — session responsibility
2. construct ActiveTerminal  — session responsibility
3. trackedPIDs()             — session responsibility
4. read channelSubs          — channel responsibility
(after Unlock)
5. startWatcherDirCmd()      — session responsibility
6. channelSubscribeCmd()     — channel responsibility
```

Conclusion: it is an **orchestrator** — coordinating multiple subsystems, belonging to no single subsystem itself. It should stay in model.go.

---

## Round 3: Convergence and Final Decision

### Alex's "Compiler as Test" Boundary

§8 says "Tests are a prerequisite", but in Go's statically typed context this requires precise interpretation:

| Operation | Compiler Guarantee | Test Required |
|-----------|-------------------|---------------|
| Move method from A.go to B.go (same package) | Fully equivalent | No |
| Rename field (`m.statusCursor` → `m.status.Cursor`) | All call sites must update or compile error | No (but review recommended) |
| Extract inline code into named method | Fully equivalent | No |
| Change method to accept parameter instead of reading struct field | Compiler checks types, not semantics | **Yes** |
| Introduce new interface | Compiler checks method set, not behavior | **Yes** |

**All Phase 1 operations fall within the "compiler-guaranteed zone".**

### Bo's Embedded Struct Risk Quantification

- `statusProjectID` — at least 8 references
- `statusIssues` — at least 7 references
- `statusCursor` — at least 10 references
- `confirmForm/Result/Action` — at least 15 references
- Total: **40+ call site changes**

Technically the compiler guarantees correctness, but this should be a separate commit from the file split (§8 small steps principle + git blame traceability).

### Carmen's Bubble Tea Warning on Embedded Structs

- Value struct embedding is copy-safe (Bubble Tea fully copies the Model when copying)
- `*huh.Form` inside `ConfirmState` is a pointer; after copy, multiple copies share it — **but this is the existing behavior** and semantics do not change
- **Key warning**: embedded structs **must not have any methods** (no own `Update()`), or Bubble Tea may get confused
- `StatusViewState.Issues` is `[]tracker.Issue` (slice); value copy only copies the header, underlying array is shared — currently safe (only whole-replacement is done), but worth noting for the future

---

## Final Consensus

### Phase 1: Same-package file split (zero risk, immediately actionable)

| New File | Contents | Estimated Lines |
|----------|----------|-----------------|
| `session.go` | `ActiveTerminal` type + session msg types (`sessionFoundMsg`, `existingSessionsMsg`, `watcherReadyMsg`, `existingSessionEntry`, `permissionSignal`) + handlers (`handleExistingSessions/Found/WatcherReady`) + cmd factories (`scan/startWatcher/waitForLog/watchNext`) + tick methods (`checkLiveness/Permission/NewSessions`) + helpers (`trackedPIDs`, `nextTrackingKey`, `signalDir`, `deletePermissionSignal`) | ~500 |
| `launch.go` | `launchClaudeCmd`, `launchClarifier/ReviewerCmd`, `deployAndLaunchAgent`, `launchFocusClaudeCmd`, `openFolderCmd`, `openSlotFolderCmd/IssueCmd`, `openTrackerCmd`, `openInBrowser`, `deployDocs`, `injectLangInstruction`, `launchableSlotStates` | ~400 |
| `tracker_ops.go` | `checkLabelsCmd`, `ensureLabelsCmd`, `loadIssuesCmd`, `confirmIssueCmd`, `openIssueURLCmd`, `startWithLabelCheck`, `showLabelConfirm` | ~200 |
| `confirm.go` | `showDeployConfirm`, `showReviewerDeployConfirm`, `showUndeployConfirm`, `showIssueConfirm`, `executePendingOp`, `undeployFiles` | ~200 |
| `channel.go` | `channelSubscribeCmd`, `channelReadNextCmd` | ~40 |

**Remaining in model.go (~800 lines)**:
- Model struct definition + enums + constants
- `NewModelWithState` / `NewModel`
- `Init` / `Update` routing / `View` routing
- `handleKey`, `handleProjectsKey`, `handleStatusKey`, `handleChannelKey`
- `handleFocusSwitch`, `handleLoopSlotsKey`, `sortedSlotKeys`
- `handleLaunchResult`, `handleAgentEvent` (cross-domain orchestrators)
- `setStatus`, `findProject`, `syncViewportContent`, `ensureCursorVisible`
- `tickCmd`, `waitForStateRefresh`, `RunServerInit`, `serverInitCmds`
- confirm form routing inside `update()`

**Additional requirements**:
- All inline handlers in `update()` extracted into named methods (one-line dispatch)
- Lock protocol doc comment at the top of each new file (three-tier annotation: Handler / Cmd factory / Tick-driven)
- Section comments inside session.go to delineate areas
- Split order: session.go → launch.go → tracker_ops.go → confirm.go → channel.go, one commit per file
- Validation: `go build ./...` + `go test ./...` + `go vet ./...`

### Phase 2 (evaluate after file split, requires test coverage)

- **Path A (recommended)**: Model field embedded struct clustering
  - `StatusViewState` (5 fields: ProjectID, Issues, Cursor, Loading, Error)
  - `ConfirmState` (4 fields: Form, Result, Action + PendingOp)
  - `LoopFocusState` (3 fields: Panel, Cursor, ProjectID)
  - `ChannelViewState` deferred (only 1 field; §2 No speculative generality)
  - Model direct fields reduced from 24 to ~14
- **Path B (excluded)**: No SessionManager/LaunchManager independent struct
- `launch.go` method parameterization (`m.cursor` → `config.ProjectConfig`) — requires tests before crossing the line

### Explicitly Excluded

- No new interfaces (no polymorphism requirement)
- No new packages (internal tui coupling is appropriate)
- No additional mutex layers (single `AppState.mu` is the correct design)

---

## Key Decision Log

| # | Decision | Conclusion | Decisive Argument | Raised By |
|---|----------|------------|-------------------|-----------|
| 1 | Whether to extract SessionManager struct in Phase 2 | **No** | `handleLaunchResult` atomicity across session/channel requires a single Lock; double-lock deadlock risk | Carmen |
| 2 | Whether file split is sufficient to resolve the God Class | **Yes** (under Bubble Tea constraints) | Elm architecture's single-Model design inherently requires centralized state; `database/sql.DB` precedent | Carmen + Bo |
| 3 | Whether embedded struct goes in Phase 1 | **No, defer to Phase 2** | §8 small steps: file split and field rename are different changes and must not be mixed; 40+ call sites should be a separate commit | Bo + Carmen |
| 4 | `handleLaunchResult` ownership | **Stays in model.go** | Cross-domain orchestrator for session/channel; belongs to no single subsystem | All three agreed |
| 5 | `ActiveTerminal` type ownership | **Goes in session.go** | Primary operators are all in session.go; Go idiom: type follows its primary operator | Bo (Alex/Carmen agreed) |
| 6 | How far can we go without tests | **File split + named method extraction** | Go compiler equivalence guarantee = zeroth-level test; call site changes cross the line | Alex (Bo/Carmen agreed) |
| 7 | Whether session.go is split into two files internally | **No, single file + section comments** | Discovery and Monitoring share `activeTerminals` and `trackedPIDs`; splitting increases cognitive load | Carmen |
| 8 | Including confirm.go in Phase 1 | **Yes** | Self-contained modal logic, orthogonal to key handling / view rendering | Alex (Carmen agreed) |

---

## Implementation Results

### Phase 1: Complete

[PR #59](https://github.com/zac15987/zpit/pull/59) merged (2026-04-03)

| File | Lines | Notes |
|------|-------|-------|
| `model.go` | 860 (down from 2433) | Core routing + key handling + orchestrators |
| `session.go` | 733 | Session lifecycle: handlers + cmds + tick + types |
| `launch.go` | 479 | Launch & Deploy: launch cmds + slot ops + utilities |
| `tracker_ops.go` | 242 | Tracker & Label: label check/ensure + issue ops |
| `confirm.go` | 208 | Confirm dialogs + executePendingOp + undeploy |
| `channel.go` | 78 | Channel subscription + event reading |

All message cases in `update()` are one-line dispatches; each new file has a lock protocol doc comment at the top.

### Phase 2: Evaluated and Decided Against

After Phase 1 completed, model.go was re-evaluated against the three God Class tests proposed by Bo:

| Test | Before Split | After Split |
|------|-------------|-------------|
| "Does a single change disturb unrelated code?" | Changing session required navigating 2433 lines | Only session.go needs to be opened |
| "Does understanding one feature require reading all methods?" | 50+ methods mixed together | 17 methods, all routing/key handling |
| "Does the struct have a large subset of fields unused by most methods?" | `statusIssues` used by only ~10% of methods | Still exists, but impact is minor |

**Conclusion: God Class symptoms are substantially eliminated.**

- model.go is 860 lines with 17 methods — comparable in scale to `loop_cmds.go` (856 lines); a normal Bubble Tea root Model
- model.go's responsibility is now **TUI application state machine** (routing + key handling + cross-domain orchestration) — a legitimate single abstraction under Bubble Tea's Elm architecture
- 24 fields are a structural constraint of Bubble Tea (single source of truth), not a design defect
- Embedded struct field clustering (StatusViewState, ConfirmState, LoopFocusState) is technically feasible but falls under §2 No speculative generality — the current structure is clear enough, no forced refactoring needed

The Phase 2 embedded struct clustering and launch method parameterization are retained as "known optional improvements" to be re-evaluated if concrete pain points emerge in the future.

| # | Decision | Conclusion | Rationale |
|---|----------|------------|-----------|
| 9 | Whether to execute Phase 2 | **No (not needed)** | God Class symptoms eliminated after Phase 1; 24 fields are a Bubble Tea structural constraint; §2 No speculative generality |

---

## Risk Notes

1. **Known coupling point**: `handleLaunchResult` operating on both `activeTerminals` and `channelSubs` simultaneously is an architecture-level coupling. If channel functionality expands significantly, this handler may need refactoring.
2. **Slice sharing**: `StatusViewState.Issues` (`[]tracker.Issue`) only copies the slice header during Bubble Tea value copy. Currently safe (whole-replacement only), but in-place mutation must be handled carefully in the future.
3. **No methods on embedded structs**: If embedded struct clustering is done in the future, it must be purely a field grouping — do not add `Init/Update/View` methods, or Bubble Tea may get confused.

---

## Referenced Principles

- **§2 Design**: Managing complexity is the central goal; High cohesion, low coupling; Information hiding; No speculative generality
- **§3 Classes**: Avoid God Classes; Keep data members at roughly 7±2
- **§6 Control Structures**: Table-driven methods (update switch → one-line dispatch)
- **§8 Refactoring**: Tests are a prerequisite; Small steps
- **§10 Layout**: Comments explain why, not what (lock protocol doc comments)
- **Core Belief**: Enable the developer to work correctly while holding the minimum amount of code in mind
