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

## Current State (M3 Complete)

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
- Hook deployment script (`scripts/setup-hooks.sh`, also deploys agents)
- TrackerBridge: `claude -p` + MCP 統一橋接層（sonnet, $1.00 budget）
- Issue Spec validation (`ValidateIssueSpec`) + parsing (`ParseIssueSpec`)
- `[c]` Clarify: opens new terminal with `claude --agent clarifier` (auto-deploys if missing, huh confirm dialog)
- `[s]` Status: readonly issue list via TrackerBridge + `[y]` confirm (pending→todo) + `[p]` open in browser
- `[p]` Open Tracker: opens project issue tracker in browser
- MCP availability check on startup (warns if MCP server not found)
- Clarifier agent template (`agents/clarifier.md`, embedded via go:embed)
- 28 tracker tests + 22 watcher tests + 6 notify tests

### What's stubbed (shows "coming in MX" message)
- `[l]` Loop → M4
- `[r]` Review → M4
- `[a]` Add Project → M5
- `[e]` Edit Config → M5
- `[?]` Help → TBD

### What's not implemented yet
- Worktree Manager (M4)
- Loop engine (M4)
- Agent prompt assembly (M4)
- Coding/Reviewer agent templates (M4)

## Package Structure

```
main.go                          # Entry point: load config, embed agents, run Bubble Tea
agents/
└── clarifier.md                 # Clarifier agent template (go:embed → auto-deploy)
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
│   └── watcher_test.go          # 22 tests
├── notify/
│   ├── notify.go                # Notifier: cooldown logic + dispatch
│   ├── toast_windows.go         # Windows Toast via PowerShell
│   ├── toast_unix.go            # no-op on non-Windows
│   ├── sound_windows.go         # SystemSounds.Asterisk via PowerShell
│   ├── sound_unix.go            # paplay/aplay fallback
│   └── notify_test.go           # 6 tests
├── tui/
│   ├── model.go                 # Root Bubble Tea model, Update, key routing, confirm dialog (huh)
│   ├── keymap.go                # Key bindings (incl. Back, Confirm)
│   ├── styles.go                # Lip Gloss styles with named color constants
│   ├── view_projects.go         # Main screen: project list + hotkeys + active terminals
│   ├── view_status.go           # Status sub-view: issue list + [y] confirm + [p] browser
│   └── msg.go                   # Custom tea.Msg types (IssuesLoadedMsg, IssueConfirmedMsg, etc.)
└── tracker/
    ├── types.go                 # Issue/PR structs + canonical status constants
    ├── bridge.go                # TrackerBridge: claude -p + MCP 統一橋接
    ├── bridge_test.go           # 11 tests (mock exec)
    ├── issuespec.go             # ValidateIssueSpec + ParseIssueSpec
    ├── issuespec_test.go        # 12 tests
    ├── urls.go                  # BuildIssueURL + BuildTrackerURL
    └── urls_test.go             # 6 tests
hooks/
├── path-guard.sh                # Confine Write/Edit to worktree dir
├── bash-firewall.sh             # Block destructive commands
├── git-guard.sh                 # Block push/merge/rebase; allow commit/status/diff
└── hooks_test.go                # Go tests shelling out to each hook (29 tests)
scripts/setup-hooks.sh           # Deploy hooks + docs + agents to a project's .claude/
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

### TrackerBridge (M3)

Zpit 不直接實作各 tracker 的 HTTP API client。改用 `claude -p`（headless 模式）+ MCP tools 作為統一橋接層：

```
Zpit (Go) → claude -p --model sonnet --output-format json --json-schema ...
                → MCP → gitea/github/plane/linear server
```

- `--json-schema` constrained decoding 確保結構化輸出可靠
- 新增 tracker 只需安裝 MCP server + config 加入 `mcp_server` 欄位
- Config 中 `providers.tracker.*.mcp_server` 對應 `claude mcp add` 的 server name
- Clarifier agent 在終端中直接透過 MCP 推 issue（使用者確認後）
- TUI `[s]` status 透過 TrackerBridge 拉 issue 列表，`[y]` 改狀態

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

Provider entries include `mcp_server` field mapping to the MCP server name (e.g. `mcp_server = "gitea"` → tools prefixed `mcp__gitea__*`).

## Conventions

- Branch naming: `feat/ISSUE-ID-description` or `fix/ISSUE-ID-description`
- Commit messages: `[ISSUE-ID] short description`
- Issue statuses flow: pending_confirm → todo → in_progress → ai_review → waiting_review → (needs_verify) → done
- Agents must stop and ask on uncertain technical decisions, even with bypass-all-permissions enabled
- Hook exit codes: 0 = allow, 2 = block (stderr message fed back to Claude), never use exit 1 for safety hooks
- Code quality baseline: `docs/code-construction-principles.md` — Reviewer agent checks against this
