#!/usr/bin/env bash
set -euo pipefail

# Bash Firewall — PreToolUse hook for Bash
# Blocks destructive or dangerous shell commands.
# Exit 0 = allow, Exit 2 = block

# Skip enforcement for non-agent sessions (plain Claude Code) — checked
# BEFORE the jq dependency check so non-zpit users aren't blocked when
# jq is absent.
[ -z "${ZPIT_AGENT:-}" ] && exit 0

# Require jq: hook parses stdin JSON via jq. Fail closed when missing —
# without jq the firewall cannot inspect commands, so destructive ones
# would otherwise slip through as "non-blocking" hook errors.
if ! command -v jq >/dev/null 2>&1; then
  echo "BLOCKED: 'jq' is required for zpit safety hooks but is not installed." >&2
  echo "Install it:  winget install jqlang.jq  |  brew install jq  |  apt install jq" >&2
  exit 2
fi

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

# Clarifier role — block mutation verbs and writes to source-code extensions
if [ "${ZPIT_AGENT_TYPE:-}" = "clarifier" ]; then
  # Allow clarifier to delete its own tracker temp files. Strict shape:
  # `rm [-f] [./]tmp_<name>.{md,txt}` — single target, optional -f. -f is
  # harmless on a single fixed-prefix file (cannot recurse, cannot cross
  # dirs) and is the bash-idiomatic way to ignore missing/read-only files;
  # -r/-R/-rf stay blocked. Mirrors the redirect carve-out below and parity
  # with pwsh-firewall.sh's -Force allowance (clarifier.md step 17b says
  # "Delete the temp file after use").
  if echo "$COMMAND" | grep -qE '^[[:space:]]*rm([[:space:]]+-f)?[[:space:]]+(\./)?tmp_[A-Za-z0-9_-]+\.(md|txt)[[:space:]]*$'; then
    exit 0
  fi

  # Mutation verbs at command start or after a separator (;, &, |, whitespace)
  CLARIFIER_BLOCKED=(
    '(^|[;&|[:space:]])rm([[:space:]]|$)'
    '(^|[;&|[:space:]])mv([[:space:]]|$)'
    '(^|[;&|[:space:]])cp([[:space:]]|$)'
    '(^|[;&|[:space:]])mkdir([[:space:]]|$)'
    '(^|[;&|[:space:]])touch([[:space:]]|$)'
    '(^|[;&|[:space:]])sed[[:space:]]+-i'
  )
  for pattern in "${CLARIFIER_BLOCKED[@]}"; do
    if echo "$COMMAND" | grep -qE "$pattern"; then
      echo "BLOCKED: Clarifier cannot execute '$COMMAND'. Only read-only commands and tracker CLI (gh / forgejo) are allowed. File changes must go through Issue SCOPE + Coding Agent." >&2
      exit 2
    fi
  done

  # Redirect to source-code file extensions — block unless target is a tmp_*.{md,txt} tracker file
  if echo "$COMMAND" | grep -qE '>[[:space:]]*[^[:space:]|&;]+\.(go|ts|tsx|js|jsx|astro|md|json|toml|yaml|yml|css|scss|sh|py|java|cs|cpp|c|h)([[:space:]]|$)'; then
    CLARIFIER_TGT=$(echo "$COMMAND" | grep -oE '>[[:space:]]*[^[:space:]|&;]+' | sed -E 's/^>[[:space:]]*//' | tail -1)
    CLARIFIER_TGT_BASE=$(basename "$CLARIFIER_TGT")
    case "$CLARIFIER_TGT_BASE" in
      tmp_*.md|tmp_*.txt) : ;;
      *)
        echo "BLOCKED: Clarifier cannot redirect output to '$CLARIFIER_TGT'. Only tmp_*.{md,txt} tracker temp files are allowed. File changes must go through Issue SCOPE + Coding Agent." >&2
        exit 2
        ;;
    esac
  fi
fi

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
