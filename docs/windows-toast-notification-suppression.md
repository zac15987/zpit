# Windows Toast Notification 靜默壓制問題

> 調查日期：2026-04-06

## 現象

TUI log 持續記錄 `notification sent: key=zpit`，但使用者從未看到 Windows toast banner 彈出，也沒有聽到提示音。

```
2026/04/06 15:56:17 notification sent: key=zpit
2026/04/06 15:59:57 notification sent: key=zpit
2026/04/06 16:03:07 notification sent: key=zpit
2026/04/06 16:06:24 notification sent: key=zpit
```

## 調查過程

### 1. 確認設定正確

使用者 config (`~/.zpit/config.toml`) 通知設定皆已啟用：

```toml
[notification]
windows_toast = true
sound = true
sound_file = 'C:\Users\Jeff\Music\zelda-navi-listen.mp3'
re_remind_minutes = 2
```

- 音效檔案存在且可播放
- Focus Assist / Do Not Disturb 為關閉狀態
- 全域 toast 通知已啟用 (`ToastEnabled = 1`)
- "Zpit" app 的 registry 無 `Enabled = 0` 停用旗標

### 2. 確認程式碼路徑有執行

`model.go:845` 記錄 "notification sent" 的條件是 `NotifyWaiting()` 回傳 `true`，代表 cooldown 檢查通過。實際的 toast/sound 在 goroutine 中以 fire-and-forget 方式執行：

```go
// notify.go — NotifyWaiting()
if n.cfg.WindowsToast {
    go func() {
        if err := sendToast(projectName, questionText); err != nil {
            n.logger.Printf("sendToast failed: ...")
        }
    }()
}
```

Log 中沒有任何 `sendToast failed` 或 `playSound failed` 記錄。

### 3. 手動測試 toast 與 sound

從 PowerShell 直接執行 toast 與 sound 指令：

- `sendToast` 的 PowerShell script — 執行成功，`Setting: Enabled`
- `playSound` 的 MediaPlayer — 執行成功，`HasAudio: True`，`Position` 有前進
- 從 Go `exec.Command` 呼叫 — 同樣成功，`err=nil`

手動測試皆通過，排除 API 層面問題。

### 4. 檢查 notification center 歷史記錄

透過 WinRT API 查詢 notification history：

```powershell
$items = [Windows.UI.Notifications.ToastNotificationManager]::History.GetHistory("Zpit")
$items.Count  # → 85
```

**發現 85 筆 toast notification 堆積在 notification center**，全部沒有設定 `Tag`。

## 根因

### Toast notification 堆積導致 Windows 11 靜默壓制 banner

`sendToast` 建立 `ToastNotification` 時沒有設定 `Tag` 屬性：

```go
// toast_windows.go（修改前）
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Zpit").Show($toast)
```

沒有 `Tag` 的結果：

1. 每次 `Show()` 都在 notification center **新增**一筆，而非取代舊的
2. 通知持續堆積（本次調查時已累積 85 筆）
3. Windows 11 偵測到同一 app 大量堆積通知後，開始**壓制 banner popup** — toast 仍然送達 notification center，但不再以彈出視窗形式顯示
4. 使用者完全沒有感知到通知

### 「notification sent」log 的誤導性

`model.go:845` 的 log 只代表 cooldown 檢查通過並啟動了 goroutine，不代表 toast 實際顯示成功。即使 toast 被 Windows 靜默壓制，`sendToast` 仍然回傳 `nil`（PowerShell exit code 0），不會產生錯誤 log。

## 修復

在 `toast_windows.go` 加上 `Tag` 屬性，使每次新通知取代上一次的通知：

```go
// toast_windows.go（修改後）
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
$toast.Tag = "agent-waiting"
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Zpit").Show($toast)
```

效果：

- Notification center 中永遠只有**一筆** Zpit 通知（最新的那筆）
- 不再堆積 → Windows 不會觸發壓制機制
- Banner popup 正常彈出

同時手動清除了已堆積的 85 筆歷史通知：

```powershell
[Windows.UI.Notifications.ToastNotificationManager]::History.Clear("Zpit")
```

## 經驗教訓

1. **Toast notification 必須設定 `Tag`** — 無 Tag 的通知會無限堆積，最終觸發 Windows 的靜默壓制
2. **「notification sent」log 不等於通知已送達** — fire-and-forget goroutine 的結果無法從呼叫端確認；PowerShell exit code 0 也不代表 banner 實際顯示
3. **Windows notification API 不會回報壓制狀態** — `CreateToastNotifier.Setting` 回傳 `Enabled`、`Show()` 不拋例外，但 banner 仍然不顯示，只能透過檢查 notification history 堆積數量來發現問題
