# Agent Behavioral Guidelines

Rules that all Zpit-managed AI agents must follow.
These are soft constraints (Layer 1). Hooks enforce critical rules at Layer 3.

## Git Operations

Allowed:
- `git add <specific-file>` (stage individual files only)
- `git commit`
- `git status` / `git diff` / `git log`
- `git push` to your `feat/*` branch (needed to open PRs)

Forbidden:
- Force push (`--force`, `-f`)
- Push to protected branches (`main`, `master`, `develop`, `dev`)
- `merge`, `rebase`, `branch -d/-D`, `tag`, `remote add/set-url/remove`
- `git add -A` / `git add .` (too broad — stage specific files only)
- `git reset --hard`, `git clean -fd`, `git stash drop`

## File Operations

Forbidden to modify:
- `.claude/` directory (hooks, settings, docs — managed by Zpit)
- `.git/` directory
- `.env` files (secrets)
- `CLAUDE.md` (project-owned, not agent-managed)

Stay within your designated SCOPE. If you must modify files outside SCOPE,
stop immediately and ask the user.

## Decision Protocol

Stop and ask the user when:
- The approach is unclear or has multiple valid options
- You need to modify files outside the designated SCOPE
- A constraint conflicts with the approach
- Any hardware-related logic you are unsure about (timeout values, safe-state behavior, etc.)

Never proceed with an uncertain technical decision on your own.

## Tracker Operations

- Write long text (PR body, issue body, comments) to a temp file first,
  then read it back with the Read tool before passing to the API
- Never embed long text directly in bash commands or MCP parameters
- Delete temp files after use
- Follow `.claude/docs/tracker.md` for API-specific instructions

## Commit Messages

- Implementation: `[ISSUE-ID] short description`
- Revision fix: `[ISSUE-ID] fix: short description`
