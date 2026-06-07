# 6. Agent Definitions and i18n

---

## 6.1 Clarifier Agent (.claude/agents/clarifier.md)

**Deployment:** Embedded in the Zpit binary via go:embed and deployed to each project's `.claude/agents/`.
Template content is identical across projects — project-specific context comes from CLAUDE.md (read automatically when the agent starts).

```yaml
name: clarifier
description: Requirements clarification and technical advisor
disallowedTools: Edit
```

**Core behaviors:**
- Transforms ambiguous requirements into a structured Issue Spec
- Proactively compares 2–3 implementation approaches and their trade-offs
- Asks one clarifying question at a time
- Confirms branch strategy (reads tracker.md for defaults; asks the user if needed)
- Self-validates Issue Spec format completeness before pushing to the Tracker
- Must receive explicit user confirmation in the terminal before pushing (label: pending)
- **Mandatory WebSearch** to look up current information (never guesses from training data)
- Uses WebFetch when reading third-party source code
- Write tool restricted to temporary files (MCP long-text workaround)

**Meeting Protocol:**

Meeting mode activates automatically when the Channel tool is available (`.mcp.json` exists → MCP server running) **and** another clarifier agent is detected via `list_projects` `agents.clarifier` count. If either condition is unmet (no channel or no other clarifier), behavior is identical to single-agent mode.

Meeting mode uses a **Facilitator/Advisor role model**:

- **Role assignment**: The first agent to broadcast `[Joining Meeting]` becomes the Facilitator; agents joining afterward automatically become Advisors.
- **Facilitator**: Drives the full workflow (steps 1–17), checks the channel for Advisor analysis before key steps, is the sole agent that asks the user questions and writes the Issue Spec, and relays user responses to Advisors.
- **Advisor**: Independently reads the codebase and sends analysis to the Facilitator, then enters follow mode — responds to Facilitator messages with agreement, disagreement, or additions; does not independently execute steps 5–17; does not ask the user questions directly (except `[⚠ Warning]` emergency alerts).
- **Convergence**: The Facilitator verifies that all SCOPE paths exist before converging; broadcasts `[Meeting Closed]` after the issue is pushed.

The meeting protocol layers on top of the original workflow (steps 1–17) as an additional communication layer for the Facilitator; Advisors do not execute the full flow independently. See the Meeting Protocol section in `agents/clarifier.md` for the full protocol specification.

Full template: `agents/clarifier.md`.

---

## 6.2 Reviewer Agent (.claude/agents/reviewer.md)

```yaml
name: reviewer
description: Code Review expert
disallowedTools: Edit
```

**Core behaviors:**
- Checks each ACCEPTANCE_CRITERIA item line by line: ✅ / ❌ / ⚠️
- Checks for SCOPE violations and CONSTRAINTS compliance
- Verifies that the PR target branch matches the expected base branch
- Spot-checks code quality per `code-construction-principles.md` (**all violations must be marked 🔴**)
- Produces a Review Report (severity markers: 🔴 MUST FIX / 🟡 SUGGEST / 🟢 NICE)
- Writes the report to both the PR comment and the issue comment
- Sets the verdict label: `ai-review` (PASS) or `needs-changes` (NEEDS CHANGES)

**Severity classification (important):**
- 🔴 MUST FIX: unmet AC, CONSTRAINTS violations, **correctness bugs (broken functionality, dead code, dangling references, noise-suppression patterns like `void x`)**, code-construction-principles violations, any technical debt that requires a follow-up PR to clean up
- 🟡 SUGGEST: **genuine style/taste preferences only** (equivalent refactors, alternative names, optional extractions). Any correctness issue must be escalated to 🔴 — "non-blocking / minor / nit" is not a valid reason to pass it
- 🟢 NICE: things done well

**Verdict rules:**
- Any 🔴 MUST FIX → NEEDS CHANGES (regardless of whether all AC are ✅)
- Any AC ❌ → NEEDS CHANGES
- SCOPE/CONSTRAINTS violation → NEEDS CHANGES (regardless of AC status)
- All AC ✅, no 🔴, only 🟡 → PASS with suggestions
- All AC ✅, no 🔴, no 🟡 → PASS

**Design motivation**: The original version treated AC as the sole correctness criterion, which caused real bugs like an undefined `.blink` CSS class and `void TAB_ANCHORS` dead code to be downgraded to 🟡 and passed through because "the AC didn't mention it," accumulating as technical debt. The new rules decouple "correctness" from AC — when the reviewer encounters clearly broken behavior, it must mark 🔴; AC silence is not a pass permit.

Full template: `agents/reviewer.md`.

---

## 6.3 Task Runner Subagent (.claude/agents/task-runner.md)

**Deployment:** Embedded in the Zpit binary via go:embed. Deployed to the worktree's `.claude/agents/` by `loopWriteAgentCmd()` only when the Issue Spec contains `## TASKS`.

```yaml
name: task-runner
description: Single-task execution subagent
tools: Read, Write, Edit, Bash, Glob, Grep
```

**Core behaviors:**
- Implements **exactly one** task (assigned by the main coding agent)
- Reads CLAUDE.md, agent-guidelines.md, and code-construction-principles.md on startup
- Modifies only files within the assigned scope; reports to the main agent if out-of-scope files need changing
- Commit format: `[ISSUE-ID] T{N}: {short description}` (uses `git add` with explicit file paths, not `-A`)
- Error handling: attempts one fix; if it still fails, reports to the main agent
- On completion provides a summary: modified files, what was implemented, commit hash (success) or error details (failure)

**Usage:**
- Sequential tasks: the main coding agent delegates one at a time via the Agent tool's `subagent_type: "task-runner"`
- Parallel tasks (`[P]`): the main coding agent dispatches a **parallel subagent batch** — one `task-runner` subagent per `[P]` task (using Claude Code's standard subagent path + `isolation: "worktree"`, **not** Claude Code's Agent Team / teammate mechanism)

**Per-Subagent Worktree Model (replaces the historical Parallel Commit Protocol v1/v2/v3):**

Each `[P]` parallel subagent runs in its own child worktree on its own branch, eliminating staging-index and `refs/heads/<branch>.lock` races at the architecture level (see `docs/known-issues.md` §2 for the historical context of what v1/v2/v3 were fixing).

Flow:

1. **Orchestrator calls the Agent tool with `isolation: "worktree"`** (this is a Claude Code runtime parameter, not a subagent frontmatter key).
2. Claude Code fires zpit's `WorktreeCreate` hook (`hooks/worktree-create.sh`). The hook reads `cwd` and `name` from stdin JSON, runs `git -C <cwd> worktree add -B <parent-branch>-<slug> $HOME/.zpit/children/<8-hex-sha256(cwd+slug)> HEAD` (two key points: (a) forked from the orchestrator's HEAD, not Claude Code's built-in `origin/<defaultBranch>`, which would miss commits from earlier sequential tasks; (b) the child path is placed under a flat short path in `$HOME/.zpit/children/` rather than the earlier nested `<cwd>/.zpit-children/<slug>` layout, because nested paths under a deep parent worktree would hit the Windows MAX_PATH limit — see known-issues §9), then copies `.claude/` and `.mcp.json` into the child and prints the worktree path to stdout for Claude Code to consume.
3. Each parallel subagent commits normally in its child worktree with `git add -- <files> && git commit` — no index isolation, no `mkdir` lock, no cross-shell env issues.
4. The Agent tool returns `{worktreePath}` to the orchestrator. **Note: `worktreeBranch` is always `undefined`** — Claude Code's `WorktreeCreate`-hook path propagates only the path, not the branch (root cause tracked in known-issues §3).
5. Before cleanup, the orchestrator discovers each subagent's branch name with a single Bash call: `for path in <paths>; do git -C "$path" rev-parse --abbrev-ref HEAD; done`. This is the authoritative source — no string inference, no naming-convention assumptions.
6. With the branch names in hand, the orchestrator runs a one-shot `git cherry-pick <branch1> <branch2> ...` (in task-ID order) on the parent worktree. Cherry-pick conflicts (a spec bug where two `[P]` tasks write the same file) are caught immediately by `git cherry-pick --abort` → stall for human intervention — no silent partial merges.
7. Cleanup is split into **two separate Bash calls** (never chained with `&&` — a hook blocking one must not take down the other):
   - Call 1: `for path in <paths>; do git worktree remove --force "$path"; done` — `--force` is used from the start because the `cp -r`-copied `.claude/` inside the child is untracked, causing a plain remove to fail.
   - Call 2: `for branch in <branches>; do git branch -D "$branch"; done` — if this call is blocked by a hook or fails, it must be retried independently; it cannot be skipped (leaving `*-agent-<hex>` orphan branches pollutes the local branch list — this is the actual bug documented in known-issues §4).

Sequential tasks do not follow this flow (no `isolation: "worktree"` call); they commit directly in the parent worktree. For parallel subagent behavioral rules, see `agents/task-runner.md` (the `Parallel Commit Protocol` section has been removed).

Known bugs and corresponding workarounds: see `docs/known-issues.md` §3 (Claude Code `WorktreeCreate` hook does not return `worktreeBranch`), §4 (`git-guard.sh` early blocking of `git branch -D`), §5 (pre-202a0f3 deploy artifacts: `.gitattributes` and `.claude/settings.local.json` `.gitignore` entries).

---

## 6.4 Efficiency Agent (.claude/agents/efficiency.md)

**Deployment:** Embedded in the Zpit binary via go:embed. Deployed via the `[f]` shortcut (or written as part of a `[d]` batch redeploy). Uses `deployAndLaunchAgentLite` (no hooks deployed, no `ZPIT_AGENT=1`).

```yaml
name: efficiency
description: Lightweight fast-track agent for rapid iteration
```

**Core behaviors:**
- Lightweight rapid-iteration agent — no Issue Spec, no tracker integration, no worktree, no hooks
- Reads CLAUDE.md, agent-guidelines.md, and code-construction-principles.md on startup
- Plan-before-act workflow: presents a modification plan (files + changes + expected behavior) and waits for user confirmation before editing
- Post-implementation self-review: re-reads modified files, compares against the plan, and evaluates quality per code-construction-principles
- Conventional commit format (feat: / fix: / refactor: / chore: / docs: / test: / style: / perf:)
- Plan mode discipline: file editing is prohibited while in plan mode

**Deployment semantic differences (vs. clarifier/reviewer):**

| Item | clarifier/reviewer | efficiency |
|------|--------------------|------------|
| Deploy function | `deployAndLaunchAgent` | `deployAndLaunchAgentLite` |
| Hooks | ✅ DeployHooksToProject | ❌ not deployed |
| ZPIT_AGENT=1 | ✅ set | ❌ not set |
| Tracker integration | ✅ label check | ❌ none |
| Worktree | ✅ (Loop mode) | ❌ works directly in project directory |

Full template: `agents/efficiency.md`.

---

## 6.5 Desktop Agent (.claude/agents/desktop.md)

**Deployment:** Embedded in the Zpit binary via go:embed. Launched via the `[w]` shortcut, but the **agent .md is not deployed to a project** — because the desktop agent is globally scoped (cwd = `$HOME` / `%USERPROFILE%`), has no `project.Path`, and has no per-project `.claude/` directory. The agent definition is written by zpit directly to `~/.claude/agents/desktop.md` (or specified at launch via `--agent`); policy is controlled by `~/.zpit/desktop-policy.toml`.

```yaml
name: desktop
description: Desktop-control agent — drives the user's desktop through the zpit desktop-proxy MCP server
model: opus[1m]
```

**Core behaviors:**
- Operates the OS via `mcp__desktop-proxy__*` MCP tools (mouse / keyboard / screenshot / window / accessibility actions)
- Reads `~/.zpit/desktop-policy.toml` on startup, plans reachable/unreachable actions per the active profile, and informs the user upfront
- Plan-before-act: presents a plan first; executes only after user confirmation
- Reply language follows user input (**not** forced to English — no commit / PR / Issue Spec is produced, so there is no artifact language consistency requirement)
- Does not proactively read or write project files (there is no project); if file access is needed, the user must guide it manually
- Uses `ToolSearch` to fetch schemas for tools that cannot be found (deferred tool mechanism)

**Deployment semantic differences (vs. other agents):**

| Item | clarifier/reviewer | efficiency | desktop |
|------|--------------------|------------|---------|
| Working directory | project root | project root | `$HOME` / `%USERPROFILE%` |
| Hooks (`.claude/hooks/*.sh`) | ✅ all | ❌ not deployed | ❌ not deployed (hooks are meaningless against SendInput) |
| ZPIT_AGENT=1 | ✅ set | ❌ not set | ❌ not set |
| Tracker integration | ✅ | ❌ | ❌ |
| Worktree | Loop mode ✅ | ❌ | ❌ |
| Safety layer | 5-layer stack | Layer 1+2 | **Go MCP proxy (sole layer)** |
| Multiple instances | ✅ parallel | ✅ parallel | ❌ **single-instance lock** |
| Platform | all | all | macOS + Windows (Linux no-op) |

**Proxy architecture (replaces the hook stack):**

```
Claude Code (desktop agent) ─ stdio ─> zpit serve-desktop-proxy (Go)
                                                  │
                                                  ├── policy gate (per-call allow/deny)
                                                  └── stdio ─> npx zpit-desktop-mcp (Node)
```

Each JSON-RPC `tools/call` frame is intercepted by the proxy and evaluated against one of five profiles (`read-only` / `strict` / `standard` / `none-script` / `trusted`):

1. **Tool allowlist** — any tool not on the list is immediately rejected (hard-blocks `run_script` / `filesystem` / `process_kill` / `registry` / `notification` / `scrape` / all virtual-desktop tools / `resize_window`)
2. **Parameter policy** — `deny_keys` does substring matching on the `key` tool (default blocks Windows `win+r` / `ctrl+alt+del` / `alt+f4`; macOS `cmd+q` / `cmd+option+esc` / `cmd+ctrl+q`); `allow_bundles` are named shortcut groups that the user pre-approves as exceptions
3. **Single-instance lock** — `AppState.activeDesktopAgent` mutex is shared across all TUI connections; a second `[w]` invocation is rejected with a status toast

**Upstream:** [`zpit-desktop-mcp`](https://github.com/zac15987/computer-use-mcp) is zpit's fork (forked from [`@zavora-ai/computer-use-mcp@6.1.0`](https://github.com/zavora-ai/computer-use-mcp)), adding Windows AUMID launch, Win32 `.exe` launch + PID return, stdio-entrypoint backslash fix, and SDK Zod 4 bump.

**Why a proxy instead of hooks:** MCP tool parameters are nested JSON — shell hook parsing is fragile; a Go proxy can return structured errors that let the agent reason about the failure cause, and can simultaneously enforce the single-instance lock. For the full selection rationale, tool-by-tool allowlist justification, `deny_keys` platform table, and future Phase 2 extension points, see [desktop-agent.md](desktop-agent.md).

Full template: `agents/desktop.md`; security model details: `09-safety.md` §9.10.

---

## 6.6 go:embed Deployment Flow

Agents, hooks, and docs are embedded in the binary and deployed automatically each time an agent is launched:

```
main.go (go:embed vars)
  → NewAppState(cfg, clarifierMD, reviewerMD, taskRunnerMD, efficiencyMD, guidelinesMD, principlesMD, hookScripts, logWriter)
    → stored in AppState fields
      → DeployHooksToProject()/DeployHooksToWorktree() on every agent launch ([c]/[r]/[l]) or redeploy ([d]) — [f] uses deployAndLaunchAgentLite (no hooks)
        → writes to target project's .claude/hooks/, .claude/agents/, .claude/docs/
        → merges hook config into .claude/settings.json (or settings.local.json for worktrees)
      → loopWriteAgentCmd() deploys task-runner.md when Issue Spec contains TASKS
```

Changes to `agents/*.md`, `hooks/*.sh`, or `docs/agent-guidelines.md` require a rebuild to take effect.

---

## 6.7 Internationalization (i18n)

**Three-track strategy**: TUI chrome is localizable; coding/reviewer agents are forced to English; the clarifier follows the locale for conversation but produces English artifacts.

- **TUI strings** (localized): `internal/locale/` package, looked up via `T(key)`. `language = "en" | "zh-TW"` (config.toml); translations live in `en.go` and `zh_tw.go`. Adding a new language requires a new `locale/{lang}.go` file and a new case in `SetLanguage()`.
- **Agent output — strict English track** (coding / reviewer / revision / efficiency / task-runner): `locale.ResponseInstruction()` always returns a non-negotiable English instruction that does not change based on the `language` setting. The rule covers: agent replies, commit messages, PR descriptions, channel messages, and tracker labels. Users may input in any language; agents always respond in English.
- **Agent output — Clarifier exception track**: `locale.ClarifierResponseInstruction()` switches based on `currentLang`. When `en`, it is equivalent to the strict English rule. When `zh-TW`, it changes to: use Traditional Chinese for dialogue, status updates, and channel messages, but the **Issue Spec (title + all sections) and tracker labels are still forced to English** so that downstream coding/reviewer/task-runner agents receive a canonical artifact in a single language. Any other unknown locale falls back to strict English.
- **Agent .md files**: language instructions are injected at deploy time by `injectLangInstruction()` (strict track: `reviewer.md` / `efficiency.md` / `task-runner.md`) or `injectClarifierLangInstruction()` (exception track: `clarifier.md`), inserted after the YAML frontmatter. Both share the same `injectLangInstructionWith()` core; the only difference is the instruction string passed in.
- **Prompt builders**: `BuildCodingPrompt` / `BuildReviewerPrompt` / `BuildRevisionPrompt` call `ResponseInstruction()` (strict track) at the start of their output.
- **Domain term exception**: For proper nouns with no precise English equivalent, the Issue Spec may retain the original term in parentheses (e.g. `stocktake (盤點)`); the clarifier's conversation may use the term directly. Both the Issue Format and Meeting Protocol sections of `clarifier.md` explicitly list this rule.

**Design motivation**: CJK characters tokenize at roughly 2× the density of English in the Claude tokenizer. Forcing English on the longest and most token-heavy conversations — coding implementation traces, reviewer comments, and parallel task-runner batches — significantly reduces overall token consumption. The clarifier also converses with the user, but its exchanges are relatively short and the Q&A experience is noticeably better in the user's native language, so it gets its own exception. TUI chrome goes through `T()` and never passes through the model, so i18n has no effect on it at all.

---

## 6.8 Per-Role Model Selection

Each agent role receives its model at launch time via `--model <id>` passed to the Claude Code CLI. Controlled by the `[agent_models]` block (`internal/config/config.go:AgentModelsConfig`):

```toml
[agent_models]
clarifier = "opus[1m]"      # requirements clarification — deepest reasoning (1M context)
coding = "opus[1m]"         # feature implementation (1M context)
reviewer = "opus[1m]"       # PR review (1M context)
task_runner = "opus[1m]"    # advisory — inherited from coding session
efficiency = "opus[1m]"     # efficiency review agent — deep reasoning
```

**Design decisions:**

- **Why Opus for all roles**: In practice, running Sonnet for the coding role caused roughly 90% of loop iterations to be judged `needs-changes` on the first coding → review pass, requiring a second round before passing. A second round costs a full additional coding run plus a full additional review run, which amortizes out more expensive than a single Opus run, and the extra round-trip also slows loop throughput. The default therefore sets coding/reviewer/task_runner to `opus[1m]`; clarifier/efficiency were already single-pass high-value reasoning and remain on Opus. If you want to experiment with a lower-cost combination (e.g. `coding = "haiku[1m]"`), you can override, but verify empirically that the single-round review pass rate holds up.
- **Why add `[1m]` to all roles**: Over an agent's lifetime it may consume an entire Issue Spec, multi-file diffs, and a channel message thread. The 1M context tier eliminates context window pressure; on direct API / pay-as-you-go connections the 1M tier carries no long-context premium, so the practical effect is additional token consumption, not a higher per-token price.
- **Why aliases instead of full IDs**: On direct Anthropic API connections, aliases (`opus` → 4.7, `sonnet` → 4.6) automatically track the official latest version without needing to update config after model releases. The trade-off is cross-provider inconsistency (on Bedrock/Vertex/Foundry, `opus` resolves to 4.6); users on those providers should override with a full ID (e.g. `claude-opus-4-7[1m]`).
- **task_runner is currently advisory**: task-runner subagents are spawned by the coding orchestrator via the Claude Code Agent tool and inherit the parent session's model by default (currently Opus 4.7 1M), so the `task_runner` field is not actively used by the prompt builder today. It is retained as a future escape hatch (e.g. to run all parallel `[P]` tasks on Haiku).

**Wiring:**

- **Manual launch** (`[c]` / `[r]` / `[f]` / Enter / `[d]`): each of the six launch functions in `internal/tui/launch.go` reads the corresponding field and injects the `--model` argument. `deployAndLaunchAgent` dispatches by `agentName` via the `resolveAgentModel()` helper.
- **Loop launch** (coding / reviewer): `loopLaunchCoderCmd` / `loopWriteAndLaunchReviewerCmd` in `internal/tui/loop_cmds.go` read the corresponding fields and inject the argument.
- **Copy-before-closure**: model values are copied to a local variable outside the closure (`model := m.state.cfg.AgentModels.X`), in compliance with the AppState concurrent access rules.

**Hot-reload**: `agent_models.*` fields are hot-reloadable — changes take effect on the next agent launch; sessions already running keep the model they were launched with (Claude Code cannot change models mid-session).

---

## 6.9 CLAUDE.md Template

One file placed at the root of each target project; agents read it automatically on startup.
The following is the recommended template structure (Zpit does not auto-generate this; it is maintained by the user):

```markdown
# CLAUDE.md — [Project Name]

## Project Overview
- Type: [machine / web / desktop / android]
- Stack: [WPF .NET 4.8 / Astro / Kotlin / ...]
- Purpose: [one-sentence description]

## Architecture Principles (must not be violated)
- [e.g. all hardware operations must have a timeout]
- [e.g. UI updates must return to the UI thread]

## Code Quality Baseline
- Follow `.claude/docs/code-construction-principles.md`

## Logging Status and Standards
### Existing system
- Using: [NLog / Serilog / custom]

### Standards for new code
- Format: logger.Info("[{Module}] [{Method}] {Message}", ...)
- When touching old code: add module/method tags in passing
- Do not proactively refactor old log statements

## Agent Behavioral Principles
- When facing an uncertain technical decision, stop and ask the user
- This applies even in bypass-all-permissions mode
- When stopping, clearly state: what you are stuck on, what the options are, and what your recommendation is

## Git Standards
- Branch naming: feat/ISSUE-ID-description
- Commit message: [ISSUE-ID] short description
```
