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
go run .                 # Run (reads ~/.zpit/config.toml)
ZPIT_CONFIG=./testdata/config.toml go run .  # Run with test config
```

## Current State (M4b Complete)

### What works now
- TUI project list with ↑↓ navigation and profile icons
- Config loading from `~/.zpit/config.toml` (TOML, with defaults)
- Environment detection (Windows Terminal / WSL / Linux tmux)
- Terminal Launcher: `Enter` opens Claude Code in new WT tab or tmux window
- `[o]` opens project folder in file manager
- Session Log Watcher: fsnotify monitors JSONL logs, detects agent state (Working/Waiting/Ended/Permission)
- Active Terminals area: 🟢 Working / 🟡 Waiting for input / 🟠 Waiting for permission / ⚫ Session ended (auto-removes after 10s)
- Agent waiting detection: extracts question text from session log, shows preview in TUI
- Permission prompt detection: Notification hook writes signal file to `~/.zpit/signals/`, TUI polls every 2s
- Windows Toast notification when agent waits for input or permission (via PowerShell)
- Sound alert (SystemSounds.Asterisk)
- Notification cooldown (re_remind_minutes, per-project)
- Session liveness check: PID monitoring every 5s, detects closed sessions + `/resume` session switches
- Startup session scan: detects already-running Claude Code sessions on launch, auto-attaches watchers
- 3 PreToolUse hook scripts + 1 Notification hook + 1 env wrapper with 32 tests
- Hook auto-deploy: hook scripts embedded via go:embed, deployed to `.claude/hooks/` + `settings.json` merged on every agent launch (`[c]`/`[r]`/`[l]`)
- Hook deployment script (`scripts/setup-hooks.sh`, manual fallback)
- 14 client tests + 12 issuespec tests + 6 url tests + 25 watcher tests + 6 notify tests + 7 config tests
- 11 slug tests + 5 worktree manager tests + 11 hook config tests + 5 prompt tests
- TrackerClient: 直接 REST API（Forgejo / GitHub），token_env auth
- Issue Spec validation (`ValidateIssueSpec`) + parsing (`ParseIssueSpec`)
- `[c]` Clarify: opens new terminal with `claude --agent clarifier` (label check + auto-deploy, overlay confirm dialogs)
- `[s]` Status: readonly issue list via TrackerClient + `[y]` confirm (pending→todo) + `[p]` open in browser
- `[p]` Open Tracker: opens project issue tracker in browser
- `[r]` Review: opens new terminal with `claude --agent reviewer` (label check + auto-deploy, overlay confirm dialogs)
- `[u]` Undeploy: removes all Zpit-deployed files (.claude/agents/, .claude/docs/, .claude/hooks/) with overlay confirm
- `[l]` Loop: auto-dispatch coding + reviewer agents per todo issue
- Clarifier agent template (`agents/clarifier.md`, embedded via go:embed)
- Reviewer agent template (`agents/reviewer.md`, embedded via go:embed)
- Worktree Manager: Create / Remove / List worktrees, hook mode auto-config (settings.local.json)
- Prompt assembly: BuildCodingPrompt + BuildReviewerPrompt + BuildRevisionPrompt (Issue Spec → agent prompt with log_policy injection)
- Profile config: `[profiles.*]` with `log_policy` (strict/standard/minimal)
- Per-project `base_branch` config (default "dev")
- Makefile with `test-hooks` target
- Loop engine: poll todo → create worktree → launch coding agent → label poll detects "review" → launch reviewer → label poll detects "ai-review"/"needs-changes" → auto-retry or wait merge → cleanup
- Label-driven transitions: loop polls issue labels (not PID monitoring) to detect agent completion. Coding agent sets "review" label → reviewer starts. Reviewer sets "ai-review"/"needs-changes" → next step. Agents don't need to exit for loop to progress.
- NEEDS CHANGES auto-retry: reviewer 設定 needs-changes label → 自動重跑 coding agent → 再次 review（max_review_rounds 預設 3）
- LaunchClaudeInDir: worktree path override for loop launches
- FindPRByBranch: PR detection by branch name (Forgejo + GitHub)
- TrackerDoc auto-deploy: `.claude/docs/tracker.md` written on agent deploy (Forgejo→gitea MCP/REST, GitHub→gh CLI/REST)
- Loop Status display in TUI main view
- Multi-agent parallel execution (max_per_project worktrees)
- On-demand label check: operations ([y]/[c]/[r]/[l]) check required labels before execution, overlay confirm dialog if missing
- Overlay confirm dialogs: huh forms rendered as centered overlay on top of background view (bubbletea-overlay)
- Per-issue branch control: Issue Spec `## BRANCH` → coding agent PR 必須 target 指定 branch，reviewer 驗證 target branch
- i18n: all prompts/agents in English, TUI strings via locale package (en + zh-TW), config `language` field
- Focus Panel: `Tab` switches focus to Loop Status area, `↑↓` selects slot, `Enter` opens plain Claude Code in slot's worktree (only launchable states: coding/reviewing/waitingPRMerge/needsHuman/error)

### What's stubbed (shows "coming in MX" message)
- `[a]` Add Project → M5
- `[e]` Edit Config → M5
- `[?]` Help → TBD

### What's not implemented yet
- Agent teams (M5)
- Machine push → auto review trigger (M5)
- Recent activity feed (M5)
- shared-core cross-project detection (M5)

## Package Structure

```
main.go                          # Entry point: config template, log file, embed agents, run Bubble Tea
Makefile                         # build, test, test-hooks, test-all targets
agents/
├── clarifier.md                 # Clarifier agent template (go:embed → auto-deploy)
└── reviewer.md                  # Reviewer agent template (go:embed → auto-deploy)
internal/
├── config/config.go             # Config structs + Load() + BaseDir() + WriteTemplate() + defaults
├── platform/detect.go           # Environment detection + ResolvePath()
├── locale/
│   ├── locale.go                # SetLanguage() + T() + ResponseInstruction()
│   ├── keys.go                  # Key constants for all TUI display strings
│   ├── en.go                    # English translations
│   └── zh_tw.go                 # Traditional Chinese translations
├── loop/
│   └── types.go                 # Loop state machine: SlotState, Slot, LoopState
├── terminal/
│   ├── launcher.go              # LaunchClaude() + LaunchClaudeInDir() dispatch + arg builders
│   ├── launcher_windows.go      # wt.exe (build tag: windows)
│   └── launcher_unix.go         # tmux (build tag: !windows)
├── watcher/
│   ├── encode.go                # EncodeCwd() path encoding + ClaudeHome()
│   ├── parse.go                 # AgentState enum + ParseLine() JSONL parser
│   ├── session.go               # FindActiveSessions() + ReadSessionByPID() + IsProcessAlive() + LogFilePath()
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
│   ├── keymap.go                # Key bindings (incl. Back, Confirm, FocusSwitch)
│   ├── styles.go                # Lip Gloss styles with named color constants
│   ├── view_projects.go         # Main screen: project list + hotkeys + active terminals + loop status
│   ├── view_status.go           # Status sub-view: issue list + [y] confirm + [p] browser
│   ├── msg.go                   # Custom tea.Msg types (IssuesLoadedMsg, Loop*Msg, etc.)
│   ├── loop_cmds.go             # Loop tea.Cmd functions (poll, create worktree, launch, cleanup)
│   └── loop_handler.go          # Loop message handlers (state machine transitions)
├── worktree/
│   ├── slug.go                  # Slugify() issue title → URL-safe slug
│   ├── manager.go               # Worktree Manager: Create/Remove/List + runGit helper
│   ├── hooks.go                 # HookScripts + DeployHooks() + SetupHookMode() + settings.json merge
│   ├── slug_test.go             # 11 slug tests
│   ├── manager_test.go          # Worktree lifecycle tests (real git)
│   └── hooks_test.go            # 5 hook config tests
├── prompt/
│   ├── coding.go                # BuildCodingPrompt() — Issue Spec → coding agent prompt
│   ├── reviewer.go              # BuildReviewerPrompt() — Issue Spec → reviewer prompt
│   ├── revision.go              # BuildRevisionPrompt() — Issue Spec → revision coding prompt (NEEDS CHANGES retry)
│   └── prompt_test.go           # 5 prompt assembly tests
└── tracker/
    ├── types.go                 # Issue/PR structs + canonical status constants + LabelDef + RequiredLabels
    ├── client.go                # TrackerClient interface + NewClient factory + MapLabelsToStatus
    ├── labels.go                # LabelManager interface + CheckLabels (read-only) + EnsureLabels (create missing)
    ├── restapi.go               # Shared REST HTTP helper (restClient, doJSON, splitRepo)
    ├── forgejo.go               # ForgejoClient: Forgejo/Gitea REST API v1
    ├── github.go                # GitHubClient: GitHub REST API
    ├── client_test.go           # 14 client tests (httptest mock)
    ├── labels_test.go           # 9 label tests (mock + httptest)
    ├── issuespec.go             # ValidateIssueSpec + ParseIssueSpec (IssueSpec.Branch for per-issue PR target)
    ├── issuespec_test.go        # 15 tests
    ├── urls.go                  # BuildIssueURL + BuildTrackerURL
    ├── urls_test.go             # 6 tests
    └── trackerdoc.go            # BuildTrackerDoc(baseBranch) → .claude/docs/tracker.md content + branch strategy
hooks/
├── path-guard.sh                # Confine Write/Edit to worktree dir
├── bash-firewall.sh             # Block destructive commands
├── git-guard.sh                 # Block push/merge/rebase; allow commit/status/diff
├── notify-permission.sh         # Notification hook: write permission signal for TUI
├── zpit-env.cmd                 # Windows wrapper: sets ZPIT_AGENT=1 for agent launches
└── hooks_test.go                # Go tests shelling out to each hook (32 tests)
scripts/setup-hooks.sh           # Manual fallback: deploy hooks + docs + agents to a project's .claude/
testdata/
├── config.toml                  # Real project config used for tests + manual TUI testing
├── config_minimal.toml          # Minimal config for defaults testing
├── session_working.jsonl        # JSONL fixture: agent working (tool_use)
└── session_waiting.jsonl        # JSONL fixture: agent waiting (end_turn)
docs/
├── zpit-architecture.md         # Full architecture document
├── agent-guidelines.md          # Agent behavioral rules (Layer 1 soft constraints, deployed to .claude/docs/)
├── code-construction-principles.md  # Code quality baseline for agent review
├── sycophancy-mitigation.md     # LLM sycophancy: definition, manifestations, prompt-level mitigations
└── llm-stale-context-and-tool-underuse.md  # LLM failure modes: stale context assumption + tool underuse
```

## Architecture

### Session Log Monitoring (M2)

The TUI monitors Claude Code sessions via their JSONL log files:

1. **Path encoding**: `EncodeCwd()` converts project path to Claude's directory name (non-alphanumeric → `-`)
2. **Session discovery**: scans `~/.claude/sessions/{pid}.json` for matching project + alive PID
3. **Two-phase startup**: finds session PID immediately (enables liveness check), then waits for JSONL file creation (happens on first user input)
4. **State detection**: parses `stop_reason` from assistant messages — `"end_turn"` = waiting, `"tool_use"` = working
5. **Permission detection**: Notification hook (`notify-permission.sh`) writes signal file to `~/.zpit/signals/permission-{sessionID}.json` when Claude Code needs tool permission approval. TUI polls signal dir every 2s, maps sessionID to ActiveTerminal, sets StatePermission. Signal file is cleaned up when JSONL shows new activity or session ends.
6. **Notifications**: on state transition to waiting or permission → Windows Toast + sound (respects config + cooldown)
7. **Liveness check**: every 5s verifies PID is alive; ended sessions auto-remove after 3s display
8. **`/resume` detection**: liveness check re-reads `{pid}.json` each cycle; if `sessionId` changed, stops old watcher and restarts for new session. `waitForLogCmd` also re-checks during JSONL wait phase.

### TrackerClient (M3)

Zpit 透過直接 REST API 與各 tracker 互動（TrackerClient interface）：

```
Zpit (Go) → TrackerClient interface
                ├─ ForgejoClient → Forgejo/Gitea REST API
                └─ GitHubClient  → GitHub REST API
```

- 直接 API < 1 秒回應，適合 `[s]` status 即時顯示和 Loop 頻繁 poll
- Auth 透過 `token_env` 指向環境變數，不在 config 存明文
- Agent（Clarifier/Coding/Reviewer）仍透過 MCP 操作 tracker（推 issue、開 PR、寫 comment）
- TUI `[s]` status 透過 TrackerClient 拉 issue 列表，`[y]` 改 label

### Hook-Based Safety System (5 Layers)

1. **agent-guidelines.md behavioral rules** (soft — `.claude/docs/agent-guidelines.md`, auto-deployed by TUI)
2. **--allowedTools per agent role** (medium)
3. **PreToolUse hooks** (hard — enforced even with bypass-all-permissions, auto-deployed from embedded binary):
   - `path-guard.sh` — Write/Edit confined to worktree dir; denies `.claude/`, `CLAUDE.md`, `.git/`, `.env`
   - `bash-firewall.sh` — blocks destructive commands (rm -rf, curl|bash, force push, etc.)
   - `git-guard.sh` — push whitelist (only `feat/*` branches allowed), blocks merge, rebase, branch delete
   - `notify-permission.sh` — Notification hook: writes signal file to `~/.zpit/signals/` when permission prompt appears (not a safety hook; enables TUI permission detection)
4. **Git worktree isolation** (physical)
5. **Human PR review** (final gate)

Hook scripts are embedded in the binary via `go:embed` and auto-deployed to `.claude/hooks/` on every agent launch (`[c]`/`[r]`/`[l]`). Hook config is merged into `.claude/settings.json` (preserving existing keys like `enabledPlugins`). For worktrees, `settings.local.json` overlay is used instead. `scripts/setup-hooks.sh` remains as a manual fallback.

Hook strictness is per-project via `hook_mode` (default `"strict"`): `strict` (all hooks), `standard` (path-guard + git-guard), `relaxed` (git-guard only).

**ZPIT_AGENT environment variable**: PreToolUse hook scripts check for `ZPIT_AGENT=1` — if absent, they `exit 0` (allow everything). This ensures hooks only enforce restrictions in Zpit-launched agent sessions, not in plain Claude Code sessions. On Windows (wt.exe), env var is injected via `.claude/hooks/zpit-env.cmd` wrapper (wt.exe doesn't inherit parent env). On Unix (tmux), `ZPIT_AGENT=1` is inline-prefixed to the command string. Detection is automatic via `--agent` flag in launch args.

### Agent Anti-Sycophancy & Objectivity Rules

All agents read `agent-guidelines.md` which includes an **Objectivity Protocol**:
- Prioritize accuracy over agreement; flag plan gaps before executing
- No flattery phrases; respond directly
- Agreement requires stated reasoning; reversal requires new evidence
- Semantic/ownership changes to existing artifacts must be flagged as decision points

Role-specific rules (see `docs/sycophancy-mitigation.md` for research background):
- **Clarifier**: challenge before acceptance, attach confidence levels, no premature closure
- **Reviewer**: critic-only stance, no praise, ⚠️ must itemize what's missing or becomes ❌
- **Coding agent**: stop if APPROACH has a flaw or gap discovered during implementation
- **Revision agent**: challenge reviewer feedback that appears incorrect instead of silently complying

### Known LLM Failure Modes (see `docs/llm-stale-context-and-tool-underuse.md`)

Two failure modes that impact agent reliability:
1. **Stale context assumption** — LLM operates on a snapshot of state instead of re-verifying. Caused by positional attention bias (U-shaped curve) and context rot. Mitigation: explicit re-verification rules, sub-agent isolation, worktree isolation.
2. **Tool underuse** — LLM generates from training data instead of calling available tools (web search, file reads). Caused by RLHF bias toward fluent responses over verification. Mitigation: tool-first instructions, mandatory WebSearch for clarifier, Chain of Verification pattern.

Current gaps: no re-read-before-edit mandate for coding agents, no WebSearch mandate for coding/revision agents, no freshness signals in long sessions.

## Config Location

`~/.zpit/config.toml` — terminal settings, notification preferences, provider credentials (via env var references), and all project definitions. Override with `ZPIT_CONFIG` env var.

First run: if config doesn't exist, Zpit auto-creates a template and exits. User edits it, then runs again.

Logs: `~/.zpit/logs/zpit-YYYY-MM-DD.log` — daily rotation, auto-cleanup after 30 days.

Provider entries include `token_env` field pointing to environment variable name for API auth (e.g. `token_env = "FORGEJO_TOKEN"`).

Each project has `base_branch` (default `"dev"`) — worktree feature branches are created from this branch.

Top-level `language` field (default `"en"`) controls TUI display language and agent response language. Supported: `"en"`, `"zh-TW"`. All agent prompts and .md templates are written in English; response language is injected dynamically via `locale.ResponseInstruction()`.

## Conventions

- Branch naming: `feat/ISSUE-ID-slug` — Zpit Loop 統一用 `feat/` 前綴建 branch，PR title 由 agent 決定 feat/fix 分類。branch 名必須包含 issue ID，slug 從 issue title 自動產生
- Per-issue branch control: Issue Spec `## BRANCH` section 指定 PR target branch（optional）。Clarifier 問使用者，Loop engine 優先用 Issue Spec 的值，fallback 到 project config 的 `base_branch`。Coding agent prompt 明確禁止 target 錯誤 branch。
- Git branching model: `main` ← `dev` ← feature branches（所有功能從 dev 分出，完成合併回 dev，穩定後 dev 合併至 main）
- Commit messages: `[ISSUE-ID] short description`
- Issue statuses flow: pending_confirm → todo → in_progress → ai_review → waiting_review → (needs_verify) → done
- Loop label flow: todo → wip → review → ai-review (PASS) / needs-changes (NEEDS CHANGES → auto-retry up to max_review_rounds)
- Agents must stop and ask on uncertain technical decisions, even with bypass-all-permissions enabled
- Hook exit codes: 0 = allow, 2 = block (stderr message fed back to Claude), never use exit 1 for safety hooks
- Agent behavioral rules: `docs/agent-guidelines.md` — deployed to `.claude/docs/`, all agents read on startup
- Code quality baseline: `docs/code-construction-principles.md` — all agents reference during implementation and review
- Logging: Use `m.logger` to log all state transitions and lifecycle events (session attach/found/ready/lost/ended/removed, loop dispatch/launch/exit/verdict/cleanup). Logs must include sufficient identifiers (key, PID, state, issue ID, role, round) for post-hoc debugging. Do not log normal ticks or renders — only log state changes.
- **LOG 是必要的**：所有新功能、狀態機、lifecycle 事件都必須包含 `m.logger` 呼叫。僅用 `setStatus` 不夠 — `setStatus` 是給 TUI 顯示的，`m.logger` 才會寫入 log 檔供事後除錯。在 goroutine closure 中先捕捉 `logger := m.logger` 再使用。
