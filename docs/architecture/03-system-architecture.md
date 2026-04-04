# 3. 系統架構（調度模式）

> **運行模式：**
> - `zpit` — 本機 TUI（直接操作）
> - `zpit serve` — 無頭 SSH daemon（Wish），多個 SSH 客戶端共享同一 AppState
> - `zpit connect` — SSH 連線便利包裝

```
┌─────────────────────────────────────────────────────────────────────┐
│                     你的電腦 (Windows / WSL)                        │
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │  zpit (Go binary, Bubble Tea TUI)     ← 常駐運行             │  │
│  │                                                               │  │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐   │  │
│  │  │ 專案選擇 │  │ 狀態總覽 │  │ Loop 監控 │  │ Session    │   │  │
│  │  │          │  │          │  │          │  │ Log Watcher│   │  │
│  │  └────┬─────┘  └────┬─────┘  └────┬─────┘  └─────┬──────┘   │  │
│  │       │             │             │               │           │  │
│  │       ▼             ▼             ▼               ▼           │  │
│  │  ┌─────────────────────────────────────────────────────────┐  │  │
│  │  │                    Core Engine                          │  │  │
│  │  │  ┌───────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ │  │  │
│  │  │  │ProjectMgr │ │ Tracker  │ │ Terminal │ │ LogTail  │ │  │  │
│  │  │  │(toml)     │ │ Provider │ │ Launcher │ │ (fsnotify│ │  │  │
│  │  │  │           │ │ (抽象層) │ │ (wt/tmux)│ │  + parse)│ │  │  │
│  │  │  │           │ │ ┌──────┐ │ │          │ │          │ │  │  │
│  │  │  │           │ │ │Forgej│ │ │          │ │          │ │  │  │
│  │  │  │           │ │ │GitHub│ │ │          │ │          │ │  │  │
│  │  │  │           │ │ └──────┘ │ │          │ │          │ │  │  │
│  │  │  └───────────┘ └──────────┘ └────┬─────┘ └────┬─────┘ │  │  │
│  │  └──────────────────────────────────┼────────────┼────────┘  │  │
│  └─────────────────────────────────────┼────────────┼───────────┘  │
│                                        │            │               │
│       ┌────────────────────────────────┘            │               │
│       ▼                                             │               │
│  ┌─────────────────────────────────────┐            │               │
│  │  獨立終端視窗 (你可隨時切過去)       │            │               │
│  │                                     │            │               │
│  │  ┌─ WT Tab 1 ───────────────────┐  │            │               │
│  │  │ claude (ASE 檢測機台)         │  │            │               │
│  │  │ > 正在修改 EtherCatService... │──┼──writes──▸ │               │
│  │  └───────────────────────────────┘  │  session   │               │
│  │  ┌─ WT Tab 2 ───────────────────┐  │  log       │               │
│  │  │ claude (個人網頁)             │──┼──writes──▸ │               │
│  │  └───────────────────────────────┘  │            │               │
│  └─────────────────────────────────────┘            │               │
│                                                     │               │
│  Claude Code session logs ◂─────────────────────────┘               │
│  ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl                │
│                                                                     │
│  <encoded-cwd> = 專案絕對路徑編碼（非英數字元替換為 -）             │
│  例: D:\Documents\MyProjects\zpit → D--Documents-MyProjects-zpit    │
│  Session log 直接存在此目錄下，不在 sessions/ 子目錄                 │
│                                                                     │
│  每個專案的 repo:                                                    │
│  ├── .claude/agents/clarifier.md                                    │
│  ├── .claude/agents/reviewer.md                                     │
│  ├── .claude/agents/task-runner.md  (deployed when TASKS exist)     │
│  ├── .claude/docs/agent-guidelines.md                               │
│  ├── .claude/docs/code-construction-principles.md                   │
│  ├── .claude/docs/tracker.md                                        │
│  └── CLAUDE.md                                                      │
│                                                                     │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                        ┌────────┴────────┐
                        │  Synology NAS   │
                        │  (或雲端服務)    │
                        │  ┌────────────┐ │
                        │  │ Issue      │ │
                        │  │ Tracker    │ │
                        │  │ (可抽換)   │ │
                        │  ├────────────┤ │
                        │  │ Git Host   │ │
                        │  │ (可抽換)   │ │
                        │  └────────────┘ │
                        └─────────────────┘
```

---

## 3.1 Terminal Launcher 模組

負責在正確的環境中開啟新的終端視窗並啟動 Claude Code。
行為由 config.toml 的 `[terminal]` 區塊控制。
實作位於 `internal/terminal/`。

```go
// 虛擬碼
func LaunchClaude(project Project, config Config) {
    path := project.PathForCurrentOS()

    switch detectEnvironment() {
    case WindowsTerminal:
        switch config.Terminal.WindowsMode {
        case "new_tab":
            // 預設：在 Windows Terminal 開新分頁
            exec("wt.exe", "new-tab", "-d", path, "--title", project.Name, "--", "claude")
        case "new_window":
            exec("wt.exe", "-w", "new", "-d", path, "--title", project.Name, "--", "claude")
        }

    case WSL_Tmux, Linux_Tmux:
        windowName := project.ID
        switch config.Terminal.TmuxMode {
        case "new_window":
            exec("tmux", "new-window", "-n", windowName, "-c", path, "claude")
        case "new_pane":
            exec("tmux", "split-window", "-h", "-c", path, "claude")
        }
    }
}
```

---

## 3.2 Session Log Watcher 模組

Claude Code 的每次 session 會產生 JSONL log 檔。TUI 透過監控這些檔案
即時更新狀態，不需要跟 Claude Code 直接通訊。
實作位於 `internal/watcher/`。

**路徑編碼規則：** 專案絕對路徑中所有非英數字元替換為 `-`，作為目錄名。
例如 `D:\Documents\MyProjects\zpit` → `~/.claude/projects/D--Documents-MyProjects-zpit/`

**Session 發現機制：**
- 活躍 session：讀取 `~/.claude/sessions/{pid}.json`，內含 `pid`、`sessionId`、`cwd`、`startedAt`
- **PID + 程序名稱雙重驗證**：除了檢查 PID 是否仍在運行，還驗證程序名稱是否為 Claude Code（`claude`/`node`），防止 PID 重用誤判
- Log 檔位置：`~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`
- **啟動時自動掃描**：TUI 啟動時對所有 project 呼叫 `FindActiveSessions()`，自動偵測已在跑的 Claude Code session 並 attach watcher。重啟 zpit 不會遺失正在運行的 session
- **`/resume` session 切換偵測**：使用者在 Claude Code 中執行 `/resume` 時，PID 不變但 `sessionId` 會更新。Zpit 在兩個階段偵測此變化：
  (1) `waitForLogCmd` 等待 JSONL 期間，每次 retry 重讀 `{pid}.json`，發現 sessionId 變更即回傳 `sessionFoundMsg` 重新開始
  (2) `checkSessionLiveness` 每 5 秒對已 attach 的 watcher 重讀 `{pid}.json`，發現變更即停止舊 watcher 並啟動新的

**JSONL 事件格式：** 每行一個 JSON 物件，`type` 欄位區分事件類型：

| type | 說明 |
|------|------|
| `user` | 使用者訊息，或 `tool_result` 回傳（含 `toolUseResult` metadata） |
| `assistant` | 模型回應，`message.content[]` 含 `thinking`/`text`/`tool_use` block |
| `system` | 系統事件：`turn_duration`（回合結束）、`compact_boundary`（壓縮） |
| `progress` | hook/sub-agent/web-search 進度 |
| `last-prompt` | Session 閒置時的最後一則提示 |

**Agent 狀態判斷：** 讀取最後一筆 `type: "assistant"` 事件的 `message.stop_reason`：
- `"end_turn"` → 等待使用者輸入（觸發通知）
- `"tool_use"` → 工作中（呼叫工具）
- `null` → 串流中（尚未完成）

```go
// 虛擬碼
func WatchSessionLog(project Project) <-chan AgentEvent {
    events := make(chan AgentEvent)
    go func() {
        encodedCwd := encodeCwd(project.Path)
        projectDir := filepath.Join(claudeHome, "projects", encodedCwd)
        sessionID := findActiveSession(project.Path)
        logPath := filepath.Join(projectDir, sessionID+".jsonl")

        watcher := fsnotify.NewWatcher()
        watcher.Add(logPath)

        for event := range watcher.Events {
            if event.Op == fsnotify.Write {
                newLines := readNewLines(logPath)
                for _, line := range newLines {
                    events <- parseSessionLog(line)
                }
            }
        }
    }()
    return events
}
```
