# Zpit

A TUI-based AI development cockpit that orchestrates [Claude Code](https://claude.ai/code) agents across multiple projects. Zpit acts as a **dispatch center** — it selects projects, launches agents in separate terminal windows, monitors their progress, and coordinates the full issue lifecycle from requirement clarification to PR.

> **Key principle:** Claude Code runs in independent terminal windows. Zpit never wraps or embeds it — it monitors via session logs and coordinates via issue trackers.

## TUI Preview

```
  Zpit v0.1                                    03/23 22:24  Windows Terminal

  Projects                                Hotkeys
  ────────────────────────────────        ──────────────────────────

 › ⚙️ AI Inspection Cleaning Demo         [Enter] Launch Claude Code
     machine │ wpf, ethercat, basler      [c] Clarify requirement
                                          [l] Loop auto-implement
   ⚙️ ENR DUC                             [r] Review changes
     machine │ wpf, secsgem               [s] Status overview
                                          [o] Open project folder
   🖥️ DisplayProfileManager               [p] Open Issue Tracker
     desktop │ wpf, nlog
                                          [Tab] Switch to slots
   🖥️ Zpit                                [?] Help
     desktop │ go, bubbletea              [q] Quit


  Active Terminals
  ──────────────────────────────────────────────────
  [1] AI Inspection Cleaning Demo │ 🟢 Working 02:15

  Loop Status
  ──────────────────────────────────────────────────
  AI Inspection Cleaning Demo (running)
   › 🟡 #17 Refactor config to TOML  waiting PR merge
    🟢 #19 Write README docs         coding

  Enter: open Claude  ↑↓: navigate  Tab/Esc: back
```

## Status

> **This project is under active development.** Core features are functional but some areas remain untested.

| Area | Status |
|------|--------|
| Clarifier agent → Coding agent → Reviewer agent | ✅ Working |
| Reviewer agent auto-retry (NEEDS CHANGES → Coding agent) | ⚠️ Not yet tested end-to-end |
| Worktree cleanup after PR merge | ⚠️ Not fully tested |

## How It Works

```
You (TUI)                    Claude Code Agents
    │
    ├─ [c] Clarify ──────────► Clarifier agent (new terminal)
    │   requirement              asks questions, creates structured issue
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
    └─ [Enter] ───────────────► launches Claude Code directly
```

## Features

- **Multi-project dashboard** — switch between projects with arrow keys, mouse scroll support
- **Loop engine** — fully automated: poll todo issues → create worktree → coding agent → reviewer → PR merge → cleanup
- **Agent monitoring** — real-time status via session log parsing (Working / Waiting / Ended), auto-detects running sessions on startup
- **Notifications** — Windows Toast + sound when an agent needs your input
- **Issue tracker integration** — Forgejo/Gitea and GitHub via REST API + MCP
- **5-layer safety system** — agent-guidelines.md, allowed tools, PreToolUse hooks, git worktree isolation, human PR review
- **Per-issue branch control** — clarifier asks target branch, coding agent enforces it
- **Auto-retry** — reviewer judges NEEDS CHANGES → coding agent auto-fixes → re-review (configurable rounds)

## Requirements

- [Go](https://go.dev/) 1.22+
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
```

## Configuration

Config lives at `~/.zpit/config.toml`. Override with `ZPIT_CONFIG` env var.

```toml
[terminal]
windows_mode = "new_tab"    # new_tab | new_window
tmux_mode = "new_window"    # new_window | new_pane

[notification]
tui_alert = true
windows_toast = true
sound = true
re_remind_minutes = 15

[worktree]
base_dir_windows = "D:/worktrees"
base_dir_wsl = "/mnt/d/worktrees"
max_per_project = 5
poll_seconds = 15           # todo issue polling interval
pr_poll_seconds = 30        # PR merge polling interval

# Tracker providers — token read from env var, never stored in config
[providers.tracker.my-forgejo]
type = "forgejo_issues"
url = "https://your-forgejo.example.com"
token_env = "FORGEJO_TOKEN"

# Profiles control logging strictness for agents
[profiles.machine]
log_policy = "strict"       # strict | standard | minimal

# Projects
[[projects]]
name = "My Project"
id = "my-project"
profile = "machine"
hook_mode = "strict"        # strict | standard | relaxed
tracker = "my-forgejo"
repo = "org/repo"
base_branch = "dev"
tags = ["go"]

[projects.path]
windows = "D:/Projects/my-project"
wsl = "/mnt/d/Projects/my-project"
```

## Hotkeys

| Key | Action |
|-----|--------|
| `Enter` | Launch Claude Code in new terminal |
| `c` | Clarify — open clarifier agent to create structured issue |
| `l` | Loop — toggle automated coding + review cycle |
| `r` | Review — launch reviewer agent |
| `s` | Status — view issue list from tracker |
| `o` | Open project folder |
| `p` | Open issue tracker in browser |
| `Tab` | Switch focus to Loop Status slots (↑↓ select, Enter opens Claude Code in worktree) |
| `q` | Quit |

## Loop Engine

The loop engine automates the full coding cycle:

1. **Poll** tracker for `todo` issues (configurable interval)
2. **Create worktree** — isolated git worktree from `base_branch`
3. **Launch coding agent** — writes implementation, commits, opens PR
4. **Detect PR** — polls tracker for PR by branch name
5. **Launch reviewer** — checks acceptance criteria, writes review report
6. **Verdict** — PASS (wait for human merge) or NEEDS CHANGES (auto-retry up to `max_review_rounds`)
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
- `git-guard.sh` — blocks push, merge, rebase; agents only commit

Hook strictness is per-project: `strict` (all hooks), `standard` (path-guard + git-guard), `relaxed` (git-guard only).

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

## REFERENCES
[Optional: URLs, related files]
```

## Development

```bash
go build ./...           # Build
go test ./...            # Run all tests
make test-hooks          # Run hook tests (requires bash)
```

Logs: `~/.zpit/logs/zpit-YYYY-MM-DD.log` — daily rotation, 30-day retention.

## Architecture

See [docs/zpit-architecture.md](docs/zpit-architecture.md) for the full architecture document.

## Open Source Attributions

Zpit is built on top of the following open source libraries:

| Library | Purpose | License |
|---------|---------|---------|
| [Bubble Tea](https://github.com/charmbracelet/bubbletea) | TUI framework | MIT |
| [Bubbles](https://github.com/charmbracelet/bubbles) | TUI components (list, text input, etc.) | MIT |
| [Lip Gloss](https://github.com/charmbracelet/lipgloss) | TUI styling and layout | MIT |
| [Huh](https://github.com/charmbracelet/huh) | Form and confirm dialogs | MIT |
| [BurntSushi/toml](https://github.com/BurntSushi/toml) | TOML config parser | MIT |
| [fsnotify](https://github.com/fsnotify/fsnotify) | Filesystem watcher (session log monitoring) | BSD-3-Clause |
| [go-colorful](https://github.com/lucasb-eyer/go-colorful) | Color math for terminal rendering | MIT |
| [muesli/termenv](https://github.com/muesli/termenv) | Terminal environment detection | MIT |
| [rivo/uniseg](https://github.com/rivo/uniseg) | Unicode text segmentation | MIT |
| [mattn/go-runewidth](https://github.com/mattn/go-runewidth) | Rune display width calculation | MIT |
| [golang.org/x/sys, x/text, x/sync](https://pkg.go.dev/golang.org/x) | Go extended standard library | BSD-3-Clause |

All Charmbracelet libraries (`bubbletea`, `bubbles`, `lipgloss`, `huh`) are copyright © Charmbracelet, Inc., licensed under the MIT License.
`fsnotify` and `golang.org/x/*` are BSD-3-Clause; their copyright notices are retained as required.

## License

MIT
