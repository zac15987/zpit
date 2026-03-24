#!/usr/bin/env bash
set -euo pipefail

# Git Guard — PreToolUse hook for Bash
# Blocks git operations that agents should not perform.
# Exit 0 = allow, Exit 2 = block

COMMAND=$(cat | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

# Only process git commands — non-git goes to bash-firewall
echo "$COMMAND" | grep -qiE '^\s*git\s' || exit 0

# --- Push whitelist ---
# Agents may only push feat/* branches (needed to open PRs).
if echo "$COMMAND" | grep -qiE 'git\s+push'; then
  # Always block force push
  if echo "$COMMAND" | grep -qiE '(-f|--force)'; then
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
  if echo "$COMMAND" | grep -qi${GREP_FLAG:1} "$pattern"; then
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
