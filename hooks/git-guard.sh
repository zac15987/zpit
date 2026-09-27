#!/usr/bin/env bash
set -euo pipefail

# Git Guard — PreToolUse hook for Bash
# Blocks git operations that agents should not perform.
# Exit 0 = allow, Exit 2 = block

# Skip enforcement for non-agent sessions (plain Claude Code) — checked
# BEFORE the jq dependency check so non-zpit users aren't blocked when
# jq is absent.
[ -z "${ZPIT_AGENT:-}" ] && exit 0

# Require jq: hook parses stdin JSON via jq. Fail closed when missing —
# without jq the firewall cannot inspect commands, so destructive git
# operations would otherwise slip through as "non-blocking" hook errors.
if ! command -v jq >/dev/null 2>&1; then
  echo "BLOCKED: 'jq' is required for zpit safety hooks but is not installed." >&2
  echo "Install it:  winget install jqlang.jq  |  brew install jq  |  apt install jq" >&2
  exit 2
fi

COMMAND=$(cat | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

# Only process git commands — non-git goes to bash-firewall
echo "$COMMAND" | grep -qiE '^\s*git\s' || exit 0

# --- Push whitelist ---
# Agents may only push feat/* branches (needed to open PRs).
if echo "$COMMAND" | grep -qiE 'git\s+push'; then
  # Always block force push. Match -f / --force / --force-with-lease only when
  # they appear as standalone flags (surrounded by whitespace or at start/end),
  # so branch names like "feat/89-...-fetch-pull" aren't false positives.
  if echo "$COMMAND" | grep -qiE '(^|[[:space:]])(-f|--force[a-z-]*)([[:space:]]|$)'; then
    echo "BLOCKED: Force push is not allowed." >&2
    exit 2
  fi
  # Allow if command contains a feat/ branch name
  if echo "$COMMAND" | grep -qE 'feat/'; then
    exit 0
  fi
  # Block everything else (bare push, push to main/dev, etc.)
  echo "BLOCKED: Only pushing feat/* branches is allowed. Other push operations are managed by Zpit." >&2
  exit 2
fi

# Per-subagent worktree cleanup: allow `git branch -D` when every branch arg
# matches the parallel-subagent naming convention `<parent>-agent-<hex>` (what
# worktree-create.sh produces using Claude Code's default isolation slug).
# Arbitrary `git branch -D` still falls through to the blocklist below.
#
# Branch args end at the first shell control/redirect char, so a trailing
# `2>&1 | tail -20` doesn't void the whitelist. That trailing part is NOT
# trusted: it becomes CHECK_CMD and still runs through the blocklist, so
# `git branch -D x-agent-1 && git branch -D dev` stays blocked.
CHECK_CMD="$COMMAND"
IS_BRANCH_DELETE=0
if [[ "$COMMAND" =~ ^[[:space:]]*git[[:space:]]+branch[[:space:]]+-[dD][[:space:]]+(.+)$ ]]; then
  IS_BRANCH_DELETE=1
  rest="${BASH_REMATCH[1]}"
  cut_chars='[;&|<>`$()]'
  head="${rest%%$cut_chars*}"
  tail="${rest:${#head}}"
  read -ra branch_args <<< "$head"
  # `2>&1` / `2>/dev/null`: the fd number is glued to the redirect, not a branch.
  if [[ "$tail" == [\<\>]* && "$head" != *[[:space:]] && ${#branch_args[@]} -gt 0 \
        && "${branch_args[-1]}" =~ ^[0-9]+$ ]]; then
    unset 'branch_args[-1]'
  fi
  all_subagent_branches=1
  [ ${#branch_args[@]} -eq 0 ] && all_subagent_branches=0
  for b in "${branch_args[@]}"; do
    if ! [[ "$b" =~ -agent-[0-9a-f]+$ ]]; then
      all_subagent_branches=0
      break
    fi
  done
  if [ "$all_subagent_branches" = "1" ]; then
    [ -z "$tail" ] && exit 0
    CHECK_CMD="$tail"
  fi
fi

# Blocked git operations
GIT_BLOCKED=(
  'git\s+reset\s+--hard'
  'git\s+clean\s+-fd'
  'git\s+checkout\s+(main|master|develop)'
  'git\s+branch\s+-[dD]\s'
  'git\s+merge\s'
  'git\s+rebase\s'
  'git\s+tag\s'
  'git\s+remote\s+(add|set-url|remove)'
  'git\s+stash\s+drop'
  'git\s+add\s+-A'
  'git\s+add\s+\.'
)

# Check if grep supports -P (PCRE). Fall back to -E if not.
GREP_FLAG="-P"
echo "test" | grep -P "test" > /dev/null 2>&1 || GREP_FLAG="-E"

for pattern in "${GIT_BLOCKED[@]}"; do
  if echo "$CHECK_CMD" | grep -qi${GREP_FLAG:1} "$pattern"; then
    if [ "$IS_BRANCH_DELETE" = "1" ] && [ "$CHECK_CMD" = "$COMMAND" ]; then
      echo "BLOCKED: 'git branch -D' is only allowed for parallel-subagent branches (<parent>-agent-<hex>), and every argument must be such a branch. For post-batch cleanup, retry as a standalone call: git branch -D <branch1> <branch2> ..." >&2
      exit 2
    fi
    echo "BLOCKED: Git operation '$COMMAND' is not allowed. Agents should only commit to the worktree branch." >&2
    exit 2
  fi
done

# Allowed git operations (for documentation):
# git add <specific-file>       ✓
# git commit                    ✓
# git status                    ✓
# git diff                      ✓
# git log                       ✓
# git push feat/* branch        ✓ (whitelist above)

exit 0
