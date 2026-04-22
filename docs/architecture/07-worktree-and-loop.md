# 7. Git Worktree 平行開發 + 自動化 Loop + Issue 狀態流

---

## 7.1 為什麼需要 Worktree

同一個專案同時跑多個 agent（各做不同 issue）時，它們不能共用
同一個 working directory。Git worktree 讓每個 agent 有自己獨立的
工作目錄，各自在不同 branch 上，互不干擾。

```
D:/Projects/ASE_Inspection/             ← 主 repo (dev branch)
  ├── .git/                             ← 唯一的 .git 目錄
  ├── src/
  └── ...

D:/Projects/.worktrees/                 ← 所有 worktree 集中管理
  └── ASE_Inspection/
      ├── ASE-47--ethercat-reconnect/   ← Agent A 的工作目錄
      │   ├── src/                        (branch: feat/ASE-47-ethercat-reconnect)
      │   └── ...
      ├── ASE-48--vision-timeout/       ← Agent B 的工作目錄
      │   ├── src/                        (branch: feat/ASE-48-vision-timeout)
      │   └── ...
      └── ASE-49--z-axis-homing/        ← Agent C 的工作目錄
          ├── src/                        (branch: feat/ASE-49-z-axis-homing)
          └── ...
```

---

## 7.2 Worktree 生命週期

```
Issue 進入 In Progress
    │
    ├─ 1. 從 base branch 建立 feature branch（統一 feat/ 前綴）
    │     git branch feat/ISSUE-ID-slug {base_branch}
    │     base branch 來源：Issue Spec ## BRANCH > project config base_branch
    │
    ├─ 2. 建立 worktree
    │     git worktree add <worktree-path> feat/ISSUE-ID-slug
    │
    ├─ 3. 部署 hooks + agents + docs 到 worktree
    │     DeployHooksToWorktree() → .claude/hooks/ + settings.local.json
    │
    ├─ 4. 在新終端中啟動 Claude Code（可見，使用者可隨時介入）
    │     工作目錄 = worktree 路徑（path override）
    │     ZPIT_AGENT=1 環境變數注入（啟用 hook 強制執行）
    │
    ├─ 5. Agent 實作 + Review + 開 PR
    │
    ├─ 6. PR merge 後清理
    │     git worktree remove <worktree-path>
    │     git branch -d feat/ISSUE-ID-slug
    │     git fetch origin <base>:<base>  （更新主目錄本地 base branch ref）
    │
    └─ 7. Issue → Done
```

**Zpit 有兩層 worktree**（本節描述的是 **issue 層**）：

- **Issue 層**（本節）：每個 Loop slot / issue 一個 worktree，由 `internal/worktree/Manager` 在 Go 端管理，掛在 `base_dir_*` 下。這是 coding agent / reviewer 的工作目錄。
- **Teammate 層**（Task Execution Model）：`[P]` 平行批次時，orchestrator 在 issue worktree 內建立 child worktree — 路徑 `<issue-wt>/.zpit-children/<slug>`，由 `WorktreeCreate` hook（`hooks/worktree-create.sh`）管理，不走 Go Manager。Batch 結束後 orchestrator 自己 `git worktree remove --force` + `git branch -D`。`.zpit-children/` 是 zpit gitignore rule，所以永遠不會進 PR。詳見 `06-agents.md §6.3`。

兩層互不干涉：issue worktree 的 lifecycle（建→ agent 工作 → PR merge → 清理）在 Go 層；teammate worktree 的 lifecycle 在 coding agent 的 prompt 層（hook 建、orchestrator 清）。

---

## 7.3 Worktree Config

```toml
[worktree]
base_dir_windows = "D:/Projects/.worktrees"
base_dir_wsl = "/mnt/d/Projects/.worktrees"
dir_format = "{project_id}/{issue_id}--{slug}"   # slug 由 issue title 自動產生
auto_cleanup = true           # PR merge 後自動清理
max_per_project = 5           # 每個專案最大同時 worktree 數量
max_review_rounds = 3         # coding↔review 最大循環次數（超過進入 NeedsHuman）
poll_seconds = 10             # todo issue polling 間隔
pr_poll_seconds = 10          # PR/label 狀態 polling 間隔

# base_branch 在各 project 中設定（預設 "dev"）
```

**注意事項：**
- CLAUDE.md 和 .claude/ 存在主 repo 中，worktree 會自動繼承
- worktree 不是 clone：共用同一個 .git，同一份歷史
- 機台電腦不用 worktree：一次只看一個 branch，不需要平行化
- 如果多個 agent 改到同一檔案導致衝突，人工處理

---

## 7.4 自動化 Loop 流程

TUI 按 [l] 後，在 TUI 內以 goroutine 啟動 loop（非獨立子命令）。
支援同一專案多個 agent 平行工作，每個 agent 在自己的 worktree 中運行。
TUI 關閉時 loop 停止，但已啟動的 Claude Code agent 不受影響（獨立 process）。

**核心原則：Zpit 只負責調度，不介入 agent 工作內容。**
Build、test、review、開 PR、更新 tracker status 都是 agent 自己的職責。

```
TUI 按 [l]
│
│  ┌── loop 在 TUI goroutine 中運行（純調度）──────────────┐
│  │                                                        │
│  │ 1. 查詢 Tracker API: 抓此專案 status=Todo 的最高優先   │
│  │    issue，如果沒有 → 定期 poll（每 10 秒）             │
│  │                                                        │
│  │ 2. 檢查此專案目前有幾個活躍 worktree                   │
│  │    如果 >= max_per_project → 等待                      │
│  │                                                        │
│  │ 3. Zpit 建立 branch + worktree + 部署 hooks           │
│  │    base branch = Issue Spec ## BRANCH || config        │
│  │    git branch feat/ISSUE-ID-slug {base_branch}         │
│  │    git worktree add <path> feat/ISSUE-ID-slug          │
│  │    DeployHooksToWorktree() 配置 settings               │
│  │                                                        │
│  │ 4. 寫入臨時 agent 檔案到 worktree                      │
│  │    .claude/agents/coding-{issue-id}.md                 │
│  │    （由 BuildCodingPrompt 組裝 Issue Spec → prompt）   │
│  │    若 Issue Spec 含 TASKS → 同時部署 task-runner.md    │
│  │    （subagent 定義，供 coding agent 委派 task 使用）   │
│  │                                                        │
│  │ 5. 啟動 coding agent（新終端，可見）                   │
│  │    工作目錄 = worktree 路徑，ZPIT_AGENT=1              │
│  │                                                        │
│  │ 6. 輪詢 issue labels（每 10 秒 GetIssue）              │
│  │    偵測到 "review" label = coding agent 完成            │
│  │    （label 驅動，非 PID 驅動，終端保留不關）           │
│  │                                                        │
│  │ 7. 啟動 reviewer agent（同一 worktree，唯讀）          │
│  │                                                        │
│  │ 8. 輪詢 issue labels                                   │
│  │    ├─ ai-review → PASS → 等待 PR merge                │
│  │    ├─ needs-changes → NEEDS CHANGES                    │
│  │    │  └─ round < max_review_rounds?                    │
│  │    │     ├─ 是 → 寫修正版 prompt，重跑 coding agent   │
│  │    │     └─ 否 → NeedsHuman 狀態，通知你介入          │
│  │    └─ label 未變 → 繼續輪詢                            │
│  │                                                        │
│  │ 9. 偵測 PR merged → 清理 worktree + branch             │
│  │    + 同步本地 base branch（git fetch origin <base>:<base>）│
│  │    失敗時 log warning，不中斷 issue 關閉流程             │
│  │                                                        │
│  │ 10. 回到步驟 1 抓下一個 issue                          │
│  │                                                        │
│  └────────────────────────────────────────────────────────┘
```

---

## 7.5 Loop 狀態機

所有狀態定義在 `internal/loop/types.go`：

```
SlotCreatingWorktree    建立 worktree 中
       ↓
SlotWritingAgent        準備 agent prompt 檔案
       ↓
SlotLaunchingCoder      啟動 coding agent 中
       ↓
SlotCoding              coding agent 工作中（poll labels 等待 "review"）
       ↓
SlotLaunchingReviewer   啟動 reviewer agent 中
       ↓
SlotReviewing           reviewer 工作中（poll labels 等待 "ai-review" 或 "needs-changes"）
       ↓                           ↓
SlotWaitingPRMerge      SlotCoding (needs-changes → 重跑，round++)
       ↓
SlotCleaningUp          清理 worktree + branch + 同步本地 base branch ref
       ↓
SlotDone                完成

異常狀態:
SlotNeedsHuman          超過 max_review_rounds，需人工介入
SlotError               流程中發生錯誤
```

狀態轉換是 **label 驅動**（poll issue labels，非 PID 監控）：
- Coding agent 設定 `review` label → reviewer 啟動
- Reviewer 設定 `ai-review` (PASS) 或 `needs-changes` (auto-retry)

**Polling chain 的心跳實作（tick-driven）：** 三個等待型狀態各自對應一條獨立的 `tea.Tick` 心跳鏈：

| 狀態 | Poll 內容 | 下一個 tick 由誰排 |
|---|---|---|
| 任何 Active loop | 抓 todo issues | `handleLoopPollTick` |
| `SlotCoding` / `SlotReviewing` | 抓 issue labels | `handleLoopLabelPollTick` |
| `SlotWaitingPRMerge` | 抓 PR 狀態 | `handleLoopPRPollTick` |

**關鍵不變量：心跳的 reschedule 只發生在 `model.go` 的 tick case（`loop_handler.go` 的 `handleLoop*Tick` 系列），不發生在 business handler（`handleLoopPoll` / `handleLoopLabelPoll` / `handleLoopPRStatus`）。** 每個 tick handler 在入口檢查 gate（loop `Active` + slot state 正確），通過就 `tea.Batch(pollCmd, scheduleNextTick)` 預先排好下一跳；不通過就 return nil，心跳自然停止。business handler 只負責 state transition，不得自行 reschedule。

這個設計避免了「handler return nil 路徑漏寫 reschedule 導致整條 poll 鏈永久啞掉」的 bug（2026-04-18 log 觀察到）。新增狀態或 poll 鏈時：`loopSchedulePoll` / `loopSchedulePRPoll` / `loopScheduleLabelPoll` 只能在 **kickoff** 時機呼叫（loop 啟動、transition 進入等待狀態、resume），禁止在 business handler 的 mid-chain 呼叫。`internal/tui/loop_tick_test.go` 覆蓋這個不變量。

`Slot` struct 追蹤每個 issue 在 pipeline 中的狀態：

```go
type Slot struct {
    ProjectID    string
    IssueID      string
    IssueTitle   string
    BranchName   string    // e.g. "feat/ISSUE-ID-slug"
    BaseBranch   string    // PR target branch
    WorktreePath string
    State        SlotState
    ReviewRound  int       // 0-based; NEEDS CHANGES 時遞增
    Error        error
    SessionPID   int
    LaunchedAt   int64     // unix timestamp
}
```

---

## 7.6 Issue 狀態流（所有 Tracker 通用）

```
                          ┌─────────────────────────────────┐
                          ▼                                 │
┌────────┐  ┌──────┐  ┌──────────┐  ┌───────────┐  ┌──────┴──────┐
│待確認  │─▸│ Todo │─▸│ AI 實作中 │─▸│ AI Review │─▸│等待你Review │
│(Clarify│  │      │  │          │  │           │  │             │
│ 產出)  │  │(你按 │  │(Loop自動)│  │(自動)     │  │(PR 已開)    │
└────────┘  │ 確認)│  └──────────┘  └───────────┘  └──────┬──────┘
    │       └──────┘       ▲                              │
    │ (你拒絕/要修改)       │ (needs-changes)               │
    ▼                      └──────────────────────────────┘
  (刪除或                                                  │ (approve)
   回到 Clarify)                                   ┌───────▼───────┐
                                   (純軟體專案) ───▸│     Done      │
                                                   └───────────────┘
                                                           ▲
                                                           │ (驗證通過)
                                                   ┌───────┴───────┐
                                   (機台/Android)─▸│  待實體驗證    │
                                                   └───────────────┘
```

**關鍵設計：「待確認」門檻**

Clarifier Agent 產出的 issue 預設進入「待確認」狀態（label: pending），不是「Todo」。
Loop 只會抓 Todo（label: todo）的 issue，所以沒有你明確確認，agent 不會開始動手。

確認方式：
- 在 TUI 的 Status 畫面按 [y] 確認 → pending → todo
- 在 Tracker 網頁上手動改 label
