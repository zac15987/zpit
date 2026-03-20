#!/usr/bin/env bash
set -euo pipefail

# setup-hooks.sh — Deploy hooks, docs, and settings.json to a project's .claude/ directory
# Usage: ./scripts/setup-hooks.sh <project-path> [hook_mode]
# hook_mode: strict (default) | standard | relaxed

PROJECT_DIR="${1:?Usage: setup-hooks.sh <project-path> [hook_mode]}"
HOOK_MODE="${2:-strict}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
HOOKS_SRC="$REPO_ROOT/hooks"
DOCS_SRC="$REPO_ROOT/docs"

CLAUDE_DIR="$PROJECT_DIR/.claude"
HOOKS_DST="$CLAUDE_DIR/hooks"
DOCS_DST="$CLAUDE_DIR/docs"

echo "Setting up .claude/ for: $PROJECT_DIR"
echo "Hook mode: $HOOK_MODE"

# Copy hook scripts
mkdir -p "$HOOKS_DST"
cp "$HOOKS_SRC/path-guard.sh" "$HOOKS_DST/"
cp "$HOOKS_SRC/bash-firewall.sh" "$HOOKS_DST/"
cp "$HOOKS_SRC/git-guard.sh" "$HOOKS_DST/"
chmod +x "$HOOKS_DST"/*.sh
echo "Hooks copied to $HOOKS_DST"

# Copy shared docs (code quality baseline for agents)
mkdir -p "$DOCS_DST"
cp "$DOCS_SRC/code-construction-principles.md" "$DOCS_DST/"
echo "Docs copied to $DOCS_DST"

# Generate settings.json based on hook_mode
generate_settings() {
    local mode="$1"
    local file="$2"

    case "$mode" in
        strict)
            cat > "$file" << 'SETTINGS'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/path-guard.sh"
          }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/bash-firewall.sh"
          },
          {
            "type": "command",
            "command": ".claude/hooks/git-guard.sh"
          }
        ]
      }
    ]
  }
}
SETTINGS
            ;;
        standard)
            cat > "$file" << 'SETTINGS'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/path-guard.sh"
          }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/git-guard.sh"
          }
        ]
      }
    ]
  }
}
SETTINGS
            ;;
        relaxed)
            cat > "$file" << 'SETTINGS'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/git-guard.sh"
          }
        ]
      }
    ]
  }
}
SETTINGS
            ;;
        *)
            echo "Unknown hook_mode: $mode (expected strict|standard|relaxed)" >&2
            exit 1
            ;;
    esac
}

# Always generate the base settings.json as strict
generate_settings "strict" "$CLAUDE_DIR/settings.json"
echo "Generated $CLAUDE_DIR/settings.json (strict)"

# If mode is not strict, generate settings.local.json as overlay
if [ "$HOOK_MODE" != "strict" ]; then
    generate_settings "$HOOK_MODE" "$CLAUDE_DIR/settings.local.json"
    echo "Generated $CLAUDE_DIR/settings.local.json ($HOOK_MODE)"
fi

echo "Done! Hook setup complete for $PROJECT_DIR"
