# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Zpit is a TUI-based AI development cockpit written in Go with Bubble Tea. It acts as a **dispatch center** (not a wrapper) — it selects projects, launches Claude Code agents in separate terminal windows, monitors their progress via session logs, and coordinates the full issue lifecycle from requirement clarification to PR.

Key design principle: Claude Code runs in independent terminal windows. The TUI monitors via fsnotify on session logs (`~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`), never wrapping or embedding Claude Code directly.

## Build & Run

```bash
go build ./...           # Build
go test ./...            # Run all tests
go test ./internal/...   # Run a specific package's tests
go test -run TestName    # Run a single test
go run .                 # Run (reads ~/.config/zpit/config.toml)
ZPIT_CONFIG=./testdata/config.toml go run .  # Run with test config
```

## Current State (M2 Complete)

### What works now
- TUI project list with ↑↓ navigation and profile icons
- Config loading from `~/.config/zpit/config.toml` (TOML, with defaults)
- Environment detection (Windows Terminal / WSL / Linux tmux)
- Terminal Launcher: `Enter` opens Claude Code in new WT tab or tmux window
- `[o]` opens project folder in file manager
- Session Log Watcher: fsnotify monitors JSONL logs, detects agent state (Working/Waiting/Ended)
- Active Terminals area: 🟢 Working / 🟡 Waiting for input / ⚫ Session ended (auto-removes after 10s)
- Agent waiting detection: extracts question text from session log, shows preview in TUI
- Windows Toast notification when agent waits for input (via PowerShell)
- Sound alert (SystemSounds.Asterisk)
- Notification cooldown (re_remind_minutes, per-project)
- Session liveness check: PID monitoring every 10s, detects closed sessions
- 3 PreToolUse hook scripts with 29 tests
- Hook deployment script (`scripts/setup-hooks.sh`)

### What's stubbed (shows "coming in MX" message)
- `[c]` Clarify → M3
- `[l]` Loop → M4
- `[r]` Review → M4
- `[s]` Status → M3
- `[p]` Open Tracker → M3
- `[a]` Add Project → M5
- `[e]` Edit Config → M5
- `[?]` Help → TBD

### What's not implemented yet
- Tracker Provider API clients (M3)
- Issue Spec validation/parsing (M3)
- Worktree Manager (M4)
- Loop engine (M4)
- Agent prompt assembly (M4)

## Package Structure

```
main.go                          # Entry point: load config → run Bubble Tea
internal/
├── config/config.go             # Config structs + Load() + defaults
├── platform/detect.go           # Environment detection + ResolvePath()
├── terminal/
│   ├── launcher.go              # LaunchClaude() dispatch + arg builders
│   ├── launcher_windows.go      # wt.exe (build tag: windows)
│   └── launcher_unix.go         # tmux (build tag: !windows)
├── watcher/
│   ├── encode.go                # EncodeCwd() path encoding + ClaudeHome()
│   ├── parse.go                 # AgentState enum + ParseLine() JSONL parser
│   ├── session.go               # FindActiveSessions() + IsProcessAlive() + LogFilePath()
│   ├── watcher.go               # Watcher: fsnotify tail + WatchOnce()
│   ├── process_windows.go       # PID liveness check (Windows tasklist)
│   ├── process_unix.go          # PID liveness check (Unix signal 0)
│   └── watcher_test.go          # 17 tests: encode, parse, session discovery, state
├── notify/
│   ├── notify.go                # Notifier: cooldown logic + dispatch
│   ├── toast_windows.go         # Windows Toast via PowerShell
│   ├── toast_unix.go            # no-op on non-Windows
│   ├── sound_windows.go         # SystemSounds.Asterisk via PowerShell
│   ├── sound_unix.go            # paplay/aplay fallback
│   └── notify_test.go           # 6 tests: cooldown, reset, config respect
├── tui/
│   ├── model.go                 # Root Bubble Tea model, Update, watcher lifecycle, liveness check
│   ├── keymap.go                # Key bindings
│   ├── styles.go                # Lip Gloss styles with named color constants
│   ├── view_projects.go         # Main screen: project list + hotkeys + active terminals
│   └── msg.go                   # Custom tea.Msg types (AgentEventMsg, TickMsg, etc.)
└── tracker/types.go             # IssueTracker + GitHost interfaces (stubs)
hooks/
├── path-guard.sh                # Confine Write/Edit to worktree dir
├── bash-firewall.sh             # Block destructive commands
├── git-guard.sh                 # Block push/merge/rebase; allow commit/status/diff
└── hooks_test.go                # Go tests shelling out to each hook
scripts/setup-hooks.sh           # Deploy hooks + docs to a project's .claude/
testdata/
├── config.toml                  # Real project config used for tests + manual TUI testing
├── config_minimal.toml          # Minimal config for defaults testing
├── session_working.jsonl        # JSONL fixture: agent working (tool_use)
└── session_waiting.jsonl        # JSONL fixture: agent waiting (end_turn)
docs/
├── zpit-architecture.md         # Full architecture document
└── code-construction-principles.md  # Code quality baseline for agent review
```

## Architecture

### Session Log Monitoring (M2)

The TUI monitors Claude Code sessions via their JSONL log files:

1. **Path encoding**: `EncodeCwd()` converts project path to Claude's directory name (non-alphanumeric → `-`)
2. **Session discovery**: scans `~/.claude/sessions/{pid}.json` for matching project + alive PID
3. **Two-phase startup**: finds session PID immediately (enables liveness check), then waits for JSONL file creation (happens on first user input)
4. **State detection**: parses `stop_reason` from assistant messages — `"end_turn"` = waiting, `"tool_use"` = working
5. **Notifications**: on state transition to waiting → Windows Toast + sound (respects config + cooldown)
6. **Liveness check**: every 10s verifies PID is alive; ended sessions auto-remove after 10s display

### Provider Abstraction

Two key interfaces drive extensibility:

```go
type IssueTracker interface {
    ListIssues, GetIssue, CreateIssue, UpdateStatus, AddComment
}

type GitHost interface {
    CreatePR, GetPRStatus
}
```

Each project in `config.toml` references a tracker and git provider by key. Providers are defined under `[providers.tracker.*]` and `[providers.git.*]`.

### Hook-Based Safety System (5 Layers)

1. **CLAUDE.md behavioral guidelines** (soft)
2. **--allowedTools per agent role** (medium)
3. **PreToolUse hooks** (hard — enforced even with bypass-all-permissions):
   - `path-guard.sh` — Write/Edit confined to worktree dir; denies `.claude/`, `CLAUDE.md`, `.git/`, `.env`
   - `bash-firewall.sh` — blocks destructive commands (rm -rf, curl|bash, force push, etc.)
   - `git-guard.sh` — blocks push, merge, rebase, branch delete; agents only commit
4. **Git worktree isolation** (physical)
5. **Human PR review** (final gate)

Hook strictness is per-project via `hook_mode`: `strict` (all hooks), `standard` (path-guard + git-guard), `relaxed` (git-guard only). Applied via `settings.local.json` overlay at worktree creation.

## Config Location

`~/.config/zpit/config.toml` — terminal settings, notification preferences, provider credentials (via env var references), and all project definitions. Override with `ZPIT_CONFIG` env var.

## Conventions

- Branch naming: `feat/ISSUE-ID-description` or `fix/ISSUE-ID-description`
- Commit messages: `[ISSUE-ID] short description`
- Issue statuses flow: pending_confirm → todo → in_progress → ai_review → waiting_review → (needs_verify) → done
- Agents must stop and ask on uncertain technical decisions, even with bypass-all-permissions enabled
- Hook exit codes: 0 = allow, 2 = block (stderr message fed back to Claude), never use exit 1 for safety hooks
- Code quality baseline: `docs/code-construction-principles.md` — Reviewer agent checks against this
