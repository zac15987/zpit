#!/usr/bin/env bash
set -euo pipefail

# Path Guard — PreToolUse hook for Write/Edit/MultiEdit
# Ensures file operations stay within the agent's worktree directory.
# Exit 0 = allow, Exit 2 = block

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '
  .tool_input.file_path //
  .tool_input.path //
  .tool_input.file //
  empty
')

# No file path — let other mechanisms handle it
[ -z "$FILE_PATH" ] && exit 0

# Skip enforcement for non-agent sessions (plain Claude Code)
[ -z "${ZPIT_AGENT:-}" ] && exit 0

# Allowed working directory (Claude Code sets this to cwd at startup)
ALLOWED_DIR="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# Resolve relative paths to absolute
if [[ "$FILE_PATH" != /* ]]; then
  FILE_PATH="${ALLOWED_DIR}/${FILE_PATH}"
fi
# Normalize path (remove ../ etc.) — -m allows nonexistent paths
FILE_PATH=$(realpath -m "$FILE_PATH" 2>/dev/null || echo "$FILE_PATH")

# Deny patterns — blocked even inside worktree
DENY_PATTERNS=(
  '\.claude/agents/'
  '\.claude/settings'
  '\.git/'
  '\.env'
)

for pattern in "${DENY_PATTERNS[@]}"; do
  if echo "$FILE_PATH" | grep -qE "$pattern"; then
    echo "BLOCKED: Cannot modify '$FILE_PATH' — this path is protected. Notify the user if changes are needed." >&2
    exit 2
  fi
done

# Clarifier role — only tracker temp files are writable
if [ "${ZPIT_AGENT_TYPE:-}" = "clarifier" ]; then
  BASENAME=$(basename "$FILE_PATH")
  case "$BASENAME" in
    tmp_*.md|tmp_*.txt) : ;;
    *)
      echo "BLOCKED: Clarifier role can only Write to tmp_*.{md,txt} tracker temp files. '$FILE_PATH' is not allowed. If this change is real, scope it into the Issue SCOPE section and let the Coding Agent execute it." >&2
      exit 2
      ;;
  esac
fi

# Whitelist — must be inside allowed directory
if [[ "$FILE_PATH" != "${ALLOWED_DIR}"/* ]]; then
  echo "BLOCKED: Path '$FILE_PATH' is outside the working directory '${ALLOWED_DIR}'. Agents can only modify files in their own worktree." >&2
  exit 2
fi

exit 0
