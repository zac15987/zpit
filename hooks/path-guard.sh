#!/usr/bin/env bash
set -euo pipefail

# Path Guard — PreToolUse hook for Write/Edit/MultiEdit
# Ensures file operations stay within the agent's worktree directory.
# Exit 0 = allow, Exit 2 = block

# Skip enforcement for non-agent sessions (plain Claude Code) — checked
# BEFORE the jq dependency check so non-zpit users aren't blocked when
# jq is absent.
[ -z "${ZPIT_AGENT:-}" ] && exit 0

# Require jq: hook parses stdin JSON via jq. Fail closed when missing —
# without jq the guard cannot resolve the target path, so writes outside
# the worktree would otherwise slip through as "non-blocking" hook errors.
if ! command -v jq >/dev/null 2>&1; then
  echo "BLOCKED: 'jq' is required for zpit safety hooks but is not installed." >&2
  echo "Install it:  winget install jqlang.jq  |  brew install jq  |  apt install jq" >&2
  exit 2
fi

# ── Path helpers — duplicated in bash-firewall.sh / pwsh-firewall.sh (each
#    hook is deployed standalone, so there is no shared lib to source). ──

# is_abs_path <'/'-separated path> — POSIX absolute or Windows drive-letter.
# The drive-letter form matters: Claude Code on Windows passes OS-native
# paths (C:\foo), which a bare `/*` test mistakes for relative — the old
# check then glued them onto the worktree root and let any drive-letter
# path through the whitelist.
is_abs_path() { [[ "$1" == /* || "$1" =~ ^[A-Za-z]:/ ]]; }

# norm_path <path> [base] — '/'-separated, absolute (relative paths resolve
# against base), ./.. collapsed, drive letter upper-cased. On Git for Windows
# MSYS forms (/c/x, /tmp) are mapped to C:/x via cygpath so every source
# (Claude Code, git rev-parse, env vars) compares in one form.
norm_path() {
  local p="${1//\\//}" base="${2:-}"
  base="${base//\\//}"
  if ! is_abs_path "$p" && [ -n "$base" ]; then p="${base%/}/$p"; fi
  if [[ "$p" =~ ^[A-Za-z]:/ ]] && ! command -v cygpath >/dev/null 2>&1; then
    # Non-Windows realpath treats C:/x as relative — collapse ./.. without
    # letting it prepend the cwd.
    p=$(realpath -m -- "/$p" 2>/dev/null || printf '/%s' "$p"); p="${p#/}"
  else
    p=$(realpath -m -- "$p" 2>/dev/null || printf '%s' "$p")
  fi
  if [[ "$p" == /* ]] && command -v cygpath >/dev/null 2>&1; then
    p=$(cygpath -m -- "$p" 2>/dev/null || printf '%s' "$p")
  fi
  if [[ "$p" =~ ^([a-z]):(.*)$ ]]; then
    p="$(printf '%s' "${BASH_REMATCH[1]}" | tr '[:lower:]' '[:upper:]'):${BASH_REMATCH[2]}"
  fi
  printf '%s' "$p"
}

# Temp roots Claude Code may place the scratchpad under (CLAUDE_CODE_TMPDIR
# overrides os.tmpdir(), which follows TMPDIR / TEMP / TMP).
TEMP_ROOTS=()
for r in "${CLAUDE_CODE_TMPDIR:-}" "${TMPDIR:-}" "${TEMP:-}" "${TMP:-}" /tmp; do
  [ -n "$r" ] && TEMP_ROOTS+=("$(norm_path "$r")")
done

# is_own_scratchpad <normalized path> — true only inside THIS session's
# Claude Code scratchpad.
#   Primary: exact prefix match on `scratchpad_dir` from hook stdin — the
#   path Claude Code itself assigned (shared by the session's subagents).
#   Fallback (older Claude Code without that field): reconstruct
#   <temp-root>/claude[-*]/<encoded-cwd>/<session_id>/scratchpad/, anchored to
#   a real temp root + the exact session_id so a look-alike tree elsewhere on
#   disk, or another session's scratchpad, never qualifies.
is_own_scratchpad() {
  local p="$1" sp root rest
  if [ -n "${SCRATCHPAD_DIR:-}" ]; then
    sp=$(norm_path "$SCRATCHPAD_DIR")
    is_abs_path "$sp" && [[ "$p" == "$sp"/?* ]]
    return
  fi
  [[ "${SESSION_ID:-}" =~ ^[A-Za-z0-9_-]+$ ]] || return 1
  for root in "${TEMP_ROOTS[@]}"; do
    rest="${p#"$root"/}"
    [ "$rest" = "$p" ] && continue
    [[ "$rest" =~ ^claude(-[^/]+)?/[^/]+/${SESSION_ID}/scratchpad/[^/] ]] && return 0
  done
  return 1
}

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '
  .tool_input.file_path //
  .tool_input.path //
  .tool_input.file //
  empty
')
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
SCRATCHPAD_DIR=$(echo "$INPUT" | jq -r '.scratchpad_dir // empty')

# No file path — let other mechanisms handle it
[ -z "$FILE_PATH" ] && exit 0

# Allowed working directory — the root of the *active* git worktree.
# Claude Code pins CLAUDE_PROJECT_DIR to the orchestrator's project root
# (not the worktree path — see claude-code-source-code src/utils/hooks.ts:813),
# so when a task-runner subagent runs with isolation:'worktree', its child
# worktree would otherwise be judged by the parent project's boundary —
# too permissive (subagent could write to any sibling of the child dir).
# git rev-parse --show-toplevel self-adapts: parent worktree returns the
# parent, child worktree returns the child. Falls back to CLAUDE_PROJECT_DIR
# when the CWD is not inside a git repo.
ALLOWED_DIR=$(norm_path "$(git rev-parse --show-toplevel 2>/dev/null || echo "${CLAUDE_PROJECT_DIR:-$(pwd)}")")

# Resolve to a normalized absolute path (relative → against the worktree).
FILE_PATH=$(norm_path "$FILE_PATH" "$ALLOWED_DIR")

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

# Own-session scratchpad — writable by every role (outside the repo, private
# to this session, cleaned up by Claude Code). Checked after the deny list and
# before the clarifier / worktree rules, which only govern repo paths.
if is_own_scratchpad "$FILE_PATH"; then
  exit 0
fi

# Clarifier role — only tracker temp files are writable
if [ "${ZPIT_AGENT_TYPE:-}" = "clarifier" ]; then
  BASENAME=$(basename "$FILE_PATH")
  case "$BASENAME" in
    tmp_*.md|tmp_*.txt) : ;;
    *)
      echo "BLOCKED: Clarifier role can only Write to its session scratchpad or tmp_*.{md,txt} tracker temp files. '$FILE_PATH' is not allowed. If this change is real, scope it into the Issue SCOPE section and let the Coding Agent execute it." >&2
      exit 2
      ;;
  esac
fi

# Whitelist — must be inside allowed directory
if [[ "$FILE_PATH" != "${ALLOWED_DIR}"/* ]]; then
  echo "BLOCKED: Path '$FILE_PATH' is outside the working directory '${ALLOWED_DIR}'. Agents can only modify files in their own worktree (or their session scratchpad)." >&2
  exit 2
fi

exit 0
