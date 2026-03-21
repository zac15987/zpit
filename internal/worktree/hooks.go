package worktree

import (
	"fmt"
	"os"
	"path/filepath"
)

// Hook configuration JSON for each mode.
// The base settings.json (strict) lives in the main repo and is inherited by worktrees.
// settings.local.json overlays a lower security mode when needed.

const settingsStandard = `{
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
}`

const settingsRelaxed = `{
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
}`

// SetupHookMode configures .claude/settings.local.json in the worktree
// based on the project's hook_mode.
// For "strict", no overlay is written (the base settings.json is inherited).
// For "standard" or "relaxed", a settings.local.json overlay is written.
func SetupHookMode(worktreePath, hookMode string) error {
	switch hookMode {
	case "strict":
		// No overlay needed — worktree inherits the strict settings.json from main repo.
		return nil
	case "standard":
		return writeSettingsLocal(worktreePath, settingsStandard)
	case "relaxed":
		return writeSettingsLocal(worktreePath, settingsRelaxed)
	default:
		return fmt.Errorf("unknown hook_mode: %s (expected strict|standard|relaxed)", hookMode)
	}
}

func writeSettingsLocal(worktreePath, content string) error {
	claudeDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude dir: %w", err)
	}
	path := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing settings.local.json: %w", err)
	}
	return nil
}
