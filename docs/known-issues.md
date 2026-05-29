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

Worktree 目錄因 Windows file lock 無法刪除，殘留在磁碟上。

> **已修正（部分）：** `CloseIssue` 不再因 cleanup 失敗而被跳過。`loopCleanupCmd` 現在無論 `Remove()` 結果如何都會執行 `CloseIssue`（Fix Direction #3 已實作）。但 worktree 目錄的 Permission denied 問題在 Windows 上仍存在。

### Root Cause

Windows 不允許刪除正在被任何 process 當作 working directory (CWD) 的目錄。

Loop engine 的 cleanup 時序：

1. Loop 啟動 reviewer agent → Claude Code process 的 CWD 設在 worktree 目錄
2. Reviewer 完成工作、設定 `ai-review` label → Loop 偵測到 PR merged
3. Loop 呼叫 `Remove()` 嘗試刪除 worktree 目錄
4. **此時 reviewer 的 Claude Code session 可能尚未完全結束**（process 仍佔用 CWD）
5. `git worktree remove --force` → Permission denied
6. Fallback `removeDirRetry` → 同樣被 Windows file lock 擋住

Linux / macOS 允許刪除被佔用為 CWD 的目錄，因此此問題僅出現在 Windows。

### Workaround

等佔用 worktree 目錄的 process 結束後，手動清除：

```bash
git -C <repo-path> worktree remove --force <worktree-path>
```

或重新啟動 Loop，讓它在下一輪 resume 時 retry cleanup。

### Potential Fix Directions

1. **Wait for agent process exit before cleanup** — 在 `loopCleanupCmd` 中，確認 agent session PID 已結束後再呼叫 `Remove()`。需注意 PID reuse 的 race condition。
2. **Deferred cleanup queue** — cleanup 失敗時將 worktree path 加入 retry queue，下一輪 poll 時重試。
3. ~~**Separate CloseIssue from Remove**~~ — **已實作。** `loopCleanupCmd` 現在無論 `Remove()` 結果如何都會執行 `CloseIssue`，worktree 清除降級為 best-effort。

### Related

- Issue #36: `git branch -d` → `-D` fix（已合併，與此問題無關但在同次 cleanup 中觸發）
- `internal/worktree/manager.go`: `removeDirRetry` fallback 邏輯
- `internal/tui/loop_cmds.go` `loopCleanupCmd`: cleanup 失敗時仍執行 `CloseIssue`

---

## 2. Shared-worktree git index race during `[P]` parallel task batch

**Affected OS:** all platforms
**Component:** `agents/task-runner.md`, `internal/prompt/coding.go` (`buildTeamDelegation`)
**First observed:** 2026-04-18 (Issue #9, coding session `b51aef45-6d51-48d3-926f-f1ba50ddcd7f`)
**Status:** ✅ 已修復（v2, 2026-04-20）— 初版修復在 linked worktree 下路徑解析錯誤，見下方 Fix v2。

### Symptom

當 coding orchestrator 把 `[P]` batch 分派給 Agent Team（T1~T9 同時跑）時，teammate 共用同一 worktree 的 `.git/index`，幾次 commit 內容互相錯置——某個 T{N} commit 裡混進了其他 teammate 的檔案。當場的 task-runner subagent 以 `git commit -- <pathspec>` 自行修正個別錯誤 commit，並在報告中留言：

> T1~T9 並行 subagent 時，shared worktree 的 git index 競爭造成幾次 commit 內容錯置，subagent 均以 `git commit -- pathspec` scope 自行修正。未來大量並行任務建議分批或改為序列。

### Root Cause

兩層 race 同時存在：

1. **Index race** — 多個 teammate 的 `git add` 同時寫入 `.git/index`，staging 內容互相覆蓋。`git commit` 隨後讀到的 tree 就是混合 staging 的結果。
2. **Ref race** — 多個 `git commit` 同時更新 `refs/heads/<branch>`，`refs/heads/<branch>.lock` 競爭造成其中一個失敗（intermittent "cannot lock ref"）。

Claude Code 的 Agent Team 不讓 orchestrator 給 teammate 設不同 cwd，所以「每個 teammate 開獨立 worktree」在現行模型下做不到；必須用 git 原生的 index 隔離機制處理。

### Fix

在 orchestrator prompt 與 `task-runner` subagent doc 中加入 **Parallel Commit Protocol**（三層防禦，僅 `[P]` teammate 適用；循序 task 獨佔 index 不走此流程）：

1. **Index 隔離** — `export GIT_INDEX_FILE=.git/index.zpit.T{N}`，每個 teammate 用獨立 staging index
2. **Pathspec 安全網** — `git add -- <declared files only>`（即使 Layer 1 漏掉，pathspec 仍能避免跨 task 汙染）
3. **Commit serialize** — `mkdir .git/zpit-commit.lock`（atomic、跨平台）取得鎖、重試最多 5 次 jittered sleep、commit 完 `rmdir` 釋放

觸發條件：orchestrator 在每個 teammate 的 spawn prompt 注入 `parallel_task_id: T{N}` 一行；`task-runner.md` 的 Parallel Commit Protocol 段落以該行為啟動訊號。

### Code Locations

- `agents/task-runner.md` §Parallel Commit Protocol — teammate 側完整步驟與命令
- `internal/prompt/coding.go` `buildTeamDelegation` — orchestrator 注入 `parallel_task_id` 與協定摘要
- `docs/agent-guidelines.md` §Git Operations — 交叉引用
- `internal/prompt/prompt_test.go` — `TestBuildCodingPrompt_WithParallelTasks` / `WithTasks` 雙向斷言（sequential prompt 不得洩漏協定字串）

### Fix v2（2026-04-20，Issue #11 session `14b85919-…`）

**Regression：** 初版修復把協定指令寫成字面路徑 `.git/index.zpit.T{N}` 與 `.git/zpit-commit.lock`。但 zpit 正常發射目標是 linked worktree，裡面的 `.git` 是一個**指向 `<main-repo>/.git/worktrees/<name>` 的 pointer file，不是 directory**。實戰中 33 個 task-runner subagent 全數命中 `fatal: Unable to create '.git/index.zpit.T1.lock': No such file or directory`，改採自救路徑（`GIT_DIR=$(git rev-parse --git-dir)`）時又因為初版協定從未指示「用 `git read-tree HEAD` seed 私有 index」，commit 出現 54 檔 / 2593 deletions 之類的大規模誤刪（T1 `b27d210`、T5 `8ff9ef8` 等）。

**Fix v2：** 改寫 `agents/task-runner.md` §Parallel Commit Protocol 與 `internal/prompt/coding.go` `buildTeamDelegation` 的 summary：

1. 路徑改以 `git rev-parse --git-dir`（per-worktree index）與 `git rev-parse --git-common-dir`（跨 worktree lock）解析，**不再 hard-code `.git/...`**。
2. 新增 `GIT_INDEX_FILE="$IDX" git read-tree HEAD` 步驟，seed 私有 index 讓 commit 不會把未 stage 的檔案全部記為刪除。
3. 明確要求整段序列跑在**同一個 Bash tool 呼叫**裡（Claude Code 每個 Bash tool call 都是全新 shell，`export` 不跨呼叫生效）；所有 git 指令以 `GIT_INDEX_FILE="$IDX"` 前綴 inline。
4. 失敗路徑也做 `rm -f "$IDX"`，避免殘留。

**參考：** StackOverflow / git-scm docs 確認 `git commit` 不會對 `refs/heads/*.lock` 自動 retry（VS Code #47141、Graphite blog），因此 mkdir lock + jittered retry 仍是必要的；`pre-commit` #2295 警示 `GIT_INDEX_FILE` 在 linked worktree 下的路徑誤解。

### Fix v3（2026-04-21，Issue #13 session `3192ffd3-…` / `12e4f992-…` / `3359f5f1-…` / `b6f7633d-…`）

**Regression：** Fix v2 解決了「parallel teammate 的 isolated index 必須從 HEAD seed」，但沒照顧到它的鏡像問題——**當 parallel batch 全部跑完，shared worktree 的 main index（`$GIT_DIR/index`）從沒被更新，仍停在 batch 開始前那棵 tree。** 後續任何 sequential task 走正常流程 `git add -- <files> && git commit` 時，commit 所依據的 tree 是「舊 main index」＋「剛 stage 的小幅修改」，等於悄悄把整個 batch 的工作撤回。

Issue #13 現場：T1–T9 以 v2 協定並行 commit 成功推進 HEAD，T10（sequential、修改 `CLAUDE.md`）照指示執行 `git add -- CLAUDE.md && git commit`，結果 commit `a4a7f9b` 觸碰 10 個檔案：
- `CLAUDE.md`：+22 / -0 ✅（預期）
- `src/hooks/useIsMobile.ts`：status: removed, -32 ❌（T1 新檔被「刪除」）
- 其餘 8 個 mobile 元件：全被還原回 pre-T1 狀態 ❌

下一個 commit `fe3a799`（人工 restore）才把遺失的改動搶救回來。PR #14 因此在 git 歷史中出現「加入 → 刪除 → 復原」的 U 字型怪異 diff。

Root Cause：
1. `GIT_INDEX_FILE=$IDX git commit` 只更新私有 index 與 HEAD，不會反寫到 `$GIT_DIR/index`。
2. Main index 的唯一同步點原本應該是每個 teammate 做完後由某處 `git read-tree HEAD` 重新裝載——但 v2 protocol 沒寫這一步，teammate 各自 `rm -f "$IDX"` 就結束了。
3. 下游 sequential task 拿到 stale main index + 自己的新檔，commit 出來的 tree = 舊世界 + 新檔，其他檔案被當成「被刪除」。

**Fix v3（orchestrator-side resync）：** 在 `internal/prompt/coding.go` `buildTaskWorkflow` 的 Task Execution Order 區塊，**每個 parallel group 結束後** 都印出一行指示：「Before any later `git add` / `git commit` against the main index, run `git read-tree HEAD` in the worktree root」。由 orchestrator 在 batch 結束後呼叫一次，確保後續 sequential task 或 orchestrator 自己的 final-adjustment commit 都建立在正確 baseline 上。

為什麼不放在 teammate 協定裡？**並行寫 `$GIT_DIR/index` 本身就會 race**——9 個 teammate 同時 `git read-tree HEAD`，其中 8 個會撞到 `index.lock: File exists`，反而需要再加一層 mkdir-lock。Orchestrator 是唯一知道「batch 已完成」的角色，resync 一次、無鎖、清楚。

**Fix v3 code changes：**
- `internal/prompt/coding.go` `buildTaskWorkflow` — parallel group 的 Task Execution Order 輸出附加 resync 指示
- `agents/task-runner.md` 新增 §What NOT to do：明確禁止 teammate 自己去 resync main index
- `internal/prompt/prompt_test.go` — 新增 `TestBuildCodingPrompt_ParallelBatchResync`（每個 `[P]` group 都要觸發 resync 指示）、更新 `WithTasks` / `WithParallelTasks` 的斷言（sequential-only prompt 不得洩漏 "Resync main index" 字串；parallel prompt 必須包含且順序在 Parallel group 指示之後）
- `CLAUDE.md` §Task Execution Model Parallel Commit Protocol 段落補上 orchestrator-side resync 的說明

**參考：** `git-read-tree` docs（plain 模式只替換 index、不動 worktree）、`git-reset` docs（`--mixed` 有 ORIG_HEAD + reflog 副作用，選 `read-tree` 更乾淨）、`pluralsight` / Microsoft Learn on `index.lock`（支撐「不要並行 resync」的決策）。

### 未來風險

- 若未來 `[P]` batch 普遍超過 5 個或 `[P]` 規則頻繁被違反（真的 touch 同檔案），本協定擋不住語意層的衝突，需升級為「每個 `[P]` task 一個 worktree」；或改採 plumbing `commit-tree` + `update-ref` CAS retry 完全繞開 index/ref lock。當時的討論留在 plan `C:\Users\Jeff\.claude\plans\1-2-vast-lark.md` §Design 開頭。
- **Fix v3 是第三次在 shared-worktree 模型上打補丁。** 產業慣例（Cursor, Claude Code docs, Augment, spec-kit）皆走「per-teammate worktree」。若再出現一次（v4 規模）incident，正確回應不是 v4 補丁，而是切換到 per-teammate worktree 架構——現有 `internal/worktree/` 已經提供 worktree 生命週期管理，延伸到 batch-ephemeral worktree 的工程成本可控，勝於持續堆疊 shared-worktree 協定複雜度。

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

Empirical measurements against KV Studio's "打開專案(Ctrl+O)" toolbar button (PID 22208, same Integrity Level as MCP, no cross-IL slowdown):

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
