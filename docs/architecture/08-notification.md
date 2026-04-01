# 8. Agent 阻塞與通知機制

---

## 8.1 核心原則：不確定就停下來問

無論 agent 的權限模式是什麼（包括 bypass all permissions），
遇到以下情況必須停下來問你：

- 技術決策不確定（多種實作路徑、不清楚該用哪個 pattern）
- Issue 的 acceptance criteria 描述模糊，無法判斷該怎麼做
- 改動範圍超出 issue 預期（發現需要連帶修改其他模組）
- 涉及 shared-core 的改動
- 硬體相關邏輯不確定（timeout 值、retry 次數、安全狀態定義）

**all permissions ≠ 自己做所有決定**
all permissions 是「允許執行 read/write/bash 等操作不用逐一確認」，
不是「允許 agent 自己決定技術方向」。

---

## 8.2 TUI 通知系統

### 偵測方式

**Session Log 偵測（agent 等待輸入）：**
- Session log 最後一筆 assistant message 的 `stop_reason == "end_turn"` → 等待使用者
- TUI 即時更新活躍終端區域

**Permission 偵測（Notification hook）：**
- `notify-permission.sh` hook 在 Claude Code 需要 tool 權限時觸發
- 寫入信號檔到 `~/.zpit/signals/permission-{sessionID}.json`
- TUI 每 2 秒 tick 掃描信號目錄
- 使用者 approve/deny 後，JSONL 新記錄出現 → 狀態恢復 → 信號檔刪除

### 通知管道

```
Agent 停下來 / 需要權限
    │
    ├─ ① TUI 主畫面警示（即時）
    │   活躍終端區域狀態變更
    │   顯示問題摘要 / permission message
    │   顯示切換指令
    │
    ├─ ② Windows Toast 通知
    │   透過 PowerShell 呼叫 WinRT ToastNotification API
    │   標題: "{專案名} - Agent waiting"
    │   內容: 問題摘要（截斷至 100 字元）
    │
    └─ ③ 音效提示
        Windows: PowerShell SystemSounds.Beep
        Unix: BEL 字元 (\a)
```

**主畫面顯示範例：**

```
╠══════════════════════════════════════════════════════════════════════╣
║  活躍終端                                                          ║
║  [1] ASE 檢測  │ 🟡 等待你回應                            05:32   ║
║      問題: "ReconnectAsync 要用 SemaphoreSlim 還是用       ║
║             現有的 LockObject？"                           ║
║      切換: tmux select-window -t ase-inspection            ║
║  [2] 個人網頁  │ 🟠 等待授權                              00:08   ║
║      P: Claude needs your permission to use Bash           ║
║  [3] Zpit      │ 🟢 實作中: Three.js 場景優化     02:15   ║
╠══════════════════════════════════════════════════════════════════════╣
```

### 通知設定

```toml
[notification]
tui_alert = true          # TUI 主畫面警示
windows_toast = true      # Windows Toast 通知
sound = true              # 音效提示
re_remind_minutes = 15    # 超過 N 分鐘未回應，再次發送提醒
```

實作位於 `internal/notify/`（`notify.go` + 平台特定檔案 `toast_windows.go`/`toast_unix.go`、`sound_windows.go`/`sound_unix.go`）。
