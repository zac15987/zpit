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
make test-hooks          # Run hook tests (requires bash)
make test-all            # Run all tests including hooks
go run .                 # Run local TUI (reads ~/.zpit/config.toml)
go run . serve           # Start headless SSH server (Wish)
go run . connect         # SSH connect to local server (convenience wrapper)
ZPIT_CONFIG=./testdata/config.toml go run .  # Run with test config
```

## Package Structure

```
main.go                  # Entry point: subcommand routing, go:embed declarations, config/log init
agents/                  # Agent templates (clarifier.md, reviewer.md) — embedded via go:embed
hooks/                   # PreToolUse hook scripts — embedded via go:embed, + hook_test.go
docs/                    # Agent behavioral rules, code quality baseline, LLM failure mode research
docs/architecture/       # Architecture docs (split by topic) — see docs/architecture/README.md for index
scripts/                 # Manual hook deployment fallback (setup-hooks.sh)
testdata/                # Config fixtures + JSONL session fixtures
internal/
├── broker/              # HTTP event broker for cross-agent channel communication
├── config/              # Config structs, Load(), defaults, BaseDir(), WriteTemplate()
├── locale/              # i18n: SetLanguage(), T(), ResponseInstruction() — en + zh-TW
├── loop/                # Loop state machine types: SlotState enum, Slot, LoopState
├── mcp/                 # MCP stdio server for agent↔broker communication (zpit serve-channel)
├── notify/              # Notification dispatch: cooldown logic, Windows Toast, sound alerts
├── platform/            # Environment detection (Windows Terminal / WSL / tmux), ResolvePath()
├── prompt/              # Prompt assembly: BuildCodingPrompt, BuildReviewerPrompt, BuildRevisionPrompt
├── ssh/                 # Wish SSH server: StartServer(), auth config (pub-key + password)
├── terminal/            # LaunchClaude() dispatch + platform-specific launchers (wt.exe / tmux)
├── tracker/             # TrackerClient interface: ForgejoClient + GitHubClient REST abstractions
├── watcher/             # Session log monitoring: EncodeCwd, ParseLine, FindActiveSessions, Watcher
├── worktree/            # Worktree Manager, Slugify(), DeployHooks(), settings.json merge
└── tui/                 # Bubble Tea TUI — see "TUI Message Flow" below
    ├── appstate.go      # AppState struct, RWMutex, Subscribe/NotifyAll pub/sub
    ├── keymap.go        # Key bindings definition (Help, Channel, etc.)
    ├── model.go         # Root Model, Update (key routing + message dispatch), View routing
    ├── msg.go           # All custom tea.Msg types
    ├── loop_cmds.go     # Loop tea.Cmd functions (poll, create worktree, launch, cleanup)
    ├── loop_handler.go  # Loop message handlers (state machine transitions)
    ├── view_channel.go  # Channel event timeline view ([m] key)
    ├── view_projects.go # Main screen rendering
    ├── view_status.go   # Issue list sub-view
    └── validate.go      # Input validation helpers with RLock
```

## Architecture

### go:embed Deployment Flow

Agents, hooks, and docs are embedded in the binary and deployed at runtime to target projects:

```
main.go (go:embed vars)
  → NewAppState(cfg, clarifierMD, reviewerMD, guidelinesMD, principlesMD, hookScripts)
    → stored in AppState fields
      → DeployHooks() on every agent launch ([c]/[r]/[l])
        → writes to target project's .claude/hooks/, .claude/agents/, .claude/docs/
        → merges hook config into .claude/settings.json (or settings.local.json for worktrees)
```

This means changes to `agents/*.md`, `hooks/*.sh`, or `docs/agent-guidelines.md` require a rebuild to take effect.

### TUI Message Flow (Bubble Tea Pattern)

The TUI follows Bubble Tea's Elm architecture with a consistent pattern across all features:

1. **msg.go** — defines all `tea.Msg` types (data carriers, no logic)
2. **loop_cmds.go** / model.go — `tea.Cmd` functions that perform async work (API calls, file I/O, polling), return messages. These acquire `RLock` for reads.
3. **loop_handler.go** / model.go `Update()` — message handlers that mutate state and return next commands. These acquire `Lock` for writes + call `NotifyAll`.
4. **view_projects.go** / view_status.go — pure rendering functions, acquire `RLock` for reads.

Loop engine example: `loopPollCmd` (cmd) → `LoopPollMsg` (msg) → handler creates worktree cmd → `LoopWorktreeCreatedMsg` → handler launches agent → `LoopAgentLaunchedMsg` → handler starts label polling → `LoopLabelPollMsg` → handler detects "review" label → launches reviewer...

### Session Log Monitoring

The TUI monitors Claude Code sessions via JSONL log files:

1. `EncodeCwd()` converts project path to Claude's directory name (non-alphanumeric → `-`)
2. Session discovery: scans `~/.claude/sessions/{pid}.json` for matching project + alive PID
3. Two-phase startup: finds PID immediately (enables liveness check), then waits for JSONL file creation
4. State detection: parses `stop_reason` from assistant messages — `"end_turn"` = waiting, `"tool_use"` = working
5. Permission detection: `notify-permission.sh` hook writes signal file to `~/.zpit/signals/`, TUI polls every 2s
6. Liveness check every 5s; `/resume` detection re-reads `{pid}.json` for session ID changes

### AppState + Multi-Client Architecture

`AppState` (`internal/tui/appstate.go`) holds all shared mutable state. Multiple `tea.Program` instances share one `*AppState`:

```
zpit serve
  └─ AppState (one instance)
       ├─ cfg, env, clients, broker   (read-only after init — no locks needed)
       ├─ activeTerminals, loops,    (mutable — protected by sync.RWMutex)
       │  channelEvents, channelSubs
       └─ Subscribe/NotifyAll        (pub/sub — separate sync.Mutex)
            ├─ SSH Client A → Model { state: *AppState, isRemote: true }
            ├─ SSH Client B → Model { state: *AppState, isRemote: true }
            └─ Local TUI    → Model { state: *AppState, isRemote: false }
```

**Concurrency model:**
- Two independent mutexes: `mu` (RWMutex) for state, `subMu` (Mutex) for subscribers — avoids deadlock when `NotifyAll` is called while `mu` is held
- Copy-before-closure pattern: cmd closures never hold references to AppState fields; mutable data copied to locals before lock release
- Action-defer pattern: handlers collect actions under write lock, create cmds after unlock to avoid nested lock acquisition
- Buffered channel (size 1): coalesces rapid state changes into single notification per subscriber

### Cross-Agent Channel (Broker + MCP)

When `channel_enabled = true` for a project, agents can communicate in real time via a local HTTP broker:

```
Agent A (Claude Code)          Agent B (Claude Code)
  └─ MCP stdio server            └─ MCP stdio server
       ↓ HTTP POST                     ↓ HTTP POST
     ┌──────────────────────────────────────┐
     │ Broker (HTTP on 127.0.0.1:broker_port)│
     │ ├─ /api/artifacts/{project}/{issue}  │
     │ ├─ /api/messages/{project}/{to}      │
     │ └─ /api/events/{project} (SSE)       │
     └──────────────────────────────────────┘
       ↓ EventBus (in-memory pub/sub)
     TUI: AppState.channelEvents → ViewChannel ([m] key)
```

**Broker** (`internal/broker/`): Lightweight HTTP server with REST endpoints for artifacts + messages, plus SSE streaming. In-memory storage, non-blocking publish (buffered channels, drop-on-full). Started in `NewAppState()` only if any project has `channel_enabled`.

**MCP Server** (`internal/mcp/`): Stdio server invoked by agents via `.mcp.json`. Exposes three tools: `publish_artifact`, `list_artifacts`, `send_message`. Maintains a background SSE listener that forwards events as MCP notifications, with self-echo filtering. Entry point: `zpit serve-channel` subcommand.

**TUI integration**: Loop start calls `loopChannelSubscribeCmd()` → subscribes to EventBus → `channelReadNextCmd()` blocks on channel → `ChannelEventMsg` appended to `AppState.channelEvents[projectID]` → `ViewChannel` renders timeline.

**Config**: `channel_enabled` (per-project), `broker_port` (global, default 17731), `zpit_bin` (global, explicit binary path for `.mcp.json` generation).

### TrackerClient

Dual-backend REST API abstraction (`internal/tracker/`):

```
TrackerClient interface
  ├─ ForgejoClient → Forgejo/Gitea REST API v1
  └─ GitHubClient  → GitHub REST API
```

- Auth via `token_env` (env var name, never stored directly)
- TUI uses TrackerClient for status display + label polling
- Agents interact with trackers via MCP (separate from TrackerClient)

### Loop Engine State Machine

The loop automates: poll todo → create worktree → coding agent → reviewer → PR merge → cleanup.

State transitions are **label-driven** (poll issue labels, not PID monitoring). Agents set labels to signal completion:
- Coding agent sets `review` → reviewer starts
- Reviewer sets `ai-review` (PASS) or `needs-changes` (auto-retry up to `max_review_rounds`)

States defined in `internal/loop/types.go`: `SlotCreatingWorktree` → `SlotCoding` → `SlotReviewing` → `SlotWaitingPRMerge` → `SlotCleaningUp` → `SlotDone`. Error/human-intervention states: `SlotNeedsHuman`, `SlotError`.

### Hook-Based Safety System (5 Layers)

1. **agent-guidelines.md** (soft — deployed to `.claude/docs/`, agents read on startup)
2. **--allowedTools per agent role** (medium — Claude Code enforced)
3. **PreToolUse hooks** (hard — enforced even with `--bypass-all-permissions`):
   - `path-guard.sh` — Write/Edit confined to worktree dir; denies `.claude/`, `CLAUDE.md`, `.git/`, `.env`
   - `bash-firewall.sh` — blocks destructive commands (rm -rf, curl|bash, force push, etc.)
   - `git-guard.sh` — push whitelist (only `feat/*`), blocks merge/rebase/branch-delete
   - `notify-permission.sh` — not safety; writes signal file for TUI permission detection
4. **Git worktree isolation** (physical)
5. **Human PR review** (final gate)

Hook strictness per-project via `hook_mode`: `strict` (all hooks), `standard` (path-guard + git-guard), `relaxed` (git-guard only).

**ZPIT_AGENT=1**: Hook scripts check this env var — if absent, they `exit 0` (allow everything). This ensures hooks only restrict Zpit-launched agents, not plain Claude Code sessions. On Windows, injected via `zpit-env.cmd` wrapper; on Unix, inline-prefixed to command.

## Config

`~/.zpit/config.toml` — override with `ZPIT_CONFIG` env var. First run auto-creates a template.

Logs: `~/.zpit/logs/zpit-YYYY-MM-DD.log` — daily rotation, 30-day retention.

See `testdata/config.toml` for a working example and `README.md` for full config reference.

## Conventions

- **Branch naming**: `feat/ISSUE-ID-slug` — Loop always uses `feat/` prefix; PR title classification (feat/fix) decided by agent
- **Per-issue branch control**: Issue Spec `## BRANCH` specifies PR target branch (optional, falls back to project `base_branch`)
- **Git model**: `main` ← `dev` ← feature branches
- **Commit messages**: `[ISSUE-ID] short description`
- **Issue status flow**: pending_confirm → todo → in_progress → ai_review → waiting_review → done
- **Loop label flow**: todo → wip → review → ai-review (PASS) / needs-changes (auto-retry)
- **Hook exit codes**: 0 = allow, 2 = block (stderr fed back to Claude), never use exit 1
- **Agent docs**: `docs/agent-guidelines.md` (behavioral rules), `docs/code-construction-principles.md` (quality baseline)
- **Logging**: Use `m.state.logger` for all state transitions and lifecycle events (not `setStatus`, which is TUI-only). Include identifiers (key, PID, state, issue ID, role, round). In goroutine closures, capture `logger := m.state.logger` before use. Do not log ticks or renders.
- **i18n**: All user-facing strings in TUI views must go through `locale.T()`. Never hardcode display text — define a key in `internal/locale/keys.go`, add translations in `en.go` and `zh_tw.go`.
- **Concurrency**: All mutations to AppState mutable fields (`activeTerminals`, `loops`, `channelEvents`, `channelSubs`, `lastLivenessCheck`, `lastPermissionCheck`) must hold `m.state.Lock()`; reads must hold `m.state.RLock()`. Call `m.state.NotifyAll()` after mutations. Never hold `mu` when calling cmd methods that acquire their own `RLock` — use action-defer pattern.
