# Windows Toast Notification Silent Suppression Issue

> Investigation date: 2026-04-06

## Symptom

The TUI log continuously recorded `notification sent: key=zpit`, but the user never saw a Windows toast banner pop up and never heard an alert sound.

```
2026/04/06 15:56:17 notification sent: key=zpit
2026/04/06 15:59:57 notification sent: key=zpit
2026/04/06 16:03:07 notification sent: key=zpit
2026/04/06 16:06:24 notification sent: key=zpit
```

## Investigation

### 1. Verify configuration is correct

The user's config (`~/.zpit/config.toml`) had all notification settings enabled:

```toml
[notification]
windows_toast = true
sound = true
sound_file = 'C:\Users\Jeff\Music\zelda-navi-listen.mp3'
re_remind_minutes = 2
```

- The sound file existed and was playable
- Focus Assist / Do Not Disturb was disabled
- Global toast notifications were enabled (`ToastEnabled = 1`)
- No `Enabled = 0` disable flag was present in the registry for the "Zpit" app

### 2. Confirm the code path is executing

The condition in `model.go:845` that logs "notification sent" is that `NotifyWaiting()` returns `true`, meaning the cooldown check passed. The actual toast/sound is dispatched in a goroutine as fire-and-forget:

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

There were no `sendToast failed` or `playSound failed` entries anywhere in the log.

### 3. Manually test toast and sound

Executed the toast and sound commands directly from PowerShell:

- The `sendToast` PowerShell script — ran successfully, `Setting: Enabled`
- The `playSound` MediaPlayer — ran successfully, `HasAudio: True`, `Position` advanced
- Called from Go via `exec.Command` — also succeeded, `err=nil`

All manual tests passed, ruling out an API-level problem.

### 4. Check notification center history

Queried the notification history via WinRT API:

```powershell
$items = [Windows.UI.Notifications.ToastNotificationManager]::History.GetHistory("Zpit")
$items.Count  # → 85
```

**Found 85 toast notifications piled up in the notification center**, none of which had a `Tag` set.

## Root Cause

### Toast notification accumulation causes Windows 11 to silently suppress banners

`sendToast` created `ToastNotification` objects without setting the `Tag` property:

```go
// toast_windows.go (before fix)
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Zpit").Show($toast)
```

Consequences of having no `Tag`:

1. Every `Show()` call **adds** a new entry to the notification center instead of replacing the previous one
2. Notifications accumulate continuously (85 had piled up by the time of this investigation)
3. Once Windows 11 detects a large accumulation of notifications from the same app, it begins **suppressing the banner popup** — toasts still arrive in the notification center but are no longer displayed as pop-up windows
4. The user receives no perceptible notification at all

### Misleading nature of the "notification sent" log

The log entry at `model.go:845` only indicates that the cooldown check passed and the goroutine was launched — it does not indicate that the toast was actually displayed. Even when Windows silently suppresses a toast, `sendToast` still returns `nil` (PowerShell exit code 0) and produces no error log.

## Fix

Added the `Tag` property in `toast_windows.go` so each new notification replaces the previous one:

```go
// toast_windows.go (after fix)
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
$toast.Tag = "agent-waiting"
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Zpit").Show($toast)
```

Effect:

- The notification center always holds **exactly one** Zpit notification (the most recent one)
- No more accumulation → Windows no longer triggers the suppression mechanism
- Banner popups appear normally

The 85 already-accumulated history notifications were also cleared manually:

```powershell
[Windows.UI.Notifications.ToastNotificationManager]::History.Clear("Zpit")
```

## Lessons Learned

1. **Toast notifications must have a `Tag` set** — notifications without a Tag accumulate indefinitely and eventually trigger Windows's silent suppression mechanism
2. **"notification sent" in the log does not mean the notification was delivered** — the result of a fire-and-forget goroutine cannot be confirmed from the call site; a PowerShell exit code of 0 does not mean the banner was actually shown
3. **The Windows notification API does not report suppression status** — `CreateToastNotifier.Setting` returns `Enabled`, `Show()` throws no exception, yet the banner still does not appear; the only way to discover the problem is by inspecting the notification history for an unusually large entry count
