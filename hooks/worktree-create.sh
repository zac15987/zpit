#!/usr/bin/env bash
set -euo pipefail

# WorktreeCreate — Claude Code hook invoked when a subagent is spawned
# with `isolation: 'worktree'`. Bypasses Claude Code's built-in worktree
# creation (which would fork from origin/<defaultBranch>) so zpit can
# fork from the orchestrator's current HEAD instead — otherwise
# sequential task work (e.g. T1 committed, then [P] batch depending on
# T1) would be invisible to [P] parallel subagents.
#
# Input (stdin, JSON):  { name: <slug>, cwd: <orchestrator CWD>, ... }
# Output (stdout):      absolute path to created worktree

# Non-zpit Claude Code sessions inherit zpit's settings.local.json when
# the project has zpit-deployed assets, but they don't want zpit's HEAD-
# based fork semantics. Exiting 0 with no output makes Claude Code error
# the specific isolation:'worktree' call (discoverable by the user)
# instead of silently producing a zpit-style worktree.
[ -z "${ZPIT_AGENT:-}" ] && exit 0

# Require jq: hook parses stdin JSON via jq to extract name/cwd. Without
# it we cannot produce a valid worktree path — fail with a clear error
# instead of silently misbehaving.
if ! command -v jq >/dev/null 2>&1; then
  echo "WorktreeCreate: 'jq' is required but not installed. Install it (winget install jqlang.jq | brew install jq | apt install jq) and retry." >&2
  exit 1
fi

# Require sha256sum: used to derive the short hash that flattens child
# worktree paths under $HOME/.zpit/children. Ships in Git for Windows,
# WSL, coreutils, and macOS via Homebrew coreutils.
if ! command -v sha256sum >/dev/null 2>&1; then
  echo "WorktreeCreate: 'sha256sum' is required but not installed. Install coreutils (brew install coreutils on macOS) and retry." >&2
  exit 1
fi

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

# Flatten the child path under <home>/.zpit/children/<8-hex-sha256> to
# avoid Windows MAX_PATH (260). Nesting children under the orchestrator's
# worktree (the historical $CWD/.zpit-children/$SLUG layout) compounded
# every deep parent path and tripped MAX_PATH once git internals + the
# project tree + the copied .claude/ piled on top.
#
# Prefer $USERPROFILE on Windows-like shells: in Git Bash $HOME expands
# to /c/Users/<user> (Unix-style), which downstream tools that call
# `chdir`/`os.Stat` against the path (Node, Go) do not understand on
# Windows. $USERPROFILE inherits the OS-native form (C:\Users\<user>).
# Falls back to $HOME on Linux/macOS where USERPROFILE is unset.
HOME_DIR="${USERPROFILE:-$HOME}"
HASH=$(printf '%s\0%s' "$CWD" "$SLUG" | sha256sum | cut -c1-8)
WT_PATH="$HOME_DIR/.zpit/children/$HASH"
WT_BRANCH="${ORCHESTRATOR_BRANCH}-${SLUG}"

# Defense in depth: on Windows-like shells without long-path support,
# warn if the computed base path is already deep enough that git's
# internals + project files would likely overflow MAX_PATH. With the
# flattened path above this branch almost never fires — but if $HOME
# itself is unusually deep, the user gets an actionable message instead
# of a confusing `git worktree add` failure further down.
case "$(uname -s)" in
  MINGW*|CYGWIN*|MSYS*)
    LONG_PATHS=$(git config --get core.longpaths 2>/dev/null || true)
    if [ "$LONG_PATHS" != "true" ] && [ ${#WT_PATH} -gt 160 ]; then
      echo "WorktreeCreate: child worktree base path is ${#WT_PATH} chars. Windows MAX_PATH (260) will likely be exceeded once git internals + project files are added. Enable long paths:" >&2
      echo "  (1) git config --global core.longpaths true" >&2
      echo "  (2) (admin) reg add HKLM\\SYSTEM\\CurrentControlSet\\Control\\FileSystem /v LongPathsEnabled /t REG_DWORD /d 1 /f" >&2
      exit 2
    fi
    ;;
esac

mkdir -p "$HOME_DIR/.zpit/children"

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
# parallel-subagent hooks fire, task-runner can Read agent-guidelines, and
# MCP channel tools are available if the subagent opts in.
if [ -d "$CWD/.claude" ]; then
  cp -r "$CWD/.claude" "$WT_PATH/.claude"
fi
if [ -f "$CWD/.mcp.json" ]; then
  cp "$CWD/.mcp.json" "$WT_PATH/.mcp.json"
fi

trap - ERR

# Claude Code reads the first non-empty stdout line as the worktree path.
echo "$WT_PATH"
