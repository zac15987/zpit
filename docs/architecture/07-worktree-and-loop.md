# 7. Git Worktree Parallel Development + Automated Loop + Issue Status Flow

---

## 7.1 Why Worktrees Are Needed

When multiple agents work on the same project simultaneously (each handling a different issue), they cannot share the same working directory. Git worktrees give each agent its own isolated working directory, each on a different branch, with no interference between them.

```
D:/Projects/ASE_Inspection/             ← main repo (dev branch)
  ├── .git/                             ← single .git directory
  ├── src/
  └── ...

D:/Projects/.worktrees/                 ← centralized worktree storage
  └── ASE_Inspection/
      ├── ASE-47--ethercat-reconnect/   ← Agent A's working directory
      │   ├── src/                        (branch: feat/ASE-47-ethercat-reconnect)
      │   └── ...
      ├── ASE-48--vision-timeout/       ← Agent B's working directory
      │   ├── src/                        (branch: feat/ASE-48-vision-timeout)
      │   └── ...
      └── ASE-49--z-axis-homing/        ← Agent C's working directory
          ├── src/                        (branch: feat/ASE-49-z-axis-homing)
          └── ...
```

---

## 7.2 Worktree Lifecycle

```
Issue enters In Progress
    │
    ├─ 1. Create feature branch from base branch (unified feat/ prefix)
    │     git branch feat/ISSUE-ID-slug {base_branch}
    │     base branch source: Issue Spec ## BASE_BRANCH (or legacy ## BRANCH fallback) > project config base_branch
    │     PR target source:   Issue Spec ## PR_TARGET  (or legacy ## BRANCH fallback) > project config base_branch
    │
    ├─ 2. Create worktree
    │     git worktree add <worktree-path> feat/ISSUE-ID-slug
    │
    ├─ 3. Deploy hooks + agents + docs to worktree
    │     DeployHooksToWorktree() → .claude/hooks/ + settings.json + settings.local.json
    │     (dual-write; CC does not inherit settings from the main repo, so both files
    │      must be explicitly present inside the worktree)
    │
    ├─ 4. Launch Claude Code in a new terminal (visible; user can intervene at any time)
    │     working directory = worktree path (path override)
    │     ZPIT_AGENT=1 env var injected (enables hook enforcement)
    │
    ├─ 5. Agent implements + reviews + opens PR
    │
    ├─ 6. Clean up after PR merge
    │     git worktree remove <worktree-path>
    │     git branch -d feat/ISSUE-ID-slug
    │     git fetch origin <base>:<base>  (update local base branch ref in main directory)
    │
    └─ 7. Issue → Done
```

**Zpit has two layers of worktrees** (this section describes the **issue layer**):

- **Issue layer** (this section): one worktree per Loop slot / issue, managed on the Go side by `internal/worktree/Manager`, mounted under `base_dir_*`. This is the working directory for the coding agent and reviewer.
- **Parallel subagent layer** (Task Execution Model): for `[P]` parallel batches, the orchestrator creates child worktrees via the `WorktreeCreate` hook (`hooks/worktree-create.sh`) — path `$HOME/.zpit/children/<8-hex-sha256(parent_cwd+slug)>` (flat short path, avoids Windows MAX_PATH; the earlier `<issue-wt>/.zpit-children/<slug>` nested layout would hit the 260-character limit under deep parents — see known-issues §9). This does not go through the Go Manager. Child paths are outside the project, so no gitignore rule is needed. After the batch completes, the orchestrator cleans up with `git worktree remove --force` + `git branch -D`. See `06-agents.md §6.3`.

The two layers do not interfere with each other: the issue worktree lifecycle (create → agent work → PR merge → cleanup) lives in the Go layer; the parallel-subagent worktree lifecycle lives in the coding agent's prompt layer (hook creates, orchestrator cleans).

### in_project Isolation Mode (No Worktree)

When a project sets `isolation = "in_project"` (default `"worktree"`), the Loop **does not create a worktree** — it checks out a `feat/<id>-<slug>` branch directly in the project directory and works in place. The motivation: in repos like UE / 3D projects that commit large binary assets into git, `git worktree add` would physically copy the entire working directory (hundreds of MB of tracked files), which is prohibitively expensive; `[P]` child worktrees would each require their own copy.

Differences (vs worktree mode):

| Aspect | worktree | in_project |
|---|---|---|
| Creation | `Manager.Create` (fetch + `git worktree add`) | `Manager.CreateInPlace` (dirty-tree check + in-place `git checkout -b` / checkout existing branch for resume) |
| Concurrent slots | `max_per_project` | Fixed 1 (single working directory) |
| `[P]` parallel batches | Enabled | Disabled (`DisableParallelBatches` on the prompt side normalizes all tasks to sequential) |
| Hook deployment | `DeployHooksToWorktree` (dual-write settings.json + settings.local.json) | `DeployHooksToProject` (merges into the real repo's settings.json) |
| Cleanup | `Manager.Remove` (`git worktree remove --force` + `branch -D`) | `Manager.RemoveInPlace` (`git checkout <base>` + `branch -D`; no-op when `branch == base`, never deletes base) |
| Dirty working directory | Does not affect main repo | At dispatch → `ErrWorkingTreeDirty` → `SlotNeedsHuman` (never auto-stashed) |
| Crash resume | Detected via existing worktree list | Detected via the repo's current `feat/<id>-…` branch (`git.CurrentBranch`) |

⚠️ Do not open external editors (e.g. Unreal Editor) or manually run git in the project while an in_project loop is active — the background `git checkout` rewrites the working directory. `isolation` is an enum that leaves room for future strategies such as `worktree_cow` (ReFS/Dev-Drive/APFS block-clone) and `worktree_shared_cache`.

---

## 7.3 Worktree Config

```toml
[worktree]
base_dir_windows = "D:/Projects/.worktrees"
base_dir_wsl = "/mnt/d/Projects/.worktrees"
dir_format = "{project_id}/{issue_id}--{slug}"   # slug auto-generated from issue title
auto_cleanup = true           # auto-cleanup after PR merge
max_per_project = 5           # maximum concurrent worktrees per project
max_review_rounds = 3         # max coding↔review cycles (exceeding this enters NeedsHuman)
poll_seconds = 10             # todo issue polling interval
pr_poll_seconds = 10          # PR/label status polling interval

# base_branch is set per project (default "dev")
```

**Notes:**
- CLAUDE.md and .claude/ live in the main repo; worktrees inherit them automatically
- Worktrees are not clones: they share the same .git and the same history
- Machine-control computers do not need worktrees: they work on one branch at a time, no parallelism needed
- If multiple agents modify the same file and cause a conflict, handle it manually

---

## 7.4 Automated Loop Flow

After pressing [l] in the TUI, the loop starts as a goroutine inside the TUI (not a separate subcommand). Multiple agents can work in parallel on the same project, each running in its own worktree. When the TUI closes, the loop stops, but any already-launched Claude Code agent processes are unaffected (they are independent processes).

**Core principle: Zpit is responsible only for dispatch — it does not intervene in the agent's work.**
Building, testing, reviewing, opening PRs, and updating tracker status are all the agent's own responsibilities.

```
[l] pressed in TUI
│
│  ┌── loop runs in TUI goroutine (pure dispatch) ──────────────┐
│  │                                                              │
│  │ 1. Query Tracker API: fetch the highest-priority            │
│  │    status=Todo issue for this project;                       │
│  │    if none → poll periodically (every 10 seconds)           │
│  │                                                              │
│  │ 2. Check how many active worktrees this project has         │
│  │    if >= max_per_project → wait                             │
│  │                                                              │
│  │ 3. Zpit creates branch + worktree + deploys hooks           │
│  │    base = Issue Spec ## BASE_BRANCH (or legacy BRANCH) ||   │
│  │           project config base_branch                         │
│  │    git branch feat/ISSUE-ID-slug {base_branch}              │
│  │    git worktree add <path> feat/ISSUE-ID-slug               │
│  │    DeployHooksToWorktree() writes settings.json +           │
│  │      settings.local.json (both) into the worktree           │
│  │                                                              │
│  │ 4. Write temporary agent file into the worktree             │
│  │    .claude/agents/coding-{issue-id}.md                      │
│  │    (assembled by BuildCodingPrompt: Issue Spec → prompt)    │
│  │    if Issue Spec contains TASKS → also deploy task-runner.md│
│  │    (subagent definition for the coding agent to delegate to)│
│  │                                                              │
│  │ 5. Launch coding agent (new terminal, visible)              │
│  │    working directory = worktree path, ZPIT_AGENT=1          │
│  │                                                              │
│  │ 6. Poll issue labels (GetIssue every 10 seconds)            │
│  │    "review" label detected = coding agent finished          │
│  │    (label-driven, not PID-driven; terminal stays open)      │
│  │                                                              │
│  │ 7. Launch reviewer agent (same worktree, read-only)         │
│  │                                                              │
│  │ 8. Poll issue labels                                        │
│  │    ├─ ai-review → PASS → wait for PR merge                 │
│  │    ├─ needs-changes → NEEDS CHANGES                         │
│  │    │  └─ round < max_review_rounds?                         │
│  │    │     ├─ yes → write revision prompt, rerun coding agent │
│  │    │     └─ no  → NeedsHuman state, notify for intervention │
│  │    └─ label unchanged → continue polling                     │
│  │                                                              │
│  │ 9. PR merged detected → clean up worktree + branch          │
│  │    + sync local base branch (git fetch origin <base>:<base>)│
│  │    failure → log warning, does not interrupt issue close    │
│  │                                                              │
│  │ 10. Return to step 1 to fetch the next issue                │
│  │                                                              │
│  └──────────────────────────────────────────────────────────────┘
```

---

## 7.5 Loop State Machine

All states are defined in `internal/loop/types.go`:

```
SlotCreatingWorktree    creating worktree
       ↓
SlotWritingAgent        preparing agent prompt file
       ↓
SlotLaunchingCoder      launching coding agent
       ↓
SlotCoding              coding agent working (polling labels, waiting for "review")
       ↓
SlotLaunchingReviewer   launching reviewer agent
       ↓
SlotReviewing           reviewer working (polling labels, waiting for "ai-review" or "needs-changes")
       ↓                           ↓
  (ai-review fork)     SlotCoding (needs-changes → rerun, round++)
       │
       ├─ auto_merge=false → SlotWaitingPRMerge   (poll PR status, waiting for manual merge)
       └─ auto_merge=true  → SlotAutoMerging      (call tracker merge API)
                                  ↓
                            SlotCleaningUp       clean up worktree + branch + sync local base branch ref
                                  ↓
                            SlotDone             complete

Error states:
SlotNeedsHuman          exceeded max_review_rounds, or auto-merge permanent failure / transient retries exhausted
SlotError               error during pipeline (including auto-merge auth errors)
```

State transitions are **label-driven** (polling issue labels, not PID monitoring):
- Coding agent sets `review` label → reviewer launches
- Reviewer sets `ai-review` (PASS) or `needs-changes` (auto-retry)

**Polling chain heartbeat (tick-driven):** Each of the three waiting states has its own independent `tea.Tick` heartbeat chain:

| State | Polls | Next tick scheduled by |
|---|---|---|
| Any Active loop | fetch todo issues | `handleLoopPollTick` |
| `SlotCoding` / `SlotReviewing` | fetch issue labels | `handleLoopLabelPollTick` |
| `SlotWaitingPRMerge` | fetch PR status | `handleLoopPRPollTick` |

**Critical invariant: heartbeat rescheduling only happens in the tick cases in `model.go` (the `handleLoop*Tick` family in `loop_handler.go`), never in the business handlers (`handleLoopPoll` / `handleLoopLabelPoll` / `handleLoopPRStatus`).** Each tick handler checks a gate at entry (loop `Active` + correct slot state); if the gate passes, it returns `tea.Batch(pollCmd, scheduleNextTick)` to pre-schedule the next hop; if the gate fails, it returns nil and the heartbeat naturally stops. Business handlers are responsible only for state transitions and must not reschedule on their own.

This design prevents the bug where "a nil return path in a handler silently kills the entire poll chain forever" (observed in the 2026-04-18 log). When adding new states or poll chains: `loopSchedulePoll` / `loopSchedulePRPoll` / `loopScheduleLabelPoll` may only be called at **kickoff** moments (loop start, transition into a new waiting state, resume) — calling them mid-chain inside a business handler is forbidden. `internal/tui/loop_tick_test.go` covers this invariant.

The `Slot` struct tracks each issue's position in the pipeline:

```go
type Slot struct {
    ProjectID    string
    IssueID      string
    IssueTitle   string
    BranchName   string    // e.g. "feat/ISSUE-ID-slug"
    BaseBranch   string    // PR target branch
    WorktreePath string
    State        SlotState
    ReviewRound  int       // 0-based; incremented on NEEDS CHANGES
    Error        error
    SessionPID   int
    LaunchedAt   int64     // unix timestamp
}
```

---

### Auto-Merge Branch

When a project has `auto_merge = true` (per-project, default false), after the reviewer sets the `ai-review` label, the slot does not enter `SlotWaitingPRMerge` — it enters `SlotAutoMerging` instead, where Go directly calls the tracker's merge API.

**Retry strategy (transient errors):**
- Up to 3 attempts, backoff 1s / 4s / 16s.
- Transient classification: HTTP 5xx / 408 / 429, `context.DeadlineExceeded`, `net.Error.Timeout() == true`.
- Each attempt uses an independent 30-second context timeout.

**Short-circuit (exit immediately, no retry):**
- Permanent: HTTP 409 (conflict) / 405 (not allowed) / 422 (not mergeable), or PR returns state=`closed` without being merged → transitions to `SlotNeedsHuman`, preserving the worktree and branch for manual handling.
- Auth: HTTP 401 / 403 → transitions to `SlotError`; this is a one-time config issue (expired token or insufficient permissions), retrying is pointless.

**Commit title:** `[<IssueID>] <IssueTitle>` (uses slot.IssueTitle; no additional API call).

**Merge method:** determined by `project.merge_method` (`squash` | `merge` | `rebase`); empty value defaults to `squash`.

**Security consideration:** the merge API is called directly by the Go process and does not go through `git-guard.sh`'s push whitelist. This is intentional — the Layer 5 safety gate shifts from "human review" to "AI reviewer PASS judgment". Users must evaluate whether they trust the reviewer model's quality before enabling this. See `09-safety.md`.

---

## 7.6 Issue Status Flow (Universal Across All Trackers)

```
                          ┌─────────────────────────────────┐
                          ▼                                 │
┌──────────┐  ┌──────┐  ┌──────────┐  ┌───────────┐  ┌────┴────────┐
│ Pending  │─▸│ Todo │─▸│ AI Impl  │─▸│ AI Review │─▸│ Waiting for │
│ Confirm  │  │      │  │          │  │           │  │ Your Review │
│(Clarifier│  │(you  │  │(Loop     │  │(automatic)│  │(PR opened)  │
│ output)  │  │confirm│  │automatic)│  │           │  │             │
└──────────┘  └──────┘  └──────────┘  └───────────┘  └──────┬──────┘
    │                        ▲                               │
    │ (you reject/revise)    │ (needs-changes)               │
    ▼                        └───────────────────────────────┘
  (delete or                                                 │ (approve)
   back to Clarify)                                  ┌───────▼───────┐
                                  (software only) ──▸│     Done      │
                                                     └───────────────┘
                                                             ▲
                                                             │ (verification passed)
                                                     ┌───────┴───────┐
                              (machine/Android) ────▸│ Pending       │
                                                     │ Physical      │
                                                     │ Verification  │
                                                     └───────────────┘
```

**Key design: the "Pending Confirm" gate**

Issues produced by the Clarifier Agent enter the "Pending Confirm" state by default (label: pending), not "Todo". The Loop only picks up issues with the Todo label (label: todo), so no agent will start working without your explicit confirmation.

How to confirm:
- Press [y] on the Status screen in the TUI → pending → todo
- Manually change the label on the Tracker web interface
