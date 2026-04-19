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
go run .                 # Run local TUI (or auto-serve if ssh.auto_serve=true)
go run . serve           # Start headless SSH server (Wish)
go run . connect         # SSH connect to local server (convenience wrapper)
ZPIT_CONFIG=./testdata/config.toml go run .  # Run with test config
```

## Package Structure

```
main.go                  # Entry point: subcommand routing, go:embed declarations, config/log init
agents/                  # Agent templates (clarifier.md, reviewer.md, task-runner.md, efficiency.md) — embedded via go:embed
build.cmd, build.sh      # Fetch-pull-build-install convenience scripts
hooks/                   # PreToolUse hook scripts + env/exit wrappers — embedded via go:embed, + hooks_test.go
docs/                    # Agent behavioral rules, code quality baseline, known issues, release planning, research notes
docs/architecture/       # Architecture docs (split by topic) — see docs/architecture/README.md for index
scripts/                 # Manual hook deployment fallback (setup-hooks.sh)
testdata/                # Config fixtures + JSONL session fixtures
internal/
├── broker/              # HTTP event broker for cross-agent channel communication
├── config/              # Config structs, Load(), Reload(), Diff(), defaults, BaseDir(), WriteTemplate(), DefaultConfigPath()
├── git/                 # Git ops wrappers: Fetch, Pull, LogGraph, Branches + parsers
├── locale/              # i18n: SetLanguage(), T(), ResponseInstruction() — en + zh-TW
├── loop/                # Loop state machine types: SlotState enum, Slot, LoopState
├── mcp/                 # MCP stdio server for agent↔broker communication (zpit serve-channel)
├── notify/              # Notification dispatch: cooldown logic, Windows Toast, sound alerts
├── platform/            # Environment detection (Windows Terminal / WSL / tmux), ResolvePath()
├── prompt/              # Prompt assembly: BuildCodingPrompt (subagent/team delegation), BuildReviewerPrompt, BuildRevisionPrompt
├── ssh/                 # Wish SSH server: StartServerAsync(), ServerHandle, StartServer(), auth config
├── terminal/            # LaunchClaude() dispatch + platform-specific launchers (wt.exe / tmux)
├── tracker/             # TrackerClient interface: ForgejoClient + GitHubClient REST abstractions
├── watcher/             # Session log monitoring: EncodeCwd, ParseLine, FindActiveSessions, Watcher
├── worktree/            # Worktree Manager, Slugify(), DeployHooksToProject(), DeployHooksToWorktree(), settings.json merge
└── tui/                 # Bubble Tea TUI — see "TUI Message Flow" below
    ├── appstate.go      # AppState struct, RWMutex, Subscribe/NotifyAll pub/sub
    ├── channel.go       # Channel EventBus subscription and event reading
    ├── confirm.go       # Confirm dialogs, executePendingOp, undeploy, redeploy
    ├── editconfig.go    # Edit config sub-menu: channel toggle, listen edit, $EDITOR launch
    ├── keymap.go        # Key bindings definition (Help, Channel, etc.)
    ├── launch.go        # Terminal launch cmds, slot operations, deploy helpers
    ├── loop_cmds.go     # Loop tea.Cmd functions (poll, create worktree, launch, cleanup)
    ├── loop_handler.go  # Loop message handlers (state machine transitions)
    ├── model.go         # Root Model, Init, Update (one-line dispatch), View routing, key handlers
    ├── msg.go           # All custom tea.Msg types
    ├── styles.go        # Color palette and lipgloss style definitions used across all views
    ├── session.go       # Session lifecycle, discovery, monitoring, liveness, permission detection
    ├── tracker_ops.go   # Label check/ensure, issue load/confirm, label check flow
    ├── validate.go      # Input validation helpers with RLock
    ├── view_channel.go  # Channel event timeline view ([m] key)
    ├── view_editconfig.go # Edit config sub-menu rendering + channel_listen multi-select
    ├── view_projects.go # Main screen rendering
    └── view_status.go   # Issue list sub-view
```

## Architecture

### go:embed Deployment Flow

Agents, hooks, and docs are embedded in the binary and deployed at runtime to target projects:

```
main.go (go:embed vars)
  → NewAppState(cfg, clarifierMD, reviewerMD, taskRunnerMD, efficiencyMD, guidelinesMD, principlesMD, hookScripts, logWriter)
    → stored in AppState fields
      → DeployHooksToProject()/DeployHooksToWorktree() on every agent launch ([c]/[r]/[l]) or redeploy ([d]) — [f] uses deployAndLaunchAgentLite (no hooks)
        → writes to target project's .claude/hooks/, .claude/agents/, .claude/docs/
        → merges hook config into .claude/settings.json (or settings.local.json for worktrees)
      → loopWriteAgentCmd() deploys task-runner.md when Issue Spec contains TASKS
```

This means changes to `agents/*.md`, `hooks/*.sh`, or `docs/agent-guidelines.md` require a rebuild to take effect.

### TUI Message Flow (Bubble Tea Pattern)

The TUI follows Bubble Tea's Elm architecture with a consistent pattern across all features:

1. **msg.go** — defines all `tea.Msg` types (data carriers, no logic)
2. **loop_cmds.go** / session.go / launch.go / tracker_ops.go / channel.go — `tea.Cmd` functions that perform async work (API calls, file I/O, polling), return messages. These acquire `RLock` for reads.
3. **loop_handler.go** / session.go / launch.go / tracker_ops.go / confirm.go / channel.go — message handlers dispatched from model.go `Update()` (one-line dispatch). These acquire `Lock` for writes + call `NotifyAll`.
4. **view_projects.go** / view_status.go / view_channel.go — pure rendering functions, acquire `RLock` for reads.

Loop engine example: `loopPollCmd` (cmd) → `LoopPollMsg` (msg) → handler creates worktree cmd → `LoopWorktreeCreatedMsg` → handler writes agent file → `LoopAgentWrittenMsg` → handler launches agent → `LoopAgentLaunchedMsg` → handler starts label polling → `LoopLabelPollMsg` → handler detects "review" label → launches reviewer...

**Tick-driven poll heartbeat**: The three periodic poll chains (todo poll, PR poll, label poll) use `tea.Tick` with reschedule logic owned by the *tick* handlers (`handleLoopPollTick` / `handleLoopPRPollTick` / `handleLoopLabelPollTick` in `loop_handler.go`), not the business handlers (`handleLoopPoll` / `handleLoopPRStatus` / `handleLoopLabelPoll`). Each tick handler checks a gate (loop `Active` + slot in the expected state) and returns `tea.Batch(pollCmd, scheduleNextTick)` up-front, so any nil/error path in the poll cmd or business handler cannot silently break the chain. Business handlers must NOT call `loopSchedule{Poll,PRPoll,LabelPoll}` mid-chain — those functions are reserved for **kickoff** (loop start, state transitions into a new polling state, resume). Adding a new `return m, m.loopSchedule*Poll(...)` inside a business handler would create two competing tick chains; the regression tests in `loop_tick_test.go` guard this invariant.

**Focus panel system + Dock layout**: The main view (`ViewProjects`) uses a lazygit-style dock — left column stacks Projects / Active Terminals / Loop Engine; right column is the Hotkeys reference panel. Each panel owns its own `viewport.Model` (`projectsVP` / `terminalsVP` / `loopVP` / `hotkeysVP` on `Model`) so scrolling is independent; only the focused panel reacts to `↑↓/PgUp/PgDn`, and mouse-wheel dispatches to whichever panel the cursor hovers via `hitTestDockPanel`. Three panels are focusable via `FocusedPanel` enum (`FocusProjects` → `FocusTerminals` → `FocusLoopSlots`); Tab cycles through those with content (terminals skipped if no active terminals, loop skipped if no loop slots) and Hotkeys stays docked but non-focusable. Each focusable panel has its own cursor (`cursor` for projects, `termCursor`, `loopCursor`); terminals/loop rebuild `termLineStarts` / `loopLineStarts` during sync to drive variable-stride cursor-follow. The `x` key kills the selected terminal when `FocusTerminals` is active (with confirm dialog, force-kills the process). Visual treatment: Catppuccin Mocha palette (see `styles.go`), single-column `▎` mauve bar on the focused panel's chrome only — it is *not* rendered on body rows. Panels stacked below the first in a column get a 1-row gutter (`panelChromeRows(stacked)` returns 3 vs 2). Layout sizing lives in `computePanelRects` (70/30 ideal split, min widths `dockMinLeftWidth`/`dockMinRightWidth`, weight-based height split); `TestComputePanelRects` is the regression guard. `FocusedPanel` is per-Model (per-connection UI state), not shared in AppState.

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
zpit serve  (or zpit with auto_serve=true)
  └─ AppState (one instance)
       ├─ cfg, env, clients, broker   (read-only after init — no locks needed)
       ├─ activeTerminals, loops,    (mutable — protected by sync.RWMutex)
       │  channelEvents, channelSubs
       └─ Subscribe/NotifyAll        (pub/sub — separate sync.Mutex)
            ├─ SSH Client A → Model { state: *AppState, isRemote: true }
            ├─ SSH Client B → Model { state: *AppState, isRemote: true }
            └─ Local TUI    → Model { state: *AppState, isRemote: false }
```

**Auto-serve mode** (`ssh.auto_serve = true`): When `zpit` is run without subcommand and `auto_serve` is enabled, it automatically starts the SSH server in-process via `StartServerAsync()`, then connects to itself via `ssh localhost -p <port>`. The user sees the same TUI but over SSH — allowing seamless mobile access. When the local SSH session ends, the server shuts down automatically. Implementation: `runAutoServe()` in `main.go`, which uses `ServerHandle` from `internal/ssh/server.go` for lifecycle management.

**Concurrency model:**
- Two independent mutexes: `mu` (RWMutex) for state, `subMu` (Mutex) for subscribers — avoids deadlock when `NotifyAll` is called while `mu` is held
- Copy-before-closure pattern: cmd closures never hold references to AppState fields; mutable data copied to locals before lock release
- Action-defer pattern: handlers collect actions under write lock, create cmds after unlock to avoid nested lock acquisition
- Buffered channel (size 1): coalesces rapid state changes into single notification per subscriber

### Config Hot-Reload

The `[e]` key opens a sub-menu for config editing:
- `[1]` Toggle channel — instant on/off for `channel_enabled` with broker lazy start
- `[2]` Edit channel_listen — multi-select list of other projects + `_global`
- `[3]` Open config in editor — `$EDITOR` launch via `tea.ExecProcess`, auto-reload on close

**Hot-reloadable fields** (applied immediately): `language`, `notification.*`, `worktree.poll_seconds/pr_poll_seconds/max_review_rounds`, `terminal.*`, `agent_models.*` (picked up on the next agent launch — already-running sessions keep their original model), per-project `channel_enabled/channel_listen/hook_mode/base_branch/log_policy`.

**Restart-required fields** (status bar warning): `broker_port`, `ssh.*` (including `auto_serve`), `providers.*`, new/removed `[[projects]]`, `worktree.base_dir_*/dir_format/max_per_project`.

Channel quick-toggle uses targeted TOML writing (`internal/config/toml_writer.go`) — locates the matching `[[projects]]` block by `id` and updates only the `channel_enabled` or `channel_listen` line, preserving all other content including comments.

SSH remote mode: `[3]` shows the config file path instead of launching an editor; `[r]` triggers manual reload.

### Cross-Agent Channel (Broker + MCP)

When `channel_enabled = true` for a project, agents can communicate in real time via a local HTTP broker. Supports same-project, cross-project, and global broadcast communication:

```
Agent A (Project X)            Agent B (Project Y)
  └─ MCP stdio server            └─ MCP stdio server
       ↓ HTTP POST                     ↓ HTTP POST
     ┌──────────────────────────────────────────┐
     │ Broker (HTTP on 127.0.0.1:broker_port)   │
     │ ├─ POST /api/artifacts/{project}/{issue_id} │
     │ ├─ GET  /api/artifacts/{project}           │
     │ ├─ POST /api/messages/{project}/{to}       │
     │ ├─ GET  /api/messages/{project}/{issue_id} │
     │ ├─ GET  /api/events/{project} (SSE)        │
     │ └─ GET  /api/projects (discovery)          │
     └──────────────────────────────────────────┘
       ↓ EventBus (in-memory pub/sub, keyed by project)
     TUI: AppState.channelEvents → ViewChannel ([m] key)
```

**Cross-project targeting model** — agents choose communication scope via `target_project`:

| `target_project` | `to` | Effect |
|---|---|---|
| omitted (default) | `"3"` | Same project, specific issue |
| `"project-a"` | `"5"` | Cross-project, specific issue |
| `"project-a"` | `"_project"` | Broadcast to all agents in target project |
| `"_global"` | `"_all"` | Global broadcast to all listening agents |

`_global` and cross-project keys are regular project keys in the EventBus — no special broker logic.

**Broker** (`internal/broker/`): Lightweight HTTP server with REST endpoints for artifacts + messages, SSE streaming, and project discovery. In-memory storage, non-blocking publish (buffered channels, drop-on-full). Tracks SSE connections per project per agent type for discovery. SSE endpoint accepts optional `?agent_type=X` query parameter. Started in `NewAppState()` only if any project has `channel_enabled`.

**AgentName**: Each agent gets a human-readable name (`AgentName` field on `Message` and `Artifact` structs, json tag `agent_name`). Format: `{type}-{4hex}` for manual launches (e.g. `clarifier-a3f7`, `efficiency-a3f7`), `{role}-#{issueID}` for loop launches (e.g. `coding-#42`). Generated by TUI at launch time via `crypto/rand`, passed through `ZPIT_AGENT_NAME` env var → `ServerConfig.AgentName` → HTTP POST body → broker storage → SSE → Channel view display as `[agent-name]` tag.

**MCP Server** (`internal/mcp/`): Stdio server invoked by agents via `.mcp.json`. Exposes seven tools: `publish_artifact`, `list_artifacts`, `send_message`, `list_projects`, `subscribe_project`, `unsubscribe_project`, `list_subscriptions`. Tools accept optional `target_project` parameter for cross-project communication. Includes `AgentName` in HTTP POST bodies for `publish_artifact` and `send_message`. Spawns one SSE listener goroutine per subscribed project (own + `ListenProjects`), with self-echo filtering via per-instance UUID. Supports runtime dynamic subscription management via `subscribe_project`/`unsubscribe_project`/`list_subscriptions` tools (per-project context with mutex-protected cancel map). Entry point: `zpit serve-channel` subcommand.

**Meeting Protocol**: When multiple clarifier agents are launched for the same project, they auto-discover each other via `list_projects` (checking `agents.clarifier` count) and enter meeting mode with Facilitator/Advisor roles. The first agent to broadcast `[Joining Meeting]` becomes Facilitator (drives the workflow, asks questions, drafts issues); subsequent agents become Advisors (provide analysis, follow Facilitator's rhythm). See `agents/clarifier.md` Meeting Protocol section for the full role assignment rules and message format.

**TUI integration**: Loop start / manual launch calls `channelSubscribeCmd()` for own project + each `channel_listen` entry → subscribes to EventBus → `channelReadNextCmd()` blocks on channel → `ChannelEventMsg` appended to `AppState.channelEvents[projectID]` → `ViewChannel` merges events from own + listen projects, sorted by timestamp, with `[source]` tag for cross-project events. Loop stop unsubscribes all related channels (own + listen).

**Config**: `channel_enabled` (per-project), `channel_listen` (per-project, list of additional project keys to subscribe, e.g. `["_global"]`), `broker_port` (global, default 17731), `zpit_bin` (global, explicit binary path for `.mcp.json` generation). Env var `ZPIT_LISTEN_PROJECTS` (comma-separated) passes listen config to MCP server. Env var `ZPIT_AGENT_NAME` passes the generated agent name to MCP server. Env var `ZPIT_AGENT_TYPE` passes the agent type (e.g. `clarifier`, `coding`, `reviewer`, `efficiency`, `claude`) to MCP server for SSE registration.

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

States defined in `internal/loop/types.go`: `SlotCreatingWorktree` → `SlotWritingAgent` → `SlotLaunchingCoder` → `SlotCoding` → `SlotLaunchingReviewer` → `SlotReviewing` → `SlotWaitingPRMerge` → `SlotCleaningUp` → `SlotDone`. Error/human-intervention states: `SlotNeedsHuman`, `SlotError`.

### Task Execution Model (Subagent + Agent Teams)

When an Issue Spec contains `## TASKS`, the coding agent acts as an **orchestrator** — it delegates each task to a `task-runner` subagent instead of implementing tasks itself. This provides context isolation between tasks.

**Execution strategy:**
- **Sequential tasks** (no `[P]`): Delegated one at a time to `task-runner` subagent via the Agent tool. Each subagent runs in its own context window, implements the task, and commits.
- **Parallel tasks** (`[P]` marked): Consecutive `[P]` tasks form a parallel batch. When all dependencies for the batch are satisfied, the orchestrator creates an Agent Team with one teammate per task (each using `task-runner` subagent type). Teammates work in parallel. **Marking rule:** adjacent tasks sharing the same dependency set and touching different files must ALL be `[P]`; omitting `[P]` on any one breaks the batch.
- **Mixed**: Groups execute in dependency order — sequential tasks use subagent, parallel groups use Agent Team.
- **No tasks**: `buildStandardWorkflow()` generates the same prompt as before (no delegation).

**Prompt generation** (`internal/prompt/coding.go`):
- `groupTasks()` partitions tasks into sequential singletons and parallel batches
- `buildSubagentDelegation()` generates Agent tool delegation instructions
- `buildTeamDelegation()` generates Agent Team instructions + **Parallel Commit Protocol** hand-off (only when `[P]` tasks exist) — orchestrator must inject `parallel_task_id: T{N}` into each teammate's spawn prompt so the teammate activates the protocol from `.claude/agents/task-runner.md`
- Task Execution Order section sequences the groups correctly

**task-runner subagent** (`agents/task-runner.md`): Restricted tools (`Read, Write, Edit, Bash, Glob, Grep`), reads CLAUDE.md + agent-guidelines on startup, commits with `[ISSUE-ID] T{N}: {description}` format, stays within assigned file scope.

**Parallel Commit Protocol** (shared-worktree race mitigation): Parallel `[P]` teammates all commit into the same worktree, racing on `.git/index` and `refs/heads/<branch>.lock`. Each teammate uses its own `GIT_INDEX_FILE=.git/index.zpit.T{N}` (index isolation), scoped `git add -- <files>` (pathspec safety net), and `.git/zpit-commit.lock` mkdir lock around `git commit` with 5 jittered retries (ref serialization). Sequential tasks skip the protocol (they own the index). Activation is driven by the `parallel_task_id` line injected by the orchestrator prompt — don't rename/remove that line without updating the agent doc too. See `docs/known-issues.md` for the incident that prompted this.

### Hook-Based Safety System (5 Layers)

1. **agent-guidelines.md** (soft — deployed to `.claude/docs/`, agents read on startup)
2. **--allowedTools per agent role** (medium — Claude Code enforced)
3. **PreToolUse hooks** (hard — enforced even with `--bypass-all-permissions`):
   - `path-guard.sh` — Write/Edit confined to worktree dir; denies `.claude/agents/`, `.claude/settings`, `.git/`, `.env`
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

**`[agent_models]`**: global per-role model selection, passed to Claude Code via `--model <id>` at launch. Defaults (all 1M context): `clarifier = opus[1m]`, `coding/reviewer/task_runner = sonnet[1m]`, `efficiency = opus[1m]`. Accepts short aliases (`opus` / `sonnet` / `haiku` — provider-dependent; Anthropic API resolves to latest, Bedrock/Vertex/Foundry one version behind) or full model IDs (pin exact version, cross-provider-consistent — e.g. `claude-opus-4-7[1m]`). `task_runner` is advisory — task-runner subagents currently inherit the coding orchestrator's model via Claude Code's Agent tool. Wired in `launch.go` (manual `[c]`/`[r]`/`[f]`/enter/`[d]`) and `loop_cmds.go` (`loopLaunchCoderCmd`, `loopWriteAndLaunchReviewerCmd`).

## Conventions

- **Branch naming**: `feat/ISSUE-ID-slug` — Loop always uses `feat/` prefix; PR title classification (feat/fix) decided by agent
- **Per-issue branch control**: Issue Spec `## BRANCH` specifies PR target branch (optional, falls back to project `base_branch`)
- **Git model**: `main` ← `dev` ← feature branches
- **Commit messages**: `[ISSUE-ID] short description`
- **Issue status flow**: pending_confirm → todo → in_progress → ai_review → waiting_review → needs_verify → done
- **Loop label flow**: todo → wip → review → ai-review (PASS) / needs-changes (auto-retry)
- **Hook exit codes**: 0 = allow, 2 = block (stderr fed back to Claude), never use exit 1
- **Agent docs**: `docs/agent-guidelines.md` (behavioral rules), `docs/code-construction-principles.md` (quality baseline)
- **Logging**: Use `m.state.logger` for all state transitions and lifecycle events (not `setStatus`, which is TUI-only). Include identifiers (key, PID, state, issue ID, role, round). In goroutine closures, capture `logger := m.state.logger` before use. Do not log ticks or renders.
- **i18n**: All user-facing strings in TUI views must go through `locale.T()`. Never hardcode display text — define a key in `internal/locale/keys.go`, add translations in `en.go` and `zh_tw.go`.
- **Agent language strategy**: TUI chrome is localized via `locale.T()`, but **all agent output is English-only**. `locale.ResponseInstruction()` is prepended to every agent prompt (coding/reviewer/revision builders) and injected into agent markdown files via `injectLangInstruction()` before deployment. The rule is non-negotiable — users may input in any language, but agents reply, write Issue Specs, commit messages, PR bodies, and channel messages in English. This is a token-efficiency choice (CJK tokenizes roughly 2× denser than English). If you add a new agent launch path, call `injectLangInstruction()` on its markdown before writing.
- **TUI icons**: Profile icons (`machine`/`desktop`/`web`/`android`/`terminal` in `internal/tui/view_projects.go`) use Nerd Font glyphs (requires a patched font such as CascadiaCode NF, bundled with Windows Terminal). All other TUI icons — session/loop status circles, channel artifact/message markers, worktree branch marker, etc. — stay as Unicode emoji so the TUI degrades gracefully in terminals without Nerd Fonts. When adding a new profile type, add a matching Nerd Font glyph to the `profileIcons` map; for any other TUI icon, use an emoji.
- **Concurrency**: All mutations to AppState mutable fields (`activeTerminals`, `loops`, `channelEvents`, `channelSubs`, `lastLivenessCheck`, `lastPermissionCheck`, `lastSessionScan`) must hold `m.state.Lock()`; reads must hold `m.state.RLock()`. Call `m.state.NotifyAll()` after mutations. Never hold `mu` when calling cmd methods that acquire their own `RLock` — use action-defer pattern.
