# 8. Agent Blocking and Notification System

---

## 8.1 Core Principle: Stop and Ask When Uncertain

Regardless of the agent's permission mode (including bypass all permissions),
the agent must stop and ask you in the following situations:

- A technical decision is uncertain (multiple implementation paths; unclear which pattern to use)
- Acceptance criteria in the issue are ambiguous and cannot determine the correct approach
- The scope of changes exceeds what the issue anticipated (discovering that other modules also need modification)
- Hardware-related logic is uncertain (timeout values, retry counts, safe-state definitions)

**all permissions ≠ making all decisions autonomously**
all permissions means "allow read/write/bash operations without per-action confirmation" —
it does NOT mean "allow the agent to decide the technical direction on its own."

---

## 8.2 TUI Notification System

### Detection Methods

**Session Log Detection (agent waiting for input):**
- The last assistant message in the session log has `stop_reason == "end_turn"` → waiting for user
- TUI updates the active terminals panel in real time

**Permission Detection (Notification hook):**
- `notify-permission.sh` hook fires when Claude Code needs tool permission
- Writes a signal file to `~/.zpit/signals/permission-{sessionID}.json`
- TUI scans the signal directory on a 2-second tick
- After the user approves/denies, a new JSONL record appears → state recovers → signal file is deleted

### Notification Channels

```
Agent pauses / needs permission
    │
    ├─ ① TUI main screen alert (real-time)
    │   Active terminals panel status change
    │   Displays question summary / permission message
    │   Displays switch command
    │
    ├─ ② Windows Toast notification
    │   Calls WinRT ToastNotification API via PowerShell
    │   Title: "{project name} - Agent waiting"
    │   Body: question summary (truncated to 100 characters)
    │
    └─ ③ Sound alert
        Windows: PowerShell SystemSounds.Beep
        Unix: BEL character (\a)
```

**Main screen display example:**

```
╠══════════════════════════════════════════════════════════════════════╣
║  Active Terminals                                                   ║
║  [1] ASE Inspection  │ 🟡 Waiting for your response        05:32   ║
║      Question: "Should ReconnectAsync use SemaphoreSlim or          ║
║                the existing LockObject?"                    ║
║      Switch: tmux select-window -t ase-inspection           ║
║  [2] Personal Site   │ 🟠 Waiting for permission           00:08   ║
║      P: Claude needs your permission to use Bash            ║
║  [3] Zpit            │ 🟢 Working: Three.js scene optimization 02:15║
╠══════════════════════════════════════════════════════════════════════╣
```

### Notification Settings

```toml
[notification]
tui_alert = true          # TUI main screen alert
windows_toast = true      # Windows Toast notification
sound = true              # Sound alert
# sound_file = "D:/sounds/notify.mp3"  # Custom notification sound path (leave empty to use system default)
re_remind_minutes = 2     # Send a follow-up reminder if no response after N minutes
```

#### Custom Sound Playback

The `sound_file` field allows users to specify a custom notification sound file path. Supported formats: WAV, MP3, M4A, OGG, WMA.

- **Windows**: Loads `PresentationCore` via PowerShell and plays back using `System.Windows.Media.MediaPlayer`; natively supports multiple formats with no additional installation required.
- **Linux**: Tries `mpv` → `ffplay` → `paplay` → `aplay` in order, stopping at the first success. The first two support all major formats; the latter two are limited to WAV/OGG.
- **Empty or unset**: Windows uses `SystemSounds::Asterisk`; Linux uses the freedesktop system sound (current behavior).
- **File not found**: Logs a warning, skips playback, and shows a one-time warning in the TUI via `setStatus`.
- **Timeout**: Uses `exec.CommandContext` with a 5-second timeout to prevent goroutine leaks.

Implementation is in `internal/notify/` (`notify.go` + platform-specific files `toast_windows.go`/`toast_unix.go`, `sound_windows.go`/`sound_unix.go`).
