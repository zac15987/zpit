# 2. TUI Interface Design

> **Note: The mockups below are design blueprints; some features are not yet implemented. Implementation status is noted for each feature.**

---

## 2.1 Main Screen — Dock Layout (Catppuccin Mocha + Four Independently Scrollable Panels) ✅ Implemented

The main screen uses a lazygit-style dock layout: the left column stacks Projects / Active Terminals / Loop Engine from top to bottom, while the right column is fixed to Hotkeys. Each of the four panels has its own independent `viewport.Model` for independent vertical scrolling. The color palette is Catppuccin Mocha; there are no borders — only a single-column-wide `▎` mauve accent bar, which appears exclusively on the chrome of the currently focused panel's title row.

```
 Zpit v0.1                                            04/19 15:04  Windows Terminal


▎ Projects  7                                         Hotkeys
  ──────                                              ──────
  › AI Inspection Cleaning Demo  ⚪ Not Deployed       [Enter] Launch Claude Code
     machine │ wpf, ethercat, basler                  [c] Clarify requirements
                                                      [l] Loop auto-implement
    ENR DUC  ⚪ Not Deployed                          [r] Review changes
     machine │ wpf, secsgem                           [f] Efficiency Agent
                                                      [s] Status overview
    DisplayProfileManager  ⚪ Not Deployed            [o] Open project folder
     desktop │ wpf, nlog                              [i] Open Issue Tracker
                                                      [p] Open PR
    Zpit  🟢 Deployed                                 [u] Remove deployed files
     terminal │ go, bubbletea                         [d] Redeploy all agents
                                                      [m] Channel communication
    Zplex  ⚪ Not Deployed                             [g] View Git status
     desktop │ go, electron, xterm                    [G] Open lazygit
                                                      [U] Run claude update
    Zacfuse  🟢 Deployed                              [a] Add project
     web │ astro, typescript, docs                    [e] Edit config
                                                      [w] Desktop Agent
                                                      [x] Close terminal
                                                      [Tab] Switch panel
  Active Terminals  1                                 [?] Help
  ──────                                              [q] Quit
  ›[1] Zpit │ 🟡 Awaiting input 00:15
      Q: Commit 2198be6 already pushed to `origin/dev`, working tre


  Press ? for help, q to quit
```

**Layout rules:**
- Left/right ratio 70/30; Hotkeys minimum 22 columns, left column minimum 18 columns — when width is insufficient both sides compress against each other (they do not stack below)
- Left column height is distributed by weight (Projects 3 / Terminals 2 / Loop 2); empty panels collapse and remaining space is given to Projects
- The `▎` mauve bar is a *panel-level* indicator — it only appears on the chrome of the focused panel; body rows do not carry `▎`
- Each panel title has a count badge on the right (e.g. `Projects 7`) and a short 6-character rule below (surface1 dim color)
- Stacked panels (Terminals, Loop) have a blank gutter row before their chrome, providing visual breathing room from the panel above

**Interactions:**
- ↑↓ to select: moves the cursor of the currently focused panel, simultaneously triggering cursor-follow scrolling in that panel's viewport
- PgUp / PgDn: scrolls only the focused panel; other panels' YOffset stays unchanged
- Mouse wheel: scrolls whichever panel the cursor is hovering over (hit-test based, independent of focus)
- Enter: opens Claude Code in a new terminal (new tab in Windows Terminal / new window in tmux)
- Hotkeys [c][l][r][s]: likewise launch the corresponding agent in a new terminal
- [i]: opens the project's Issue Tracker list (when focus is on a Loop Slot, opens that slot's issue page)
- [p]: opens the PR page — from the main screen opens the project's PR list; when focus is on a Loop Slot, uses `FindPRByBranch` to locate that slot's PR, falling back to `/pulls?head=<branch>` if not found
- [G]: opens lazygit in a new terminal — from the main screen uses the project root as the working directory; when focus is on a Loop Slot, uses that slot's worktree as the working directory
- [U]: runs `claude update` in a new terminal (Windows uses `cmd /c "claude update & pause"` to keep the window open; tmux uses `read -n1` to wait for a keypress)
- [u]: removes the agents/docs/hooks files that Zpit deployed to the project
- [d]: clears the existing deployment and rewrites all 4 agents (clarifier/reviewer/task-runner/efficiency) + hooks + docs, **without launching Claude** (runs after confirmation)
- Status indicator next to the project name: 🟢 Deployed (all 10 files present), 🟡 Partially deployed (some files missing or only a single agent was ever deployed), ⚪ Not deployed
- [f]: launches the Efficiency Agent (lightweight mode — no hooks, no tracker, self-review)
- [w]: launches the desktop agent (W for Window control) — controls the OS globally (mouse/keyboard/screenshot/window management) via the `zpit serve-desktop-proxy` policy gate; cwd = `$HOME`, shared as a single instance across all TUI connections (single-instance lock); no project selection required; on Linux this is a no-op and is hidden from the hotkeys panel (upstream `computer-use-mcp` has no Linux backend)
- Tab: cycles focus between panels — Projects → Active Terminals (if any) → Loop (if any slots) → Projects; the Hotkeys panel is excluded from the Tab cycle (reference-only; when space is tight the separator blank row and trailing `…` are auto-collapsed)
- [x]: when focus is on Active Terminals, closes the selected terminal (force-kills the process, requires confirmation)

---

## 2.2 Clarify Mode — Requirements Clarification Dialog (in a New Terminal) ✅ Implemented

After pressing [c], the TUI opens Claude Code + the clarifier agent in a new terminal window. The TUI itself displays a live progress summary via the session log.

> **Note: The mockup below shows what appears in the new terminal window, not a view inside the TUI itself.**
> You interact directly with Claude Code in that terminal; the TUI main screen only shows a summary status in the "Active Terminals" area.

**In the new terminal (where you interact directly with Claude Code):**

```
╔══════════════════════════════════════════════════════════════════════╗
║  Clarify │ ASE Inspection Cleaning Machine             [Esc] Back   ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  You: The EtherCAT disconnect-reconnect logic seems off              ║
║                                                                      ║
║  ┌─ Clarifier Agent ──────────────────────────────────────────┐      ║
║  │ I found the ReconnectAsync method in EtherCatService.cs.   │      ║
║  │ ...(requirements clarification dialogue)                   │      ║
║  │                                                            │      ║
║  │ ┌─ Issue Preview ────────────────────────────────────┐     │      ║
║  │ │ Title: Add retry backoff to EtherCAT reconnect     │     │      ║
║  │ │                                                    │     │      ║
║  │ │ ## CONTEXT                                         │     │      ║
║  │ │ ## APPROACH                                        │     │      ║
║  │ │ ## ACCEPTANCE_CRITERIA                              │     │      ║
║  │ │ ## SCOPE                                           │     │      ║
║  │ │ ## CONSTRAINTS                                     │     │      ║
║  │ │ ## BASE_BRANCH / ## PR_TARGET                      │     │      ║
║  │ │ ## TASKS (optional)                                │     │      ║
║  │ └────────────────────────────────────────────────────┘     │      ║
║  │                                                            │      ║
║  │ Push to Tracker? [y] Confirm  [e] Edit more  [n] Cancel    │      ║
║  └────────────────────────────────────────────────────────────┘      ║
║                                                                      ║
║  You: y                                                              ║
║                                                                      ║
║  ✓ Issue created → #ASE-47                                           ║
║  ✓ Marked pending — confirm in TUI [s] screen by pressing [y]        ║
║                                                                      ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## 2.3 Loop Status Monitor (TUI Main Screen, Live Updates) ✅ Implemented

After pressing [l], the TUI starts the automated loop. The loop runs inside a TUI goroutine (not as a separate subcommand) and drives the state machine via tracker API polling + label detection.

```
╔══════════════════════════════════════════════════════════════════════╗
║  Loop Monitor │ ASE Inspection Cleaning Machine       [Esc] Back    ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  Currently running: ASE-47 Add retry backoff to EtherCAT reconnect  ║
║  ─────────────────────────────────────────────────────────          ║
║                                                                      ║
║  Pipeline status:                                                    ║
║  ┌─────────────────────────────────────────────────────────┐         ║
║  │ ✓ building worktree                              00:03  │         ║
║  │ ✓ preparing agent                                00:01  │         ║
║  │ ✓ launching coder                                00:01  │         ║
║  │ ▸ coding                                         02:15  │         ║
║  │ ○ launching reviewer                                    │         ║
║  │ ○ reviewing                                             │         ║
║  │ ○ waiting PR merge                                      │         ║
║  │ ○ cleaning up                                           │         ║
║  └─────────────────────────────────────────────────────────┘         ║
║                                                                      ║
║  Queued:                                                             ║
║  ASE-48  Add timeout to vision calibration flow    Todo              ║
║  ASE-49  Fix cleaning head Z-axis homing sequence  Todo              ║
║                                                                      ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## 2.4 Status Overview ✅ Implemented

Press [s] to display the status of all issues for the selected project in the Issue Tracker:

```
╔══════════════════════════════════════════════════════════════════════╗
║  Status │ ASE Inspection Cleaning Machine              [Esc] Back   ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  Todo (3)                                                            ║
║    ASE-48  Add timeout to vision calibration flow        High        ║
║    ASE-49  Fix cleaning head Z-axis homing sequence      Med         ║
║    ASE-50  Add glue volume NG statistics report          Low         ║
║                                                                      ║
║  In Progress (1)                                                     ║
║    ASE-47  EtherCAT reconnect backoff      ▸ AI coding  02:15        ║
║                                                                      ║
║  AI Review (0)                                                       ║
║    (none)                                                            ║
║                                                                      ║
║  Awaiting Your Review (1)                                            ║
║    ASE-45  Refactor alarm management to Strategy   PR #42 pending    ║
║                                                                      ║
║  Awaiting Machine Verification (1)                                   ║
║    ASE-44  Fix motion axis homing sequence   Merged, pending test    ║
║                                                                      ║
║  Actions: [y] Confirm issue → Todo  [i] Open issue in browser        ║
║                                                                      ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## 2.5 Edit Config Sub-menu ✅ Implemented

Press [e] to enter the config editing sub-menu:

```
╔══════════════════════════════════════════════════════════════════════╗
║  Edit Config — Zpit                                  [Esc] Back     ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  [1] Toggle Channel                                                  ║
║  [2] Edit channel_listen                                             ║
║  [3] Open config in editor                                           ║
║                                                                      ║
║  channel_enabled = true                                              ║
║  channel_listen = [_global]                                          ║
║                                                                      ║
╠══════════════════════════════════════════════════════════════════════╣
║  [1/2/3] Select  [Esc] Back                                         ║
╚══════════════════════════════════════════════════════════════════════╝
```

Interactions:
- [1]: instantly toggles `channel_enabled` on/off, auto-updates config.toml and manages broker subscriptions
- [2]: enters a multi-select list showing all other projects + `_global`; Space to toggle, Enter to confirm
- [3]: in local mode, opens config.toml with `$EDITOR` and auto-reloads on close; in SSH remote mode, shows the file path
- [r]: manually triggers a config reload (useful after editing externally or in SSH remote mode)

---

## 2.6 Git Status Page ✅ Implemented

Press [g] to enter the Git Status page for the currently selected project. Displays branch information and the commit graph, with support for [f] fetch / [p] pull — you can sync remote changes without leaving Zpit to switch to a system terminal.

```
╔══════════════════════════════════════════════════════════════════════╗
║  Git Status │ ASE Inspection Cleaning Machine         [Esc] Back    ║
║  Branch: dev                                                         ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  Local Branches                                                      ║
║  ─────────────────────────────────────────────────────              ║
║  * dev                          ↑0 ↓2  origin/dev                   ║
║    feat/47-ethercat-backoff     ↑3 ↓0  origin/feat/47-ethercat-…    ║
║    main                         ↑0 ↓0  origin/main                  ║
║                                                                      ║
║  Remote-only Branches                                                ║
║  ─────────────────────────────────────────────────────              ║
║    origin/feat/50-ng-stats                                           ║
║    origin/feat/51-alarm-refactor                                     ║
║                                                                      ║
║  Commit Graph                                                        ║
║  ─────────────────────────────────────────────────────              ║
║  * a1b2c3f (HEAD -> dev, origin/dev) fix: alarm retry               ║
║  * d4e5f6a add: NG stats report                                      ║
║  |\                                                                  ║
║  | * 7f8e9d0 (origin/feat/47-ethercat-backoff) wip: backoff          ║
║  |/                                                                  ║
║  * 0c1d2e3 (origin/main, main) release v0.4                         ║
║                                                                      ║
╠══════════════════════════════════════════════════════════════════════╣
║  [f] Fetch  [p] Pull  [r] Refresh  [Esc] Back                       ║
╚══════════════════════════════════════════════════════════════════════╝
```

Interactions:
- `[f]`: runs `git fetch --all --prune` (30-second timeout)
- `[p]`: runs `git pull --ff-only` (30-second timeout)
- `[r]`: reloads the branch list and commit graph
- `[Esc]`: returns to ViewProjects
- `↑↓` / `PgUp PgDn`: scrolls the commit graph

Design decisions / notes:
- **Why `--ff-only`**: Matches the `main ← dev ← feature` branching model — fails safely on divergence rather than silently creating a merge commit, which would produce hard-to-handle merge conflicts inside the TUI.
- **Why `--all --prune`**: After merging a PR on mobile, GitHub auto-deletes the branch; `prune` removes stale remote refs and keeps the branch list clean.
- **Why shell out to git instead of go-git**: Writing a custom graph renderer would be ~600 LoC of over-engineering; git's native `--graph --oneline` output already includes ANSI color codes and the viewport displays them directly.
- **Concurrency model**: fetch/pull are non-blocking `tea.Cmd`s; the status bar shows `{spinner} fetching...` during the operation. Repeated keypresses during an active operation are ignored (`gitOpRunning` flag); on success the branch list and graph are automatically refreshed.

Related files:
- `internal/git/ops.go` — git exec wrappers (`FetchAll` / `PullFF` / `Branches` / `Graph`) and output parsers
- `internal/tui/gitstatus.go` — message handlers + `tea.Cmd` (`GitStatusMsg` / `GitOpDoneMsg`)
- `internal/tui/view_gitstatus.go` — render functions (branch table + graph viewport)

---

## 2.7 History (Session Browser) — `[h]` ✅ Implemented

Press `[h]` to enter the cross-machine session sync interface. Lists all encoded folders under `~/.claude/projects/` and provides a wizard for exporting and importing zip bundles.

### Folder List

```
╔══════════════════════════════════════════════════════════════════════╗
║  History — ~/.claude/projects/                       [Esc] Back     ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  Encoded Folders — ~/.claude/projects/                               ║
║  ────────────────────────────────────────────────────────────       ║
║                                                                      ║
║     [+] Import bundle...                                             ║
║                                                                      ║
║   › 🟢 D--Documents-MyProjects-zpit                                  ║
║         12 sessions  4.2 MB  04/26 14:30                             ║
║                                                                      ║
║     -home-jeff-projects-zacfuse                                      ║
║         3 sessions  812 KB  04/24 09:12                              ║
║                                                                      ║
╠══════════════════════════════════════════════════════════════════════╣
║  Enter: open  [E]: export all  [i]: import  [Esc] back  [q] quit   ║
╚══════════════════════════════════════════════════════════════════════╝
```

### Session List (drilled in)

```
╔══════════════════════════════════════════════════════════════════════╗
║  Sessions — D--Documents-MyProjects-zpit             [Esc] Back     ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║   › [x] 🧩 🟢 6f81a4c3-9e0d-...                                      ║
║         812 KB  04/26 14:30                                          ║
║                                                                      ║
║     [ ] 🧩    a3e7b2c1-8f1d-...                                      ║
║         412 KB  04/26 13:08                                          ║
║                                                                      ║
║     [ ]       2c0d4f8e-1a3b-...                                      ║
║         48 KB   04/25 22:14                                          ║
║                                                                      ║
║     1 selected                                                       ║
║                                                                      ║
╠══════════════════════════════════════════════════════════════════════╣
║  Space: select  [a]: toggle all  [e]: export  Enter: detail  [Esc] back ║
╚══════════════════════════════════════════════════════════════════════╝
```

### Hotkeys

#### Folder list
| Key | Action |
|-----|--------|
| `↑↓` | Move selection up/down between folders or `[+] Import bundle...` |
| `Enter` | Enter the session list for that folder; row 0 triggers the import wizard |
| `[E]` | Export all sessions from the focused folder (pre-selects all) |
| `[i]` | Open the import wizard |
| `Esc` | Return to the main screen |

#### Session list
| Key | Action |
|-----|--------|
| `↑↓` | Move selection up/down between sessions |
| `Space` | Toggle selection of the current row (`[x]` / `[ ]`) |
| `[a]` | Toggle-all-on (if any are unselected) or toggle-all-off |
| `[e]` | Open the export confirm modal |
| `Enter` | Attempt to open the detail view (v1 shows "coming in follow-up issue") |
| `Esc` | Return to the folder list |

### Export Flow

1. Triggered from the session list via `[e]` → if any selected session is still alive, an active-session warning modal is shown first (informational only — not a hard block).
2. Export confirm modal: shows total count, total size, a `[ ] Include memory/` checkbox (default OFF), and output path (pre-filled as `~/.zpit/exports/<folder>-<ts>.zip`).
3. On confirmation, `Pack` compresses the selected sessions, their subagent subtrees, the optional memory subtree, and the manifest into a zip.
4. On completion, the status bar shows `Exported N session(s) to <path>`.

### Import Flow

1. Triggered from the folder list via `[i]` → bundle path textinput.
2. Entering the bundle path triggers `LoadManifest`, which reads `manifest.json` from inside the zip → displays a manifest preview (source OS, source path, session count, whether memory is included).
3. Manifest preview: each session has a checkbox (all checked by default); `Space` to toggle, `[a]` to toggle-all, Enter to proceed to the next step.
4. Destination path textinput → must be an absolute path; Zpit uses `watcher.EncodeCwd` to derive the destination encoded folder.
5. Final preview: "Will write to `~/.claude/projects/<dest-encoded>/`, N sessions, memory: yes|no" — Enter to confirm.
6. Pre-flight collision detection: for each session that would collide, a 3-button modal appears (**Overwrite** / **Skip this session** / **Cancel entire import**); memory directory collisions follow the same flow.
7. `Unpack` streams each JSONL line: only JSON objects whose `"cwd"` field matches the bundle's `source_cwd` have that field rewritten to the destination cwd; all other content (including path-shaped strings appearing in chat content) is never modified. Subagent subtrees are copied verbatim.
8. On completion, a summary is shown: `written N skipped M cancelled K, memory: written|skipped|not-included`.

### Design Decisions

- **Why zip instead of tar.gz**: Windows can extract a zip with a double-click; tar.gz requires 7-Zip or a CLI tool.
- **Why path rewrite happens at import time**: the destination cwd is unknown at export time; a single bundle can be imported to different machines repeatedly.
- **Why JSON-aware rewrite**: line-by-line `json.Unmarshal` → modify the `cwd` field → re-marshal, ensuring that only the `cwd` field is changed and path strings inside chat content are never touched.
- **Why subagents are included by default but memory is not**: subagents are session-bound and required for loop-engine session replay; memory is project-scoped and may contain user-private content (email, dated notes), so it requires explicit opt-in.
- **Why the active-session warning is informational**: the user can always choose to force-export; the TUI should not hard-block the workflow.

Related files:
- `internal/sessionsync/manifest.go` — Manifest schema + JSON marshal/unmarshal + `DetectSourceOS`
- `internal/sessionsync/scan.go` — `ScanFolders` / `ScanSessions` / `ProjectsRoot`
- `internal/sessionsync/pack.go` — `Pack` zip writer + cwd auto-detection from session lines
- `internal/sessionsync/unpack.go` — `Unpack` + cwd rewrite + collision-checked write + `LoadManifest` + `DetectCollisions`
- `internal/tui/sessions.go` — cmd factories + msg handlers (entry/exit logs, active-session detection)
- `internal/tui/view_sessions.go` — folder list / session list / modal rendering
- `internal/tui/model.go` — `ViewHistory` constant, `[h]` key handler, wizard step state machine (steps 0–5 import, steps 0–3 export)
