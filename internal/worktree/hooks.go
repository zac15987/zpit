package worktree

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// HookScripts holds embedded hook script content for deployment.
type HookScripts struct {
	PathGuard        []byte
	BashFirewall     []byte
	GitGuard         []byte
	EnvWrapper       []byte // zpit-env.cmd — sets ZPIT_AGENT=1 for Windows agent launches
	NotifyPermission []byte // Notification hook — writes permission signal for Zpit TUI
}

// Hook configuration JSON for each mode.

const settingsStrict = `{
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
    ],
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/notify-permission.sh"
          }
        ]
      }
    ]
  }
}`

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
    ],
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/notify-permission.sh"
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
    ],
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/notify-permission.sh"
          }
        ]
      }
    ]
  }
}`

// hookModeTemplates maps valid hook_mode values to their JSON template.
var hookModeTemplates = map[string]string{
	"strict":   settingsStrict,
	"standard": settingsStandard,
	"relaxed":  settingsRelaxed,
}

// validateHookMode returns an error if hookMode is not a recognized value.
func validateHookMode(hookMode string) error {
	if _, ok := hookModeTemplates[hookMode]; !ok {
		return fmt.Errorf("unknown hook_mode: %s (expected strict|standard|relaxed)", hookMode)
	}
	return nil
}

// DeployHooksToProject writes hook scripts to .claude/hooks/ and merges hook config
// into the existing .claude/settings.json (preserving other keys like enabledPlugins).
func DeployHooksToProject(targetPath, hookMode string, scripts HookScripts) error {
	if err := validateHookMode(hookMode); err != nil {
		return err
	}
	if err := deployHookScripts(targetPath, scripts); err != nil {
		return fmt.Errorf("deploying hook scripts: %w", err)
	}
	return mergeSettingsHooks(targetPath, hookMode)
}

// DeployHooksToWorktree writes hook scripts to .claude/hooks/ and configures
// .claude/settings.local.json overlay based on hookMode.
// For "strict", no overlay is written (the worktree inherits the base settings.json).
func DeployHooksToWorktree(targetPath, hookMode string, scripts HookScripts) error {
	if err := validateHookMode(hookMode); err != nil {
		return err
	}
	if err := deployHookScripts(targetPath, scripts); err != nil {
		return fmt.Errorf("deploying hook scripts: %w", err)
	}
	return setupHookMode(targetPath, hookMode)
}

// setupHookMode configures .claude/settings.local.json in the worktree.
// For "strict", no overlay is written. hookMode is assumed already validated.
func setupHookMode(worktreePath, hookMode string) error {
	if hookMode == "strict" {
		return nil
	}
	return writeSettingsLocal(worktreePath, hookModeTemplates[hookMode])
}

func writeSettingsLocal(worktreePath, content string) error {
	claudeDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude dir: %w", err)
	}
	path := filepath.Join(claudeDir, "settings.local.json")
	existing, _ := os.ReadFile(path)
	if bytes.Equal(existing, []byte(content)) {
		return nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing settings.local.json: %w", err)
	}
	return nil
}

// deployHookScripts writes hook scripts and env wrapper to .claude/hooks/.
func deployHookScripts(targetPath string, scripts HookScripts) error {
	hooksDir := filepath.Join(targetPath, ".claude", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("creating hooks dir: %w", err)
	}
	files := map[string][]byte{
		"path-guard.sh":         scripts.PathGuard,
		"bash-firewall.sh":      scripts.BashFirewall,
		"git-guard.sh":          scripts.GitGuard,
		"zpit-env.cmd":          scripts.EnvWrapper,
		"notify-permission.sh":  scripts.NotifyPermission,
	}
	for name, content := range files {
		p := filepath.Join(hooksDir, name)
		if err := os.WriteFile(p, content, 0o755); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return nil
}

// mergeSettingsHooks reads existing .claude/settings.json, sets the "hooks" key
// based on hookMode, and writes back. Preserves all other existing keys.
// hookMode is assumed already validated.
func mergeSettingsHooks(targetPath, hookMode string) error {
	claudeDir := filepath.Join(targetPath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude dir: %w", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Read existing settings or start fresh.
	var settings map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			settings = make(map[string]interface{})
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Extract "hooks" value from the mode template.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(hookModeTemplates[hookMode]), &parsed); err != nil {
		return fmt.Errorf("parsing hook template: %w", err)
	}
	settings["hooks"] = parsed["hooks"]

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	newData := append(out, '\n')
	if bytes.Equal(data, newData) {
		return nil
	}
	return os.WriteFile(settingsPath, newData, 0o644)
}
