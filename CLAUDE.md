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
├── sessionsync/         # Session bundle pack/unpack + cross-OS cwd rewrite (zip + manifest)
├── ssh/                 # Wish SSH server: StartServerAsync(), ServerHandle, StartServer(), auth config
├── terminal/            # LaunchClaude() dispatch + platform-specific launchers; launch priority: zplex probe → wt.exe → tmux → error
├── tracker/             # TrackerClient interface: ForgejoClient + GitHubClient REST abstractions
├── watcher/             # Session log monitoring: EncodeCwd, ParseLine, FindActiveSessions, Watcher
├── worktree/            # Worktree Manager, Slugify(), DeployHooksToProject(), DeployHooksToWorktree(), settings.json merge
├── zplex/               # HTTP client for the zplex launch-backend daemon (Health/CreateSession/PatchAgentState)
└── tui/                 # Bubble Tea TUI — see docs/architecture/02-tui-design.md + 10-appstate.md
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

Full architecture lives in `docs/architecture/` (English, one file per topic — start at `docs/architecture/README.md`). **Read the relevant doc before changing a subsystem.** This section is just the map plus the invariants worth keeping in front of every agent.

### Subsystem map

| Doc | Covers |
|---|---|
| `01-vision.md` | Dispatch-mode design principle; per-environment terminal launch (wt.exe / tmux) |
| `02-tui-design.md` | Per-view screen mockups; Bubble Tea Elm pattern (msg → cmd → handler → view); lazygit-style focus-panel dock; `[h]` session-sync browser |
| `03-system-architecture.md` | Architecture diagram; terminal launcher; session-log watcher (encoded cwd, two-phase startup, `stop_reason` state, 5s liveness, `notify-permission.sh` → `~/.zpit/signals/` 2s poll) |
| `04-config.md` | `config.toml` structure, TrackerClient, Profile, config hot-reload (hot vs restart-required fields, targeted TOML writer) |
| `05-issue-spec.md` | Issue Spec format, validation logic, prompt templates |
| `06-agents.md` | Clarifier/Reviewer/Task-Runner agents; go:embed deployment; i18n; orchestrator → task-runner delegation; per-subagent worktree model |
| `07-worktree-and-loop.md` | Worktree architecture; label-driven Loop slot state machine; issue status flow; `auto_merge` fork + retry/backoff |
| `08-notification.md` | Agent blocking detection; notification channels (cooldown, Windows Toast, sound) |
| `09-safety.md` | 5-layer safety system; PreToolUse hooks; ZPIT_AGENT |
| `10-appstate.md` | One AppState shared across SSH clients + local TUI; two-mutex concurrency; pub/sub; auto-serve mode |
| `11-milestone.md` | Milestone log (M1–M4c completion records, M5 planning) |
| `12-channel.md` | Cross-agent channel: broker + MCP; same/cross/global targeting; AgentName; Meeting Protocol; TUI integration |
| `desktop-agent.md` | Desktop agent: proxied MCP, policy gate, tool allowlist + `deny_keys`, single-instance lock, `[w]` hotkey |

### Invariants (don't rediscover these)

- **TrackerClient** (`internal/tracker/`) — dual backend: `ForgejoClient` (Forgejo/Gitea REST v1) + `GitHubClient` (GitHub REST). Auth via `token_env` (the env-var *name*, never the token itself). The TUI uses it for status display + label polling; agents talk to trackers via MCP instead, never TrackerClient.
- **Loop tick chains** — the three polling chains (todo / PR / label) reschedule *only* inside their tick handlers (`handleLoop*Tick`), never inside business handlers. A business handler that calls `loopSchedule*Poll` mid-chain spawns a second competing chain; `loop_tick_test.go` guards this. Those scheduler fns are kickoff-only (loop start, state transition into a polling state, resume).
- **AppState concurrency** — two mutexes: `mu` (RWMutex, state) and `subMu` (Mutex, subscribers), so `NotifyAll` can fire while `mu` is held. Copy mutable fields to locals before releasing the lock (copy-before-closure); collect actions under the write lock and build cmds after unlock (action-defer); never hold `mu` while calling a cmd that takes its own `RLock`.
- **Parallel `[P]` task batches** — the orchestrator spawns one `task-runner` per task with `isolation: "worktree"`. After they return: discover each branch with `git -C <path> rev-parse --abbrev-ref HEAD` (CC doesn't propagate `worktreeBranch` — known-issues §3), cherry-pick in task-ID order, then clean up as **TWO separate Bash calls** — `git worktree remove --force` then `git branch -D`, never chained with `&&` (a hook block on one must not skip the other — §4). `isolation = "in_project"` force-disables `[P]` (one working tree can't host parallel children).
- **Hook gate** — every hook `exit 0`s unless `ZPIT_AGENT=1`, so non-zpit Claude Code sessions are untouched. Worktrees get a dual-write of `.claude/settings.json` + `settings.local.json` (a linked worktree doesn't inherit the main repo's settings — Issue #39); `in_project` instead *merges* into the real repo's `settings.json` and self-ignores a zpit-created `.gitignore`.
- **go:embed deploy** — agents/hooks/docs are embedded in the binary and redeployed to the target project/worktree on every launch, so editing `agents/*.md`, `hooks/*.sh`, or `docs/agent-guidelines.md` requires a rebuild to take effect. `[f]` (efficiency) launches without hooks.
- **Auto-close after review PASS** — when `auto_close_after_done` (global, default `true`) is enabled, the loop kills a slot's own coder+reviewer terminals after a review PASS verdict (`ai-review` label). For the zplex backend, panels close via process exit. `needs-changes` never triggers closing. `loop.Slot` accumulates a per-round session-reference list to track which terminals to close at round completion.

## Config

`~/.zpit/config.toml` — override with `ZPIT_CONFIG` env var. First run auto-creates a template.

Logs: `~/.zpit/logs/zpit-YYYY-MM-DD.log` — daily rotation, 30-day retention.

See `testdata/config.toml` for a working example and `README.md` for full config reference.

**`[agent_models]`**: global per-role model selection. Accepts short aliases (`opus` / `sonnet` / `haiku` — provider-dependent; Anthropic API resolves to latest, Bedrock/Vertex/Foundry one version behind) or full model IDs (pin exact version, cross-provider-consistent — e.g. `claude-opus-4-7[1m]`).

Wiring differs by role:
- **`clarifier` / `coding` / `reviewer` / `efficiency`**: passed to Claude Code via `--model <id>` at launch (the orchestrator's own model). Wired in `launch.go` (manual `[c]`/`[r]`/`[f]`/enter/`[d]`) and `loop_cmds.go` (`loopLaunchCoderCmd`, `loopWriteAndLaunchReviewerCmd`).
- **`task_runner`**: injected into `task-runner.md` frontmatter at deploy time via `injectFrontmatterModel()` in `launch.go`; Claude Code's Agent tool reads the frontmatter `model:` field as the subagent default (source: `src/tools/AgentTool/AgentTool.tsx` line 86 — explicit param > agent frontmatter > parent inheritance). This lets the task-runner subagent use a different model from the coding orchestrator — the intended pattern is orchestrator on Opus (judgment-heavy: AC self-check, cherry-pick coordination), subagents on Sonnet (mechanical, scope-isolated; parallel `[P]` batches multiply the savings). Set to empty string to skip injection and fall back to parent inheritance.

Defaults: `coding` / `reviewer` / `clarifier` / `efficiency` = `opus[1m]`; `task_runner` = `sonnet`; `desktop` = `sonnet[1m]` (UI automation is mostly mechanical — Sonnet handles it; bump to `opus[1m]` for tougher visual reasoning). History: setting all five judgment roles to Sonnet previously produced ~90% two-round coding→review cycles — the round-two cost erased the per-token savings. Splitting by role (Opus for judgment, Sonnet for scoped execution) avoids that regression.

## Conventions

- **Branch naming**: `feat/ISSUE-ID-slug` — Loop always uses `feat/` prefix; PR title classification (feat/fix) decided by agent
- **Per-issue branch control**: Issue Spec carries two separate fields — `## BASE_BRANCH` (where the orchestrator's worktree forks from) and `## PR_TARGET` (where the PR merges into). Both fall back to project `base_branch` when empty; they are usually identical, and the split only matters in the asymmetric case (fork from feature branch, PR back to integration branch). The legacy single `## BRANCH` section is still accepted (maps to both fields and logs a deprecation warning) so historic issues resume cleanly.
- **Per-project isolation mode**: `isolation` (per-project, default `"worktree"`) selects how the loop allocates a working tree. `"worktree"` forks a git worktree per issue (full physical isolation). `"in_project"` works directly in the project directory on a `feat/` branch — no copy — and therefore **disables `[P]` parallel batches** and **caps the project to 1 concurrent slot**. Single source of truth: `ProjectConfig.InProject()` (anything not exactly `"in_project"` is worktree). Preconditions/behavior in in_project mode: a **dirty working tree at dispatch → `SlotNeedsHuman`** (never auto-stashed); cleanup does `git checkout <base>` + `git branch -D <feat>` (never deletes base); resume after a pre-PR crash is detected via the repo's current `feat/<id>-…` branch. ⚠️ Do NOT open the project in an external editor (e.g. Unreal Editor) or run git in it while an in_project loop is active — the background `git checkout` rewrites the working tree. The enum leaves room for future strategies (`worktree_cow` block-clone, `worktree_shared_cache`).
- **Git model**: `main` ← `dev` ← feature branches
- **Commit messages**: `[ISSUE-ID] short description` is **only** for commits produced by the zpit agent workflow (Loop coding/reviewer slots, task-runner subagents). Manual/ad-hoc commits made outside the loop — including any commit you (Claude) make while assisting the user directly — must NOT carry an `[NNN]` prefix; use a plain conventional-commit style (`feat:`, `fix:`, `docs:`, `refactor:`, etc.) without the bracket. The `[ISSUE-ID]` tag is a workflow signal that links a commit back to a tracker issue handled by the Loop, not a generic prefix.
- **Issue status flow**: pending_confirm → todo → in_progress → ai_review → waiting_review → needs_verify → done
- **Loop label flow**: todo → wip → review → ai-review (PASS) / needs-changes (auto-retry)
- **Hook exit codes**: 0 = allow, 2 = block (stderr fed back to Claude), never use exit 1
- **Agent docs**: `docs/agent-guidelines.md` (behavioral rules), `docs/code-construction-principles.md` (quality baseline)
- **Logging**: Use `m.state.logger` for all state transitions and lifecycle events (not `setStatus`, which is TUI-only). Include identifiers (key, PID, state, issue ID, role, round). In goroutine closures, capture `logger := m.state.logger` before use. Do not log ticks or renders.
- **i18n**: All user-facing strings in TUI views must go through `locale.T()`. Never hardcode display text — define a key in `internal/locale/keys.go`, add translations in `en.go` and `zh_tw.go`.
- **Agent language strategy**: TUI chrome is localized via `locale.T()`. Coding / reviewer / revision / efficiency / task-runner agents are **English-only** — `locale.ResponseInstruction()` is prepended via prompt builders or injected into agent markdown via `injectLangInstruction()`. The rule is non-negotiable: users may input in any language, but those agents reply, write commit messages, PR bodies, and channel messages in English. This is a token-efficiency choice (CJK tokenizes roughly 2× denser than English) — coding and reviewer dominate token usage. **Clarifier is the exception**: it is conversational and low-token, so it uses `locale.ClarifierResponseInstruction()` (via `injectClarifierLangInstruction()`) — dialogue + channel messages follow the configured locale, but the **Issue Spec it pushes to the Tracker (title, all section bodies, tracker labels) must still be English** so downstream agents have a canonical artifact language. If you add a new agent launch path, call `injectLangInstruction()` (strict English) on its markdown before writing; use `injectClarifierLangInstruction()` only for the clarifier path.
- **TUI icons**: Profile icons (`machine`/`desktop`/`web`/`android`/`terminal` in `internal/tui/view_projects.go`) use Nerd Font glyphs (requires a patched font such as CascadiaCode NF, bundled with Windows Terminal). All other TUI icons — session/loop status circles, channel artifact/message markers, worktree branch marker, etc. — stay as Unicode emoji so the TUI degrades gracefully in terminals without Nerd Fonts. When adding a new profile type, add a matching Nerd Font glyph to the `profileIcons` map; for any other TUI icon, use an emoji.
- **Concurrency**: All mutations to AppState mutable fields (`activeTerminals`, `loops`, `channelEvents`, `channelSubs`, `lastLivenessCheck`, `lastPermissionCheck`, `lastSessionScan`) must hold `m.state.Lock()`; reads must hold `m.state.RLock()`. Call `m.state.NotifyAll()` after mutations. Never hold `mu` when calling cmd methods that acquire their own `RLock` — use action-defer pattern.
- **Config template parity**: When editing `internal/config/config.go` (struct fields, default-template string, or the auto-generated `~/.zpit/config.toml` template), also update `testdata/config.toml` so the fixture stays a working example. The in-code template and `testdata/config.toml` are two independent copies — neither is generated from the other, so drift is easy and silent. Rule of thumb: any change to the default template inside `config.go` needs a matching edit in `testdata/config.toml` (same field, same default value, same comment intent). `config_test.go` exercises the fixture, so out-of-date `testdata/config.toml` may produce green tests that don't reflect the shipping default.
