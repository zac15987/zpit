# model.go God Class 重構討論紀錄

> 日期：2026-04-03
> 對應 Issue：[#24](https://github.com/zac15987/zpit/issues/24) model.go 大檔案程式碼拆分評估
> 方法：3 人 Agent Team 專家討論（3 輪）
> **狀態：已完成** — Phase 1 via [PR #59](https://github.com/zac15987/zpit/pull/59) merged（2026-04-03）；Phase 2 評估後決定不執行。

---

## 參與者

| 角色 | 代號 | 關注焦點 |
|------|------|----------|
| 資深技術架構師 | **Alex** | 設計原則、抽象邊界、資訊隱藏、Code Construction Principles |
| 資深 Go 語言工程師 | **Bo** | Go 慣用法、interface 設計、可測試性、並發安全 |
| 系統 Terminal 工程師 | **Carmen** | Bubble Tea 框架限制、TUI 生命週期、即時性、value copy 語義 |

---

## 問題背景

`internal/tui/model.go` 有 **2433 行、50+ 方法、6 種職責**混合在一個 `Model` struct 上，違反 Code Construction Principles 的多項原則。

### 6 大職責區塊

| 區塊 | 行數 (約) | 函式數 | Model UI 依賴 |
|------|----------|--------|--------------|
| Core（types/Init/Update routing/View） | ~650 | 12 | 核心骨架 |
| Key Handlers | ~300 | 6 | 深度依賴（cursor, currentView, viewport） |
| Session Lifecycle | ~500 | 12 | **零依賴** — 只存取 `m.state`（AppState） |
| Launch & Deploy | ~450 | 17 | `m.cursor` 可參數化 |
| Tracker & Label Ops | ~350 | 11 | 混合：cmd factory 只需 AppState，confirm 需 Model UI |
| Channel | ~30 | 2 | 最小 |

### 現有的成功拆分模式

專案已有按職責分檔的先例：
- `loop_cmds.go`（856 行）— loop 相關 tea.Cmd factory
- `loop_handler.go`（483 行）— loop 訊息處理器
- `view_projects.go`（458 行）— 專案列表渲染
- `view_channel.go`（229 行）— channel 事件時間軸

---

## 第一輪：問題診斷與初步提案

### Alex 的分析

**核心診斷**：model.go 違反三項 Code Construction Principles：
1. **§3 God Class** — Model 承擔 6 種不同職責，每種的資訊隱藏邊界完全不同
2. **§2 抽象層次不一致** — `update()` 的 switch 同時混合高層 one-line dispatch 和低層 30+ 行 inline handler
3. **§2 核心信念** — 修改 permission detection 需要在 2434 行中跳轉三個方法，隱性耦合

**依賴分析三層分類**：
- **第一層（零 UI 依賴）**：Session 方法 ~430 行，只存取 `m.state`，不碰任何 Model UI field
- **第二層（可參數化）**：Launch 方法 ~245 行，唯一依賴 `m.cursor` 可改為接收參數
- **第三層（深度 UI 綁定）**：Key handlers + confirm ~630 行，不建議第一步抽離

**提案**：Phase 1 做 same-package file split，Phase 2 等有測試後抽取 `SessionManager` struct。

### Bo 的回應

- **同意 file split 符合 Go 慣用法**，引用 `net/http` 的 `server.go`/`request.go`/`response.go` 先例
- **補充 field 問題**：Model 有 24 個 field，§3 建議 7±2。Field 形成清晰聚類（核心、StatusView、Confirm、LoopFocus），Phase 2 可用 embedded struct 組合
- **挑戰 session.go 範圍**：內部混合 Discovery（one-shot cmd chain）和 Monitoring（tick-driven），但不堅持拆成兩檔
- **建議每個新檔案加 lock protocol doc comment**（§10：Comments explain why, not what）

### Carmen 的回應

**Bubble Tea 框架角度的三類 session 方法分類**：
1. **Cmd 工廠**（scanExistingSessionsCmd 等）— 回傳 tea.Cmd，goroutine 執行，搬走完全安全
2. **Msg handler**（existingSessionsMsg 等）— 修改 activeTerminals，遵循 action-defer pattern
3. **Tick-driven**（checkSessionLiveness 等）— pointer receiver，side-effect + cmd collector

**對 Phase 2 SessionManager struct 發出關鍵警告**：
1. Bubble Tea Model 是 value type，`*SessionManager` 指標會被多個 Model 副本共享
2. 如果 SessionManager 有自己的 mutex → 雙層鎖（Go 無可重入鎖），死鎖風險增加
3. `handleLaunchResult` 在**同一個 Lock 區間**同時操作 `activeTerminals`（session）和 `channelSubs`（channel）→ 拆 struct 破壞原子性
4. 如果共用 `AppState.mu` → 只是 facade，無真正隔離

**Carmen 的立場**：Phase 1 file split 全力支持（擴大到 session.go + launch.go + tracker_ops.go），Phase 2 struct 抽取先觀望（§2 No speculative generality）。

---

## 第二輪：核心爭議交鋒

### 爭議焦點：File split 是否足以解決 God Class？

**Alex 修正立場**：

接受 Carmen 的 `handleLaunchResult` 原子性論證，**放棄 Phase 2 SessionManager struct**。

提出新觀點：「在 Bubble Tea 約束下，God Class 的解法不是拆 struct，而是分檔降低認知負載 + field 聚類降低 field 數量。Bubble Tea 的 Elm 架構本質上就是把所有 mutable state 集中在一個 Model 裡——這是 architectural decision，不是 code smell。」

**Bo 的 `database/sql.DB` 類比**：

Go 標準庫的 `database/sql.DB` 也是一個「大 struct」（25+ field），有 connection pool、statement cache、stats 等多種職責。Go 的做法是：struct 不拆（API contract 要求）、方法按職責分散在多個檔案。Bubble Tea 的 `tea.Model` interface 就像 `database/sql` 的 public API——不能拆。

**Bo 區分 Phase 2 兩條路徑**：
- **路徑 A（推薦）**：純 UI state 的 embedded struct（StatusViewState, ConfirmState）— 不引入新鎖，不改變並發語意
- **路徑 B（否決）**：SessionManager struct 管理共享 mutable state — Carmen 的論證決定性

**Carmen 對 handleLaunchResult 歸屬的分析**：

```
handleLaunchResult 在一次 Lock/Unlock 區間內做了：
1. nextTrackingKey()         — session 職責
2. 建立 ActiveTerminal       — session 職責
3. trackedPIDs()             — session 職責
4. 讀取 channelSubs          — channel 職責
(Unlock 後)
5. startWatcherDirCmd()      — session 職責
6. channelSubscribeCmd()     — channel 職責
```

結論：它是 **orchestrator**——協調多個子系統，本身不屬於任何單一子系統。應留在 model.go。

---

## 第三輪：收斂與定稿

### Alex 的「編譯器即測試」劃線

§8 說「Tests are a prerequisite」，但在 Go 靜態類型語境下需要精確解讀：

| 操作 | Compiler 保證 | 需要測試 |
|------|--------------|---------|
| 方法從 A.go 搬到 B.go（同 package） | 完全等價 | 不需要 |
| Field 改名（`m.statusCursor` → `m.status.Cursor`） | 所有 call site 必須更新，否則 compile error | 不需要（但建議 review） |
| Inline code 抽成 named method | 完全等價 | 不需要 |
| 方法改為接收參數而非讀 struct field | Compiler 檢查類型，但不檢查語意 | **需要測試** |
| 引入新 interface | Compiler 檢查 method set，但不檢查行為 | **需要測試** |

**Phase 1 的全部操作都在「Compiler 保證區」**。

### Bo 的 Embedded Struct 風險量化

- `statusProjectID` — 至少 8 處引用
- `statusIssues` — 至少 7 處引用
- `statusCursor` — 至少 10 處引用
- `confirmForm/Result/Action` — 至少 15 處引用
- 合計：**40+ 處 call site 改動**

技術上 compiler 保證正確性，但應與 file split 分開 commit（§8 小步原則 + git blame 可追溯性）。

### Carmen 對 Embedded Struct 的 Bubble Tea 警告

- Embedded value struct 的 copy 語意安全（Bubble Tea 複製 Model 時完整複製）
- `ConfirmState` 裡的 `*huh.Form` 是 pointer，copy 後多副本共享——**但這是現有行為**，不改變語意
- **關鍵警告**：embedded struct **不要加任何方法**（不要有自己的 `Update()`），否則 Bubble Tea 可能混淆
- `StatusViewState.Issues` 是 `[]tracker.Issue`（slice），value copy 只複製 header，底層 array 共享——目前安全（只做整體替換），但未來需注意

---

## 最終共識方案

### Phase 1：Same-package file split（零風險，立即可執行）

| 新檔案 | 內容 | 預估行數 |
|--------|------|----------|
| `session.go` | `ActiveTerminal` type + session msg types（`sessionFoundMsg`, `existingSessionsMsg`, `watcherReadyMsg`, `existingSessionEntry`, `permissionSignal`）+ handlers（`handleExistingSessions/Found/WatcherReady`）+ cmd factories（`scan/startWatcher/waitForLog/watchNext`）+ tick methods（`checkLiveness/Permission/NewSessions`）+ helpers（`trackedPIDs`, `nextTrackingKey`, `signalDir`, `deletePermissionSignal`） | ~500 |
| `launch.go` | `launchClaudeCmd`, `launchClarifier/ReviewerCmd`, `deployAndLaunchAgent`, `launchFocusClaudeCmd`, `openFolderCmd`, `openSlotFolderCmd/IssueCmd`, `openTrackerCmd`, `openInBrowser`, `deployDocs`, `injectLangInstruction`, `launchableSlotStates` | ~400 |
| `tracker_ops.go` | `checkLabelsCmd`, `ensureLabelsCmd`, `loadIssuesCmd`, `confirmIssueCmd`, `openIssueURLCmd`, `startWithLabelCheck`, `showLabelConfirm` | ~200 |
| `confirm.go` | `showDeployConfirm`, `showReviewerDeployConfirm`, `showUndeployConfirm`, `showIssueConfirm`, `executePendingOp`, `undeployFiles` | ~200 |
| `channel.go` | `channelSubscribeCmd`, `channelReadNextCmd` | ~40 |

**留在 model.go（~800 行）**：
- Model struct 定義 + enums + constants
- `NewModelWithState` / `NewModel`
- `Init` / `Update` routing / `View` routing
- `handleKey`, `handleProjectsKey`, `handleStatusKey`, `handleChannelKey`
- `handleFocusSwitch`, `handleLoopSlotsKey`, `sortedSlotKeys`
- `handleLaunchResult`, `handleAgentEvent`（跨領域 orchestrator）
- `setStatus`, `findProject`, `syncViewportContent`, `ensureCursorVisible`
- `tickCmd`, `waitForStateRefresh`, `RunServerInit`, `serverInitCmds`
- `update()` 中的 confirm form routing

**額外要求**：
- `update()` 中所有 inline handler 抽為 named method（one-line dispatch）
- 每個新檔案頂部加 lock protocol doc comment（三級標注：Handler / Cmd factory / Tick-driven）
- session.go 內部用 section comments 分區
- 分檔順序：session.go → launch.go → tracker_ops.go → confirm.go → channel.go，每檔一個 commit
- 驗證：`go build ./...` + `go test ./...` + `go vet ./...`

### Phase 2（分檔後評估，需測試覆蓋）

- **路徑 A（推薦）**：Model field embedded struct 聚類
  - `StatusViewState`（5 field：ProjectID, Issues, Cursor, Loading, Error）
  - `ConfirmState`（4 field：Form, Result, Action + PendingOp）
  - `LoopFocusState`（3 field：Panel, Cursor, ProjectID）
  - `ChannelViewState` 暫不做（只有 1 field，§2 No speculative generality）
  - Model 直接 field 從 24 降到 ~14
- **路徑 B（排除）**：不做 SessionManager/LaunchManager 獨立 struct
- `launch.go` 方法參數化（`m.cursor` → `config.ProjectConfig`）— 跨線需先寫測試

### 明確排除

- 不引入 interface（無多態需求）
- 不新增 package（tui 內部耦合合理）
- 不新增 mutex 層（單一 `AppState.mu` 是正確設計）

---

## 關鍵決策記錄

| # | 決策 | 結論 | 決定性論點 | 提出者 |
|---|------|------|-----------|--------|
| 1 | Phase 2 是否抽 SessionManager struct | **否** | `handleLaunchResult` 跨 session/channel 的原子性需要單一 Lock；雙層鎖死鎖風險 | Carmen |
| 2 | File split 是否足以解決 God Class | **是**（在 Bubble Tea 約束下） | Elm 架構的 single-Model 設計本質上要求集中 state；`database/sql.DB` 先例 | Carmen + Bo |
| 3 | Embedded struct 是否 Phase 1 做 | **否，延後到 Phase 2** | §8 小步原則：分檔和 field rename 不同改動不應混合；40+ call site 應分開 commit | Bo + Carmen |
| 4 | `handleLaunchResult` 歸屬 | **留 model.go** | 跨 session/channel 的 orchestrator，不屬於任何單一子系統 | 三人一致 |
| 5 | `ActiveTerminal` type 歸屬 | **放 session.go** | 主要操作者都在 session.go；Go 慣用法：type 跟主要操作走 | Bo（Alex/Carmen 同意）|
| 6 | 無測試能做到什麼程度 | **分檔 + named method 抽取** | Go compiler 保證等價性 = 第零層測試；call site 改動才跨線 | Alex（Bo/Carmen 同意）|
| 7 | session.go 內部是否拆兩檔 | **否，單檔 + section comments** | Discovery 和 Monitoring 共享 `activeTerminals` 和 `trackedPIDs`，拆開增加認知負擔 | Carmen |
| 8 | confirm.go 納入 Phase 1 | **是** | 自成一體的模態邏輯，跟 key handling / view rendering 正交 | Alex（Carmen 同意）|

---

## 實作結果

### Phase 1：已完成 ✅

[PR #59](https://github.com/zac15987/zpit/pull/59) merged（2026-04-03）

| 檔案 | 行數 | 說明 |
|------|------|------|
| `model.go` | 860（從 2433 降） | Core routing + key handling + orchestrators |
| `session.go` | 733 | Session lifecycle：handlers + cmds + tick + types |
| `launch.go` | 479 | Launch & Deploy：launch cmds + slot ops + utilities |
| `tracker_ops.go` | 242 | Tracker & Label：label check/ensure + issue ops |
| `confirm.go` | 208 | Confirm dialogs + executePendingOp + undeploy |
| `channel.go` | 78 | Channel subscription + event reading |

`update()` 的所有 message case 均為 one-line dispatch，每個新檔案頂部有 lock protocol doc comment。

### Phase 2：評估後決定不執行 ✅

Phase 1 完成後，以 Bo 提出的三項 God Class 測試重新評估 model.go：

| 測試 | 拆分前 | 拆分後 |
|------|--------|--------|
| ①「一個修改是否牽動不相關的程式碼？」 | ❌ 改 session 要翻 2433 行 | ✅ 只開 session.go |
| ②「理解一個功能是否需要讀完所有方法？」 | ❌ 50+ 方法混在一起 | ✅ 17 方法，全是 routing/key handling |
| ③「struct 是否有大量方法不使用的 field 子集？」 | ❌ statusIssues 只被 10% 方法用 | ⚠️ 仍存在，但影響已小 |

**結論：God Class 症狀已基本消除。**

- model.go 860 行、17 方法——規模與 `loop_cmds.go`（856 行）同級，是正常的 Bubble Tea root Model
- model.go 現在的職責為 **TUI application state machine**（routing + key handling + cross-domain orchestration），是 Bubble Tea Elm 架構下合法的單一抽象
- 24 個 field 是 Bubble Tea 的結構性限制（single source of truth），不是設計缺陷
- Embedded struct field 聚類（StatusViewState, ConfirmState, LoopFocusState）技術上可行，但屬於 §2 No speculative generality——目前結構已足夠清晰，無需強行重構

Phase 2 的 embedded struct 聚類和 launch 方法參數化保留為「已知可選改善」，未來如有實際痛點再重新評估。

| # | 決策 | 結論 | 理由 |
|---|------|------|------|
| 9 | Phase 2 是否執行 | **否（不需要）** | Phase 1 後 God Class 症狀已消除；24 field 是 Bubble Tea 結構性限制；§2 No speculative generality |

---

## 風險提醒

1. **已知耦合點**：`handleLaunchResult` 同時操作 `activeTerminals` + `channelSubs` 是架構級耦合。若 channel 功能擴展，此 handler 可能需要重構。
2. **Slice 共享**：`StatusViewState.Issues`（`[]tracker.Issue`）在 Bubble Tea value copy 時只複製 slice header。目前安全（整體替換），但未來若有 in-place mutation 需注意。
3. **Embedded struct 不加方法**：若未來做 embedded struct 聚類，純粹是 field 分組，不要加 `Init/Update/View` 方法，否則 Bubble Tea 可能混淆。

---

## 引用的原則

- **§2 Design**：Managing complexity is the central goal; High cohesion, low coupling; Information hiding; No speculative generality
- **§3 Classes**：Avoid God Classes; Keep data members at roughly 7±2
- **§6 Control Structures**：Table-driven methods（update switch → one-line dispatch）
- **§8 Refactoring**：Tests are a prerequisite; Small steps
- **§10 Layout**：Comments explain why, not what（lock protocol doc comments）
- **Core Belief**：Enable the developer to work correctly while holding the minimum amount of code in mind
