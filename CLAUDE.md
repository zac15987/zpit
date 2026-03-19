# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Zpit is a TUI-based AI development cockpit written in Go with Bubble Tea. It acts as a **dispatch center** (not a wrapper) — it selects projects, launches Claude Code agents in separate terminal windows, monitors their progress via session logs, and coordinates the full issue lifecycle from requirement clarification to PR.

Key design principle: Claude Code runs in independent terminal windows. The TUI monitors via `tail` on session logs (`~/.claude/projects/<hash>/sessions/*.jsonl`), never wrapping or embedding Claude Code directly.

## Build & Run

```bash
go build ./...           # Build
go test ./...            # Run all tests
go test ./internal/...   # Run a specific package's tests
go test -run TestName    # Run a single test
```

## Architecture

### Core Engine Modules

The system is composed of four core modules under the TUI:

- **ProjectMgr** — reads `~/.config/zpit/config.toml`, manages project definitions with per-OS paths (`windows` / `wsl`)
- **Tracker Provider** — abstract `IssueTracker` interface with implementations for Plane, Linear, GitHub Issues, and Forgejo Issues. Internal status constants (`todo`, `in_progress`, `ai_review`, `waiting_review`, `needs_verify`, `done`) are mapped per-provider.
- **Terminal Launcher** — detects environment (Windows Terminal vs tmux) and launches Claude Code in new tabs/windows. Behavior controlled by `config.toml [terminal]` section.
- **LogTail (Session Log Watcher)** — uses fsnotify to watch Claude Code session logs, parses events (`tool_use`, `bash`, `thinking`), and feeds them into Bubble Tea's Update loop as `AgentEvent` messages.

### Provider Abstraction

Two key interfaces drive extensibility:

```go
type IssueTracker interface {
    ListIssues, GetIssue, CreateIssue, UpdateStatus, AddComment
}

type GitHost interface {
    CreatePR, GetPRStatus
}
```

Each project in `config.toml` references a tracker and git provider by key. Providers are defined under `[providers.tracker.*]` and `[providers.git.*]`.

### Project Profiles

Each project has a `profile` field (`machine` | `web` | `desktop` | `android`) that determines:
- Build/verify commands (msbuild, npm, gradlew)
- Log policy strictness (strict/standard/minimal)
- Post-implementation steps (ai_review, open_pr, auto_deploy)
- Auto-applied issue labels (e.g., "needs hardware verification")

### Git Worktree Parallel Development

Multiple agents can work on the same project simultaneously using git worktrees:
- Worktrees are created under a centralized `base_dir` (e.g., `D:/Projects/.worktrees/{project_id}/{issue_id}--{slug}`)
- Each agent gets its own worktree directory and branch
- `.claude/` directory (agents, hooks, settings) is inherited automatically
- Loop engine checks for file-level conflicts before assigning concurrent issues
- Cleanup (worktree remove + branch delete) happens automatically after PR merge

### Issue Spec — The Agent Communication Contract

Issues use a strict structured format (§6.2) with mandatory sections: `## CONTEXT`, `## APPROACH`, `## ACCEPTANCE_CRITERIA`, `## SCOPE`, `## CONSTRAINTS`, and optional `## REFERENCES`. This is the **only** communication interface between Clarifier, Coding Agent, and Reviewer agents. Zpit validates completeness before pushing to any tracker.

### Agent Roles

Three agent types live in each managed project's `.claude/agents/`:
- **Clarifier** (`clarifier.md`) — read-only, turns vague requirements into structured Issue Specs, proposes and compares technical approaches, pushes issues as "pending_confirm"
- **Reviewer** (`reviewer.md`) — read-only, checks implementation against AC criteria item-by-item, outputs structured Review Report with PASS/NEEDS CHANGES verdict
- **Coding Agent** — launched by Loop via `claude -p` with a prompt assembled from the Issue Spec template (§6.5)

### Automation Loop Flow

`[l]` key triggers: poll tracker for Todo issues → conflict precheck → create branch + worktree → launch Claude Code in new terminal → verify (build) → AI review → open PR → update tracker → move to next issue without waiting for human review.

### Hook-Based Safety System (5 Layers)

1. **CLAUDE.md behavioral guidelines** (soft)
2. **--allowedTools per agent role** (medium)
3. **PreToolUse hooks** (hard — enforced even with bypass-all-permissions):
   - `path-guard.sh` — Write/Edit confined to worktree dir; denies `.claude/`, `CLAUDE.md`, `.git/`, `.env`
   - `bash-firewall.sh` — blocks destructive commands (rm -rf, curl|bash, force push, etc.)
   - `git-guard.sh` — blocks push, merge, rebase, branch delete; agents only commit
4. **Git worktree isolation** (physical)
5. **Human PR review** (final gate)

Hook strictness is per-project via `hook_mode`: `strict` (all hooks), `standard` (path-guard + git-guard), `relaxed` (git-guard only). Applied via `settings.local.json` overlay at worktree creation.

## Config Location

`~/.config/zpit/config.toml` — terminal settings, notification preferences, provider credentials (via env var references), and all project definitions.

## Conventions

- Branch naming: `feat/ISSUE-ID-description` or `fix/ISSUE-ID-description`
- Commit messages: `[ISSUE-ID] short description`
- Issue statuses flow: pending_confirm → todo → in_progress → ai_review → waiting_review → (needs_verify) → done
- Agents must stop and ask on uncertain technical decisions, even with bypass-all-permissions enabled
- Hook exit codes: 0 = allow, 2 = block (stderr message fed back to Claude), never use exit 1 for safety hooks
