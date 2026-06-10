# 3. System Architecture (Dispatch Model)

> **Operating modes:**
> - `zpit` — local TUI (direct operation); if `ssh.auto_serve = true`, automatically starts an SSH server and connects via SSH
> - `zpit serve` — headless SSH daemon (Wish), multiple SSH clients share a single AppState
> - `zpit connect` — SSH connection convenience wrapper

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Your Machine (Windows / WSL)                    │
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │  zpit (Go binary, Bubble Tea TUI)     ← always running       │  │
│  │                                                               │  │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐   │  │
│  │  │ Project  │  │ Status   │  │  Loop    │  │ Session    │   │  │
│  │  │ Select   │  │ Overview │  │ Monitor  │  │ Log Watcher│   │  │
│  │  └────┬─────┘  └────┬─────┘  └────┬─────┘  └─────┬──────┘   │  │
│  │       │             │             │               │           │  │
│  │       ▼             ▼             ▼               ▼           │  │
│  │  ┌─────────────────────────────────────────────────────────┐  │  │
│  │  │                    Core Engine                          │  │  │
│  │  │  ┌───────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ │  │  │
│  │  │  │ProjectMgr │ │ Tracker  │ │ Terminal │ │ LogTail  │ │  │  │
│  │  │  │(toml)     │ │ Provider │ │ Launcher │ │ (fsnotify│ │  │  │
│  │  │  │           │ │(abstract)│ │ (wt/tmux)│ │  + parse)│ │  │  │
│  │  │  │           │ │ ┌──────┐ │ │          │ │          │ │  │  │
│  │  │  │           │ │ │Forgej│ │ │          │ │          │ │  │  │
│  │  │  │           │ │ │GitHub│ │ │          │ │          │ │  │  │
│  │  │  │           │ │ └──────┘ │ │          │ │          │ │  │  │
│  │  │  └───────────┘ └──────────┘ └────┬─────┘ └────┬─────┘ │  │  │
│  │  └──────────────────────────────────┼────────────┼────────┘  │  │
│  └─────────────────────────────────────┼────────────┼───────────┘  │
│                                        │            │               │
│       ┌────────────────────────────────┘            │               │
│       ▼                                             │               │
│  ┌─────────────────────────────────────┐            │               │
│  │  Separate terminal window           │            │               │
│  │  (you can switch to it at any time) │            │               │
│  │                                     │            │               │
│  │  ┌─ WT Tab 1 ───────────────────┐  │            │               │
│  │  │ claude (ASE inspection rig)   │  │            │               │
│  │  │ > editing EtherCatService... │──┼──writes──▸ │               │
│  │  └───────────────────────────────┘  │  session   │               │
│  │  ┌─ WT Tab 2 ───────────────────┐  │  log       │               │
│  │  │ claude (personal website)    │──┼──writes──▸ │               │
│  │  └───────────────────────────────┘  │            │               │
│  └─────────────────────────────────────┘            │               │
│                                                     │               │
│  Claude Code session logs ◂─────────────────────────┘               │
│  ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl                │
│                                                                     │
│  <encoded-cwd> = project absolute path encoded (non-alphanumeric    │
│  characters replaced with -)                                        │
│  e.g. D:\Documents\MyProjects\zpit → D--Documents-MyProjects-zpit  │
│  Session log stored directly in this directory, not under sessions/ │
│                                                                     │
│  Each project repo:                                                  │
│  ├── .claude/agents/clarifier.md                                    │
│  ├── .claude/agents/reviewer.md                                     │
│  ├── .claude/agents/task-runner.md  (deployed when TASKS exist)     │
│  ├── .claude/agents/efficiency.md   (deployed by [f] or [d] key)    │
│  ├── .claude/docs/agent-guidelines.md                               │
│  ├── .claude/docs/code-construction-principles.md                   │
│  ├── .claude/docs/tracker.md                                        │
│  ├── .mcp.json                              (channel MCP config)    │
│  └── CLAUDE.md                                                      │
│                                                                     │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                        ┌────────┴────────┐
                        │  Synology NAS   │
                        │  (or cloud svc) │
                        │  ┌────────────┐ │
                        │  │ Issue      │ │
                        │  │ Tracker    │ │
                        │  │(swappable) │ │
                        │  ├────────────┤ │
                        │  │ Git Host   │ │
                        │  │(swappable) │ │
                        │  └────────────┘ │
                        └─────────────────┘
```

---

## 3.1 Terminal Launcher Module

Responsible for opening a new terminal window in the correct environment and launching Claude Code.
Behavior is controlled by the `[terminal]` section of config.toml.
Implementation lives in `internal/terminal/`; the zplex HTTP client lives in `internal/zplex/`.

### Launch Priority

Every launch entry point (`LaunchClaude`, `LaunchClaudeInDir`, `LaunchLazygit`, `LaunchClaudeUpdate`) follows the same priority chain:

```
zplex probe → Windows Terminal (wt.exe) → tmux → error
```

### zplex Backend

When `terminal.zplex_port > 0`, zpit probes the local zplex daemon before falling back to the
platform-native terminal:

1. **Probe**: `GET http://127.0.0.1:<zplex_port>/api/health` with a 500ms timeout (no caching —
   one probe per launch call). If the response is HTTP 200, the session is created via
   `POST /api/sessions` on the daemon and appears as a panel in the zplex web frontend.
2. **Fallback**: if the probe fails (timeout / non-200 / unreachable), zpit falls back to
   `platform.Detect()` (Windows Terminal → tmux → error) exactly as before.
3. **Disabled**: when `zplex_port = 0`, the probe is skipped entirely — no HTTP request is made —
   and zpit behaves as if zplex does not exist.

**POST body** sent to `POST /api/sessions`:

| Field | Value |
|---|---|
| `shell` | required |
| `title` | required |
| `cwd` | optional working directory |
| `args` | optional argument list |
| `env` | optional; **replaces** the child environment (not merged). Agent sessions that need hook enforcement send `append(os.Environ(), "ZPIT_AGENT=1", "ZPIT_AGENT_TYPE=<role>")`. Plain claude / lazygit / update sessions omit this field. |
| `source` | `"zpit"` (identity metadata) |
| `project_id` | zpit project ID |
| `issue_id` | issue ID (if applicable) |
| `role` | agent role string |
| `agent_state` | initial agent state (`"active"` for agent sessions; empty otherwise) |

The zpit-env/zpit-exit wrapper scripts are **not** used on the zplex path. Panel lifecycle is
managed by the daemon's `session.closed` event on process exit; zpit never issues a DELETE call.

**Platform package boundary**: `platform.Detect()` and the existing `platform.Env*` constants are
unchanged. A new `platform.EnvZplex` enum value (String() = `"zplex"`) is added solely as a
display marker in `LaunchResult.Env`; the detection logic itself remains in `platform.Detect()`
and is never modified.

```go
// pseudocode
func LaunchClaude(project Project, config Config) {
    path := project.PathForCurrentOS()

    // 1. zplex probe (skipped when zplex_port == 0)
    if config.Terminal.ZplexPort > 0 {
        if probeZplex(config.Terminal.ZplexPort) {
            return launchViaZplex(config.Terminal.ZplexPort, path, project)
        }
    }

    // 2. fall back to platform-native terminal
    switch detectEnvironment() {
    case WindowsTerminal:
        switch config.Terminal.WindowsMode {
        case "new_tab":
            exec("wt.exe", "new-tab", "-d", path, "--title", project.Name, "--", "claude")
        case "new_window":
            exec("wt.exe", "-w", "new", "-d", path, "--title", project.Name, "--", "claude")
        }

    case WSL_Tmux, Linux_Tmux:
        windowName := project.ID
        switch config.Terminal.TmuxMode {
        case "new_window":
            exec("tmux", "new-window", "-n", windowName, "-c", path, "claude")
        case "new_pane":
            exec("tmux", "split-window", "-h", "-c", path, "claude")
        }
    }
}
```

---

## 3.2 Session Log Watcher Module

Each Claude Code session produces a JSONL log file. The TUI monitors these files to update
status in real time without any direct communication with Claude Code.
Implementation lives in `internal/watcher/`.

**Path encoding rule:** All non-alphanumeric characters in the project's absolute path are replaced with `-` to form the directory name.
For example, `D:\Documents\MyProjects\zpit` → `~/.claude/projects/D--Documents-MyProjects-zpit/`

**Session discovery mechanism:**
- Active sessions: read `~/.claude/sessions/{pid}.json`, which contains `pid`, `sessionId`, `cwd`, and `startedAt`
- **Dual verification via PID + process name**: in addition to checking whether the PID is still running, the process name is also verified to be Claude Code (`claude`/`node`), preventing false positives from PID reuse
- Log file location: `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`
- **Auto-scan at startup**: when the TUI starts, `FindActiveSessions()` is called for every project, automatically detecting already-running Claude Code sessions and attaching watchers. Restarting zpit does not lose sessions that are already in progress
- **`/resume` session-switch detection**: when the user runs `/resume` inside Claude Code, the PID stays the same but `sessionId` changes. Zpit detects this change at two points:
  (1) `waitForLogCmd` re-reads `{pid}.json` on each retry while waiting for the JSONL file; if the sessionId has changed it returns `sessionFoundMsg` and starts over
  (2) `checkSessionLiveness` re-reads `{pid}.json` every 5 seconds for each attached watcher; if a change is detected it stops the old watcher and starts a new one

**JSONL event format:** One JSON object per line; the `type` field distinguishes event types:

| type | Description |
|------|------|
| `user` | User message, or `tool_result` response (includes `toolUseResult` metadata) |
| `assistant` | Model response; `message.content[]` contains `thinking`/`text`/`tool_use` blocks |
| `system` | System events: `turn_duration` (turn ended), `compact_boundary` (context compaction) |
| `progress` | Hook / sub-agent / web-search progress |
| `last-prompt` | Last prompt issued when the session is idle |

**Agent state detection:** Read the `message.stop_reason` of the last `type: "assistant"` event:
- `"end_turn"` → waiting for user input (triggers notification)
- `"tool_use"` → working (calling a tool)
- `null` → streaming (not yet complete)

```go
// pseudocode
func WatchSessionLog(project Project) <-chan AgentEvent {
    events := make(chan AgentEvent)
    go func() {
        encodedCwd := encodeCwd(project.Path)
        projectDir := filepath.Join(claudeHome, "projects", encodedCwd)
        sessionID := findActiveSession(project.Path)
        logPath := filepath.Join(projectDir, sessionID+".jsonl")

        watcher := fsnotify.NewWatcher()
        watcher.Add(logPath)

        for event := range watcher.Events {
            if event.Op == fsnotify.Write {
                newLines := readNewLines(logPath)
                for _, line := range newLines {
                    events <- parseSessionLog(line)
                }
            }
        }
    }()
    return events
}
```
