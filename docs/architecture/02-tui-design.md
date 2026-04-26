# 2. TUI 介面設計

> **注意：以下 mockup 是設計藍圖，部分功能尚未實作。已標注各功能的實作狀態。**

---

## 2.1 主畫面 — Dock 版面（Catppuccin Mocha + 獨立捲動的四面板）✅ 已實作

主畫面採用 lazygit 風格的 dock 版面：左欄由上到下堆疊 Projects / Active Terminals / Loop Engine，
右欄固定 Hotkeys。四個面板各自擁有獨立的 `viewport.Model`，互不干擾地上下捲動；
Catppuccin Mocha 色盤，無邊框、單欄寬的 `▎` mauve accent bar 只出現在當下 focused 面板的標題列。

```
 Zpit v0.1                                            04/19 15:04  Windows Terminal


▎ 專案  7                                             快捷鍵
  ──────                                              ──────
  › AI Inspection Cleaning Demo  ⚪ 未部署            [Enter] 啟動 Claude Code
     machine │ wpf, ethercat, basler                  [c] 釐清需求
                                                      [l] Loop 自動實作
    ENR DUC  ⚪ 未部署                                [r] Review 變更
     machine │ wpf, secsgem                           [f] 效率 Agent
                                                      [s] 狀態總覽
    DisplayProfileManager  ⚪ 未部署                  [o] 開啟專案資料夾
     desktop │ wpf, nlog                              [i] 開啟 Issue Tracker
                                                      [p] 開啟 PR
    Zpit  🟢 已部署                                   [u] 清除部署檔案
     terminal │ go, bubbletea                         [d] 重新部署所有 agent
                                                      [m] Channel 通訊
    Zplex  ⚪ 未部署                                   [g] 檢視 Git 狀態
     desktop │ go, electron, xterm                    [G] 開啟 lazygit
                                                      [U] 執行 claude update
    Zacfuse  🟢 已部署                                [a] 新增專案
     web │ astro, typescript, docs                    [e] 編輯設定
                                                      [x] 關閉終端
                                                      [Tab] 切換面板
  執行中終端  1                                       [?] 說明
  ──────                                              [q] 離開
  ›[1] Zpit │ 🟡 等待輸入 00:15
      Q: Commit 2198be6 已 push 到 `origin/dev`，working tre


  按 ? 查看說明，q 離開
```

**版面規則：**
- 左右比例 70/30；Hotkeys 最小 22 欄、左欄最小 18 欄，寬度不足時兩邊互相擠壓（不會堆疊到下方）
- 左欄高度依權重分配（專案 3 / 終端 2 / Loop 2）；空的面板收合、剩餘空間留給專案
- `▎` mauve bar 是 *panel 級* 指示器，只出現在 focused 面板的 chrome，body row 不帶 `▎`
- 每個 panel 標題右側有 count badge（例 `專案 7`）、下方一小段 6 字的 rule（surface1 dim 色）
- 堆疊的面板（Terminals、Loop）chrome 前有一列空白 gutter，與上方面板拉開視覺呼吸

**操作方式：**
- ↑↓ 選擇：當下 focused 面板的 cursor，同時觸發該面板 viewport 的 cursor-follow 捲動
- PgUp / PgDn：只捲動 focused 面板，其他面板 YOffset 維持不變
- 滑鼠滾輪：滾動滑鼠游標所在的面板（命中測試，不看 focus）
- Enter：在新終端開啟 Claude Code（Windows Terminal 新 tab / tmux 新 window）
- 快捷鍵 [c][l][r][s]：同樣在新終端啟動對應的 agent
- [i]：開啟專案的 Issue Tracker 列表（Loop Slot 焦點時開該 slot 的 issue 頁）
- [p]：開啟 PR 頁面 — 主畫面開專案 PR 列表；Loop Slot 焦點時以 `FindPRByBranch` 定位該 slot 的 PR，查不到則 fallback `/pulls?head=<branch>`
- [G]：在新終端開啟 lazygit — 主畫面以專案根為工作目錄；Loop Slot 焦點時以 slot 的 worktree 為工作目錄
- [U]：在新終端執行 `claude update`（Windows 用 `cmd /c "claude update & pause"` 保留畫面，tmux 用 `read -n1` 等待按鍵）
- [u]：移除 Zpit 部署到專案的 agents/docs/hooks 檔案
- [d]：清除現有部署後重新寫入 4 個 agents (clarifier/reviewer/task-runner/efficiency) + hooks + docs，**不啟動 Claude**（confirm 後執行）
- 專案名稱旁的狀態標記：🟢 已部署（全部 10 個檔案齊全）、🟡 部分部署（部分檔案缺失或只部署過單一 agent）、⚪ 未部署
- [f]：啟動效率 Agent（輕量模式，無 hooks、無 tracker、self-review）
- Tab：循環切換 focus panel — 專案 → 活躍終端（有終端時）→ Loop（有 slot 時）→ 專案；Hotkeys 面板不納入 Tab cycle（純參考資訊，空間不足時自動收合 separator blank row、尾端補 `…`）
- [x]：當焦點在活躍終端時，關閉選中的終端（force kill process，需確認）

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
║  操作: [y] 確認 issue → Todo  [i] 在瀏覽器開啟 issue               ║
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

---

## 2.7 History (Session Browser) — `[h]` ✅ 已實作

按 `[h]` 進入跨機器 session 同步介面，列出 `~/.claude/projects/` 底下所有 encoded 資料夾、提供匯出 zip bundle 與匯入 zip bundle 的 wizard。

### 資料夾清單（folder list）

```
╔══════════════════════════════════════════════════════════════════════╗
║  History — ~/.claude/projects/                       [Esc] 返回    ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  Encoded Folders — ~/.claude/projects/                             ║
║  ────────────────────────────────────────────────────────────      ║
║                                                                    ║
║     [+] Import bundle...                                           ║
║                                                                    ║
║   › 🟢 D--Documents-MyProjects-zpit                                ║
║         12 sessions  4.2 MB  04/26 14:30                           ║
║                                                                    ║
║     -home-jeff-projects-zacfuse                                    ║
║         3 sessions  812 KB  04/24 09:12                            ║
║                                                                    ║
╠══════════════════════════════════════════════════════════════════════╣
║  Enter: open  [E]: export all  [i]: import  [Esc] back  [q] quit  ║
╚══════════════════════════════════════════════════════════════════════╝
```

### Session 清單（session list — drilled in）

```
╔══════════════════════════════════════════════════════════════════════╗
║  Sessions — D--Documents-MyProjects-zpit             [Esc] 返回    ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║   › [x] 🧩 🟢 6f81a4c3-9e0d-...                                    ║
║         812 KB  04/26 14:30                                        ║
║                                                                    ║
║     [ ] 🧩    a3e7b2c1-8f1d-...                                    ║
║         412 KB  04/26 13:08                                        ║
║                                                                    ║
║     [ ]       2c0d4f8e-1a3b-...                                    ║
║         48 KB   04/25 22:14                                        ║
║                                                                    ║
║     1 selected                                                     ║
║                                                                    ║
╠══════════════════════════════════════════════════════════════════════╣
║  Space: select  [a]: toggle all  [e]: export  Enter: detail  [Esc] back ║
╚══════════════════════════════════════════════════════════════════════╝
```

### 快捷鍵

#### Folder list
| Key | Action |
|-----|--------|
| `↑↓` | 上下選取資料夾或 `[+] Import bundle...` |
| `Enter` | 進入該資料夾的 session 清單；row 0 觸發匯入 wizard |
| `[E]` | 對焦中的資料夾匯出全部 sessions（pre-select all） |
| `[i]` | 開啟匯入 wizard |
| `Esc` | 返回主畫面 |

#### Session list
| Key | Action |
|-----|--------|
| `↑↓` | 上下選取 session |
| `Space` | 切換當前 row 的選取（`[x]` / `[ ]`） |
| `[a]` | toggle-all-on（任一未選）或 toggle-all-off |
| `[e]` | 開啟匯出 confirm modal |
| `Enter` | 嘗試開啟詳細檢視（v1 顯示 "coming in follow-up issue"） |
| `Esc` | 返回 folder list |

### 匯出流程（Export）

1. 從 session list `[e]` 觸發 → 若任一選取的 session 仍 alive，先彈出 active-session warning（資訊性，不會 hard-block）。
2. Export confirm modal：顯示總數、總大小、`[ ] Include memory/` 勾選框（預設 OFF）、輸出路徑（pre-fill `~/.zpit/exports/<folder>-<ts>.zip`）。
3. 確認後 `Pack` 將 sessions、subagents 子樹、可選 memory 子樹、manifest 全部塞入 zip。
4. 完成後 status bar 顯示 `Exported N session(s) to <path>`。

### 匯入流程（Import）

1. 從 folder list `[i]` 觸發 → bundle 路徑 textinput。
2. Bundle 路徑 Enter → `LoadManifest` 讀 zip 內 `manifest.json` → 顯示 manifest preview（source OS、source path、session 數、memory 與否）。
3. Manifest preview：每個 session 都有 checkbox（預設全勾），`Space` 切換、`[a]` toggle-all、Enter 進下一步。
4. Destination path textinput → 必須是絕對路徑，Zpit 用 `watcher.EncodeCwd` 推導目標 encoded 資料夾。
5. Final preview：「Will write to `~/.claude/projects/<dest-encoded>/`, N sessions, memory: yes|no」，Enter 確認。
6. Pre-flight collision detection：每個會 collision 的 session 都跳出 3-button modal（**Overwrite** / **Skip this session** / **Cancel entire import**）；memory 目錄 collision 也走同樣流程。
7. `Unpack` 串流每行 JSONL：JSON object 中 `"cwd"` 欄位等於 bundle `source_cwd` 才會被改寫成 destination cwd；其他內容（包含 chat content 中出現的 path-shaped string）絕不更動。Subagents 子樹 verbatim 複製。
8. 完成後跳出 summary：`written N skipped M cancelled K, memory: written|skipped|not-included`。

### 設計決策

- **為何 zip 而非 tar.gz**：Windows 雙擊即可解壓；tar.gz 需 7-Zip 或 CLI。
- **為何 path rewrite 在 import 時**：destination cwd 在匯出時未知；單一 bundle 可重複匯入到不同機器。
- **為何 JSON-aware rewrite**：line-by-line `json.Unmarshal` → 修改 `cwd` 欄位 → re-marshal，確保只有 `cwd` 欄位被改寫，避免改到 chat 內容裡的 path 字串。
- **為何 subagents 預設包含、memory 預設不包含**：subagents 是 session-bound、loop-engine session replay 必需；memory 是 project-scoped、可能含 user-private 內容（email、dated notes），需明確 opt-in。
- **為何 active-session warning 是資訊性**：使用者隨時可決定強制匯出，TUI 不應 hard-block 工作流程。

相關檔案：
- `internal/sessionsync/manifest.go` — Manifest schema + JSON marshal/unmarshal + `DetectSourceOS`
- `internal/sessionsync/scan.go` — `ScanFolders` / `ScanSessions` / `ProjectsRoot`
- `internal/sessionsync/pack.go` — `Pack` zip writer + cwd auto-detection from session lines
- `internal/sessionsync/unpack.go` — `Unpack` + cwd rewrite + collision-checked write + `LoadManifest` + `DetectCollisions`
- `internal/tui/sessions.go` — cmd factories + msg handlers (entry/exit logs, active-session detection)
- `internal/tui/view_sessions.go` — folder list / session list / modal rendering
- `internal/tui/model.go` — `ViewHistory` constant、`[h]` 鍵 handler、wizard 步驟狀態機（步驟 0–5 import、步驟 0–3 export）
