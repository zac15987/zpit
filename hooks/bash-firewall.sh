#!/usr/bin/env bash
set -euo pipefail

# Bash Firewall — PreToolUse hook for Bash
# Blocks destructive or dangerous shell commands.
# Exit 0 = allow, Exit 2 = block

COMMAND=$(cat | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

# Blocked command patterns
BLOCKED_PATTERNS=(
  # Destructive file operations
  'rm\s+-(r|f|rf|fr)\s+/'
  'rm\s+-(r|f|rf|fr)\s+\.\.'
  'rm\s+-(r|f|rf|fr)\s+~'
  'rmdir\s+--ignore-fail'

  # System-level
  'chmod\s+777'
  'chmod\s+-R'
  'chown\s+-R'
  'mkfs'
  'dd\s+if='
  'shutdown'
  'reboot'
  '>\s*/dev/sd'

  # Network risk
  'curl\s.*\|\s*(ba)?sh'
  'wget\s.*\|\s*(ba)?sh'
  'npm\s+publish'
  'dotnet\s+nuget\s+push'
  'pip\s+.*upload'

  # Global package installs
  'npm\s+i(nstall)?\s+-g'

  # Process management
  'kill\s+-9\s+1$'
  'killall'
  'pkill\s+-9'
)

# Check if grep supports -P (PCRE). Fall back to -E if not.
GREP_FLAG="-P"
echo "test" | grep -P "test" > /dev/null 2>&1 || GREP_FLAG="-E"

for pattern in "${BLOCKED_PATTERNS[@]}"; do
  if echo "$COMMAND" | grep -qi${GREP_FLAG:1} "$pattern"; then
    echo "BLOCKED: Dangerous command detected — '$COMMAND'. If this is truly needed, ask the user to run it manually." >&2
    exit 2
  fi
done

# Redirect escape detection — block writes outside worktree
ALLOWED_DIR="${CLAUDE_PROJECT_DIR:-$(pwd)}"
if echo "$COMMAND" | grep -qP '>+\s*/(?!tmp)' 2>/dev/null || echo "$COMMAND" | grep -qE '>+\s*/[^t]' 2>/dev/null; then
  REDIRECT_TARGET=$(echo "$COMMAND" | grep -oP '>+\s*\K/[^\s;|&]+' 2>/dev/null | head -1 || echo "$COMMAND" | grep -oE '>\s*/[^ ;|&]+' | sed 's/>\s*//' | head -1)
  if [ -n "$REDIRECT_TARGET" ] && [[ "$REDIRECT_TARGET" != "${ALLOWED_DIR}"/* ]]; then
    echo "BLOCKED: Redirect target '$REDIRECT_TARGET' is outside the working directory." >&2
    exit 2
  fi
fi

exit 0
