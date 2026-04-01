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

Cleanup 失敗後 `CloseIssue` 被跳過，issue 未被 Zpit 關閉（但若 PR body 含 `Closes #N` 則 GitHub auto-close 仍會生效）。

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
2. **Deferred cleanup queue** — cleanup 失敗時將 worktree path 加入 retry queue，下一輪 poll 時重試，而非直接回傳 error 阻斷 `CloseIssue`。
3. **Separate CloseIssue from Remove** — 即使 `Remove()` 失敗，仍然執行 `CloseIssue`，worktree 清除降級為 best-effort。

### Related

- Issue #36: `git branch -d` → `-D` fix（已合併，與此問題無關但在同次 cleanup 中觸發）
- `internal/worktree/manager.go` line 106-113: `removeDirRetry` fallback 邏輯
- `internal/tui/loop_cmds.go` `loopCleanupCmd`: cleanup 失敗時跳過 `CloseIssue`
