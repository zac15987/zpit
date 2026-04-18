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
**Status:** ✅ 已修復

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

### 未來風險

- 若未來 `[P]` batch 普遍超過 5 個或 `[P]` 規則頻繁被違反（真的 touch 同檔案），本協定擋不住語意層的衝突，需升級為「每個 `[P]` task 一個 worktree」。當時的討論留在 plan `C:\Users\Jeff\.claude\plans\1-2-vast-lark.md` §Design 開頭。
