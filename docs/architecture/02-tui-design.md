# 2. TUI 介面設計

> **注意：以下 mockup 是設計藍圖，部分功能尚未實作。已標注各功能的實作狀態。**

---

## 2.1 主畫面 — 專案總覽 + 終端調度 ✅ 已實作

```
╔══════════════════════════════════════════════════════════════════════╗
║  Zpit v0.1                              03/18 14:32  WSL  ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  專案列表                          快捷鍵                          ║
║  ─────────────────────────────     ──────────────────────          ║
║                                                                    ║
║  ⚙️  ASE 檢測清潔機台  🟢 已部署   [Enter] 開 Claude Code (新終端) ║
║     WPF+硬體 │ 3 todo │ 1 進行中   [c] Clarify 需求               ║
║     🟢 Agent 運行中 (tmux:2)       [l] Loop 自動實作               ║
║                                    [r] Review 機台改動             ║
║  ⚙️  ChipMOS 點膠機台  🟡 部分部署 [f] 效率 Agent                  ║
║     WPF+硬體 │ 1 todo │ 0 進行中   [s] 狀態總覽                    ║
║                                    [o] 開啟專案資料夾              ║
║                                    [p] 開啟 Issue Tracker          ║
║  🌐  個人品牌網頁  ⚪ 未部署         [u] Undeploy 部署檔案           ║
║     Astro    │ 5 todo │ 0 進行中   [d] Redeploy 重新部署           ║
║                                    ──────────────────────          ║
║  🖥️  報警管理工具                   [x] 關閉終端                    ║
║     WPF      │ 0 todo │ 0 進行中   [a] 新增專案                    ║
║                                    [e] 編輯設定 (子選單)            ║
║                                    [?] 說明                       ║
║ ›📱  Android 監控 App  🟢 已部署    [q] 離開                       ║
║     Kotlin   │ 2 todo │ 0 進行中                                  ║
║                                                                    ║
╠══════════════════════════════════════════════════════════════════════╣
║  活躍終端 (Tab 切換至此區域，↑↓ 選擇，x 關閉終端)                  ║
║ ›[1] ASE 檢測  │ 🟢 實作中: EtherCAT reconnect backoff │ 02:15    ║
║      切換: tmux select-window -t ase-inspection                    ║
║  [2] 個人網頁  │ 🟢 AI Review 完成，等待你確認 PR                  ║
║      切換: tmux select-window -t personal-site                     ║
╠══════════════════════════════════════════════════════════════════════╣
║  最近活動  ⚠️ 尚未實作                                              ║
║  14:20  ASE 檢測  │ 修改 EtherCatService.cs (新增 RetryBackoff)   ║
║  14:18  ASE 檢測  │ 讀取 CLAUDE.md, AlarmManager.cs               ║
║  13:45  個人網頁  │ Loop 完成 3 issues │ 已部署                    ║
╚══════════════════════════════════════════════════════════════════════╝
```

操作方式：
- ↑↓ 選擇專案
- Enter：在新終端開啟 Claude Code（Windows Terminal 新 tab / tmux 新 window）
- 快捷鍵 [c][l][r][s]：同樣在新終端啟動對應的 agent
- [u]：移除 Zpit 部署到專案的 agents/docs/hooks 檔案
- [d]：清除現有部署後重新寫入 4 個 agents (clarifier/reviewer/task-runner/efficiency) + hooks + docs，**不啟動 Claude**（confirm 後執行）
- 專案名稱旁的狀態標記：🟢 已部署（全部 10 個檔案齊全）、🟡 部分部署（部分檔案缺失或只部署過單一 agent）、⚪ 未部署
- [f]：啟動效率 Agent（輕量模式，無 hooks、無 tracker、self-review）
- Tab：三面板循環切換 — 專案列表 → 活躍終端（有終端時）→ Loop 狀態（有 slot 時）→ 專案列表
- [x]：當焦點在活躍終端時，關閉選中的終端（force kill process，需確認）
- 「活躍終端」區域：即時顯示正在運行的 Claude Code session 狀態，支援 cursor 選擇和 x 鍵關閉
  （資料來源：Claude Code session log + JSONL 解析）
- 「最近活動」區域：**尚未實作** — 設計上從 session log 解析 agent 的具體操作

---

## 2.2 Clarify 模式 — 需求釐清對話（在新終端中） ✅ 已實作

按 [c] 後，TUI 在新終端開啟 Claude Code + clarifier agent。
TUI 本身透過 session log 即時顯示進度摘要。

> **注意：以下 mockup 呈現的是新終端視窗中的畫面，不是 TUI 內部的 view。**
> 你在這個終端裡直接跟 Claude Code 對話，TUI 主畫面只會在「活躍終端」區域顯示摘要狀態。

**新終端中（你直接跟 Claude Code 對話）：**

```
╔══════════════════════════════════════════════════════════════════════╗
║  Clarify │ ASE 檢測清潔機台                          [Esc] 返回    ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  你: EtherCAT 斷線重連的邏輯怪怪的                                 ║
║                                                                    ║
║  ┌─ Clarifier Agent ──────────────────────────────────────────┐    ║
║  │ 我找到了 EtherCatService.cs 中的 ReconnectAsync 方法。     │    ║
║  │ ...（需求釐清對話）                                        │    ║
║  │                                                            │    ║
║  │ ┌─ Issue Preview ────────────────────────────────────┐     │    ║
║  │ │ 標題: EtherCAT 斷線重連加入 retry backoff 機制     │     │    ║
║  │ │                                                    │     │    ║
║  │ │ ## CONTEXT                                         │     │    ║
║  │ │ ## APPROACH                                        │     │    ║
║  │ │ ## ACCEPTANCE_CRITERIA                              │     │    ║
║  │ │ ## SCOPE                                           │     │    ║
║  │ │ ## CONSTRAINTS                                     │     │    ║
║  │ │ ## BRANCH (可選)                                   │     │    ║
║  │ │ ## TASKS (可選)                                    │     │    ║
║  │ └────────────────────────────────────────────────────┘     │    ║
║  │                                                            │    ║
║  │ 確認推上 Tracker 嗎？ [y] 確認  [e] 再修改  [n] 取消       │    ║
║  └────────────────────────────────────────────────────────────┘    ║
║                                                                    ║
║  你: y                                                             ║
║                                                                    ║
║  ✓ Issue 已建立 → #ASE-47                                         ║
║  ✓ 標記為 pending，等待你在 TUI [s] 畫面按 [y] 確認               ║
║                                                                    ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## 2.3 Loop 狀態監控（TUI 主畫面即時更新） ✅ 已實作

按 [l] 後，TUI 啟動自動化 loop。Loop 在 TUI goroutine 中運行（非獨立子命令），
透過 tracker API poll + label 偵測驅動狀態機。

```
╔══════════════════════════════════════════════════════════════════════╗
║  Loop Monitor │ ASE 檢測清潔機台                     [Esc] 返回    ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  目前執行: ASE-47 EtherCAT 斷線重連加入 retry backoff              ║
║  ─────────────────────────────────────────────────────────         ║
║                                                                    ║
║  Pipeline 狀態:                                                    ║
║  ┌─────────────────────────────────────────────────────────┐       ║
║  │ ✓ building worktree                              00:03  │       ║
║  │ ✓ preparing agent                                00:01  │       ║
║  │ ✓ launching coder                                00:01  │       ║
║  │ ▸ coding                                         02:15  │       ║
║  │ ○ launching reviewer                                    │       ║
║  │ ○ reviewing                                             │       ║
║  │ ○ waiting PR merge                                      │       ║
║  │ ○ cleaning up                                           │       ║
║  └─────────────────────────────────────────────────────────┘       ║
║                                                                    ║
║  佇列中:                                                           ║
║  ASE-48  Vision 校正流程加入 timeout    Todo                       ║
║  ASE-49  清潔頭 Z 軸回原點順序修正     Todo                       ║
║                                                                    ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## 2.4 Status 總覽 ✅ 已實作

按 [s] 顯示該專案在 Issue Tracker 上的所有 issue 狀態：

```
╔══════════════════════════════════════════════════════════════════════╗
║  Status │ ASE 檢測清潔機台                           [Esc] 返回    ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  Todo (3)                                                          ║
║    ASE-48  Vision 校正流程加入 timeout              High           ║
║    ASE-49  清潔頭 Z 軸回原點順序修正                Med            ║
║    ASE-50  新增膠量檢測 NG 統計報表                 Low            ║
║                                                                    ║
║  In Progress (1)                                                   ║
║    ASE-47  EtherCAT 斷線重連 backoff     ▸ AI 實作中  02:15       ║
║                                                                    ║
║  AI Review (0)                                                     ║
║    (無)                                                            ║
║                                                                    ║
║  等待你 Review (1)                                                 ║
║    ASE-45  Alarm 管理重構為 Strategy      PR #42 待 review         ║
║                                                                    ║
║  待機台驗證 (1)                                                    ║
║    ASE-44  Motion 軸 homing 順序修正      已 merge，待機台測試     ║
║                                                                    ║
║  操作: [y] 確認 issue → Todo  [p] 在瀏覽器開啟 issue               ║
║                                                                    ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## 2.5 Edit Config 子選單 ✅ 已實作

按 [e] 進入設定編輯子選單：

```
╔══════════════════════════════════════════════════════════════════════╗
║  編輯設定 — Zpit                                     [Esc] 返回    ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  [1] 切換 Channel                                                  ║
║  [2] 編輯 channel_listen                                           ║
║  [3] 用編輯器開啟設定檔                                             ║
║                                                                    ║
║  channel_enabled = true                                            ║
║  channel_listen = [_global]                                        ║
║                                                                    ║
╠══════════════════════════════════════════════════════════════════════╣
║  [1/2/3] 選擇  [Esc] 返回                                         ║
╚══════════════════════════════════════════════════════════════════════╝
```

操作方式：
- [1]：即時切換 `channel_enabled` on/off，自動更新 config.toml 並管理 broker 訂閱
- [2]：進入多選清單，列出所有其他專案 + `_global`，空白鍵 toggle，Enter 確認
- [3]：本機模式使用 `$EDITOR` 開啟 config.toml，關閉後自動 reload；SSH 遠端模式顯示檔案路徑
- [r]：手動觸發 config reload（適用於 SSH 遠端或外部編輯後）

---

## 2.6 Git Status 頁面 ✅ 已實作

按 [g] 進入，針對當前選取的 project 顯示 branch 資訊與提交圖，
支援 [f] fetch / [p] pull，不需離開 Zpit 切到系統終端機即可同步遠端變更。

```
╔══════════════════════════════════════════════════════════════════════╗
║  Git Status │ ASE 檢測清潔機台                       [Esc] 返回    ║
║  Branch: dev                                                       ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  Local Branches                                                    ║
║  ─────────────────────────────────────────────────────             ║
║  * dev                          ↑0 ↓2  origin/dev                  ║
║    feat/47-ethercat-backoff     ↑3 ↓0  origin/feat/47-ethercat-…   ║
║    main                         ↑0 ↓0  origin/main                 ║
║                                                                    ║
║  Remote-only Branches                                              ║
║  ─────────────────────────────────────────────────────             ║
║    origin/feat/50-ng-stats                                         ║
║    origin/feat/51-alarm-refactor                                   ║
║                                                                    ║
║  Commit Graph                                                      ║
║  ─────────────────────────────────────────────────────             ║
║  * a1b2c3f (HEAD -> dev, origin/dev) fix: alarm retry              ║
║  * d4e5f6a add: NG stats report                                    ║
║  |\                                                                ║
║  | * 7f8e9d0 (origin/feat/47-ethercat-backoff) wip: backoff        ║
║  |/                                                                ║
║  * 0c1d2e3 (origin/main, main) release v0.4                       ║
║                                                                    ║
╠══════════════════════════════════════════════════════════════════════╣
║  [f] Fetch  [p] Pull  [r] Refresh  [Esc] Back                     ║
╚══════════════════════════════════════════════════════════════════════╝
```

操作方式：
- `[f]`：執行 `git fetch --all --prune`（30 秒 timeout）
- `[p]`：執行 `git pull --ff-only`（30 秒 timeout）
- `[r]`：重新載入 branches 與 commit graph
- `[Esc]`：返回 ViewProjects
- `↑↓` / `PgUp PgDn`：捲動 commit graph

設計決策 / 註記：
- **為何 `--ff-only`**：符合 `main ← dev ← feature` branching model，分歧時安全失敗而非默默 merge，
  避免在 TUI 內產生難以處理的合併衝突。
- **為何 `--all --prune`**：手機上 merge PR 後 GitHub 會 auto-delete branch，
  prune 清除 stale remote refs，保持 branch list 乾淨。
- **為何 shell out git 而非 go-git**：自寫 graph 渲染 ~600 LoC 為過度工程；
  git 原生 `--graph --oneline` 輸出已含 ANSI 色碼，viewport 直接支援顯示。
- **並發模型**：fetch/pull 為非阻塞 `tea.Cmd`，status bar 顯示 `{spinner} fetching...`；
  操作進行中重複按鍵 ignored（`gitOpRunning` flag）；操作成功後自動重刷 branches + graph。

相關檔案：
- `internal/git/ops.go` — git exec 封裝（FetchAll / PullFF / Branches / Graph）與輸出 parser
- `internal/tui/gitstatus.go` — message handlers + tea.Cmd（GitStatusMsg / GitOpDoneMsg）
- `internal/tui/view_gitstatus.go` — render 函式（branch table + graph viewport）
