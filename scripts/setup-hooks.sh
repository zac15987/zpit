#!/usr/bin/env bash
set -euo pipefail

# setup-hooks.sh — Manual fallback for deploying zpit hooks, docs, and settings
# to a project's .claude/ directory. The normal path is `zpit` itself, which
# embeds these files via go:embed and deploys them on every agent launch.
# Use this script only when the binary isn't available (e.g. CI smoke tests,
# manual repro of Issue #39 worktree settings).
#
# Usage: ./scripts/setup-hooks.sh <project-path>

PROJECT_DIR="${1:?Usage: setup-hooks.sh <project-path>}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
HOOKS_SRC="$REPO_ROOT/hooks"
DOCS_SRC="$REPO_ROOT/docs"

CLAUDE_DIR="$PROJECT_DIR/.claude"
HOOKS_DST="$CLAUDE_DIR/hooks"
DOCS_DST="$CLAUDE_DIR/docs"

echo "Setting up .claude/ for: $PROJECT_DIR"

# Copy hook scripts (the full set; per-project hook_mode is no longer supported)
mkdir -p "$HOOKS_DST"
cp "$HOOKS_SRC/path-guard.sh"        "$HOOKS_DST/"
cp "$HOOKS_SRC/bash-firewall.sh"     "$HOOKS_DST/"
cp "$HOOKS_SRC/pwsh-firewall.sh"     "$HOOKS_DST/"
cp "$HOOKS_SRC/git-guard.sh"         "$HOOKS_DST/"
cp "$HOOKS_SRC/notify-permission.sh" "$HOOKS_DST/"
cp "$HOOKS_SRC/worktree-create.sh"   "$HOOKS_DST/"
chmod +x "$HOOKS_DST"/*.sh
echo "Hooks copied to $HOOKS_DST"

# Copy shared docs (code quality baseline for agents)
mkdir -p "$DOCS_DST"
cp "$DOCS_SRC/code-construction-principles.md" "$DOCS_DST/"
cp "$DOCS_SRC/agent-guidelines.md" "$DOCS_DST/"
echo "Docs copied to $DOCS_DST"

# Copy agent definitions
AGENTS_SRC="$REPO_ROOT/agents"
AGENTS_DST="$CLAUDE_DIR/agents"
mkdir -p "$AGENTS_DST"
if [ -d "$AGENTS_SRC" ]; then
    cp "$AGENTS_SRC"/*.md "$AGENTS_DST/"
    echo "Agents copied to $AGENTS_DST"
fi

# Generate the canonical settings template (matches internal/worktree/hooks.go
# settingsTemplate). The same content is written to both settings.json and
# settings.local.json — see hooks.go DeployHooksToWorktree for the rationale.
SETTINGS_CONTENT=$(cat << 'SETTINGS'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/path-guard.sh"
          }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/bash-firewall.sh"
          },
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/git-guard.sh"
          }
        ]
      },
      {
        "matcher": "PowerShell",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/pwsh-firewall.sh"
          }
        ]
      }
    ],
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/notify-permission.sh"
          }
        ]
      }
    ],
    "WorktreeCreate": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/worktree-create.sh",
            "timeout": 30
          }
        ]
      }
    ]
  }
}
SETTINGS
)

printf '%s\n' "$SETTINGS_CONTENT" > "$CLAUDE_DIR/settings.json"
echo "Generated $CLAUDE_DIR/settings.json"
printf '%s\n' "$SETTINGS_CONTENT" > "$CLAUDE_DIR/settings.local.json"
echo "Generated $CLAUDE_DIR/settings.local.json"

echo "Done! Hook setup complete for $PROJECT_DIR"
