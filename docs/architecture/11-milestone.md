# 11. Milestone Log

> This document is a historical record tracking the completion status of each phase.

---

## M1: Minimum Viable Version ✅

- [x] Go project skeleton + Bubble Tea base framework
- [x] config.toml reading (TOML)
- [x] Project selection → auto cd + launch Claude Code in a new terminal
- [x] Detect Windows / WSL environment, select the corresponding path and terminal launch method
- [x] Terminal Launcher module (wt new-tab / tmux new-window)
- [x] Hook script authoring + testing (path-guard / bash-firewall / git-guard)
- [x] First project's .claude/settings.json + .claude/hooks/ creation (auto-deploy from embedded binary)
- [x] Code Construction Principles integrated into the Reviewer workflow + deploy scripts

## M2: Session Log Monitoring + Notifications ✅

- [x] Session Log Watcher module (fsnotify + log parsing)
- [x] TUI "Active Terminals" panel live updates
- [x] Agent waiting-for-response detection + TUI color-change alert
- [x] Windows Toast notifications
- [x] Audio alerts

## M3: Clarifier + Tracker Integration ✅

- [x] TrackerClient module: direct REST API (Forgejo / GitHub), token_env auth
- [x] Issue Spec format validation module (ValidateIssueSpec + ParseIssueSpec)
- [x] Clarifier agent definition (agents/clarifier.md, go:embed embedded)
- [x] TUI [c] clarify: open new terminal and launch claude --agent clarifier (huh confirm auto-deploys if not yet deployed)
- [x] TUI [s] status: read-only issue list (fetched via TrackerClient) + [y] confirm + [i] open browser
- [x] TUI [i] open tracker: open browser to issue list from the main screen
- [x] "Pending Confirm" → "Todo" confirmation flow ([y] changes label via TrackerClient)

## M4a: Worktree + Prompt Templates + Profile ✅

- [x] Worktree Manager module (create / clean up / list, shell out to git)
- [x] Worktree creation automatically writes settings.json + settings.local.json (single hook set; multi-mode hook_mode merged and removed during Issue #39 fix)
- [x] Hook automated tests (make test-hooks)
- [x] Coding Agent Prompt template implementation (Issue Spec → prompt assembly + log_policy injection)
- [x] Reviewer acceptance template implementation (Issue Spec → reviewer prompt assembly)
- [x] TrackerClient extension: GetIssue (with body), GetPRStatus
- [x] Profile definitions landed in config.toml (log_policy: strict/standard/minimal)
- [x] Reviewer agent definition (agents/reviewer.md, go:embed) + TUI [r] deploy and launch
- [x] Per-project base_branch config (default "dev")
- [x] Slug utility (issue title → URL-safe slug)

## M4b: Loop Engine + Automation ✅

- [x] Loop engine implementation (fetch todo → create worktree → coding agent → PR triggers reviewer → PR merge cleanup)
- [x] Multiple agents running in parallel for the same project (bounded by max_per_project)
- [x] TUI [l] toggle + Loop Status display
- [x] PR merge detection (FindPRByBranch) + automatic worktree cleanup
- [x] LaunchClaudeInDir — worktree path override
- [x] Coding completion signal: label poll detects "review" label (not PID exit); terminal is preserved
- [x] NEEDS CHANGES automatic retry (reviewer verdict → re-run coding → re-review, bounded by max_review_rounds)
- [x] Reviewer label update (PASS → ai-review, NEEDS CHANGES → needs-changes)
- [x] BuildRevisionPrompt — revision coding prompt (read review comment → fix → resubmit)
- [x] Label on-demand check: check required labels before operations; if missing, show overlay confirm dialog to create them (LabelManager interface)
- [x] Per-issue branch control: Issue Spec `## BASE_BRANCH` + `## PR_TARGET` (legacy `## BRANCH` still accepted with a deprecation warning) → coding prompt enforces PR target, reviewer validates

## M4c: SSH Remote Access + Concurrency Safety ✅

- [x] AppState separation: shared state extracted into a standalone struct; Model retains only per-connection UI state
- [x] SSH Server (Wish): `zpit serve` headless SSH daemon + `zpit connect` convenience wrapper
- [x] SSH authentication: public key (authorized_keys) + password (env var), at least one must be enabled
- [x] SSHConfig: `[ssh]` section in config.toml
- [x] Remote session lifecycle: `isRemote=true` quit does not stop watchers/loops; server init runs only once
- [x] `main.go` refactored into subcommand routing (runLocalTUI / runServe / runConnect)
- [x] `sync.RWMutex` protects AppState mutable fields
- [x] Channel-based pub/sub (Subscribe/Unsubscribe/NotifyAll): state changes broadcast StateRefreshMsg to all clients
- [x] Two independent mutexes (`mu` for state, `subMu` for subscribers) to avoid deadlock
- [x] Copy-before-closure + action-defer pattern to avoid nested lock acquisition
- [x] All loop handlers add write lock + NotifyAll; all loop cmds add read lock
- [x] View rendering acquires RLock

## Refactoring: model.go Split ✅

> PR #59 | Issue #24

- [x] model.go (2433 lines) split into 6 files, reduced to 860 lines
- [x] New session.go (733 lines): session lifecycle, discovery, monitoring, liveness, permission detection
- [x] New launch.go (479 lines): terminal launch cmds, slot operations, deploy helpers
- [x] New tracker_ops.go (242 lines): label check/ensure, issue load/confirm
- [x] New confirm.go (208 lines): confirm dialogs, executePendingOp, undeploy
- [x] New channel.go (78 lines): broker EventBus subscription, event reading
- [x] All update() inline handlers longer than 5 lines converted to one-line dispatch (`return m.handleXxx(msg)`)
- [x] Lock protocol doc comment added at the top of each new file
- [x] Pure code movement + inline handler extraction; zero behavior changes

---

## Refactoring: Task Execution Model Refactor ✅

> PR #69 | Issue #68

- [x] New `agents/task-runner.md` subagent definition (tools: Read, Write, Edit, Bash, Glob, Grep)
- [x] Rewrote `buildTaskWorkflow()` as subagent/team delegation prompt generation
- [x] New `groupTasks()` partitions tasks into sequential singletons and parallel batches
- [x] New `buildSubagentDelegation()` and `buildTeamDelegation()` prompt builders
- [x] `main.go` adds `//go:embed agents/task-runner.md`; `AppState` adds `taskRunnerMD` field
- [x] `loopWriteAgentCmd()` deploys `task-runner.md` to worktree when spec contains TASKS
- [x] Test updates: three scenarios — sequential tasks, mixed parallel tasks, no tasks

---

## M4d: Cross-Agent Channel Communication

> Completed

- [x] HTTP broker (`internal/broker/`) — REST endpoints for artifacts + messages, SSE streaming, project discovery
- [x] MCP stdio server (`internal/mcp/`) — 7 tools: publish_artifact, list_artifacts, send_message, list_projects, subscribe_project, unsubscribe_project, list_subscriptions
- [x] Channel view in TUI (`[m]` key) — cross-project event timeline
- [x] Dynamic subscription management (subscribe/unsubscribe at runtime)
- [x] Meeting Protocol for clarifier agents (Facilitator/Advisor roles)
- [x] AgentName tracking (`{type}-{4hex}` for manual, `{role}-#{issueID}` for loop)
- [x] Self-echo filtering via per-instance UUID

## M4e: Terminal Focus Panel + Exit Wrappers

> Completed

- [x] Three-panel focus system (Projects → Terminals → LoopSlots) with Tab cycling
- [x] `[x]` key to kill selected terminal (confirm dialog, force-kill process)
- [x] `zpit-exit.cmd` / `zpit-exit.ps1` exit wrappers for auto-closing WT tabs
- [x] WindowSizeMsg on confirm dialog init for immediate button rendering

## M4f: Redeploy Hotkey + Deploy Status Indicator

> Completed

- [x] `[d]` Redeploy hotkey: one-keystroke undeploy + redeploy of 4 agents (clarifier/reviewer/task-runner/efficiency) + hooks + docs, without launching Claude; protected by a confirm dialog
- [x] `deployAllCmd()` in `internal/tui/launch.go` — reuses `undeployFiles` + `DeployHooksToProject` + `deployDocs`; no `.mcp.json` write
- [x] Project list deploy status indicator: 🟢 Fully deployed (all 10 files present) / 🟡 Partially deployed / ⚪ Not deployed
- [x] `deployStatus()` in `view_projects.go` — `os.Stat` checks all 10 files on every render, no caching
- [x] `showRedeployConfirm()` reuses the huh confirm dialog pattern
- [x] i18n: 7 new locale keys (KeyRedeploy, KeyRedeployConfirm/Button/Done, KeyDeployStatus{Full,Partial,None})

## M4g: Dock Layout + Catppuccin Mocha

> Completed

- [x] `ViewProjects` migrated from a single shared viewport to a four-panel dock: Projects / Active Terminals / Loop (left column stacked) + Hotkeys (right column fixed); each panel owns its own `viewport.Model` for independent scrolling
- [x] Layout algorithm `computePanelRects`: 70/30 column split + `dockMinLeftWidth`/`dockMinRightWidth` clamp; left column height distributed by weight (Projects 3 / Terminals 2 / Loop 2); empty panels auto-collapse; no more `<100 cols` single-column fallback — Hotkeys always docks to the right at any width
- [x] Title chrome: `▎` mauve focus bar (single column width, only on the focused panel's title row) + uppercase title + count badge + 6-char rule; stacked panels get a 1-row gutter blank separator above them
- [x] 256-color ANSI → Catppuccin Mocha 24-bit palette; `colorAccent/Text/Muted/...` semantic aliases preserved, call sites unchanged
- [x] Mouse wheel `hitTestDockPanel`: dispatches to the hovered panel's viewport based on cursor position; falls back to projectsVP on miss
- [x] `renderTerminalsBody` / `renderLoopBody` build `termLineStarts` / `loopLineStarts` for variable-stride cursor-follow
- [x] `TestComputePanelRects` with 8 cases covering wide/narrow/empty/clamp boundaries
- [x] Follow-up refactor: `panelInnerSize` + `applyPanelContent` extract repeated prologue/epilogue from 4 sync functions; `dockPanel` struct reduces `renderOne`/`renderPanelChrome` parameter counts from 7/6 to 1; all magic numbers replaced with named constants

## M4h: Session sync (cross-machine /resume)

> Completed — Issue #102

- [x] T1: `internal/sessionsync/manifest.go` — Manifest schema (`format_version`, `source_os`, `source_cwd`, `source_encoded_cwd`, `sessions[]`, `exported_at`, `include_memory`) + `MarshalManifest` / `UnmarshalManifest` / `DetectSourceOS` + JSON round-trip tests
- [x] T2: `internal/sessionsync/scan.go` — `ProjectsRoot` / `ScanFolders` / `ScanSessions` (with `HasSubagents` detection) + 8 test cases
- [x] T3: `internal/sessionsync/pack.go` — `Pack` zip writer; automatically extracts `cwd` from session lines to populate the manifest; packs subagents + optional memory subtree; session JSONL streamed with `bufio.Scanner` 64MB buffer + 9 test cases
- [x] T4: `internal/sessionsync/unpack.go` — `Unpack` + cwd rewrite (line-by-line JSON-aware: only rewrites the `cwd` field) + `LoadManifest` + `DetectCollisions` + collision-resolver callback (Overwrite/Skip/CancelAll) + 11 test cases (including cross-OS round-trip via `watcher.ParseLine`)
- [x] T5: `internal/locale/keys.go` + `en.go` + `zh_tw.go` — 42 new KeyHistory* locale keys with English and Traditional Chinese translations
- [x] T6: `internal/tui/msg.go` — seven new `tea.Msg` types (HistoryFoldersScannedMsg, HistorySessionsScannedMsg, ExportStartedMsg, ExportCompletedMsg, ImportStartedMsg, ImportProgressMsg, ImportCompletedMsg, CollisionPromptMsg)
- [x] T7: `internal/tui/keymap.go` — `History` key binding to `h`
- [x] T8: `internal/tui/sessions.go` — cmd factories (scan folders / scan sessions / detect active PIDs / export / load manifest / detect collisions / import) + msg handlers; AC-16 log format fully validated character-by-character
- [x] T9: `internal/tui/view_sessions.go` — folder list / session list / 8 modal renders (active warning / export confirm / import preview / dest entry / final preview / running / summary / collision), all going through `locale.T()`
- [x] T10: `internal/tui/model.go` — `ViewHistory` View constant, `[h]` key handler, 31 per-Model history fields, textinput widgets (bundle path / output path / dest path), import wizard step machine (0 path → 1 preview → 2 dest → 3 final → 4 running → 5 summary), export wizard step machine (0 hidden → 1 active warning → 2 confirm → 3 running), collision queue dispatch
- [x] T11: `internal/tui/view_projects.go` — Hotkey panel adds `[h] History` in the same group as `[m] Channel` (cross-session features)
- [x] T12: README.md + CLAUDE.md updated with Session sync documentation; `sessionsync/` added to Package Structure
- [x] T13: `docs/architecture/02-tui-design.md` adds section 2.7 History sub-section + `docs/architecture/11-milestone.md` adds M4h block

## M5: Desktop Agent

> Completed

Opens a new safety domain alongside the 5-layer hook stack — the desktop agent does not operate in a worktree, does not write files, and relies on a Go MCP proxy for per-call policy gating instead of `PreToolUse` hooks.

- [x] `agents/desktop.md` agent definition (`name: desktop`, `model: opus[1m]`), go:embed embedded in binary
- [x] `zpit serve-desktop-proxy` subcommand — Go stdio MCP proxy; intercepts every `tools/call` frame, evaluates against policy, then forwards or rejects
- [x] `zpit-desktop-mcp` upstream fork (github.com/zac15987/computer-use-mcp, forked from `@zavora-ai/computer-use-mcp@6.1.0`) — Windows AUMID launch, Win32 `.exe` launch + PID return, stdio-entrypoint backslash fix, `@modelcontextprotocol/sdk` Zod 4 bump
- [x] `~/.zpit/desktop-policy.toml` policy file — auto-created on first `serve-desktop-proxy` invocation
- [x] 5 profile presets (`internal/desktop/policy.go`): `read-only` / `strict` / `standard` (default) / `none-script` / `trusted`; profile field + explicit field override mechanism
- [x] Tool allowlist: hard-blocks `run_script` / `filesystem` / `process_kill` / `registry` / `notification` / `scrape` / `multi_edit` / `multi_select` / `snapshot` / all virtual-desktop tools / `resize_window`
- [x] `deny_keys` platform defaults: Windows blocks `win+r` / `ctrl+shift+esc` / `ctrl+alt+del` / `alt+f4`; macOS blocks `cmd+q` / `cmd+option+esc` / `cmd+ctrl+q`
- [x] `allow_bundles` exception group mechanism: user-pre-approved shortcut groups; matching bypasses `deny_keys`
- [x] `keyboard_focus_strategy` setting: can require a `focus_check` before pointer actions to reduce the risk of typing into the wrong window
- [x] `[w]` hotkey (W for Window control; `[g]` was taken by GitStatus) — launched from `ViewProjects` without requiring a project selection
- [x] Single-instance lock: `AppState.activeDesktopAgent` mutex field; at most one desktop agent shared across all SSH-connected TUI clients; a second `[w]` invocation is rejected with a status toast
- [x] Linux `[w]` no-op: `runtime.GOOS == "linux"` shows a status toast and hides the hotkey from the hotkeys panel (upstream `computer-use-mcp` declares `os: ["darwin", "win32"]`; the Rust NAPI module has no Linux backend)
- [x] Global scope: cwd = `$HOME` / `%USERPROFILE%`; no `project.Path`; no per-project hook deployment
- [x] `docs/architecture/desktop-agent.md` — full design rationale (proxy vs hooks vs allowedTools vs raw `.mcp.json`), security model, profile table, tool-by-tool allowlist justification, deny_keys platform table, future Phase 2 extension points, upstream credit
- [x] README.md + CLAUDE.md + `docs/architecture/README.md` (index #13) + 09-safety.md (section 9.10 exception) + 06-agents.md (section 6.5 desktop agent) + 02-tui-design.md (mockup adds `[w]`) updated in sync
