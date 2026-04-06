# 4. 設定檔與 Provider

---

## 4.1 config.toml

Zpit 的所有資料統一在 `~/.zpit/` 下：
- `~/.zpit/config.toml` — 設定檔（可用 `ZPIT_CONFIG` 環境變數覆蓋路徑）
- `~/.zpit/logs/` — 日誌（daily rotation，自動清理 30 天以上）

首次啟動時若 config 不存在，自動產生模板（`config.WriteTemplate()`），提示使用者編輯後再啟動。

```toml
# ~/.zpit/config.toml

# ──────────────────────────────────────────────
# 終端設定
# ──────────────────────────────────────────────
[terminal]
windows_mode = "new_tab"    # "new_tab" | "new_window"
tmux_mode = "new_window"    # "new_window" | "new_pane"

# ──────────────────────────────────────────────
# 通知設定
# ──────────────────────────────────────────────
[notification]
tui_alert = true
windows_toast = true
sound = true
# sound_file = "D:/sounds/notify.mp3"   # 自訂通知音效路徑（支援 WAV/MP3/M4A/OGG）
re_remind_minutes = 2

# ──────────────────────────────────────────────
# Worktree 設定
# ──────────────────────────────────────────────
[worktree]
base_dir_windows = "D:/Projects/.worktrees"
base_dir_wsl = "/mnt/d/Projects/.worktrees"
dir_format = "{project_id}/{issue_id}--{slug}"   # 預設
auto_cleanup = true           # PR merge 後自動清理
max_per_project = 5           # 每個專案最大同時 worktree 數量
max_review_rounds = 3         # coding↔review 最大循環次數
poll_seconds = 15             # todo issue polling 間隔（秒）
pr_poll_seconds = 30          # PR/label 狀態 polling 間隔（秒）

# ──────────────────────────────────────────────
# SSH Server（zpit serve）
# ──────────────────────────────────────────────
[ssh]
port = 2200                                    # 預設
host = "0.0.0.0"                               # 預設
host_key_path = "~/.zpit/ssh/host_ed25519"     # 預設，支援 ~/ 展開
password_env = "ZPIT_SSH_PASSWORD"              # env var 名稱，選配
authorized_keys_path = "~/.ssh/authorized_keys" # 預設，選配

# ──────────────────────────────────────────────
# Issue Tracker Providers
# ──────────────────────────────────────────────

[providers.tracker.my-forgejo]
type = "forgejo_issues"
url = "https://git.nas.local"
token_env = "FORGEJO_TOKEN"

[providers.tracker.my-github]
type = "github_issues"
token_env = "GITHUB_TOKEN"

# ──────────────────────────────────────────────
# Git Host Providers（agent 透過 MCP 操作，Zpit 不直接使用）
# ──────────────────────────────────────────────

[providers.git.forgejo-local]
type = "forgejo"
url = "https://git.nas.local"

[providers.git.github]
type = "github"

# ──────────────────────────────────────────────
# Profiles
# ──────────────────────────────────────────────

[profiles.machine]
log_policy = "strict"     # 所有 Service 方法有進出 log，硬體操作有指令/回應 log

[profiles.web]
log_policy = "minimal"    # 只 log 錯誤和關鍵操作

[profiles.desktop]
log_policy = "standard"   # Service 方法有進出 log，異常有完整 log

[profiles.android]
log_policy = "standard"

# ──────────────────────────────────────────────
# 專案定義
# ──────────────────────────────────────────────

[[projects]]
name = "ASE 檢測清潔機台"
id = "ase-inspection"
profile = "machine"
hook_mode = "strict"            # strict | standard | relaxed
tracker = "my-forgejo"          # 指向 providers.tracker 的 key
tracker_project = "ase-inspection"
git = "forgejo-local"
repo = "leyu/ase-inspection"
shared_core = true
log_level = "strict"
base_branch = "dev"
channel_enabled = false         # 啟用跨 agent channel 通訊
channel_listen = []             # 額外訂閱的 project key，如 ["_global", "other-proj"]
tags = ["wpf", "ethercat", "basler"]

[projects.path]
windows = "D:/Projects/ASE_Inspection"
wsl = "/mnt/d/Projects/ASE_Inspection"
```

---

## 4.2 Provider 抽象層 — TrackerClient

**核心設計決策：Zpit 透過直接 REST API 與各 tracker 互動。**

目前支援兩種 tracker：

```
TrackerClient (interface)
  ├─ ForgejoClient  → Forgejo/Gitea REST API v1
  └─ GitHubClient   → GitHub REST API
```

**為什麼用直接 API 而非 MCP 橋接？**
- Zpit 的 tracker 操作都是簡單 CRUD（列 issue、改 label、查 PR），不需要 LLM
- 直接 API < 1 秒回應，claude -p 橋接要 10-20 秒——Loop 頻繁 poll 無法接受
- `[s]` status 列表需要即時回應，使用者體驗優先

**Agent 仍透過 MCP 操作 tracker：**
- Clarifier agent 透過 MCP 推 issue（在終端中，使用者確認後）
- Coding/Reviewer agent 透過 MCP 開 PR、寫 comment、更新 label
- MCP 的安裝與配置由各專案的 `claude mcp add` 管理，與 Zpit config 無關
- Agent 讀取 `.claude/docs/tracker.md`（由 Zpit 自動部署）得知使用哪個 API

**Auth 機制：**
- 每個 provider 設定 `token_env` 欄位，指向環境變數名稱
- Zpit 啟動時從環境變數讀取 token，不在 config 中存放明文

#### TrackerClient 介面定義

```go
// internal/tracker/client.go
type TrackerClient interface {
    ListIssues(ctx context.Context, repo string) ([]Issue, error)
    GetIssue(ctx context.Context, repo string, id string) (*Issue, error)
    UpdateLabels(ctx context.Context, repo string, id string, add, remove []string) error
    GetPRStatus(ctx context.Context, repo string, prID string) (*PRStatus, error)
    FindPRByBranch(ctx context.Context, repo string, branch string) (*PRStatus, error)
    ListOpenPRs(ctx context.Context, repo string) ([]PRInfo, error)
}

func NewClient(providerType, baseURL, tokenEnv string) (TrackerClient, error)
```

---

**統一的 Issue 狀態（內部使用，各 tracker 的 label 對應由 client 實作）：**

```go
const (
    StatusPendingConfirm = "pending_confirm"  // 待確認
    StatusTodo           = "todo"
    StatusInProgress     = "in_progress"
    StatusAIReview       = "ai_review"
    StatusWaitingReview  = "waiting_review"
    StatusNeedsVerify    = "needs_verify"     // 待機台/實機驗證
    StatusDone           = "done"
)
```

各 tracker 的狀態對應：

```
內部狀態            GitHub Issues    Forgejo Issues
─────────────────────────────────────────────────
pending_confirm     label:pending    label:pending
todo                label:todo       label:todo
in_progress         label:wip        label:wip
ai_review           label:ai-review  label:ai-review
waiting_review      label:review     label:review
needs_verify        label:verify     label:verify
done                closed           closed
```

**Label on-demand 檢查：**
Zpit 需要 6 個 label（pending, todo, wip, review, ai-review, needs-changes），但不在啟動時自動建立。使用者按下操作鍵（`[y]`/`[c]`/`[r]`/`[l]`）時，每次都呼叫 `CheckLabels`（read-only）透過 API 檢查全部 6 個 label 是否存在；若有缺少，跳出 overlay confirm dialog 列出缺少的 label，使用者確認後才呼叫 `EnsureLabels` 建立。不做 session 內快取，確保外部刪除 label 後也能即時偵測。透過 `LabelManager` interface（`ListRepoLabels` + `CreateLabel`）實作，ForgejoClient 與 GitHubClient 皆滿足。

---

## 4.3 Profiles 定義

Profile 只存放 **agent prompt 需要的 metadata**。
Build、test、review、開 PR 等執行動作都是 agent 的職責（agent 從各專案 CLAUDE.md 得知 build 指令），
Zpit 不介入 agent 的工作內容。

`log_policy` 會注入到 Coding Agent 和 Reviewer Agent 的 prompt 中，
讓 agent 在實作和 review 時都遵循對應的 logging 規範：

| Profile | log_policy | 說明 |
|---------|-----------|------|
| machine | strict | 所有 Service 方法有進出 log，硬體操作有指令/回應 log，狀態機轉換有前後狀態 log |
| web | minimal | 只 log 錯誤和關鍵操作 |
| desktop | standard | Service 方法有進出 log，異常有完整 log |
| android | standard | 同 desktop |

---

## 4.4 Config Hot-Reload

Zpit 支援在 TUI 運行中重新載入 config.toml。設定欄位分為兩類：

### Hot-Reloadable（即時套用）

| 欄位 | 套用方式 |
|------|---------|
| `language` | 呼叫 `locale.SetLanguage()` |
| `notification.*` | 呼叫 `notifier.UpdateConfig()` |
| `worktree.poll_seconds` / `pr_poll_seconds` / `max_review_rounds` | 呼叫 `wtManager.UpdateConfig()` |
| `terminal.*` | 更新 cfg，下次啟動 agent 時生效 |
| per-project `channel_enabled` | 動態 subscribe/unsubscribe EventBus |
| per-project `channel_listen` | 動態管理跨專案訂閱 |
| per-project `hook_mode` / `base_branch` / `log_level` | 更新 cfg，下次操作時生效 |

### Restart-Required（需重啟）

| 欄位 | 原因 |
|------|------|
| `broker_port` | Port 已綁定 |
| `ssh.*` | SSH server 已綁定 |
| `providers.*` | Tracker client 需重新初始化 |
| 新增/刪除 `[[projects]]` | 需重建 tracker clients 和 UI 狀態 |
| `worktree.base_dir_*` / `dir_format` / `max_per_project` | 影響已進行的 worktree 路徑解析 |

### 重載機制

1. **TUI 內建**：按 `[e]` → `[3]` 用 `$EDITOR` 開啟 config.toml，編輯器關閉後自動重載
2. **手動觸發**：在 `[e]` 子選單中按 `[r]` 手動觸發重載（適用於 SSH 遠端模式）
3. **解析流程**：`config.Reload()` → `config.Diff()` 分類 → `AppState.ApplyConfig()` 套用 hot-reload 欄位，對 restart-required 欄位在 status bar 顯示提示

### 針對性 TOML 寫入

Channel 快速切換（`[1]` toggle / `[2]` listen edit）使用 `internal/config/toml_writer.go` 進行針對性寫入：

- 以行為單位操作，不做完整的 TOML 序列化
- 透過 `id` 欄位定位正確的 `[[projects]]` 區塊
- 僅修改 `channel_enabled` 和 `channel_listen` 行
- 保留檔案其餘內容（包括註解、空行、格式）

### Broker Lazy Start

若啟動時無任何專案啟用 channel（broker 為 nil），使用者透過 `[1]` toggle 首次啟用某專案的 `channel_enabled` 時，`ToggleChannel()` 會延遲啟動 broker。啟動失敗時在 status bar 顯示錯誤，不更新 `channel_enabled`。
