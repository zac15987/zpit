# 9. Safety & Control

---

## 9.1 Safety Design Philosophy

```
┌─────────────────────────────────────────────────────────────┐
│  Safety Defense Layers (soft → hard)                        │
│                                                             │
│  Layer 1: agent-guidelines.md behavioral principles (soft)  │
│    → .claude/docs/agent-guidelines.md                       │
│    → what agents "should" do; relies on LLM compliance,     │
│      unreliable but useful                                   │
│                                                             │
│  Layer 2: --allowedTools permissions (medium)               │
│    → which tools an agent "can" use                         │
│    → enforced natively by Claude Code                       │
│                                                             │
│  Layer 3: PreToolUse Hook (hard) ⬅ focus of this section   │
│    → the "last gate" before a tool executes                 │
│    → effective even with bypass all permissions             │
│    → exit 2 = block operation, exit 0 = allow              │
│                                                             │
│  Layer 4: Git Worktree isolation (physical)                 │
│    → each agent works in an isolated directory              │
│    → at the filesystem level, agents can still escape via   │
│      absolute paths — hence Layer 3 path-guard is required  │
│                                                             │
│  Layer 5: Final merge gate (conditional)                    │
│    → auto_merge=false (default): human PR review; every     │
│      change requires your approval before reaching dev      │
│    → auto_merge=true: AI reviewer's ai-review PASS replaces │
│      the human gate; Zpit calls the tracker merge API       │
│      directly                                               │
│    → evaluate whether the reviewer model's quality is       │
│      trustworthy for your repo before enabling auto_merge   │
│                                                             │
│  Analogy: industrial-control safety                         │
│  Layer 1 = operating SOP → Layer 3 = software safety limit  │
│  Layer 4 = physical isolation → Layer 5 = human sign-off   │
│  bypass all permissions ≠ disabling safety limits           │
│  bypass all permissions = no confirm prompts per action,    │
│                           but limits still apply            │
└─────────────────────────────────────────────────────────────┘
```

---

## 9.2 Permission Control

| Role | Permission mode | bypass mode | Hook protection |
|------|----------------|-------------|-----------------|
| Implementation agent | Unrestricted (no `tools` in frontmatter) | ✓ recommended | ✓ all hooks |
| Review agent | disallowedTools: Edit | optional | ✓ all hooks |
| Clarifier agent | disallowedTools: Edit | optional | ✓ all hooks |
| Manual intervention (you) | all permissions | ✓ your judgment | ✓ hooks still apply |
| Agent teams subagent | inherits lead agent | inherited | ✓ hooks apply to subagents too |

**In all modes: when facing an uncertain technical decision, the agent must stop and ask you.**

---

## 9.3 ZPIT_AGENT Environment Variable

Hook scripts check the `ZPIT_AGENT` environment variable — if it is absent, they immediately `exit 0` (allow everything).
This ensures hooks only restrict agents launched by Zpit and do not affect Claude Code sessions you open manually.

**Injection method:**
- **Windows (cmd)**: the `zpit-env.cmd` wrapper script sets `ZPIT_AGENT=1` then starts claude
- **Windows (PowerShell)**: the `zpit-env.ps1` wrapper script sets `ZPIT_AGENT=1`
- **Unix**: inline-prefixed to the command (`ZPIT_AGENT=1 claude ...`)

`zpit-env.cmd` and `zpit-env.ps1` are embedded via go:embed and deployed to `.claude/hooks/`.
`zpit-exit.cmd` and `zpit-exit.ps1` provide automatic Windows Terminal tab-close for non-agent Enter launches.

---

## 9.4 Hook System Design

### 9.4.1 Hook Architecture

```
.claude/
├── settings.json          ← Hook configuration (dynamically merged by Zpit)
├── settings.local.json    ← Worktree override (not committed)
└── hooks/
    ├── path-guard.sh      ← Path guard (Write/Edit/MultiEdit)
    ├── bash-firewall.sh   ← Bash command filter
    ├── pwsh-firewall.sh   ← PowerShell command filter (PS counterpart of bash-firewall)
    ├── git-guard.sh       ← Git operation guard
    ├── notify-permission.sh ← Notification hook (writes signal file for TUI detection)
    ├── zpit-env.cmd       ← Windows cmd agent env-var wrapper
    ├── zpit-env.ps1       ← Windows PowerShell agent env-var wrapper
    ├── zpit-exit.cmd      ← Windows cmd exit wrapper (auto-close WT tab)
    └── zpit-exit.ps1      ← Windows PowerShell exit wrapper (auto-close WT tab)
```

Hook scripts are embedded in the Zpit binary via `go:embed` and automatically deployed on every agent launch (`[c]`/`[r]`/`[l]`) or redeploy (`[d]`).

**Deployment mechanism (`internal/worktree/hooks.go`):**
- Hook configuration is defined as a single Go constant (`settingsTemplate`, containing the complete hook set). Historically there were three mode templates (`strict` / `standard` / `relaxed`); after the worktree settings inheritance bug exposed by Issue #39 they were merged into a single template, and the per-project `hook_mode` field was retired.
- `DeployHooksToProject()` — deploys to the main repo: writes hook scripts + merges hook config into `.claude/settings.json` (preserving existing keys such as `enabledPlugins`).
- `DeployHooksToWorktree()` — deploys to a worktree: writes hook scripts + **writes both `.claude/settings.json` and `.claude/settings.local.json`** (identical contents, both set to `settingsTemplate`). Rationale for dual-write: Claude Code's `getSettingsRootPathForSource()` resolves the project settings root using the process CWD, and a linked git worktree does **not** inherit the main repo's `.claude/settings.json`. Without an explicit write into the worktree, `WorktreeCreate` / `path-guard` / `bash-firewall` and all other hooks silently fail (the root cause of Issue #39). Writing `settings.json` covers the project layer; writing `settings.local.json` covers the local-override layer, so any user-side override added later does not displace Zpit's hooks.

### 9.4.2 settings.json — Hook Registration Format

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit",
        "hooks": [
          { "type": "command", "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/path-guard.sh" }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/bash-firewall.sh" },
          { "type": "command", "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/git-guard.sh" }
        ]
      },
      {
        "matcher": "PowerShell",
        "hooks": [
          { "type": "command", "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/pwsh-firewall.sh" }
        ]
      }
    ],
    "Notification": [
      {
        "hooks": [
          { "type": "command", "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/notify-permission.sh" }
        ]
      }
    ]
  }
}
```

**Key points:**
- `matcher` uses regex to match tool names
- Multiple hooks can be attached to the same matcher; they run in order, and any single exit 2 blocks the action
- Hooks also apply to subagents (agent teams)
- exit 0 = allow, exit 2 = block (stderr message is fed back to Claude)
- **Never use exit 1** (non-blocking error — the action still executes)

### 9.4.3 Hook 1: Path Guard (path-guard.sh)

**Purpose:** Ensure Write/Edit only occur within the worktree directory assigned to the agent.

**Logic:**
1. Parse `tool_input.file_path` (or `.path` / `.file`) from stdin JSON
2. Check `ZPIT_AGENT` — if absent, allow (not an agent session)
3. Resolve relative paths to absolute (relative to `CLAUDE_PROJECT_DIR`)
4. **Blocklist** (blocked even when inside the worktree): `.claude/agents/`, `.claude/settings`, `CLAUDE.md`, `.git/`, `.env`
5. **Allowlist**: path must be within `CLAUDE_PROJECT_DIR`

### 9.4.4 Hook 2: Bash Firewall (bash-firewall.sh)

**Purpose:** Intercept destructive or dangerous bash commands.

**Blocked categories:**
- Destructive file operations: `rm -rf /`, `rm -rf ..`, `rm -rf ~`
- System-level: `chmod 777`, `mkfs`, `dd if=`, `shutdown`, `reboot`
- Network risks: `curl|bash`, `wget|bash`, `npm publish`, `dotnet nuget push`
- Global package installs: `npm install -g`
- Process management: `kill -9 1`, `killall`, `pkill -9`
- **Redirect classification (per-target classifier)**: extracts each `>` / `>>` / `2>` / `&>` target and classifies them individually —
  - **Allow**: discard devices (`/dev/null`, `/dev/stdout`, `/dev/stderr`, `/dev/fd/*`), OS temp scratch (`/tmp`, `/var/tmp`, macOS `/var/folders`, and un-expanded `$TMPDIR` / `$TMP` / `$TEMP` forms), paths within the worktree (relative paths or absolute paths under `CLAUDE_PROJECT_DIR`)
  - **Block**: (1) absolute paths outside the worktree (escape attempt); (2) targets whose basename is `nul` or `NUL` (case-insensitive) — git-bash does not treat `nul` as a null device, so `2>nul` creates a **real reserved-name file** in the working directory, polluting `git status` and difficult to delete (in `in_project` mode this leaves the tree dirty on the next dispatch → `SlotNeedsHuman`); the block message guides the agent to use `/dev/null` instead
  - This classifier replaces the old `(?!tmp)` / `[^t]` heuristic (which both false-blocked `/dev/null` and missed `nul`). Note: only `>` / `>>` redirects are checked; non-redirect writes such as `curl -o` or `cp` are outside scope (Write/Edit paths are covered by path-guard)
- **Clarifier role — additional blocks**: all mutation verbs (`rm` / `mv` / `cp` / `mkdir` / `touch` / `sed -i`), with a carve-out allowing `rm tmp_*.{md,txt}` and `>` redirects targeting `tmp_*.{md,txt}` so the clarifier can manage its own tracker temp files

**grep compatibility:** Tries `-P` (PCRE) first; falls back to `-E` (ERE) if unsupported.

### 9.4.5 Hook 3: PowerShell Firewall (pwsh-firewall.sh)

**Purpose:** bash-firewall only covers the Bash tool, not the PowerShell tool. On Windows, without pwsh-firewall, `Remove-Item` / `Out-File` / `Invoke-WebRequest | iex` and all other PowerShell equivalents of the blocked bash commands pass through unchecked. pwsh-firewall translates the corresponding rules into PS cmdlets / aliases to close that gap.

**Blocked categories:**
- System control: `Stop-Computer`, `Restart-Computer`, `shutdown.exe`
- Process management: `Stop-Process -Id 1`, `kill -Force 1`
- Network risks: `Invoke-WebRequest|iex`, `iwr|iex`, `curl|iex`, `wget|iex`, `New-Object Net.WebClient` and similar download-and-execute patterns
- Package publish: `npm publish`, `dotnet nuget push`, `pip ... upload`, `npm install -g`
- Destructive file operations: `Remove-Item ... -Recurse ... /` / `~` / `..`
- **Redirect classification**: same per-target classifier as bash-firewall, with the difference that the discard set adds the PowerShell-native `$null`, and the temp set adds `$env:TEMP` / `$env:TMP`. `nul` / `NUL` are blocked equally (PowerShell also does not treat `nul` as a device — it creates a reserved-name file); the block message guides the agent to use `$null` instead
- **Clarifier role — additional blocks**: PS write cmdlets and aliases (`Remove-Item` / `rm` / `ri` / `del` / `Move-Item` / `mv` / `Copy-Item` / `cp` / `New-Item` / `mkdir` / `Set-Content` / `Add-Content` / `Out-File` / `Clear-Content`), with the same `tmp_*.{md,txt}` carve-out covering `Remove-Item`, `Set-Content`, `Out-File`, and `>` redirect write paths

### 9.4.6 Hook 4: Git Operation Guard (git-guard.sh)

**Purpose:** Restrict the scope of git operations available to agents.

**Push allowlist mechanism:**
- Agents may only push `feat/*` branches (required to open a PR)
- Force push is always blocked
- Any push to a non-`feat/*` branch is blocked (including bare `git push`)

**Other blocked operations:**
- `git reset --hard`, `git clean -fd`
- `git checkout main|master|develop`
- `git branch -d/-D` (managed by Zpit)
- `git merge`, `git rebase`, `git tag`
- `git remote add|set-url|remove`
- `git stash drop`
- `git add -A`, `git add .` (prevents staging files that should not be committed)

**Allowed git operations:** `git add <specific-file>`, `git commit`, `git status`, `git diff`, `git log`, `git push feat/*`

### 9.4.7 Hook 5: Notification Hook (notify-permission.sh)

Not a safety hook. Triggered when Claude Code requires tool permissions; writes a signal file for TUI detection.

---

## 9.5 Hook Protection Level

Historically there were three per-project `hook_mode` tiers (`strict` / `standard` / `relaxed`). After the Issue #39 fix, these were consolidated into a **single fixed hook set**:

| path-guard | bash-firewall | pwsh-firewall | git-guard | notify-permission | worktree-create |
|-----------|---------------|---------------|-----------|-------------------|-----------------|
| ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

Rationale for consolidation: the `ZPIT_AGENT=1` env guard already handles the "non-zpit Claude Code sessions are unaffected by hooks" routing. Per-project hook weakening provides no real security value (when an agent goes rogue you want more gates, not fewer), and a multi-mode design is a footgun. Legacy `hook_mode = "..."` keys in `config.toml` are still parsed but are fully ignored, with a one-time deprecation warning printed at startup.

---

## 9.6 Hook Tests

Automated tests live in `hooks/hooks_test.go` (`make test-hooks`), covering all block rules.

Manual test examples:

```bash
# path-guard: should be blocked (path is outside the worktree)
echo '{"tool_input":{"file_path":"/etc/passwd"}}' | \
  CLAUDE_PROJECT_DIR="/tmp/worktree" ZPIT_AGENT=1 \
  bash .claude/hooks/path-guard.sh
echo $?   # expected: 2

# git-guard: push feat/* — allowed
echo '{"tool_input":{"command":"git push origin feat/ASE-47-fix"}}' | \
  ZPIT_AGENT=1 bash .claude/hooks/git-guard.sh
echo $?   # expected: 0

# git-guard: push main — blocked
echo '{"tool_input":{"command":"git push origin main"}}' | \
  ZPIT_AGENT=1 bash .claude/hooks/git-guard.sh
echo $?   # expected: 2
```

---

## 9.7 Git Safety

- Agents always work on a worktree + feature branch; the main repo is never touched directly
- Each agent's Claude Code working directory is the worktree path, not the main repo
- Dangerous git operations are hard-blocked by git-guard.sh
- PR merge strategy is controlled per-project by `auto_merge` (default false requires human approval; when true, an AI reviewer PASS drives the tracker merge API — this does not go through the agent's push / push-hook path)
- Worktree and branch are automatically cleaned up after a PR merge

## 9.8 Loop Safety

- Agents run in visible terminal windows; you can switch over and intervene at any time (natural safety valve)
- `max_per_project` limits the number of concurrent worktrees per project
- If an agent awaits a response for longer than `re_remind_minutes` (default 2 minutes), the TUI sends a reminder notification

---

## 9.9 Auto-Merge Security Trade-off

When `auto_merge = true`, the final human gate in Layer 5 is replaced by the AI reviewer's PASS judgment. This is a **deliberate trust transfer**, not a vulnerability — it should only be enabled when you have decided the trade-off is worthwhile for your specific project.

**Technical facts:**
- The merge API is called by Go code, not by the agent's `git push`, so it **does not pass through `git-guard.sh`**. This is intentional — git-guard prevents agents from accidentally pushing to main/master/develop/dev, whereas auto-merge is an explicit action taken by the Zpit program itself, not by agent behavior.
- When a merge fails (permanent error or transient retries exhausted), the slot enters `SlotNeedsHuman`; the worktree and branch are preserved for you to handle. Auth errors go to `SlotError`.
- Retries are limited to transient errors (5xx / 408 / 429 / network timeout); permanent errors (409 / 405 / 422) cause an immediate exit with no retry.

**Recommendations:**
- Public forks / company projects: `auto_merge = false` (default).
- Personal experiment repos / private repos: `auto_merge = true` is worth considering, but observe reviewer quality first (e.g., confirm that 10 consecutive issue reviews are reasonable before enabling).
- **Never** enable it on untrusted projects, or in contexts where the reviewer model frequently misjudges.

---

## 9.10 Desktop Agent Security Model (Exception to the 5-Layer Stack)

The desktop agent launched via `[w]` **does not follow** the 5-layer safety stack described in 9.1. This is not an oversight — the threat model is different:

| Why it does not apply | Explanation |
|---|---|
| Layer 1 (agent-guidelines.md) | The desktop agent runs in `$HOME` / `%USERPROFILE%`, not inside any project's `.claude/docs/`; agent-guidelines are not deployed |
| Layer 2 (`--allowedTools`) | Tool-name-level filtering is not granular enough — there is no way to block `key text="win+r"` while allowing `key text="enter"`; parameter-level enforcement is required |
| Layer 3 (PreToolUse hooks) | Write/Edit/Bash hooks are meaningless against `SendInput` simulating keyboard and mouse — the agent writes no files; it injects keystrokes directly into the entire desktop |
| Layer 4 (worktree isolation) | There is no worktree — the agent operates globally |
| Layer 5 (PR merge gate) | There is no PR — no commits are produced |

**Replacement: the `zpit serve-desktop-proxy` Go MCP proxy is the sole safety layer.**

The proxy intercepts every JSON-RPC `tools/call` frame and decides whether to forward or reject it according to the profile in `~/.zpit/desktop-policy.toml`. Three enforcement layers:

1. **Tool allowlist**: any tool name not on the allowlist is immediately rejected (regardless of parameters). `run_script` / `filesystem` / `process_kill` / `registry` / `notification` / `scrape` / all virtual-desktop tools are hard-blocked.
2. **Parameter policy**: `deny_keys` performs substring matching on the `key` tool (e.g. `win+r` / `ctrl+alt+del` / `alt+f4`); `allow_bundles` are user-pre-approved shortcut groups that act as exceptions.
3. **Single-instance lock**: the `AppState.activeDesktopAgent` mutex field, shared across all connected TUI clients, enforces at most one desktop agent; a second `[w]` invocation is rejected.

**Profile default (`standard`, one of 5)**: 48 tools allowed, 10 denied, 7 deny_keys, `allow_run_script = false`. See `desktop-agent.md` for the full profile table.

**OS-level last resort:** macOS Accessibility permissions / Windows UIAutomation registration must be granted manually by you; even the proxy cannot bypass them.

**Why a Go proxy rather than hooks:**
- MCP tool parameters are nested JSON; shell-based parsing scaffolding is brittle and hard to maintain
- The Go proxy can return a structured error so the agent can reason about the failure cause; a shell hook can only return an exit code + stderr
- The proxy also handles single-instance locking, which a hook cannot do

For the full selection rationale, tool-by-tool allowlist justification, and the deny_keys platform table, see [desktop-agent.md](desktop-agent.md).

**The `ZPIT_AGENT` environment variable has no effect on the desktop agent** — it is a switch read by hooks, and the desktop agent deploys no hooks, so this env var is neither set nor consulted by the proxy.

**Mental model for releases**: do not think of the desktop agent as "another zpit agent" — it is "a different safety domain." The 5-layer stack for project-scoped agents guards against "agent writing bad code / pushing to the wrong place"; the proxy policy for the desktop agent guards against "agent pressing the wrong key / opening the wrong app / running a shell command." The two defense lines run in parallel but do not support each other.
