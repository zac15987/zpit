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

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
SCRATCHPAD_DIR=$(echo "$INPUT" | jq -r '.scratchpad_dir // empty')
[ -z "$COMMAND" ] && exit 0

# ── Path helpers — duplicated in path-guard.sh / pwsh-firewall.sh (each
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

# Clarifier role — block mutation verbs and writes to source-code extensions.
#
# Per-segment evaluation: split COMMAND on shell separators (&&, ||, ;, |) and
# check each segment independently. The `rm` carve-out is semantic, not
# shape-based — any path form (bare, ./, absolute unix, D:/...) is accepted as
# long as the basename matches tmp_*.{md,txt}, the target count is one, and
# no -r/-R/--recursive flag is present. Optional -f is allowed (bash-idiomatic
# for missing/read-only files; harmless on a single fixed-prefix file).
#
# Why per-segment: the previous whole-command regex broke as soon as the
# agent appended a confirmation chain like `&& echo "removed"`, even though
# the rm itself was safe. Splitting first lets each segment be judged on its
# own merits — the rm segment passes the carve-out, the echo segment trips
# nothing.
#
# Coarse split: separators inside quoted strings will be wrongly split, but
# the failure mode is over-splitting → stricter checking, never under-splitting.
if [ "${ZPIT_AGENT_TYPE:-}" = "clarifier" ]; then
  CLARIFIER_BLOCKED=(
    '(^|[[:space:]])rm([[:space:]]|$)'
    '(^|[[:space:]])mv([[:space:]]|$)'
    '(^|[[:space:]])cp([[:space:]]|$)'
    '(^|[[:space:]])mkdir([[:space:]]|$)'
    '(^|[[:space:]])touch([[:space:]]|$)'
    '(^|[[:space:]])sed[[:space:]]+-i'
  )

  # awk gsub is portable across BSD / GNU / git-bash awk; \n in replacement works.
  NORMALIZED=$(printf '%s' "$COMMAND" | awk '{ gsub(/&&|\|\||;|\|/, "\n"); print }')

  while IFS= read -r seg; do
    # Trim leading/trailing whitespace
    seg="${seg#"${seg%%[![:space:]]*}"}"
    seg="${seg%"${seg##*[![:space:]]}"}"
    [ -z "$seg" ] && continue

    # rm-tmp carve-out: single target whose basename is tmp_*.{md,txt},
    # optional -f, no -r/-R/--recursive. Path shape is unrestricted.
    if [[ "$seg" =~ ^rm([[:space:]]+(-f|--force))?[[:space:]]+([^[:space:]]+)[[:space:]]*$ ]]; then
      RM_TARGET="${BASH_REMATCH[3]}"
      RM_BASE="${RM_TARGET##*/}"
      case "$RM_BASE" in
        tmp_*.md|tmp_*.txt) continue ;;
      esac
      # …or any single file inside the own-session scratchpad.
      is_scratch_target "$RM_TARGET" && continue
    fi

    # sed -i carve-out: in-place edits of the clarifier's own temp files
    # (e.g. fixing a typo in a draft issue body). The segment is tokenized
    # with xargs (honours '…' / "…" quoting, executes nothing but printf);
    # backslashes are mapped to / first so Windows paths survive xargs'
    # escape handling. Then sed's own grammar: -e/-f/--expression/--file
    # consume the next token, other -flags are skipped, and the first bare
    # token is the script unless -e/-f supplied one. Every remaining token is
    # a file target and must be tmp_*.{md,txt} or inside the own-session
    # scratchpad; zero targets or unparsable quoting → no carve-out (falls
    # through to the block below). A `>` anywhere disqualifies the segment so
    # a redirect can't ride along past the source-extension redirect check.
    if [[ "$seg" =~ ^sed[[:space:]]+-i && "$seg" != *">"* ]]; then
      SED_ARGS=()
      if SED_LIST=$(printf '%s' "${seg#sed}" | tr '\\' '/' | xargs printf '%s\n' 2>/dev/null); then
        mapfile -t SED_ARGS <<< "$SED_LIST"
      fi
      SED_OK=0 SED_SCRIPT=0 SED_SKIP=0
      for tok in "${SED_ARGS[@]}"; do
        [ -z "$tok" ] && continue
        if [ "$SED_SKIP" -eq 1 ]; then SED_SKIP=0; continue; fi
        case "$tok" in
          -e|-f|--expression|--file) SED_SCRIPT=1; SED_SKIP=1; continue ;;
          -e*|-f*|--expression=*|--file=*) SED_SCRIPT=1; continue ;;
          -*) continue ;;
        esac
        if [ "$SED_SCRIPT" -eq 0 ]; then SED_SCRIPT=1; continue; fi
        case "${tok##*/}" in
          tmp_*.md|tmp_*.txt) SED_OK=1 ;;
          *) if is_scratch_target "$tok"; then SED_OK=1; else SED_OK=0; break; fi ;;
        esac
      done
      [ "$SED_OK" -eq 1 ] && continue
    fi

    # Mutation verbs in this segment
    for pattern in "${CLARIFIER_BLOCKED[@]}"; do
      if echo "$seg" | grep -qE "$pattern"; then
        echo "BLOCKED: Clarifier cannot execute '$COMMAND'. Only read-only commands and tracker CLI (gh / forgejo) are allowed. File changes must go through Issue SCOPE + Coding Agent." >&2
        exit 2
      fi
    done

    # Redirect to source-code file extensions — block unless target is tmp_*.{md,txt}
    if echo "$seg" | grep -qE '>[[:space:]]*[^[:space:]|&;]+\.(go|ts|tsx|js|jsx|astro|md|json|toml|yaml|yml|css|scss|sh|py|java|cs|cpp|c|h)([[:space:]]|$)'; then
      CLARIFIER_TGT=$(echo "$seg" | grep -oE '>[[:space:]]*[^[:space:]|&;]+' | sed -E 's/^>[[:space:]]*//' | tail -1)
      CLARIFIER_TGT_BASE="${CLARIFIER_TGT##*/}"
      case "$CLARIFIER_TGT_BASE" in
        tmp_*.md|tmp_*.txt) : ;;
        *)
          is_scratch_target "$CLARIFIER_TGT" && continue
          echo "BLOCKED: Clarifier cannot redirect output to '$CLARIFIER_TGT'. Only the session scratchpad or tmp_*.{md,txt} tracker temp files are allowed. File changes must go through Issue SCOPE + Coding Agent." >&2
          exit 2
          ;;
      esac
    fi
  done <<< "$NORMALIZED"
fi

# Redirect escape detection — confine `>`/`>>` writes to the worktree, while
# (a) allowing discard devices (/dev/null &c.) and OS temp scratch, and
# (b) blocking Windows reserved-name targets (nul/NUL): git-bash does NOT
# treat `nul` as the null device, so `2>nul` creates a real, reserved-name
# file in the repo that pollutes `git status` and resists deletion. The old
# `(?!tmp)` / `[^t]` heuristic both over-blocked /dev/null and under-blocked
# `nul`; this per-target classifier replaces it.
#
# Note: only `>`/`>>` shell redirects are inspected. Non-redirect writes
# (curl -o, cp, tar -C ...) are out of scope here — path-guard governs the
# Write/Edit tools.
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

# Extract every redirect target (the token after > >> 2> 2>> &> ...).
# fd-dup forms like `2>&1` yield no target (the & is excluded) and are skipped.
if [ "$GREP_FLAG" = "-P" ]; then
  REDIRECT_TARGETS=$(echo "$COMMAND" | grep -oP '(?:[0-9]*|&)>>?\s*\K[^\s;|&<>]+' 2>/dev/null || true)
else
  REDIRECT_TARGETS=$(echo "$COMMAND" | grep -oE '([0-9]*|&)>>?[[:space:]]*[^[:space:];|&<>]+' 2>/dev/null | sed -E 's/^([0-9]*|&)>>?[[:space:]]*//' || true)
fi

while IFS= read -r tgt; do
  [ -z "$tgt" ] && continue
  tgt=$(unquote "$tgt")

  # basename after stripping both / and \ path separators
  base="${tgt##*/}"
  base="${base##*\\}"
  base_lc=$(printf '%s' "$base" | tr '[:upper:]' '[:lower:]')

  # 1. Windows reserved name → block, steering the agent to /dev/null.
  if [ "$base_lc" = "nul" ]; then
    echo "BLOCKED: Redirect target '$tgt' resolves to the Windows reserved name 'nul'. In git-bash this creates a real, hard-to-delete file in the repo instead of discarding output. Use '/dev/null' instead (e.g. '2>/dev/null')." >&2
    exit 2
  fi

  # 2. Discard devices → allow.
  case "$tgt" in
    /dev/null|/dev/stdout|/dev/stderr|/dev/fd/*) continue ;;
  esac

  # 3. OS temp scratch → allow. The hook sees the unexpanded command string,
  #    so match literal env-var forms as well as the well-known temp roots.
  case "$tgt" in
    /tmp|/tmp/*|/var/tmp|/var/tmp/*|/var/folders/*) continue ;;
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
