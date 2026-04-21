# Known Issues

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
5. Agent tool returns `{worktreePath, worktreeBranch}` per teammate; orchestrator post-batch emits `git cherry-pick <branch1> <branch2> ...` in task-ID order, then `git worktree remove` + `git branch -D`. Cherry-pick conflicts (spec bug: two `[P]` tasks share a file) surface as `cherry-pick --abort` → NeedsHuman instead of silent reverts.

**Supporting changes:**
- `hooks/path-guard.sh` — `ALLOWED_DIR` now uses `git rev-parse --show-toplevel` so it self-adapts to whichever worktree the teammate is running inside. Claude Code pins `CLAUDE_PROJECT_DIR` to the orchestrator's project root (not the worktree path — see `claude-code-source-code/src/utils/hooks.ts:813,884`), so the old behavior would have made the check too permissive inside child worktrees.
- `internal/worktree/hooks.go` — `WorktreeCreate` hook registered in all three `hookModeTemplates`; `.zpit-children/` added to `zpitIgnoreRules`.
- `internal/prompt/prompt_test.go` — `TestBuildCodingPrompt_ParallelBatchResync` deleted; `TestBuildCodingPrompt_WithParallelTasks` rewritten to assert the new worktree-isolation strings; new `TestBuildCodingPrompt_ParallelBatchIntegration` asserts every `[P]` group emits its own cherry-pick + cleanup block.
- `agents/task-runner.md` — entire Parallel Commit Protocol section removed; frontmatter unchanged (Claude Code source confirms `isolation` is a runtime tool parameter, not a frontmatter key — `claude-code-source-code/src/tools/AgentTool/AgentTool.tsx:99`).

The three incident fixes above stay in this document as history — useful if we ever need to diagnose a symptom that resembles shared-worktree index bleed in another codebase.
