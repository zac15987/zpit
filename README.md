# Zpit

**Zpit** (zac + cockpit) — a TUI-based AI development cockpit that orchestrates [Claude Code](https://claude.ai/code) agents across multiple projects. Zpit acts as a **dispatch center** — it selects projects, launches agents in separate terminal windows, monitors their progress, coordinates the full issue lifecycle from requirement clarification to PR, and enables real-time cross-agent communication via a built-in HTTP broker + MCP channel.

> **Key principle:** Claude Code runs in independent terminal windows. Zpit never wraps or embeds it — it monitors via session logs, coordinates via issue trackers, and bridges agents via a local channel broker.

## TUI Preview

### Main View

```
 Zpit v0.1                                        03/27 14:07  Windows Terminal


  Projects                                                Hotkeys
  ────────────────────────────────                        ──────────────────────────

   ⚙️ AI Inspection Cleaning Demo                         [Enter] Launch Claude Code
     machine │ wpf, ethercat, basler                      [c] Clarify requirement
                                                          [l] Loop auto-implement
   ⚙️ ENR DUC                                             [r] Review changes
     machine │ wpf, secsgem                               [f] Efficiency agent
                                                          [s] Status overview
   🖥️ DisplayProfileManager                               [o] Open project folder
     desktop │ wpf, nlog                                  [p] Open Issue Tracker
                                                          [u] Undeploy agents
 › 🖥️ Zpit                                                [m] Channel communication
     desktop │ go, bubbletea
                                                          [a] Add project
                                                          [e] Edit config

                                                          [x] Close Terminal
                                                          [Tab] Switch Panel
                                                          [?] Help
                                                          [q] Quit


  Active Terminals
  ──────────────────────────────────────────────────
  [1] AI Inspection Cleaning Demo │ 🟡 Waiting for input 04:15
      Q: Issue B pushed: **[#25 — feat(manual-control): Safety Interlock + Soft Lim
  [2] Zpit │ 🟡 Waiting for input 08:36
      Q: Pushed to `origin/dev`. Commit `2094bea`.
  [3] Zpit │ 🟢 Launched 00:09
      Tab: Zpit


  Loop Status
  ──────────────────────────────────────────────────
  AI Inspection Cleaning Demo (running)
    🟢 #25 feat(manual-control): Safety Interlock...  coding



  Press ? for help, q to quit
```

### Status View

```
 Zpit v0.1                                        03/27 14:04  Windows Terminal


  Issues — AI Inspection Cleaning Demo
  ────────────────────────────────────────────────────────────

 › #25   [pending] feat(manual-control): Safety Interlock + Soft Limit + Reset Zero




  [y] Confirm (pending→todo)  [p] Open in browser  [Esc] Back
```

## How It Works

```
You (TUI)                    Claude Code Agents
    │
    ├─ [c] Clarify ──────────► Clarifier agent (new terminal)
    │   requirement              asks questions, creates structured issue
    │   (press multiple times)   agents auto-discover via agent_type, enter Facilitator/Advisor meeting mode
    │
    ├─ [l] Loop ──────────────► Coding agent (worktree + new terminal)
    │   auto-implement           implements, commits, opens PR
    │        │
    │        └─ PR appears ───► Reviewer agent (same worktree)
    │             │              checks AC, writes review report
    │             ├─ PASS ────► waits for human merge
    │             └─ NEEDS CHANGES → auto-retry coding (up to N rounds)
    │
    ├─ [s] Status ────────────► shows issue list from tracker
    ├─ [r] Review ────────────► launches reviewer on demand
    ├─ [f] Efficiency ────────► lightweight agent (no hooks, no tracker, self-review)
    └─ [Enter] ───────────────► launches Claude Code directly
```

## Features

- **Multi-project dashboard** — switch between projects with arrow keys, mouse scroll support
- **Loop engine** — fully automated: poll todo issues → create worktree → coding agent → reviewer → PR merge → cleanup
- **Task decomposition** — when an Issue Spec contains `## TASKS`, the coding agent delegates to `task-runner` subagents (sequential or parallel via Agent Teams)
- **Agent monitoring** — real-time status via session log parsing (Working / Waiting / Permission / Ended), auto-detects running sessions on startup, survives `/resume` session switches
- **Notifications** — Windows Toast + sound when an agent needs your input or awaits tool permission
- **Issue tracker integration** — Forgejo/Gitea and GitHub via REST API + MCP
- **Cross-agent channel** — real-time agent-to-agent communication via HTTP broker + MCP; supports same-project, cross-project, and global broadcast messaging
- **Meeting mode** — multiple clarifier agents auto-discover via `agent_type` tracking, assign Facilitator/Advisor roles, and converge to a structured issue through coordinated channel communication
- **5-layer safety system** — agent-guidelines.md, allowed tools, PreToolUse hooks, git worktree isolation, human PR review
- **Per-issue branch control** — clarifier asks target branch, coding agent enforces it
- **Auto-retry** — reviewer judges NEEDS CHANGES → coding agent auto-fixes → re-review (configurable rounds)
- **i18n** — English and Traditional Chinese (zh-TW) via `locale.T()`
- **SSH remote access** — `zpit serve` runs a headless SSH daemon (Wish), multiple clients share one dashboard with real-time state sync; `auto_serve` mode starts the server automatically when running `zpit`, enabling seamless mobile access without workflow interruption

## Requirements

- [Go](https://go.dev/) 1.26+
- [Claude Code](https://claude.ai/code) CLI installed and authenticated
- Windows Terminal (Windows) or tmux (Linux/WSL)
- A Forgejo/Gitea or GitHub issue tracker

## Quick Start

```bash
# Build
go build -o zpit .

# First run — creates config template at ~/.zpit/config.toml
./zpit
# Edit the config with your projects and tracker tokens, then:
./zpit

# SSH server mode (remote access)
./zpit serve      # Start headless SSH daemon (default port 2200)
./zpit connect    # SSH connect to local server

# Or enable auto_serve in config — then just "./zpit" starts
# the SSH server automatically and connects to it.
# You can SSH in from your phone at any time.
```

## Configuration

Config lives at `~/.zpit/config.toml`. Override with `ZPIT_CONFIG` env var.

```toml
language = "en"             # en | zh-TW
broker_port = 17731         # HTTP broker port for cross-agent channel
# zpit_bin = "/usr/local/bin/zpit"  # explicit binary path for .mcp.json generation

[terminal]
windows_mode = "new_tab"    # new_tab | new_window
tmux_mode = "new_window"    # new_window | new_pane
# windows_terminal_profile = "PowerShell 7"  # WT profile name for -p flag

[notification]
tui_alert = true
windows_toast = true
sound = true
# sound_file = "D:/sounds/notify.mp3"  # custom notification sound (WAV/MP3/M4A/OGG)
re_remind_minutes = 2

[worktree]
base_dir_windows = "D:/worktrees"
base_dir_wsl = "/mnt/d/worktrees"
max_per_project = 5
poll_seconds = 10           # todo issue polling interval
pr_poll_seconds = 10        # PR merge polling interval
max_review_rounds = 3       # auto-retry rounds before needs-human
# dir_format = "{project_id}/{issue_id}--{slug}"
# auto_cleanup = false

# Tracker providers — token read from env var, never stored in config
[providers.tracker.my-forgejo]
type = "forgejo_issues"
url = "https://your-forgejo.example.com"
token_env = "FORGEJO_TOKEN"

# Git providers (optional — for Forgejo/Gitea PR API)
# [providers.git.my-forgejo]
# type = "forgejo"
# url = "https://your-forgejo.example.com"
# token_env = "FORGEJO_TOKEN"

# Projects
[[projects]]
name = "My Project"
id = "my-project"
profile = "machine"         # display tag: machine | desktop | web | android (for TUI icon)
hook_mode = "strict"        # strict | standard | relaxed
log_policy = "standard"     # strict | standard | minimal — agent logging strictness
tracker = "my-forgejo"
# tracker_project = "My_Project"  # tracker project name if different from repo
# git = "my-forgejo"              # git provider for PR operations
repo = "org/repo"
base_branch = "dev"
# shared_core = false
channel_enabled = false     # enable cross-agent channel communication
channel_listen = []         # subscribe to other projects' events, e.g. ["_global", "other-proj"]
tags = ["go"]

[projects.path]
windows = "D:/Projects/my-project"
wsl = "/mnt/d/Projects/my-project"

# SSH server (optional — for remote TUI access)
# [ssh]
# port = 2200
# host = "0.0.0.0"
# host_key_path = "~/.zpit/ssh/host_ed25519"
# password_env = "ZPIT_SSH_PASSWORD"
# authorized_keys_path = "~/.ssh/authorized_keys"
# auto_serve = false    # when true, "zpit" auto-starts SSH server + connects
```

## Hotkeys

| Key | Action |
|-----|--------|
| `Enter` | Launch Claude Code in new terminal |
| `c` | Clarify — open clarifier agent to create structured issue |
| `l` | Loop — toggle automated coding + review cycle |
| `r` | Review — launch reviewer agent |
| `f` | Efficiency — lightweight agent (no hooks, no tracker, self-review) |
| `s` | Status — view issue list from tracker |
| `o` | Open project folder |
| `p` | Open issue tracker in browser |
| `u` | Undeploy — remove deployed agents, docs, hooks |
| `m` | Channel — view cross-agent communication events |
| `g` | Git Status — view branches (local + remote-only) and commit graph; [f] fetch, [p] pull (--ff-only) |
| `a` | Add project (coming soon) |
| `e` | Edit config — sub-menu: toggle channel, edit channel_listen, open in $EDITOR |
| `x` | Close Terminal — force-close selected terminal (when Terminals panel focused) |
| `Tab` | Switch focus between panels (Projects, Terminals, Loop Slots) |
| `?` | Help |
| `q` | Quit |

## Loop Engine

The loop engine automates the full coding cycle:

1. **Poll** tracker for `todo` issues (configurable interval)
2. **Create worktree** — isolated git worktree from `base_branch`
3. **Launch coding agent** — writes implementation, commits, opens PR, sets `review` label
4. **Detect completion** — polls issue labels for `review` (agents don't need to exit)
5. **Launch reviewer** — checks acceptance criteria, writes review report, sets verdict label
6. **Detect verdict** — polls issue labels for `ai-review` (PASS → wait for human merge) or `needs-changes` (auto-retry up to `max_review_rounds`)
7. **Cleanup** — after PR merge, removes worktree and branch

Multiple issues run in parallel, limited by `max_per_project`.

## Safety System

Zpit enforces 5 layers of safety to prevent agents from causing damage:

| Layer | Mechanism | Scope |
|-------|-----------|-------|
| 1 | agent-guidelines.md behavioral rules | Soft — agent reads on startup |
| 2 | `--allowedTools` per agent role | Medium — Claude Code enforced |
| 3 | PreToolUse hooks | Hard — enforced even with `--bypass-all-permissions` |
| 4 | Git worktree isolation | Physical — agents can't touch main repo |
| 5 | Human PR review | Final gate — nothing merges without you |

**PreToolUse hooks:**
- `path-guard.sh` — confines Write/Edit to worktree directory
- `bash-firewall.sh` — blocks destructive commands (rm -rf, curl|bash, force push, etc.)
- `git-guard.sh` — push whitelist (only `feat/*` branches), blocks merge, rebase, branch-delete, force push

**Notification hook:**
- `notify-permission.sh` — writes signal file when Claude Code needs tool permission approval; TUI detects and shows 🟠 status + toast notification

Hook strictness is per-project: `strict` (all hooks), `standard` (path-guard + git-guard), `relaxed` (git-guard only). Notification hook is always active in all modes.

## Issue Spec Format

The clarifier agent produces structured issues:

```markdown
## CONTEXT
[Problem description with specific file names and behavior]

## APPROACH
[Selected solution + reasoning]

## ACCEPTANCE_CRITERIA
AC-1: [Specific, verifiable condition]
AC-2: ...

## SCOPE
[modify] path/to/file (reason)
[create] path/to/new-file (reason)

## CONSTRAINTS
[Hard limits]

## BRANCH
[Optional: PR target branch, defaults to project base_branch]

## TASKS
[Optional: Task decomposition — triggers subagent delegation]
T1: [task description] | [modify] path/to/file
T2: [task description] [P] | [modify] path/to/other  # [P] = parallelizable
T3: [task description] [depends:T1] | [create] path/to/new

## COORDINATES_WITH
[Optional: Cross-agent coordination for parallel work]
#42: Brief description of related issue

## REFERENCES
[Optional: URLs, related files]
```

## Development

```bash
go build ./...           # Build
go test ./...            # Run all tests
make test-hooks          # Run hook tests (requires bash)
go run .                 # Local TUI (or auto-serve if ssh.auto_serve=true)
go run . serve           # SSH server mode
go run . connect         # SSH client shortcut
```

Logs: `~/.zpit/logs/zpit-YYYY-MM-DD.log` — daily rotation, 30-day retention.

## Architecture

See [docs/architecture/](docs/architecture/) for the full architecture documents (split by topic, with an [index](docs/architecture/README.md)).

## Open Source Attributions

Zpit is built on top of the following open source libraries:

| Library | Purpose | License |
|---------|---------|---------|
| [Bubble Tea](https://github.com/charmbracelet/bubbletea) | TUI framework | MIT |
| [Bubbles](https://github.com/charmbracelet/bubbles) | TUI components (list, text input, etc.) | MIT |
| [Lip Gloss](https://github.com/charmbracelet/lipgloss) | TUI styling and layout | MIT |
| [Huh](https://github.com/charmbracelet/huh) | Form and confirm dialogs | MIT |
| [Wish](https://github.com/charmbracelet/wish) | SSH server for TUI remote access | MIT |
| [BurntSushi/toml](https://github.com/BurntSushi/toml) | TOML config parser | MIT |
| [fsnotify](https://github.com/fsnotify/fsnotify) | Filesystem watcher (session log monitoring) | BSD-3-Clause |
| [go-colorful](https://github.com/lucasb-eyer/go-colorful) | Color math for terminal rendering | MIT |
| [muesli/termenv](https://github.com/muesli/termenv) | Terminal environment detection | MIT |
| [rivo/uniseg](https://github.com/rivo/uniseg) | Unicode text segmentation | MIT |
| [mattn/go-runewidth](https://github.com/mattn/go-runewidth) | Rune display width calculation | MIT |
| [bubbletea-overlay](https://github.com/rmhubbert/bubbletea-overlay) | Overlay rendering for confirm dialogs | MIT |
| [golang.org/x/sys, x/text](https://pkg.go.dev/golang.org/x) | Go extended standard library | BSD-3-Clause |

All Charmbracelet libraries (`bubbletea`, `bubbles`, `lipgloss`, `huh`, `wish`, `ssh`) are copyright © Charmbracelet, Inc., licensed under the MIT License.
`fsnotify` and `golang.org/x/*` are BSD-3-Clause; their copyright notices are retained as required.

## License

MIT
