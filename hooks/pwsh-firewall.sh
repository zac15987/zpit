#!/usr/bin/env bash
set -euo pipefail

# PowerShell Firewall — PreToolUse hook for the PowerShell tool.
# Mirrors bash-firewall.sh but matches PowerShell cmdlets and aliases.
# Exit 0 = allow, Exit 2 = block.

# Skip enforcement for non-agent sessions (plain Claude Code) — checked
# BEFORE the jq dependency check so non-zpit users aren't blocked when
# jq is absent.
[ -z "${ZPIT_AGENT:-}" ] && exit 0

# Require jq: hook parses stdin JSON via jq. Fail closed when missing —
# without jq the firewall cannot inspect commands, so destructive
# PowerShell calls would otherwise slip through as "non-blocking" hook errors.
if ! command -v jq >/dev/null 2>&1; then
  echo "BLOCKED: 'jq' is required for zpit safety hooks but is not installed." >&2
  echo "Install it:  winget install jqlang.jq  |  brew install jq  |  apt install jq" >&2
  exit 2
fi

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
SCRATCHPAD_DIR=$(echo "$INPUT" | jq -r '.scratchpad_dir // empty')
[ -z "$COMMAND" ] && exit 0

# ── Path helpers — duplicated in path-guard.sh / bash-firewall.sh (each
#    hook is deployed standalone, so there is no shared lib to source). ──

# is_abs_path <'/'-separated path> — POSIX absolute or Windows drive-letter.
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

# unquote <token> — strip one pair of surrounding ' or " from a command token.
unquote() {
  local t="$1"
  if [[ "$t" =~ ^\"(.*)\"$ || "$t" =~ ^\'(.*)\'$ ]]; then t="${BASH_REMATCH[1]}"; fi
  printf '%s' "$t"
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

# is_scratch_target <raw command token> — own-session scratchpad, any form
# (quoted, backslashes, MSYS). Relative tokens never qualify.
is_scratch_target() {
  local t
  t=$(unquote "$1")
  t="${t//\\//}"
  is_abs_path "$t" && is_own_scratchpad "$(norm_path "$t")"
}

# Check if grep supports -P (PCRE). Fall back to -E if not.
GREP_FLAG="-P"
echo "test" | grep -P "test" > /dev/null 2>&1 || GREP_FLAG="-E"

# Blocked command patterns (system-level, role-independent).
#
# PowerShell command boundary in this hook is approximated as:
#   start of string | semicolon | newline | pipeline operator | & call operator | logical chain
# Patterns below assume case-insensitive matching (grep -i).
BLOCKED_PATTERNS=(
  # Destructive Remove-Item targeting root / parent / home, with -Recurse or -Force
  'Remove-Item\s+.*-(Recurse|Force).*\s+(/|~|\.\.)'
  'Remove-Item\s+(/|~|\.\.)\s+.*-(Recurse|Force)'

  # System control
  'Stop-Computer'
  'Restart-Computer'
  '(^|[;&|[:space:]])shutdown(\.exe)?([[:space:]]|$)'
  '(^|[;&|[:space:]])reboot([[:space:]]|$)'

  # Process kill — Stop-Process on PID 1 or with -Force targeting Idle/System
  'Stop-Process\s+.*-Id\s+1([[:space:]]|$)'
  '(^|[;&|[:space:]])kill\s+-Force\s+1([[:space:]]|$)'

  # Network risk — download-and-execute via Invoke-Expression / iex
  '(Invoke-WebRequest|iwr|curl|wget)\s.*\|\s*(Invoke-Expression|iex)'
  '(Invoke-Expression|iex)\s+.*\(\s*(Invoke-WebRequest|iwr|curl|wget|New-Object\s+Net\.WebClient)'

  # Package publish / upload
  'npm\s+publish'
  'dotnet\s+nuget\s+push'
  'pip\s+.*upload'

  # Global package installs
  'npm\s+i(nstall)?\s+-g'
)

for pattern in "${BLOCKED_PATTERNS[@]}"; do
  if echo "$COMMAND" | grep -qi${GREP_FLAG:1} "$pattern"; then
    echo "BLOCKED: Dangerous PowerShell command detected — '$COMMAND'. If this is truly needed, ask the user to run it manually." >&2
    exit 2
  fi
done

# Clarifier role — block PS write/mutation cmdlets and aliases, with a
# tmp_*.{md,txt} carve-out so the clarifier can create + delete its own
# tracker temp file (clarifier.md workflow step 17b).
#
# Per-segment evaluation mirrors bash-firewall.sh: split on PS separators
# (&&, ||, ;, |) and judge each segment independently. The carve-outs are
# semantic, not shape-based — any path form (bare, .\, ./, absolute) is
# accepted as long as the basename matches tmp_*.{md,txt}. -Force is allowed
# on a single fixed-prefix target (cannot recurse, cannot cross dirs);
# -Recurse stays blocked.
#
# Why per-segment: the previous whole-command regex broke when the agent
# chained `Remove-Item tmp_x.md; Write-Host "done"` or used absolute paths.
# Splitting first lets each segment be judged on its own.
if [ "${ZPIT_AGENT_TYPE:-}" = "clarifier" ]; then
  # PS write cmdlets and their aliases (used after carve-out for unsafe segments).
  CLARIFIER_BLOCKED_PS=(
    '(^|[[:space:]])Remove-Item([[:space:]]|$)'
    '(^|[[:space:]])(rm|ri|del|erase|rd|rmdir)([[:space:]]|$)'
    '(^|[[:space:]])Move-Item([[:space:]]|$)'
    '(^|[[:space:]])(mv|mi|move)([[:space:]]|$)'
    '(^|[[:space:]])Copy-Item([[:space:]]|$)'
    '(^|[[:space:]])(cp|cpi|copy)([[:space:]]|$)'
    '(^|[[:space:]])New-Item([[:space:]]|$)'
    '(^|[[:space:]])(mkdir|md)([[:space:]]|$)'
    '(^|[[:space:]])Set-Content([[:space:]]|$)'
    '(^|[[:space:]])Add-Content([[:space:]]|$)'
    '(^|[[:space:]])Out-File([[:space:]]|$)'
    '(^|[[:space:]])Clear-Content([[:space:]]|$)'
  )

  NORMALIZED=$(printf '%s' "$COMMAND" | awk '{ gsub(/&&|\|\||;|\|/, "\n"); print }')

  while IFS= read -r seg; do
    seg="${seg#"${seg%%[![:space:]]*}"}"
    seg="${seg%"${seg##*[![:space:]]}"}"
    [ -z "$seg" ] && continue

    # Carve-out 1: Remove-Item / aliases targeting a single tmp_*.{md,txt}.
    # Semantic check — path shape unrestricted, optional -Force, -Recurse blocked.
    if [[ "$seg" =~ ^(Remove-Item|rm|ri|del|erase)[[:space:]]+([^[:space:]]+)([[:space:]]+-Force)?[[:space:]]*$ ]]; then
      RM_TARGET="${BASH_REMATCH[2]}"
      RM_BASE_FS="${RM_TARGET##*/}"
      RM_BASE="${RM_BASE_FS##*\\}"
      case "$RM_BASE" in
        tmp_*.md|tmp_*.txt) continue ;;
      esac
      # …or any single file inside the own-session scratchpad.
      is_scratch_target "$RM_TARGET" && continue
    fi

    # Carve-out 2: Set-Content / Add-Content / Out-File / Clear-Content
    # targeting tmp_*.{md,txt}. Semantic basename check.
    if [[ "$seg" =~ (Set-Content|Add-Content|Out-File|Clear-Content)[[:space:]]+(-(Path|LiteralPath)[[:space:]]+)?([^[:space:]]+) ]]; then
      WR_TARGET="${BASH_REMATCH[4]}"
      WR_BASE_FS="${WR_TARGET##*/}"
      WR_BASE="${WR_BASE_FS##*\\}"
      case "$WR_BASE" in
        tmp_*.md|tmp_*.txt) continue ;;
      esac
      is_scratch_target "$WR_TARGET" && continue
    fi

    # Carve-out 3: `> tmp_*.{md,txt}` or `>> tmp_*.{md,txt}` redirection
    # (used to short-circuit the per-segment source-extension check below).
    SEGMENT_REDIRECT_OK=0
    if echo "$seg" | grep -qE '>+[[:space:]]*[^[:space:]|&;]+' ; then
      RED_TARGET=$(echo "$seg" | grep -oE '>+[[:space:]]*[^[:space:]|&;]+' | sed -E 's/^>+[[:space:]]*//' | tail -1)
      RED_BASE_FS="${RED_TARGET##*/}"
      RED_BASE="${RED_BASE_FS##*\\}"
      case "$RED_BASE" in
        tmp_*.md|tmp_*.txt) SEGMENT_REDIRECT_OK=1 ;;
      esac
      is_scratch_target "$RED_TARGET" && SEGMENT_REDIRECT_OK=1
    fi

    # Mutation cmdlets/aliases in this segment
    for pattern in "${CLARIFIER_BLOCKED_PS[@]}"; do
      if echo "$seg" | grep -qiE "$pattern"; then
        echo "BLOCKED: Clarifier cannot execute '$COMMAND'. Only read-only PowerShell commands and tracker CLI (gh / forgejo) are allowed. File changes must go through Issue SCOPE + Coding Agent." >&2
        exit 2
      fi
    done

    # Redirect to source-code file extensions — block unless target is tmp_*.{md,txt}.
    if [ "$SEGMENT_REDIRECT_OK" -ne 1 ] && echo "$seg" | grep -qE '>+[[:space:]]*[^[:space:]|&;]+\.(go|ts|tsx|js|jsx|astro|md|json|toml|yaml|yml|css|scss|sh|ps1|py|java|cs|cpp|c|h)([[:space:]]|$)'; then
      REDIR_TGT=$(echo "$seg" | grep -oE '>+[[:space:]]*[^[:space:]|&;]+' | sed -E 's/^>+[[:space:]]*//' | tail -1)
      echo "BLOCKED: Clarifier cannot redirect output to '$REDIR_TGT'. Only the session scratchpad or tmp_*.{md,txt} tracker temp files are allowed. File changes must go through Issue SCOPE + Coding Agent." >&2
      exit 2
    fi
  done <<< "$NORMALIZED"
fi

# Redirect escape detection — confine `>`/`>>` writes to the worktree, while
# (a) allowing discard targets (PowerShell `$null`, plus the /dev/null family
# in case agents mix idioms) and OS temp scratch ($env:TEMP &c.), and
# (b) blocking the Windows reserved name `nul`/`NUL`: PowerShell does NOT treat
# `nul` as the null device either, so `> nul` creates a real, reserved-name
# file that pollutes the repo and resists deletion. Mirrors bash-firewall.sh.
#
# Allowed roots: the active git worktree (a task-runner's child worktree lives
# under ~/.zpit/children, outside CLAUDE_PROJECT_DIR) plus CLAUDE_PROJECT_DIR.
ALLOWED_DIRS=()
GIT_TOP=$(git rev-parse --show-toplevel 2>/dev/null || true)
[ -n "$GIT_TOP" ] && ALLOWED_DIRS+=("$(norm_path "$GIT_TOP")")
ALLOWED_DIRS+=("$(norm_path "${CLAUDE_PROJECT_DIR:-$(pwd)}")")

# is_under_any <normalized path> <dir>... — path is strictly inside one of dirs.
is_under_any() {
  local p="$1" d; shift
  for d in "$@"; do [[ "$p" == "$d"/* ]] && return 0; done
  return 1
}

# Extract every redirect target (the token after > >> 2> 2>> *> ...).
if [ "$GREP_FLAG" = "-P" ]; then
  REDIRECT_TARGETS=$(echo "$COMMAND" | grep -oP '(?:[0-9*]*|&)>>?\s*\K[^\s;|&<>]+' 2>/dev/null || true)
else
  REDIRECT_TARGETS=$(echo "$COMMAND" | grep -oE '([0-9*]*|&)>>?[[:space:]]*[^[:space:];|&<>]+' 2>/dev/null | sed -E 's/^([0-9*]*|&)>>?[[:space:]]*//' || true)
fi

while IFS= read -r tgt; do
  [ -z "$tgt" ] && continue
  tgt=$(unquote "$tgt")

  # basename after stripping both / and \ path separators
  base="${tgt##*/}"
  base="${base##*\\}"
  base_lc=$(printf '%s' "$base" | tr '[:upper:]' '[:lower:]')

  # 1. Windows reserved name → block, steering the agent to $null / /dev/null.
  if [ "$base_lc" = "nul" ]; then
    echo "BLOCKED: Redirect target '$tgt' resolves to the Windows reserved name 'nul'. PowerShell creates a real, hard-to-delete file instead of discarding output. Use '\$null' instead (e.g. '2>\$null')." >&2
    exit 2
  fi

  # 2. Discard targets → allow.
  case "$tgt" in
    '$null'|'${null}'|/dev/null|/dev/stdout|/dev/stderr|/dev/fd/*) continue ;;
  esac

  # 3. OS temp scratch → allow. The hook sees the unexpanded command string,
  #    so match literal env-var forms as well as the well-known temp roots.
  case "$tgt" in
    /tmp|/tmp/*|/var/tmp|/var/tmp/*|/var/folders/*) continue ;;
    '$env:TEMP'*|'${env:TEMP}'*|'$env:TMP'*|'${env:TMP}'*|'$env:TMPDIR'*) continue ;;
    '$TMPDIR'*|'${TMPDIR}'*|'$TMP'*|'${TMP}'*|'$TEMP'*|'${TEMP}'*) continue ;;
  esac

  # 4. Absolute path (POSIX or drive-letter) → allow only inside the worktree
  #    or an OS temp root (which covers the session scratchpad); anything else
  #    is an escape. Relative targets resolve inside the worktree and are
  #    allowed (path-guard covers Write/Edit).
  tgt_fs="${tgt//\\//}"
  if is_abs_path "$tgt_fs"; then
    tgt_abs=$(norm_path "$tgt_fs")
    is_under_any "$tgt_abs" "${ALLOWED_DIRS[@]}" "${TEMP_ROOTS[@]}" && continue
    echo "BLOCKED: Redirect target '$tgt' is outside the working directory. Use a path inside the worktree or your session scratchpad." >&2
    exit 2
  fi
done <<< "$REDIRECT_TARGETS"

exit 0
