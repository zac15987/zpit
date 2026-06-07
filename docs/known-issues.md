# Known Issues

> **Terminology note (2026-04-24):** Incident entries below refer to parallel `[P]` subagents as "teammates" and to the parallel-batch delegation as "Agent Team". Those terms were updated project-wide to "parallel subagent" / "parallel subagent batch" to avoid confusion with Claude Code's Agent Team feature (`team_name + name` path in `AgentTool.tsx`) — zpit does not use that feature. The incident entries preserve the original wording to keep the time-ordered narrative consistent; any new entry below should use the new terminology.

## 1. Windows: Worktree cleanup fails when reviewer agent session is still active

**Affected OS:** Windows only
**Component:** `internal/worktree/Manager.Remove()`, Loop engine cleanup (`loopCleanupCmd`)
**First observed:** 2026-04-01 (Issue #36 cleanup phase)

### Symptom

Loop engine detects PR merged → calls `Remove()` → fails with:

```
cleanup error: removing worktree: git worktree remove --force <path>:
error: failed to delete '<path>': Permission denied: exit status 255
```

The worktree directory cannot be deleted due to a Windows file lock and remains on disk.

> **Partially fixed:** `CloseIssue` is no longer skipped when cleanup fails. `loopCleanupCmd` now executes `CloseIssue` regardless of the `Remove()` result (Fix Direction #3 is implemented). However, the Permission Denied error when deleting the worktree directory on Windows still persists.

### Root Cause

Windows does not allow deleting a directory that any process is using as its current working directory (CWD).

Loop engine cleanup sequence:

1. Loop launches reviewer agent → Claude Code process CWD is set to the worktree directory
2. Reviewer finishes work, sets the `ai-review` label → Loop detects PR merged
3. Loop calls `Remove()` to delete the worktree directory
4. **At this point the reviewer's Claude Code session may not have fully exited** (process still holds the CWD)
5. `git worktree remove --force` → Permission denied
6. Fallback `removeDirRetry` → also blocked by the Windows file lock

Linux / macOS allow deleting a directory that is held as a CWD, so this issue only occurs on Windows.

### Workaround

After the process holding the worktree directory exits, clean up manually:

```bash
git -C <repo-path> worktree remove --force <worktree-path>
```

Or restart the Loop and let it retry cleanup during the next resume round.

### Potential Fix Directions

1. **Wait for agent process exit before cleanup** — in `loopCleanupCmd`, confirm the agent session PID has exited before calling `Remove()`. Be mindful of the PID-reuse race condition.
2. **Deferred cleanup queue** — when cleanup fails, add the worktree path to a retry queue and retry on the next poll cycle.
3. ~~**Separate CloseIssue from Remove**~~ — **Implemented.** `loopCleanupCmd` now executes `CloseIssue` regardless of the `Remove()` result; worktree deletion is degraded to best-effort.

### Related

- Issue #36: `git branch -d` → `-D` fix (merged, unrelated to this issue but triggered in the same cleanup)
- `internal/worktree/manager.go`: `removeDirRetry` fallback logic
- `internal/tui/loop_cmds.go` `loopCleanupCmd`: executes `CloseIssue` even when cleanup fails

---

## 2. Shared-worktree git index race during `[P]` parallel task batch

**Affected OS:** all platforms
**Component:** `agents/task-runner.md`, `internal/prompt/coding.go` (`buildTeamDelegation`)
**First observed:** 2026-04-18 (Issue #9, coding session `b51aef45-6d51-48d3-926f-f1ba50ddcd7f`)
**Status:** ✅ Fixed (v2, 2026-04-20) — the initial fix had path resolution errors under linked worktrees; see Fix v2 below.

### Symptom

When the coding orchestrator dispatched a `[P]` batch to an Agent Team (T1–T9 running concurrently), the teammates shared the same worktree's `.git/index` and within a few commits the contents were cross-contaminated — some T{N} commits contained files from other teammates. The task-runner subagents corrected individual erroneous commits in place using `git commit -- <pathspec>`, and left the following note in their reports:

> When T1–T9 ran as parallel subagents, the shared worktree's git index race caused several commits to have cross-contaminated contents; each subagent self-corrected using `git commit -- pathspec`. For large parallel batches in the future, batching or switching to sequential is recommended.

### Root Cause

Two levels of race occurred simultaneously:

1. **Index race** — multiple teammates' `git add` commands wrote to `.git/index` concurrently, overwriting each other's staging area. The tree read by a subsequent `git commit` was therefore a mixed staging result.
2. **Ref race** — multiple `git commit` commands concurrently updated `refs/heads/<branch>`, and contention on `refs/heads/<branch>.lock` caused intermittent "cannot lock ref" failures.

Claude Code's Agent Team does not allow the orchestrator to assign a different CWD to each teammate, so "give each teammate its own worktree" was not achievable under the existing model; the git-native index isolation mechanism had to be used instead.

### Fix

The orchestrator prompt and the `task-runner` subagent doc were extended with a **Parallel Commit Protocol** (three-layer defense, applicable only to `[P]` teammates; sequential tasks own the index exclusively and do not follow this flow):

1. **Index isolation** — `export GIT_INDEX_FILE=.git/index.zpit.T{N}`: each teammate uses a private staging index
2. **Pathspec safety net** — `git add -- <declared files only>` (even if Layer 1 is bypassed, the pathspec prevents cross-task contamination)
3. **Commit serialization** — `mkdir .git/zpit-commit.lock` (atomic, cross-platform) acquires the lock, retries up to 5 times with jittered sleep, releases with `rmdir` after commit

Trigger: the orchestrator injects a `parallel_task_id: T{N}` line into each teammate's spawn prompt; the `task-runner.md` Parallel Commit Protocol section uses that line as the activation signal.

### Code Locations

- `agents/task-runner.md` §Parallel Commit Protocol — complete teammate-side steps and commands
- `internal/prompt/coding.go` `buildTeamDelegation` — orchestrator injects `parallel_task_id` and protocol summary
- `docs/agent-guidelines.md` §Git Operations — cross-reference
- `internal/prompt/prompt_test.go` — `TestBuildCodingPrompt_WithParallelTasks` / `WithTasks` bidirectional assertions (sequential prompt must not leak protocol strings)

### Fix v2 (2026-04-20, Issue #11 session `14b85919-…`)

**Regression:** The initial fix hard-coded the protocol commands as literal paths `.git/index.zpit.T{N}` and `.git/zpit-commit.lock`. However, zpit normally dispatches to a linked worktree, where `.git` is a **pointer file pointing to `<main-repo>/.git/worktrees/<name>`, not a directory**. In practice, all 33 task-runner subagents hit `fatal: Unable to create '.git/index.zpit.T1.lock': No such file or directory`. When they fell back to self-recovery via `GIT_DIR=$(git rev-parse --git-dir)`, the original protocol never instructed them to seed the private index with `git read-tree HEAD`, so commits produced massive spurious deletions — 54 files / 2593 deletions (T1 `b27d210`, T5 `8ff9ef8`, etc.).

**Fix v2:** Rewrote `agents/task-runner.md` §Parallel Commit Protocol and the summary in `internal/prompt/coding.go` `buildTeamDelegation`:

1. Paths are now resolved via `git rev-parse --git-dir` (per-worktree index) and `git rev-parse --git-common-dir` (cross-worktree lock) — **no more hard-coded `.git/...`**.
2. Added a `GIT_INDEX_FILE="$IDX" git read-tree HEAD` step to seed the private index so commits do not treat all unstaged files as deleted.
3. Explicitly required that the entire sequence run in **a single Bash tool call** (Claude Code spawns a fresh shell for each Bash tool call; `export` does not persist across calls); all git commands are prefixed inline with `GIT_INDEX_FILE="$IDX"`.
4. The failure path also runs `rm -f "$IDX"` to avoid leaving stale files.

**References:** StackOverflow / git-scm docs confirm that `git commit` does not auto-retry on `refs/heads/*.lock` (VS Code #47141, Graphite blog), so the mkdir lock + jittered retry is still necessary; `pre-commit` #2295 warns about path misinterpretation of `GIT_INDEX_FILE` under linked worktrees.

### Fix v3 (2026-04-21, Issue #13 session `3192ffd3-…` / `12e4f992-…` / `3359f5f1-…` / `b6f7633d-…`)

**Regression:** Fix v2 resolved "the parallel teammate's isolated index must be seeded from HEAD", but did not address the mirror problem — **when the entire parallel batch completes, the shared worktree's main index (`$GIT_DIR/index`) has never been updated; it still reflects the tree from before the batch started.** Any subsequent sequential task following the normal flow of `git add -- <files> && git commit` builds on "stale main index" plus a small set of new staged changes, effectively silently reverting the entire batch.

Issue #13 situation: T1–T9 advanced HEAD successfully using the v2 protocol; T10 (sequential, modifying `CLAUDE.md`) followed the instructions and ran `git add -- CLAUDE.md && git commit`, but commit `a4a7f9b` touched 10 files:
- `CLAUDE.md`: +22 / -0 ✅ (expected)
- `src/hooks/useIsMobile.ts`: status: removed, -32 ❌ (T1's new file was "deleted")
- 8 other mobile components: all reverted to their pre-T1 state ❌

The next commit `fe3a799` (manual restore) recovered the lost changes. PR #14 therefore shows a U-shaped "add → delete → restore" pattern in git history.

Root Cause:
1. `GIT_INDEX_FILE=$IDX git commit` only updates the private index and HEAD; it does not write back to `$GIT_DIR/index`.
2. The only intended sync point for the main index was a `git read-tree HEAD` reload somewhere after each teammate finished — but the v2 protocol omitted this step; teammates each ran `rm -f "$IDX"` and exited.
3. Downstream sequential tasks picked up the stale main index plus their own new files; the committed tree = old world + new files, and everything else was treated as "deleted".

**Fix v3 (orchestrator-side resync):** In `internal/prompt/coding.go` `buildTaskWorkflow`'s Task Execution Order block, a resync instruction is emitted **after each parallel group completes**: "Before any later `git add` / `git commit` against the main index, run `git read-tree HEAD` in the worktree root". The orchestrator calls this once after each batch completes, ensuring that any subsequent sequential task or final-adjustment commit by the orchestrator is built on the correct baseline.

Why not put this in the teammate protocol? **Parallel writes to `$GIT_DIR/index` themselves cause a race** — if 9 teammates simultaneously run `git read-tree HEAD`, 8 will hit `index.lock: File exists`, requiring yet another mkdir-lock layer. The orchestrator is the only party that knows "the batch is done"; a single, lock-free resync is cleaner.

**Fix v3 code changes:**
- `internal/prompt/coding.go` `buildTaskWorkflow` — the parallel-group Task Execution Order output appends the resync instruction
- `agents/task-runner.md` gains a new §What NOT to do: explicitly forbids teammates from resyncing the main index themselves
- `internal/prompt/prompt_test.go` — adds `TestBuildCodingPrompt_ParallelBatchResync` (every `[P]` group must trigger a resync instruction); updates `WithTasks` / `WithParallelTasks` assertions (sequential-only prompts must not contain the "Resync main index" string; parallel prompts must contain it and it must appear after the Parallel group instructions)
- `CLAUDE.md` §Task Execution Model Parallel Commit Protocol section updated to document the orchestrator-side resync

**References:** `git-read-tree` docs (plain mode replaces only the index, does not touch the worktree); `git-reset` docs (`--mixed` has ORIG_HEAD + reflog side effects, making `read-tree` cleaner); pluralsight / Microsoft Learn on `index.lock` (supports the "don't resync in parallel" decision).

### Future Risks

- If `[P]` batches routinely exceed 5 tasks or the `[P]` rules are frequently violated (tasks actually touching shared files), this protocol cannot handle semantic-level conflicts. The correct upgrade is "one worktree per `[P]` task", or switching to plumbing `commit-tree` + `update-ref` CAS retry to bypass the index/ref lock entirely. The discussion at the time is in plan `C:\Users\Jeff\.claude\plans\1-2-vast-lark.md` §Design opening.
- **Fix v3 is the third patch on the shared-worktree model.** Industry convention (Cursor, Claude Code docs, Augment, spec-kit) all use "per-teammate worktree". If another incident occurs (v4 scale), the correct response is not a v4 patch but rather switching to the per-teammate worktree architecture — the existing `internal/worktree/` already provides worktree lifecycle management, and extending it to batch-ephemeral worktrees is engineering work of manageable scope, preferable to continued accumulation of shared-worktree protocol complexity.

### Resolution (2026-04-21, per-teammate worktree via Claude Code `WorktreeCreate` hook)

Fix v1/v2/v3 are now dormant archive — the Parallel Commit Protocol string is no longer emitted by `buildTeamDelegation` and the orchestrator-side `git read-tree HEAD` resync is no longer emitted by `buildTaskWorkflow`. Migration triggered by the predicted v4 incident risk, not by a new regression: zacfuse issue #11 (PR #12, 2026-04-20, ran under Fix v2) produced three recovery commits (`799267c`, `effbe57`, `febda14`) for silent index-bleed during parallel batches, confirming shared-worktree races were not fully closed by v2. Rather than wait for a v4 incident, the architecture was switched.

**New design** — see CLAUDE.md §Task Execution Model → "Per-Teammate Worktree Model":

1. Orchestrator calls the Agent tool with `isolation: "worktree"` per `[P]` teammate.
2. Claude Code invokes zpit's `WorktreeCreate` hook (`hooks/worktree-create.sh`), which forks a child worktree from orchestrator's current HEAD under `<parent>/.zpit-children/<slug>` (Claude Code's built-in path would fork from `origin/<defaultBranch>` — confirmed in `D:\Documents\MyProjects\claude-code-source-code\src\utils\worktree.ts:284-302` — losing any sequential task commits landed earlier in the loop).
3. Hook copies `.claude/` + `.mcp.json` into the child so path-guard/bash-firewall/git-guard fire correctly there.
4. Teammate commits normally inside the child on branch `<parent-branch>-<slug>` — no shared index, no shared ref-lock.
5. Agent tool returns `{worktreePath}` per teammate. (The migration plan assumed `worktreeBranch` would also be returned, but §3 below documents that Claude Code's hook-based path never actually propagates it — orchestrator discovers each branch via `git -C <worktreePath> rev-parse --abbrev-ref HEAD` before cleanup.) Orchestrator post-batch emits `git cherry-pick <branch1> <branch2> ...` in task-ID order, then `git worktree remove --force` and `git branch -D` as TWO SEPARATE Bash calls (never chained — see §4). Cherry-pick conflicts (spec bug: two `[P]` tasks share a file) surface as `cherry-pick --abort` → NeedsHuman instead of silent reverts.

**Supporting changes:**
- `hooks/path-guard.sh` — `ALLOWED_DIR` now uses `git rev-parse --show-toplevel` so it self-adapts to whichever worktree the teammate is running inside. Claude Code pins `CLAUDE_PROJECT_DIR` to the orchestrator's project root (not the worktree path — see `claude-code-source-code/src/utils/hooks.ts:813,884`), so the old behavior would have made the check too permissive inside child worktrees.
- `internal/worktree/hooks.go` — `WorktreeCreate` hook registered in all three `hookModeTemplates`; `.zpit-children/` added to `zpitIgnoreRules`.
- `internal/prompt/prompt_test.go` — `TestBuildCodingPrompt_ParallelBatchResync` deleted; `TestBuildCodingPrompt_WithParallelTasks` rewritten to assert the new worktree-isolation strings; new `TestBuildCodingPrompt_ParallelBatchIntegration` asserts every `[P]` group emits its own cherry-pick + cleanup block.
- `agents/task-runner.md` — entire Parallel Commit Protocol section removed; frontmatter unchanged (Claude Code source confirms `isolation` is a runtime tool parameter, not a frontmatter key — `claude-code-source-code/src/tools/AgentTool/AgentTool.tsx:99`).

The three incident fixes above stay in this document as history — useful if we ever need to diagnose a symptom that resembles shared-worktree index bleed in another codebase.

---

## 3. Claude Code `WorktreeCreate` hook does not propagate `worktreeBranch`

### Symptom

When the orchestrator calls the Agent tool with `isolation: "worktree"` and zpit's `WorktreeCreate` hook is registered, the Agent tool result populates `worktreePath` but returns `worktreeBranch: undefined`.

Observed during the per-teammate worktree smoke test (`zpit-test-worktree` issue #1, 2026-04-22, session `092f074a-…`):

```
tool_result: worktreePath = D:\Documents\.worktrees\...\.zpit-children\agent-a977314b
             worktreeBranch = undefined
```

The orchestrator guessed the actual branch name from the zpit naming convention (`<parent-branch>-<slug>`) and proceeded successfully, but this is fragile — if zpit's naming ever drifts the inference silently breaks.

### Root Cause

Confirmed in Claude Code source (`D:\Documents\MyProjects\claude-code-source-code`):

- `src/utils/hooks.ts` — `executeWorktreeCreateHook()` parses stdout as plain text and returns `{worktreePath: string}`. No mechanism to parse or propagate a branch name.
- `src/utils/worktree.ts:902–951` — `createAgentWorktree()` branches on `hasWorktreeCreateHook()`. In the hook path it returns `{worktreePath, hookBased: true}` and never sets `worktreeBranch`. Only the built-in `git worktree add -b` path sets it.
- `src/tools/AgentTool/AgentTool.tsx:643–685` — `cleanupWorktreeIfNeeded()` forwards `worktreeBranch` from the internal worktreeInfo object. If `hookBased` it's never set, hence `undefined`.
- `src/entrypoints/sdk/coreSchemas.ts:961–970` — `WorktreeCreateHookSpecificOutputSchema` documents the hook's output as `{hookEventName, worktreePath}` only.

This is a Claude Code design gap, not a bug in zpit's hook.

### Workaround

Orchestrator discovers each teammate's branch via `git`:

```
for path in <worktreePath-T{N1}> <worktreePath-T{N2}> ...; do
  git -C "$path" rev-parse --abbrev-ref HEAD
done
```

Authoritative (doesn't depend on slug inference or any naming convention), cheap (one git call per teammate), and must run BEFORE cleanup — removing the worktree also removes the branch mapping.

### Related code

- `internal/prompt/coding.go` — `buildTeamDelegation` notes the gap; `buildTaskWorkflow` emits the rev-parse discovery step before the cherry-pick block.

### Upstream fix options (for future if Claude Code addresses this)

- Extend `WorktreeCreateHookSpecificOutputSchema` to accept `{worktreePath, worktreeBranch}` JSON on stdout instead of plain text.
- Have `createAgentWorktree()` run `git -C <path> rev-parse --abbrev-ref HEAD` after the hook returns, since it already has `worktreePath` in hand.

Neither is in current Claude Code — workaround lives in zpit's orchestrator prompt until then.

---

## 4. `git-guard.sh` blocked `git branch -D`, breaking per-teammate cleanup

### Symptom

After the per-teammate worktree cherry-pick, the orchestrator's cleanup attempted:

```
git worktree remove --force ... && git branch -D <teammate-branch>
```

git-guard.sh blocked the compound command with `BLOCKED: Git operation … is not allowed`. Orchestrator split the call but never retried `git branch -D` separately, leaking teammate branches (`feat/1-…-agent-a4035b9d`, `…-agent-a977314b`) as local orphans.

### Root Cause

Two issues compounded:

1. `hooks/git-guard.sh` blocklist (`GIT_BLOCKED`) included `git\s+branch\s+-[dD]\s`, intended to prevent agents from deleting real feature branches. The per-teammate worktree migration (commit `ec6b95e`) introduced a legitimate need to delete ephemeral teammate branches, but did not update the hook.
2. `internal/prompt/coding.go` `buildTaskWorkflow` emitted the cleanup as a single `for … done; for … done` bash compound, so any hook rejection on the `git branch -D` portion killed the `git worktree remove` portion too. The orchestrator fallback split the commands but did not retry the blocked step.

Also observed: `git worktree remove` without `--force` failed because child worktrees contain the copied `.claude/` directory, which `git worktree remove` considers "untracked files".

### Fix (2026-04-22)

- `hooks/git-guard.sh` — whitelist `git branch -D` when every branch argument matches the teammate convention `…-agent-<hex>` (Claude Code's default isolation slug). Arbitrary `git branch -D` still blocked.
- `internal/prompt/coding.go` — emit cleanup as TWO SEPARATE Bash tool calls (worktree-remove, branch-delete), always pass `--force` to `git worktree remove`, instruct orchestrator to retry `branch -D` independently if it fails.

### Code locations

- `hooks/git-guard.sh` — teammate whitelist block (inserted before `GIT_BLOCKED` loop).
- `internal/prompt/coding.go` — `buildTaskWorkflow` parallel-group cleanup block.
- `hooks/hooks_test.go` — `TestGitGuard_AllowsTeammateBranchDelete`, `TestGitGuard_BlocksMixedTeammateAndOtherBranchDelete`.

---

## 5. Stale `.claude/settings.json text eol=lf` in `.gitattributes` + `.claude/settings.local.json` in `.gitignore`

### Symptom

Fresh agent launches on pre-existing zpit projects showed uncommitted drift in `.gitignore` (`.claude/settings.local.json` being auto-appended on every launch). Some projects also carried a dead `.claude/settings.json text eol=lf` line in `.gitattributes` — leftover from when `.claude/settings.json` was committed to git (pre-`202a0f3`, 2026-04-21).

### Root Cause

Commit `202a0f3` gitignored `.claude/settings.json` (closing a fresh-clone bug) but left two artifacts:

1. `EnsureGitattributes` still wrote `.claude/settings.json text eol=lf` — pointless once the file is gitignored, but kept appending the line on every launch, polluting user-owned `.gitattributes`.
2. `EnsureGitignore` auto-added `.claude/settings.local.json` to every project's `.gitignore` at agent launch, causing drift on each run.

### Fix (2026-04-22)

- `internal/worktree/hooks.go` — drop `.claude/settings.local.json` from `zpitIgnoreRules`; delete `zpitGitattributesRules` and `EnsureGitattributes` entirely.
- `internal/tui/launch.go`, `internal/tui/loop_cmds.go` — remove the four `EnsureGitattributes` call sites.

`.claude/settings.json` itself remains in `zpitIgnoreRules` — that's the invariant from `202a0f3`. Users with stale `.gitattributes` / `.gitignore` entries in existing projects can clean them manually; zpit does not retroactively edit user-owned files.

---

## 6. Silent data loss via teammate `cd` + `cherry-pick --skip`

### Symptom

During a `[P]` batch integration, the orchestrator's cherry-pick fails with `error: The previous cherry-pick is now empty, possibly due to conflict resolution.` The orchestrator runs `git cherry-pick --skip` to "recover" and proceeds. On post-run inspection:

- `git branch -D <teammate-branch>` reports `was <parent-pre-batch-HEAD>` for every teammate (not a teammate commit SHA) — the teammate branches never advanced.
- All task commits are somehow on the parent branch anyway, with the correct files.

### Root Cause

`task-runner` subagents spawned with `isolation: "worktree"` ignored the CWD that Claude Code set for them (the child worktree at `.zpit-children/<slug>`) and manually `cd`-ed to the parent worktree's path. Most likely trigger: the orchestrator mentioned the parent worktree path in the teammate's spawn prompt as "project root" context, and the teammate naively `cd`-ed there.

All git commands then operated on the shared parent worktree's index:

- Commits landed directly on the parent branch (visible in the final PR — "it looks like it worked").
- The designated child-worktree branches stayed at the pre-batch parent HEAD.
- Cherry-picking an empty branch produced the "empty commit" error (because the parent branch already contained the content, via the teammate's direct commit).
- `git cherry-pick --skip` silenced the signal and the orchestrator moved on.

The per-teammate worktree isolation — which exists to prevent index/ref races between parallel teammates (see §2 for the three iterations of the retired Parallel Commit Protocol it replaced) — was completely defeated. In issue #99 the files happened to be present (teammates committed them to the parent directly, so the final PR was correct). **In a future run where teammates actually use their worktrees but one `cd`s out, `--skip` would silently drop that teammate's work.** This is why the architectural fix below is required, not "run it again and hope".

### Fix (2026-04-22)

Four layers:

1. **Detection (prompt)**: `internal/prompt/coding.go` `buildTaskWorkflow` now emits a `PARENT_HEAD=$(git rev-parse HEAD)` snapshot instruction before dispatching any `[P]` batch, and the post-batch loop (previously just `git -C <path> rev-parse --abbrev-ref HEAD`) now additionally runs `git -C <path> rev-parse HEAD` and compares the tip against `$PARENT_HEAD`. Any teammate branch still at `$PARENT_HEAD` aborts the batch with a visible `ABORT: teammate branch ... still points at PARENT_HEAD ...` error. The prompt also explicitly forbids `git cherry-pick --skip` — if the sanity check passes, any later "empty commit" error means something is genuinely wrong and must be surfaced, not skipped.
2. **Prevention (task-runner)**: `agents/task-runner.md` gains a `## Working Directory` section immediately after `## Startup`, forbidding `cd` out of the spawned CWD, with a `git status` self-check recipe (branch name must end in `-agent-<hex>`).
3. **Prevention (agent-guidelines)**: `docs/agent-guidelines.md`'s `[P]` teammate bullet now includes the `cd` ban, the `git status` self-check, and the data-loss rationale.
4. **Prevention (orchestrator prompt)**: `buildTeamDelegation` warns the orchestrator not to embed worktree paths, absolute paths, or `cd` instructions in the teammate's spawn `prompt` argument — the most likely trigger for the `cd` behavior.

### Evidence

Issue #99 smoke test (2026-04-22, PR #100 — NOT reverted, files landed correctly by accident because teammates committed them to the parent directly):

- Session log: `C:/Users/Peanut/.claude/projects/D--Documents--worktrees-zpit-99--loop-sync-local-base-branch-after-pr-mer/daffd87e-fb24-4720-aab9-e6513c774b96.jsonl`
- Both T1 and T2 teammates ran `cd "D:\Documents\.worktrees\zpit\99--..."` as their first Bash command. Commits landed on the parent branch immediately (`522d2f6` for T1, `b170506` for T2). Teammate branches remained empty at `b35f0d2`. Orchestrator's cherry-pick produced `You are currently cherry-picking commit b35f0d2` and `The previous cherry-pick is now empty` — `b35f0d2` was the pre-batch parent HEAD. Orchestrator then ran `git cherry-pick --skip`.
- Contrast: issue #3 smoke test (2026-04-22 earlier, PR #4) where teammate branches correctly advanced past parent HEAD (`git branch -D` reported `was 24b81e4` / `was 87c6aa0`). Same prompt version, different orchestrator behavior — the `cd` trigger is non-deterministic and depends on what the orchestrator happens to write in the teammate's spawn prompt. Reinforces why the architectural fix (Layer 1 sanity check) is required, not just prompt guidance.

---

## 7. Windows: Desktop agent cannot interact with elevated (High IL) installers / admin apps

**Affected OS:** Windows only
**Component:** Desktop agent (`zpit serve-desktop-proxy` → `npx zpit-desktop-mcp`), Windows UIPI (User Interface Privilege Isolation)
**First observed:** 2026-05-14 (KV Studio installer dialog, session `022a118c-…`)

### Symptom

Desktop agent operates an installer or other admin app. Tool calls return success but the target UI shows zero state change:

```
mcp__desktop-proxy__activate_window  →  {"activated": true}
mcp__desktop-proxy__left_click       →  "Clicked (1052, 580)"
mcp__desktop-proxy__key {text:"return"} →  "Pressed return"
mcp__desktop-proxy__screenshot       →  identical to previous frame
```

Same dialog persists across multiple click / key attempts. `press_button` may also fail upstream with `No button matches label="OK"` because the target uses owner-drawn controls invisible to UI Automation — but the deeper issue is that even coordinate-based input is silently dropped.

### Root Cause

Windows **UIPI** blocks `SendInput` / `PostMessage` / `SetCursorPos` from a lower Integrity Level (IL) process to a higher-IL target window. The Win32 API call returns success; the message is discarded before delivery.

Default zpit launch chain:

```
unelevated pwsh / cmd  (Medium IL)
  └─ zpit.exe             (Medium IL)
       └─ claude.exe       (Medium IL)
            └─ zpit serve-desktop-proxy  (Medium IL)
                 └─ npx zpit-desktop-mcp / Node  (Medium IL)
                      └─ SendInput → setup.exe  (High IL)  ✗ dropped by UIPI
```

`ConsentPromptBehaviorAdmin=0` (silent UAC elevation) does NOT mitigate this — it only suppresses the UAC popup, it does not lower the installer's IL or raise the desktop agent's IL.

### Diagnostic recipe

Confirm the target is High IL from an unelevated PowerShell (no admin rights needed for these calls):

```powershell
$pid_target = <installer-PID>

# Indirect IL probe via OpenProcess access rights
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public class ProcCheck {
    [DllImport("kernel32.dll", SetLastError=true)]
    public static extern IntPtr OpenProcess(uint dwDesiredAccess, bool bInheritHandle, uint dwProcessId);
    [DllImport("kernel32.dll", SetLastError=true)]
    public static extern bool CloseHandle(IntPtr h);
}
"@
$h1 = [ProcCheck]::OpenProcess(0x0400, $false, $pid_target)   # PROCESS_QUERY_INFORMATION
$err1 = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
$h2 = [ProcCheck]::OpenProcess(0x1000, $false, $pid_target)   # PROCESS_QUERY_LIMITED_INFORMATION
$err2 = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
"0x0400: handle=$h1 err=$err1"
"0x1000: handle=$h2 err=$err2"
```

Diagnosis table:

| 0x0400 result | 0x1000 result | IL of target |
|---|---|---|
| success | success | same IL as caller (Medium) — UIPI is NOT the issue, look elsewhere |
| err=5 (ACCESS_DENIED) | success | **target is High IL** — UIPI confirmed |
| err=5 | err=5 | different user / protected process — not solvable by elevating zpit |

A secondary tell: `Get-CimInstance Win32_Process -Filter "ProcessId=$pid"` returns empty `ExecutablePath` and `CommandLine` for a High IL target queried from Medium IL (those fields require 0x0400).

### Workaround

Run zpit from an **elevated** Windows Terminal / PowerShell 7 session for the duration of the admin task:

1. Right-click Windows Terminal → "Run as administrator" (or use an elevated WT profile)
2. Inside, run `zpit` normally — every child process inherits High IL
3. Press `[w]` to launch the desktop agent; the MCP chain is now High IL end-to-end and `SendInput` reaches admin apps

Verification inside the elevated WT:

```powershell
whoami /groups | findstr Mandatory   # → "Mandatory Label\High Mandatory Level"
```

Title bar should show `Administrator:` prefix. `wt.exe` keeps elevated / non-elevated `WindowsTerminal.exe` instance pools fully isolated, so calling `wt.exe` from a High IL process always spawns or joins the High IL pool — the new tab's shell is reliably High IL.

### Caveats of running zpit elevated

- **Safety boundary widens** — the desktop agent now has High IL against every app, not just the installer. `~/.zpit/desktop-policy.toml` (`deny_keys`, `allow_bundles`, tool allowlist) becomes the only safety layer for the elevated surface; review it before launching the agent under elevation.
- **`ssh.auto_serve = true` inherits elevation** — remote SSH sessions opened against an elevated zpit are also High IL. Avoid if you SSH from untrusted networks.
- **File Explorer drag-and-drop stops working into the elevated WT** — Medium IL Explorer cannot drag into High IL windows (UIPI in the other direction). Use `Copy as path` + paste instead.
- **Treat as temporary mode** — elevate only for the admin task at hand, then close the elevated WT. Do NOT set zpit.exe's compatibility flag to "always run as administrator"; that would leave every zpit session at High IL.

### Why this is filed under known-issues rather than fixed

A code-level fix would require zpit / desktop-proxy to self-elevate via UAC (`runas` verb + `ShellExecute`), which spawns a new elevated process tree rather than elevating the current one — meaning the user still hits a UAC prompt, and the TUI state would not transfer cleanly across the elevation boundary. The manual "open elevated WT first" workflow has the same UAC cost with a cleaner mental model and zero TUI/state migration.

If this becomes a frequent friction point, the cleanest direction is a **per-session elevation prompt** in the TUI: when the user presses `[w]` and any visible top-level window has a higher IL than zpit, surface a one-line warning ("Target apps may need admin — relaunch zpit elevated?") rather than letting the agent run blind into silent input drops.

### Related code / docs

- `docs/architecture/desktop-agent.md` — desktop agent architecture and policy file format
- `~/.zpit/desktop-policy.toml` — the only safety layer once running elevated; auto-created on first `zpit serve-desktop-proxy` invocation
- `internal/terminal/launcher_windows.go` `launchWindows` — `exec.Command("wt.exe", ...)` is standard `CreateProcess`, inherits parent IL (this is what makes the elevated-WT workaround work end-to-end)

---

## 8. UIA `InvokePattern.Invoke` blocks until modal dialog dismissal — `press_button` hangs on "open dialog" buttons

**Affected OS:** Windows (UIA is Windows-only in zpit-desktop-mcp)
**Component:** zpit-desktop-mcp `perform_action` (Rust NAPI `accessibility::perform_action`)
**First observed:** 2026-05-14 (KV Studio installer session `022a118c-…`)
**Status:** Mitigated in zpit-desktop-mcp 1.2.3 (2026-05-19) — `press_button` rewritten to use SendInput. `set_value` still uses UIA `ValuePattern.SetValue` and may exhibit the same pattern; not yet mitigated.

### Symptom

`press_button` / `set_value` against a button whose action opens a modal dialog returns successfully — but takes 50–90 seconds, with the dialog visible the whole time. The agent perceives "MCP hung". Manual mouse click on the same button opens the same dialog in <1s.

Empirical measurements against KV Studio's "Open Project (Ctrl+O)" toolbar button (PID 22208, same Integrity Level as MCP, no cross-IL slowdown):

| Path | Time | Outcome |
|---|---|---|
| `findElement` (locate the button) | 99–141 ms | Returns the element |
| `mouseClick` at button-center coords (SendInput) | 15 ms | Dialog opens, click returns |
| `performAction` AXPress (UIA `InvokePattern.Invoke`) before 1.2.3 | 53,315 ms | Dialog opens immediately, **Invoke blocks until dialog dismissed**, then returns `performed: true` |
| `performAction` on "Cancel" inside the open dialog (Invoke for a modal-closing action) | 211 ms | Closes dialog, returns immediately |

The Cancel-button test is the null hypothesis check: Invoke is fast EXCEPT when the action keeps a modal open. So it isn't `find_first` slowness, isn't KV Studio's UIA tree size (~76 nodes), isn't cross-IL, isn't WPF-specific.

### Root cause

This is documented Microsoft behavior, not a provider bug. From `IInvokeProvider::Invoke` (Win32 UIA):

> "IInvokeProvider::Invoke is an asynchronous call and must return immediately without blocking. **Note** This is particularly critical for controls that, directly or indirectly, launch a modal dialog when invoked. **Any Microsoft UI Automation client that instigated the event will remain blocked until the modal dialog is closed.**"
> — [learn.microsoft.com/.../iinvokeprovider-invoke](https://learn.microsoft.com/en-us/windows/win32/api/uiautomationcore/nf-uiautomationcore-iinvokeprovider-invoke)

And from `InvokePattern.Invoke` (.NET client side):

> "Calls to Invoke should return immediately without blocking. However, this behavior is entirely dependent on the Microsoft UI Automation provider implementation. **In scenarios where calling Invoke causes a blocking issue (such as a modal dialog) a separate helper thread may be required to call the method.**"
> — [learn.microsoft.com/.../invokepattern.invoke](https://learn.microsoft.com/en-us/dotnet/api/system.windows.automation.invokepattern.invoke?view=windowsdesktop-7.0)

Microsoft instructs *providers* to be async, but real-world providers (WPF, Common File Dialog, many industrial apps) frequently are not. Microsoft's recommended *client-side* workaround is a helper thread; zpit-desktop-mcp 1.2.3 took the simpler equivalent — bypass Invoke entirely.

### Investigation trail

Three iterations before the right fix landed:

1. **First hypothesis (wrong): `find_first` tree walk is the bottleneck.** Theory: `FindAll(TreeScope_Descendants, ...)` walks every UIA descendant cross-process, and KV Studio's tree is huge. Fix in 1.2.2 (commit `0535487`, `perf(accessibility): FindFirst fast path`) added a `FindFirst` short-circuit. Direct NAPI benchmark showed `findElement` for the same button completes in 99 ms — the tree was only 76 nodes total. **Find_first was never slow; the fix is still correct and useful for cross-IL scenarios but didn't move the needle here.**

2. **Second hypothesis (wrong): KV Studio's WPF `AutomationPeer.Invoke` is broken / synchronously runs a slow command.** Tested by calling `performAction` directly via Node + NAPI bench script, no dialog appeared (so we briefly thought the action wasn't reaching the button). User reported the dialog DID appear in their interactive session — manually closing the dialog unblocked the Invoke return. Reframed the question: not "why doesn't Invoke trigger" but "why doesn't Invoke return when the dialog opens".

3. **Correct root cause: Invoke is synchronous with the bound command's full lifetime; for a modal-opening command, that's "dialog dismissal".** Confirmed via the Cancel-button test (action = close modal → returns in 211 ms) + the Microsoft Learn quotes above.

### Fix (zpit-desktop-mcp 1.2.3)

`native/src/accessibility.rs::perform_action` AXPress branch rewritten:

1. **TogglePattern first** — checkboxes / toggle buttons stay on the UIA path. Toggle is semantic, doesn't open modals, no blocking risk.
2. **Otherwise:** read the element's `CurrentBoundingRectangle`, compute the center, send a `MOUSEEVENTF_LEFTDOWN`/`LEFTUP` pair via `SendInput`. Fire-and-forget — queues to the OS input pipeline and returns. Empirically 37 ms end-to-end vs the previous 53,315 ms, same resulting state.

Caveats of the SendInput path:
- Requires the target window to be foreground at click time. `session.ts`'s `ensureFocusV4` already runs before every `press_button`, so production callers are covered.
- Loses theoretical UIA Invoke semantics for keyboard-accelerator-only commands (commands bound to a button via UIA but with no visible-click handler). Empirically irrelevant for normal GUI buttons.

### `set_value` and `get_ui_tree` 76 s / 96 s session timings: not reproducible, cause unknown

The same May 19 session that surfaced the press_button modal-blocking also showed `get_ui_tree(wid=Open dialog, depth=4)` taking 76 s (user-interrupted) and the earlier May 14 session showed `set_value` on the File name field taking 96 s. Initial speculation attributed both to cross-IL UIA proxy slowdown (see §7), but the user clarified both sessions were running same-IL (no elevated zpit). That hypothesis is therefore retracted.

Targeted reproduction attempts under matching conditions — same-IL, empty project, dialog defaulting to Documents folder, `*.kpr` filter, real-existing path identical to the session call — failed to surface anything close:

| Call | Stress-test stats (5 iters, same-IL) | Session timing |
|---|---|---|
| `find_element` AXButton on main window | 96–152 ms (p50 103 ms) | n/a |
| `get_ui_tree` main window d=6 | 166–176 ms | n/a |
| `get_ui_tree` Open dialog d=4 (160 nodes) | 595–633 ms | **76 000 ms** |
| `set_value` AXTextField "File name:" = real path | 189–194 ms (max var 5 ms across 5 iters) | **96 000 ms** |
| `find_element` Cancel button | 284–293 ms | n/a |

The MCP-layer path was also exercised end-to-end (spawned `dist/server.js` as subprocess, talked JSON-RPC over stdio exactly like the desktop-proxy does) — `set_value` and `get_ui_tree` measured identically to the direct NAPI bench. `press_button` shows a one-shot ~5 s cold-start on the first call (the `ensureFocusV4` → `activateApp` → `activateWindow` Win32 dance bringing the app from background to foreground), then drops to 24–26 ms — same cold-start pattern that's visible in the session log around 02:23:50 (`activate_window` 5.19 s) and 02:24:09 (`left_click` 5.26 s). That cold-start is not the 76 s / 96 s mystery.

### Most plausible explanation — global UIA event subscribers

After exhausting the in-process hypotheses, the cause that best fits the observed pattern is **another process on the system holding a global UIA event subscription**. The mechanism is documented in [this gist by Skydev0h](https://gist.github.com/Skydev0h/3a8c08b148a38e8d270c02b563130ff6) — the author tracked a year-long systemic UIA slowdown to `PAD.BridgeToUIAutomation2.exe`:

> "The overhead is on the provider side, not the client side. When any client subscribes to global UI Automation events: Every application with a visible window becomes a provider. On every repaint, each provider must check if the accessibility tree changed, build event notifications, and send them via cross-process COM calls. This happens synchronously on the UI thread of each application, and the provider has already done the work even if the client filters most events."

Applied to our case: while the user's session was running, some process subscribed to global UIA events. That forced KV Studio into provider-overhead mode — its UI thread was busy with global event bookkeeping on every repaint. zpit-desktop-mcp's per-property cross-process calls (`CurrentName`, `CurrentBoundingRectangle`, `GetCurrentPatternAs`, etc.) all paid the inflated cost. `set_value`'s ~5 internal COM calls × 20 s amplified per-call latency = ~96 s; `get_ui_tree` at depth 4 with 160 nodes × ~500 ms per node = ~80 s. Numerically consistent.

Once the global-subscriber process closed or ended its subscription, the slowdown vanished — which is why reproduction failed under "identical" conditions.

### Known global-UIA-event subscriber candidates

If you see this slowdown reappear, scan for these processes (in rough order of how often they cause this):

- **`PAD.BridgeToUIAutomation2.exe`** (Microsoft Power Automate Desktop) — the documented worst offender
- `UiPath.*`, `AutomationAnywhere*`, `WinAppDriver` — RPA / automation suites
- `TestComplete`, `Ranorex`, `Inspect.exe`, `AccEvent.exe` — UI test / accessibility-inspection tools
- `Narrator`, `NVDA`, `JAWS` — screen readers (only when set to always-on)
- `ClickToDo`, `PowerToys.QuickAccess` — newer Windows features that scan UI for AI / quick-access workflows; intermittent
- `DeskIn`, TeamViewer, AnyDesk, remote-desktop helpers — often UIA-active for screen content awareness
- `MsMpEng` / `MpDefenderCoreService` — Microsoft Defender behavior-monitor can briefly hold UIA hooks during process introspection

The mere PRESENCE of these processes is not sufficient — the slowdown only occurs while they hold an active global event subscription. They can flip in and out of that state during normal operation.

### Diagnostic recipe

PowerShell one-liner to list processes that have loaded the UIA client DLL (a necessary, not sufficient, condition for being a global subscriber):

```powershell
Get-Process | Where-Object { $_.Modules.ModuleName -contains 'UIAutomationCore.dll' } |
  Select-Object Id, Name, @{N='Title';E={$_.MainWindowTitle}}, @{N='WS_MB';E={[math]::Round($_.WS/1MB,0)}} |
  Format-Table -AutoSize
```

For the definitive answer (per the gist's recommendation):

- **ETW trace with `Microsoft-Windows-UIAutomationCore` provider** — captures every UIA event in real time; the chatty processes show up as top talkers. Use `xperf -on ...` or the Windows Performance Recorder UI.
- **AccEvent (Windows SDK)** — visual real-time accessibility-event viewer. If you launch it and see floods of events from a specific app or with global subscribers attached, that's the perpetrator.

### Why `set_value` is still not being rewritten

This is an environmental issue triggered by an out-of-process subscriber, not a `ValuePattern.SetValue` bug. Rewriting `set_value` to use SendInput-style keystroke simulation would technically bypass the slow provider path, but:

- Same-IL stress testing (5 iterations, real path, matching session conditions) was rock-solid at 189–194 ms — `set_value` is fast under normal conditions, including against Common File Dialog
- The SendInput-equivalent for `set_value` is significantly more complex than for `press_button` — needs to focus the field, clear existing text, type the new value, handle non-ASCII characters, deal with autocomplete suggestions, etc. Each adds a failure surface
- The same global-subscriber slowdown would also affect `get_ui_tree`, `find_element`, and every other UIA read — those can't all be SendInput-replaced (no SendInput equivalent for "read the UI tree")

The proper mitigation is at the system level: identify and disable / quit the global-subscriber process. `agents/desktop.md` carries the fallback rule for agents (UIA write takes >5 s → fall back to click + key).

### Activate-app cold start: a smaller, related observation

The MCP-layer bench surfaced one repeatable pattern: the **first** `press_button` / `left_click` / `activate_window` after a long idle period (or after the target app sat in the background) measures ~5 s. The cost is in `ensureFocusV4` → `native.activateApp` → `AttachThreadInput` + `SetForegroundWindow`, which Windows throttles when a background app tries to steal focus (the well-known `LockSetForegroundWindow` / foreground-lock-timeout dance). Subsequent calls drop to <50 ms once the target is already in the foreground.

This is documented Win32 behavior, not a bug. It surfaces in the session at the same magnitude (5.19 s / 5.26 s before any modal interaction). If you ever need to optimize an interactive agent loop that wakes a background app frequently, the workaround is to keep the target activated; otherwise, accept the ~5 s tax on the first call.

### Related code / docs

- `zpit-desktop-mcp` fork: `native/src/accessibility.rs::perform_action`, `send_left_click_at` helper, commit `05d64ee` ([github.com/zac15987/computer-use-mcp](https://github.com/zac15987/computer-use-mcp))
- [IUIAutomationInvokePattern::Invoke - Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/api/uiautomationclient/nf-uiautomationclient-iuiautomationinvokepattern-invoke)
- [Implementing the Invoke Control Pattern - Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/winauto/uiauto-implementinginvoke)
- `agents/desktop.md` Rules section — agent-facing guidance on the residual `set_value` risk

---

## 9. Windows MAX_PATH overflow on nested child worktrees during `[P]` batches

**Affected OS:** Windows (path length is unbounded on Linux/macOS)
**Component:** `hooks/worktree-create.sh`, `internal/worktree/hooks.go` (`zpitIgnoreRules`)
**First observed:** 2026-05-28 (`ai-inspection-cleaning` issue #45, phase 6 playback)
**Status:** Mitigated — child worktrees now live at `$HOME/.zpit/children/<8-hex>` (flat); a Windows-only pre-check inside the hook fires `exit 2` with actionable instructions if the path still looks too deep.

### Symptom

The orchestrator hit a `[P]` batch (`[T1, T10]`), dispatched two `task-runner` subagents with `isolation: "worktree"`, and got back failures from `hooks/worktree-create.sh` → `git worktree add`. The orchestrator silently degraded to sequential execution with no clear hint why. Parent worktree path at the time of failure: `D:\Documents\.worktrees\ai-inspection-cleaning\45-…-twinclient-playback-api` (~89 chars). With the historical child layout (`<parent>/.zpit-children/<slug>` → ~130 chars before any file inside) plus git's internal `.git\worktrees\<leaf>\…` paths plus the deepest project file under the child, the leaf path crossed 260.

### Root cause

`git worktree add` opens a file under the *main repo's* `.git/worktrees/<leaf>/…` AND lays out the working tree at `<WT_PATH>/<repo-files>`. Both inherit the parent's depth in the historical layout. Once any of those crossed 260, `git worktree add` errored. The hook surfaced git's error to Claude Code, which (correctly) reported worktree creation failed — but the orchestrator's natural recovery path is to fall back to sequential, and the user had no signal that this was a fixable environmental issue rather than a "parallel batch isn't supported here" outcome.

### Fix

1. **Flatten the child path**: `hooks/worktree-create.sh` now computes `WT_PATH="$HOME/.zpit/children/$(sha256(cwd + slug)[:8])"`. On Windows this collapses the base to ~40 chars regardless of how deep the orchestrator's worktree sits. The branch name (`<parent-branch>-<slug>`) is unchanged and stays human-readable, so `cat <child>/.git` still reveals the parent identity for debugging.
2. **Pre-check**: on `MINGW*`/`CYGWIN*`/`MSYS*` shells with `git config core.longpaths != true`, the hook checks `${#WT_PATH} > 160` before `git worktree add` and `exit 2`s with instructions to enable `core.longpaths` and `HKLM\…\LongPathsEnabled`. With the flattened path this almost never fires, but if `$HOME` itself is unusually deep the user gets an explainable error.
3. **Removed `.zpit-children/` from `zpitIgnoreRules`** (`internal/worktree/hooks.go`) and from the repo's own `.gitignore` — children no longer live under the project, so the rule is dead weight.

### What this does NOT cover

- **Parent worktree path length**: the orchestrator's own worktree, created from `base_dir + dir_format` in `internal/worktree/manager.go`, is still un-budget-checked. If a user's `base_dir` + project + issue exceeds the budget, that's a separate config-layer problem (shorter `dir_format`, shorter `base_dir`, or `core.longpaths`).
- **Migration of existing `.zpit-children/` dirs** in user projects. They're now just regular untracked dirs; `rm -rf .zpit-children/` cleans them up.
- **Orphan `~/.zpit/children/<hash>` dirs** left by crashed hooks. Same slot-driven cleanup as before; if orphans accumulate in practice, a periodic sweeper can be added later.

### Related references

- §2 and §3 above describe the original `<parent>/.zpit-children/<slug>` design and its quirks — they remain accurate as historical context for the per-subagent worktree model.
- `internal/prompt/coding.go` did NOT need changes: `worktreePath` is opaque to the orchestrator prompt, branch discovery uses `git -C "$path" rev-parse --abbrev-ref HEAD`, and cleanup uses `git worktree remove --force "$path"` + `git branch -D <branch>`.

---

## 10. Windows git-bash: `2>nul` / `>NUL` creates a real reserved-name junk file in the repo

### Symptom

An agent suppressing output with the Windows CMD-style discard `command 2>nul` (or `>NUL`) does **not** discard anything — instead a literal file named `nul` appears in the working directory, shows up in `git status`, and cannot be deleted by Explorer or a plain `rm`. In `in_project` mode this dirty file makes the next loop dispatch fail its clean-tree precheck → `SlotNeedsHuman`. Claude Code has many upstream reports of this (one session reportedly produced 1,290 `NUL` files).

### Root Cause

Claude Code's Bash tool runs under **git-bash** on Windows. Unlike `cmd.exe`, git-bash does **not** treat `nul` as the null device — it interprets `> nul` as "redirect to a file literally named `nul`". Because `nul` is a Windows *reserved device name*, the resulting file resists deletion (needs a `\\?\` path prefix) and breaks tools like OneDrive sync.

The correct discard form on git-bash is `/dev/null` (`2>/dev/null`), which git-bash translates properly and never touches the filesystem.

A compounding factor: the old redirect-escape detection in `bash-firewall.sh` **blocked** `/dev/null` (it only allowed literal `/tmp`) while **allowing** `2>nul` (not a `/`-absolute path). So the firewall pushed agents away from the safe form toward the junk-file-producing one.

### Fix (2026-06-07)

`hooks/bash-firewall.sh` and `hooks/pwsh-firewall.sh` redirect detection rewritten as a per-target classifier (see `docs/architecture/09-safety.md` §9.4.4):

1. Redirect targets whose basename is `nul`/`NUL` (case-insensitive) are **blocked** with a message steering the agent to `/dev/null` (bash) / `$null` (PowerShell).
2. `/dev/null` and the OS temp roots are now **allowed**, so agents no longer need to route scratch/discard output into the repo.

Tests: `hooks/hooks_test.go` — `TestBashFirewall_BlocksNul*`, `TestBashFirewall_AllowsDevNull*`, `TestPwshFirewall_BlocksNul`, `TestPwshFirewall_AllowsNullDiscard*`.

### Cleanup for existing junk files

```powershell
Get-ChildItem -Path . -Filter 'NUL' -File -Recurse -Force | ForEach-Object {
    [System.IO.File]::Delete("\\?\$($_.FullName)")
}
```

### Related

- Upstream: anthropics/claude-code #23942, #15799 (NUL files on Windows git-bash).
