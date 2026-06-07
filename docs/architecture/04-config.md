# 4. Config & Providers

---

## 4.1 config.toml

All Zpit data lives under `~/.zpit/`:
- `~/.zpit/config.toml` — configuration file (path can be overridden with the `ZPIT_CONFIG` env var)
- `~/.zpit/logs/` — logs (daily rotation, automatic cleanup after 30 days)

On first launch, if no config exists, a template is generated automatically (`config.WriteTemplate()`), prompting the user to edit it before restarting.

```toml
# ~/.zpit/config.toml

# ──────────────────────────────────────────────
# Terminal settings
# ──────────────────────────────────────────────
[terminal]
windows_mode = "new_tab"    # "new_tab" | "new_window"
tmux_mode = "new_window"    # "new_window" | "new_pane"
# windows_terminal_profile = "PowerShell 7"  # WT profile name for -p flag

# ──────────────────────────────────────────────
# Notification settings
# ──────────────────────────────────────────────
[notification]
tui_alert = true
windows_toast = true
sound = true
# sound_file = "D:/sounds/notify.mp3"   # Custom notification sound path (supports WAV/MP3/M4A/OGG)
re_remind_minutes = 2

# ──────────────────────────────────────────────
# Worktree settings
# ──────────────────────────────────────────────
[worktree]
base_dir_windows = "D:/Projects/.worktrees"
base_dir_wsl = "/mnt/d/Projects/.worktrees"
dir_format = "{project_id}/{issue_id}--{slug}"   # default
auto_cleanup = true           # auto-cleanup after PR merge
max_per_project = 5           # max concurrent worktrees per project
max_review_rounds = 3         # max coding↔review cycle count
poll_seconds = 10             # todo issue polling interval (seconds)
pr_poll_seconds = 10          # PR/label status polling interval (seconds)

# ──────────────────────────────────────────────
# Per-Role Model Selection
# ──────────────────────────────────────────────
# Passed to Claude Code via --model <id> at agent launch.
# Accepts short aliases (opus/sonnet/haiku — resolved to different versions per provider) or
# full model IDs (consistent behavior across providers). Append [1m] to enable the 1M-context tier.
# See §6.7.
[agent_models]
clarifier = "opus[1m]"      # requirements clarification — deepest reasoning (1M context)
coding = "opus[1m]"         # feature implementation (1M context)
reviewer = "opus[1m]"       # PR review (1M context)
task_runner = "opus[1m]"    # advisory — inherited from coding session
efficiency = "opus[1m]"     # efficiency review agent (launched via [f]) — deep reasoning

# ──────────────────────────────────────────────
# SSH Server (zpit serve)
# ──────────────────────────────────────────────
[ssh]
port = 2200                                    # default
host = "0.0.0.0"                               # default
host_key_path = "~/.zpit/ssh/host_ed25519"     # default, supports ~/ expansion
password_env = "ZPIT_SSH_PASSWORD"              # env var name, optional
authorized_keys_path = "~/.ssh/authorized_keys" # default, optional
auto_serve = false                             # default false; when true, zpit auto-starts SSH server and connects into it

# ──────────────────────────────────────────────
# Issue Tracker Providers
# ──────────────────────────────────────────────

[providers.tracker.my-forgejo]
type = "forgejo_issues"
url = "https://git.nas.local"
token_env = "FORGEJO_TOKEN"

[providers.tracker.my-github]
type = "github_issues"
token_env = "GITHUB_TOKEN"

# ──────────────────────────────────────────────
# Git Host Providers (used by agents via MCP; Zpit does not use these directly)
# ──────────────────────────────────────────────

[providers.git.forgejo-local]
type = "forgejo"
url = "https://git.nas.local"

[providers.git.github]
type = "github"

# ──────────────────────────────────────────────
# Project definitions
# ──────────────────────────────────────────────

[[projects]]
name = "ASE Inspection Cleaning Machine"
id = "ase-inspection"
profile = "machine"             # display icon: machine | desktop | web | android | terminal (TUI icon)
log_policy = "strict"           # strict | standard | minimal
isolation = "worktree"          # worktree (default, forks a git worktree per issue) | in_project (works directly in the project directory, disables [P] parallel batches, caps slot count to 1; intended for large repos where worktree copying is prohibitively expensive)
tracker = "my-forgejo"          # key pointing to providers.tracker
tracker_project = "ase-inspection"
git = "forgejo-local"
repo = "leyu/ase-inspection"
base_branch = "dev"
channel_enabled = false         # enable cross-agent channel communication
channel_listen = []             # additional project keys to subscribe, e.g. ["_global", "other-proj"]
auto_merge = false              # when true, Zpit automatically calls the tracker merge API (opt-in, default false)
merge_method = "squash"         # squash | merge | rebase, used when auto_merge=true
tags = ["wpf", "ethercat", "basler"]

[projects.path]
windows = "D:/Projects/ASE_Inspection"
wsl = "/mnt/d/Projects/ASE_Inspection"
```

---

## 4.2 Provider Abstraction Layer — TrackerClient

**Core design decision: Zpit interacts with each tracker directly via REST API.**

Two tracker backends are currently supported:

```
TrackerClient (interface)
  ├─ ForgejoClient  → Forgejo/Gitea REST API v1
  └─ GitHubClient   → GitHub REST API
```

**Why direct API instead of MCP bridging?**
- Zpit's tracker operations are simple CRUD (list issues, update labels, query PR status) — no LLM required
- Direct API responds in under 1 second; `claude -p` bridging takes 10–20 seconds — unacceptable for the Loop's frequent polling
- The `[s]` status list requires immediate response; user experience is the priority

**Agents still interact with the tracker via MCP:**
- The clarifier agent pushes issues via MCP (in the terminal, after the user confirms)
- Coding/Reviewer agents open PRs, write comments, and update labels via MCP
- MCP installation and configuration is managed by each project's `claude mcp add` — separate from the Zpit config
- Agents read `.claude/docs/tracker.md` (auto-deployed by Zpit) to know which API to use

**Auth mechanism:**
- Each provider config has a `token_env` field pointing to an environment variable name
- Zpit reads the token from the env var at startup; tokens are never stored in plain text in the config

#### TrackerClient Interface Definition

```go
// internal/tracker/client.go
type TrackerClient interface {
    ListIssues(ctx context.Context, repo string) ([]Issue, error)
    GetIssue(ctx context.Context, repo string, id string) (*Issue, error)
    UpdateLabels(ctx context.Context, repo string, id string, add, remove []string) error
    CloseIssue(ctx context.Context, repo string, id string) error
    GetPRStatus(ctx context.Context, repo string, prID string) (*PRStatus, error)
    FindPRByBranch(ctx context.Context, repo string, branch string) (*PRStatus, error)
    ListOpenPRs(ctx context.Context, repo string) ([]PRInfo, error)
}

func NewClient(providerType, baseURL, tokenEnv string) (TrackerClient, error)
```

---

**Unified issue status (used internally; label mapping for each tracker is implemented by the client):**

```go
const (
    StatusPendingConfirm = "pending_confirm"  // awaiting confirmation
    StatusTodo           = "todo"
    StatusInProgress     = "in_progress"
    StatusAIReview       = "ai_review"
    StatusWaitingReview  = "waiting_review"
    StatusNeedsVerify    = "needs_verify"     // awaiting hardware/physical verification
    StatusDone           = "done"
)
```

Status mapping per tracker:

```
Internal status     GitHub Issues    Forgejo Issues
─────────────────────────────────────────────────
pending_confirm     label:pending    label:pending
todo                label:todo       label:todo
in_progress         label:wip        label:wip
ai_review           label:ai-review  label:ai-review
waiting_review      label:review     label:review
needs_verify        label:verify     label:verify
done                closed           closed
```

**On-demand label check:**
Zpit requires 6 labels (pending, todo, wip, review, ai-review, needs-changes) but does not create them automatically at startup. When the user presses an action key (`[y]`/`[c]`/`[r]`/`[l]`), `CheckLabels` (read-only) is called each time to verify via API that all 6 labels exist. If any are missing, an overlay confirm dialog lists the missing labels; `EnsureLabels` is only called to create them after the user confirms. No in-session caching is performed, so labels deleted externally are detected immediately. Implemented via the `LabelManager` interface (`ListRepoLabels` + `CreateLabel`), satisfied by both `ForgejoClient` and `GitHubClient`.

---

## 4.3 log_policy and profile Fields

`log_policy` is a **per-project** setting injected into the Coding Agent and Reviewer Agent prompts, so that both implementation and review adhere to the corresponding logging standards. Three possible values:

| log_policy | Description |
|-----------|------|
| strict | All service methods have entry/exit logs; hardware operations have command/response logs; state machine transitions have before/after state logs |
| standard | Service methods have entry/exit logs; exceptions have full logs |
| minimal | Only errors and critical operations are logged |

Building, testing, reviewing, and opening PRs are all the agent's responsibility (agents learn the build commands from each project's `CLAUDE.md`). Zpit does not intervene in the agent's work.

The `profile` field is a **display-only label** used to select an icon in the TUI project list (machine / desktop / web / android). It has no effect on agent behavior; it may be used in the future for group classification visualization in the TUI.

---

## 4.4 Config Hot-Reload

Zpit supports reloading `config.toml` while the TUI is running. Config fields fall into two categories:

### Hot-Reloadable (applied immediately)

| Field | How it is applied |
|------|---------|
| `language` | Calls `locale.SetLanguage()` |
| `notification.*` | Calls `notifier.UpdateConfig()` |
| `worktree.poll_seconds` / `pr_poll_seconds` / `max_review_rounds` | Calls `wtManager.UpdateConfig()` |
| `terminal.*` | Updates cfg; takes effect on the next agent launch |
| `agent_models.*` | Updates cfg; takes effect on the next agent launch (already-running sessions keep the model from their original launch) |
| per-project `channel_enabled` | Dynamically subscribe/unsubscribe from the EventBus |
| per-project `channel_listen` | Dynamically manage cross-project subscriptions |
| per-project `base_branch` / `log_policy` / `isolation` / `auto_merge` / `merge_method` | Updates cfg; takes effect on the next operation (an in-flight merge uses the config captured when the handler entered; an `isolation` change applies to the next issue dispatch — an in-progress slot retains the working tree allocated under the original config). `hook_mode` is deprecated; if still present in the config it is ignored and a deprecation warning is printed. |

### Restart-Required

| Field | Reason |
|------|------|
| `broker_port` | Port is already bound |
| `ssh.*` (including `auto_serve`) | SSH server is already bound / startup behavior changes |
| `providers.*` | Tracker client must be re-initialized |
| Adding/removing `[[projects]]` | Tracker clients and UI state must be rebuilt |
| `worktree.base_dir_*` / `dir_format` / `max_per_project` | Affects path resolution for worktrees already in progress |

### Reload Mechanism

1. **Built into the TUI**: Press `[e]` → `[3]` to open `config.toml` in `$EDITOR`; the config is automatically reloaded when the editor closes
2. **Manual trigger**: Press `[r]` in the `[e]` sub-menu to manually trigger a reload (useful in SSH remote mode)
3. **Parse flow**: `config.Reload()` → `config.Diff()` classifies fields → `AppState.ApplyConfig()` applies hot-reloadable fields; restart-required fields display a prompt in the status bar

### Targeted TOML Writing

The channel quick-toggle (`[1]` toggle / `[2]` listen edit) uses `internal/config/toml_writer.go` for targeted writes:

- Operates line by line, without full TOML serialization
- Locates the correct `[[projects]]` block by its `id` field
- Modifies only the `channel_enabled` and `channel_listen` lines
- Preserves all remaining file content (including comments, blank lines, and formatting)

### Broker Lazy Start

If no project has channel enabled at startup (broker is nil), the first time the user enables `channel_enabled` on a project via the `[1]` toggle, `ToggleChannel()` starts the broker lazily. If the broker fails to start, an error is shown in the status bar and `channel_enabled` is not updated.
