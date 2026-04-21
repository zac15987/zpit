#!/usr/bin/env bash
set -euo pipefail

# WorktreeCreate — Claude Code hook invoked when a subagent is spawned
# with `isolation: 'worktree'`. Bypasses Claude Code's built-in worktree
# creation (which would fork from origin/<defaultBranch>) so zpit can
# fork from the orchestrator's current HEAD instead — otherwise
# sequential task work (e.g. T1 committed, then [P] batch depending on
# T1) would be invisible to [P] teammates.
#
# Input (stdin, JSON):  { name: <slug>, cwd: <orchestrator CWD>, ... }
# Output (stdout):      absolute path to created worktree

# Non-zpit Claude Code sessions inherit zpit's settings.local.json when
# the project has zpit-deployed assets, but they don't want zpit's HEAD-
# based fork semantics. Exiting 0 with no output makes Claude Code error
# the specific isolation:'worktree' call (discoverable by the user)
# instead of silently producing a zpit-style worktree.
[ -z "${ZPIT_AGENT:-}" ] && exit 0

INPUT=$(cat)
NAME=$(echo "$INPUT" | jq -r '.name // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')

if [ -z "$NAME" ] || [ -z "$CWD" ]; then
  echo "WorktreeCreate: missing name or cwd in hook input" >&2
  exit 1
fi

# Sanitize slug: / → +, then anything else outside [A-Za-z0-9._+-] → -.
# Keeps Claude-Code-style identifiers (e.g. agent-a1b2c3d4) intact while
# rejecting shell-meaningful characters in branch/path positions.
SLUG=$(echo "$NAME" | sed 's|/|+|g; s|[^A-Za-z0-9._+-]|-|g')

ORCHESTRATOR_BRANCH=$(git -C "$CWD" rev-parse --abbrev-ref HEAD 2>/dev/null || true)
if [ -z "$ORCHESTRATOR_BRANCH" ] || [ "$ORCHESTRATOR_BRANCH" = "HEAD" ]; then
  echo "WorktreeCreate: orchestrator is in detached HEAD at $CWD — cannot fork a child worktree" >&2
  exit 1
fi

WT_PATH="$CWD/.zpit-children/$SLUG"
WT_BRANCH="${ORCHESTRATOR_BRANCH}-${SLUG}"

mkdir -p "$CWD/.zpit-children"

# Fork from current HEAD (not origin/defaultBranch). -B resets any orphan
# branch left behind by a previously removed worktree dir (matches Claude
# Code's built-in semantics in worktree.ts:328).
git -C "$CWD" worktree add -B "$WT_BRANCH" "$WT_PATH" HEAD >&2

# Roll back the worktree+branch if asset copy fails — otherwise the
# subagent gets handed a child without .claude/hooks or agent-guidelines
# and will fail in confusing ways.
cleanup_on_error() {
  git -C "$CWD" worktree remove --force "$WT_PATH" 2>/dev/null || true
  git -C "$CWD" branch -D "$WT_BRANCH" 2>/dev/null || true
}
trap cleanup_on_error ERR

# .claude/ (agents/, docs/, hooks/) and .mcp.json are gitignored in the
# parent, so `git worktree add` does not bring them over. Copy them so
# teammate hooks fire, task-runner can Read agent-guidelines, and MCP
# channel tools are available if the teammate opts in.
if [ -d "$CWD/.claude" ]; then
  cp -r "$CWD/.claude" "$WT_PATH/.claude"
fi
if [ -f "$CWD/.mcp.json" ]; then
  cp "$CWD/.mcp.json" "$WT_PATH/.mcp.json"
fi

trap - ERR

# Claude Code reads the first non-empty stdout line as the worktree path.
echo "$WT_PATH"
