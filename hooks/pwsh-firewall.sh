#!/usr/bin/env bash
set -euo pipefail

# PowerShell Firewall — PreToolUse hook for the PowerShell tool.
# Mirrors bash-firewall.sh but matches PowerShell cmdlets and aliases.
# Exit 0 = allow, Exit 2 = block.

COMMAND=$(cat | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

# Skip enforcement for non-agent sessions (plain Claude Code)
[ -z "${ZPIT_AGENT:-}" ] && exit 0

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
if [ "${ZPIT_AGENT_TYPE:-}" = "clarifier" ]; then
  # Allowlist 1: Remove-Item (or aliases rm/ri/del/erase) targeting tmp_*.{md,txt}.
  # Strict shape: single argument, optional .\ or ./ prefix, no -Recurse/-Force.
  if echo "$COMMAND" | grep -qiE '^[[:space:]]*(Remove-Item|rm|ri|del|erase)[[:space:]]+(\.[\\/])?tmp_[A-Za-z0-9_-]+\.(md|txt)[[:space:]]*$'; then
    exit 0
  fi

  # Allowlist 2: Set-Content / Add-Content / Out-File / Clear-Content targeting tmp_*.{md,txt}.
  # Matches typical PS write idioms: `Set-Content tmp_foo.md -Value ...`,
  # `... | Out-File tmp_foo.md`, etc. The target file name appears immediately
  # after the cmdlet (positional -Path) or after -Path/-LiteralPath.
  if echo "$COMMAND" | grep -qiE '(Set-Content|Add-Content|Out-File|Clear-Content)\s+(-(Path|LiteralPath)\s+)?(\.[\\/])?tmp_[A-Za-z0-9_-]+\.(md|txt)([[:space:]]|$)'; then
    exit 0
  fi

  # Allowlist 3: `> tmp_*.{md,txt}` or `>> tmp_*.{md,txt}` redirection.
  # Same shape as bash-firewall's redirect carve-out; PS uses identical syntax.
  if echo "$COMMAND" | grep -qE '>+[[:space:]]*(\.[\\/])?tmp_[A-Za-z0-9_-]+\.(md|txt)([[:space:]]|$)'; then
    REDIRECT_OK=1
  else
    REDIRECT_OK=0
  fi

  # PS write cmdlets and their aliases — block at command start or after a separator.
  # `rm` here is PS's alias for Remove-Item (the strict allowlist above already
  # cleared the tmp_*.{md,txt} use case).
  CLARIFIER_BLOCKED_PS=(
    '(^|[;&|[:space:]])Remove-Item([[:space:]]|$)'
    '(^|[;&|[:space:]])(rm|ri|del|erase|rd|rmdir)([[:space:]]|$)'
    '(^|[;&|[:space:]])Move-Item([[:space:]]|$)'
    '(^|[;&|[:space:]])(mv|mi|move)([[:space:]]|$)'
    '(^|[;&|[:space:]])Copy-Item([[:space:]]|$)'
    '(^|[;&|[:space:]])(cp|cpi|copy)([[:space:]]|$)'
    '(^|[;&|[:space:]])New-Item([[:space:]]|$)'
    '(^|[;&|[:space:]])(mkdir|md)([[:space:]]|$)'
    '(^|[;&|[:space:]])Set-Content([[:space:]]|$)'
    '(^|[;&|[:space:]])Add-Content([[:space:]]|$)'
    '(^|[;&|[:space:]])Out-File([[:space:]]|$)'
    '(^|[;&|[:space:]])Clear-Content([[:space:]]|$)'
  )
  for pattern in "${CLARIFIER_BLOCKED_PS[@]}"; do
    if echo "$COMMAND" | grep -qiE "$pattern"; then
      echo "BLOCKED: Clarifier cannot execute '$COMMAND'. Only read-only PowerShell commands and tracker CLI (gh / forgejo) are allowed. File changes must go through Issue SCOPE + Coding Agent." >&2
      exit 2
    fi
  done

  # Redirect to source-code file extensions — block unless target is tmp_*.{md,txt}.
  if [ "$REDIRECT_OK" -ne 1 ] && echo "$COMMAND" | grep -qE '>+[[:space:]]*[^[:space:]|&;]+\.(go|ts|tsx|js|jsx|astro|md|json|toml|yaml|yml|css|scss|sh|ps1|py|java|cs|cpp|c|h)([[:space:]]|$)'; then
    REDIR_TGT=$(echo "$COMMAND" | grep -oE '>+[[:space:]]*[^[:space:]|&;]+' | sed -E 's/^>+[[:space:]]*//' | tail -1)
    echo "BLOCKED: Clarifier cannot redirect output to '$REDIR_TGT'. Only tmp_*.{md,txt} tracker temp files are allowed. File changes must go through Issue SCOPE + Coding Agent." >&2
    exit 2
  fi
fi

# Redirect escape detection — block writes outside the working directory.
# PS shares `>` redirect syntax with bash, so reuse the same logic.
ALLOWED_DIR="${CLAUDE_PROJECT_DIR:-$(pwd)}"
if echo "$COMMAND" | grep -qP '>+\s*/(?!tmp)' 2>/dev/null || echo "$COMMAND" | grep -qE '>+\s*/[^t]' 2>/dev/null; then
  REDIRECT_TARGET=$(echo "$COMMAND" | grep -oP '>+\s*\K/[^\s;|&]+' 2>/dev/null | head -1 || echo "$COMMAND" | grep -oE '>\s*/[^ ;|&]+' | sed 's/>\s*//' | head -1)
  if [ -n "$REDIRECT_TARGET" ] && [[ "$REDIRECT_TARGET" != "${ALLOWED_DIR}"/* ]]; then
    echo "BLOCKED: Redirect target '$REDIRECT_TARGET' is outside the working directory." >&2
    exit 2
  fi
fi

exit 0
