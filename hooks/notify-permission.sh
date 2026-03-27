#!/usr/bin/env bash
set -euo pipefail

# Notification hook — writes permission signal for Zpit TUI.
# Fires when Claude Code needs permission approval.
# No ZPIT_AGENT check: permission notifications apply to all sessions.

INPUT=$(cat)

# Check notification_type (may be missing per known bug anthropics/claude-code#11964).
NTYPE=$(echo "$INPUT" | jq -r '.notification_type // ""')
MSG=$(echo "$INPUT" | jq -r '.message // ""')

# If notification_type is present and NOT permission_prompt, skip.
if [ -n "$NTYPE" ] && [ "$NTYPE" != "permission_prompt" ]; then
    exit 0
fi

# If notification_type is missing, fall back to message content check.
if [ -z "$NTYPE" ]; then
    case "$MSG" in
        *[Pp]ermission*) ;;
        *) exit 0 ;;
    esac
fi

SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // ""')
[ -z "$SESSION_ID" ] && exit 0

SIGNAL_DIR="${HOME}/.zpit/signals"
mkdir -p "$SIGNAL_DIR"
echo "$INPUT" > "$SIGNAL_DIR/permission-${SESSION_ID}.json"
exit 0
