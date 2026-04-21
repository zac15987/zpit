# Agent Behavioral Guidelines

Rules that all Zpit-managed AI agents must follow.
These are soft constraints (Layer 1). Hooks enforce critical rules at Layer 3.

## Git Operations

Allowed:
- `git add <specific-file>` (stage individual files only)
- `git commit`
- `git status` / `git diff` / `git log`
- `git push` to your `feat/*` branch (needed to open PRs)

When running as a parallel `[P]` teammate, the orchestrator spawns you through Claude Code's `isolation: "worktree"` mechanism, so you already have your own child worktree on your own branch. Just `git add -- <your-scoped-files>` + `git commit` normally; the orchestrator will cherry-pick your branch back onto the parent branch and remove your worktree after the batch. You do NOT need to `git push`, `git worktree remove`, or `git branch -D` yourself.

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

## Objectivity Protocol

- Prioritize technical accuracy over agreement. If the user's plan has a gap, say so before executing.
- Never use flattery phrases ("great idea", "excellent point", "you're absolutely right"). Respond directly.
- Before agreeing with a design decision, state your reasoning. Agreement without justification is not permitted.
- If you change your technical position, state what new information caused the change. Do not reverse a position simply because the user pushed back.
- When an implementation changes the semantics or ownership of an existing artifact (file, config, API, convention), flag it as a decision point — even if the overall plan is already approved.

## Re-verification Protocol

- Before any git operation (commit, push, status check for decision-making), run `git status` to confirm current branch and working tree state. Do not rely on earlier observations.
- Before editing a file, re-read it if more than 5 tool calls have passed since you last read it. File contents may have changed.
- After making changes, re-read each modified file to verify your edits are consistent with the current file state.

## Tracker Operations

- Before performing any tracker operation (create issue, post comment, update label),
  you MUST first read `.claude/docs/tracker.md`.
- Use ONLY the tools and methods specified in tracker.md — do not use other MCP servers or CLIs not listed there.
- Never embed long text directly in bash commands or MCP parameters.
  Use the Write tool + `--body-file` pattern:
  1. Use the Write tool to write content to a temp file in the working directory (e.g. `./tmp_body.md`)
  2. Use `gh` with `--body-file ./tmp_body.md` or `curl` with `-d @./tmp_body.md`
  3. Delete the temp file: `rm ./tmp_body.md`
- **Do NOT use Bash heredoc** (`cat << 'EOF' > file`) — heredoc passes content through the shell,
  which fails on long content containing backticks, single quotes, backslash paths, or mixed CJK text.

## Commit Messages

- Implementation: `[ISSUE-ID] short description`
- Revision fix: `[ISSUE-ID] fix: short description`
