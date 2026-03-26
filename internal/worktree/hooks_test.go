package worktree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupHookMode_Strict(t *testing.T) {
	dir := t.TempDir()
	if err := setupHookMode(dir, "strict"); err != nil {
		t.Fatalf("setupHookMode(strict): %v", err)
	}
	// No file should be written.
	path := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(path); err == nil {
		t.Error("settings.local.json should not exist for strict mode")
	}
}

func TestSetupHookMode_Standard(t *testing.T) {
	dir := t.TempDir()
	if err := setupHookMode(dir, "standard"); err != nil {
		t.Fatalf("setupHookMode(standard): %v", err)
	}
	content := readSettingsLocal(t, dir)
	// Should have path-guard and git-guard, but NOT bash-firewall.
	if !containsHook(content, "path-guard.sh") {
		t.Error("standard should include path-guard")
	}
	if !containsHook(content, "git-guard.sh") {
		t.Error("standard should include git-guard")
	}
	if containsHook(content, "bash-firewall.sh") {
		t.Error("standard should NOT include bash-firewall")
	}
}

func TestSetupHookMode_Relaxed(t *testing.T) {
	dir := t.TempDir()
	if err := setupHookMode(dir, "relaxed"); err != nil {
		t.Fatalf("setupHookMode(relaxed): %v", err)
	}
	content := readSettingsLocal(t, dir)
	// Should only have git-guard.
	if !containsHook(content, "git-guard.sh") {
		t.Error("relaxed should include git-guard")
	}
	if containsHook(content, "path-guard.sh") {
		t.Error("relaxed should NOT include path-guard")
	}
	if containsHook(content, "bash-firewall.sh") {
		t.Error("relaxed should NOT include bash-firewall")
	}
}

func TestSetupHookMode_ValidJSON(t *testing.T) {
	for _, mode := range []string{"standard", "relaxed"} {
		dir := t.TempDir()
		if err := setupHookMode(dir, mode); err != nil {
			t.Fatalf("setupHookMode(%s): %v", mode, err)
		}
		data := readSettingsLocal(t, dir)
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("%s: invalid JSON: %v", mode, err)
		}
	}
}

func TestValidateHookMode_Unknown(t *testing.T) {
	if err := validateHookMode("banana"); err == nil {
		t.Error("expected error for unknown hook_mode")
	}
}

func TestValidateHookMode_Valid(t *testing.T) {
	for _, mode := range []string{"strict", "standard", "relaxed"} {
		if err := validateHookMode(mode); err != nil {
			t.Errorf("unexpected error for %q: %v", mode, err)
		}
	}
}

// --- DeployHooksToProject / DeployHooksToWorktree tests ---

var testScripts = HookScripts{
	PathGuard:    []byte("#!/bin/bash\n# path-guard"),
	BashFirewall: []byte("#!/bin/bash\n# bash-firewall"),
	GitGuard:     []byte("#!/bin/bash\n# git-guard"),
}

func TestDeployHooksToProject_ScriptsWritten(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, "strict", testScripts); err != nil {
		t.Fatalf("DeployHooksToProject: %v", err)
	}
	for _, name := range []string{"path-guard.sh", "bash-firewall.sh", "git-guard.sh"} {
		p := filepath.Join(dir, ".claude", "hooks", name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("hook %s not found: %v", name, err)
		}
	}
	// Verify content
	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "hooks", "path-guard.sh"))
	if string(data) != string(testScripts.PathGuard) {
		t.Error("path-guard.sh content mismatch")
	}
}

func TestDeployHooksToProject_MergeSettings_NewFile(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, "strict", testScripts); err != nil {
		t.Fatalf("DeployHooksToProject: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	if !containsHook(data, "path-guard.sh") {
		t.Error("settings.json should include path-guard for strict mode")
	}
	if !containsHook(data, "bash-firewall.sh") {
		t.Error("settings.json should include bash-firewall for strict mode")
	}
}

func TestDeployHooksToProject_MergeSettings_PreservesExistingKeys(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	_ = os.MkdirAll(claudeDir, 0o755)
	existing := `{"enabledPlugins":{"csharp-lsp@claude-plugins-official":true}}`
	_ = os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(existing), 0o644)

	if err := DeployHooksToProject(dir, "strict", testScripts); err != nil {
		t.Fatalf("DeployHooksToProject: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := parsed["hooks"]; !ok {
		t.Error("hooks key missing after merge")
	}
	if _, ok := parsed["enabledPlugins"]; !ok {
		t.Error("enabledPlugins was lost during merge")
	}
}

func TestDeployHooksToProject_MergeSettings_ReplacesStaleHooks(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	_ = os.MkdirAll(claudeDir, 0o755)
	stale := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"old-hook.sh"}]}]}}`
	_ = os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(stale), 0o644)

	if err := DeployHooksToProject(dir, "strict", testScripts); err != nil {
		t.Fatalf("DeployHooksToProject: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if containsHook(data, "old-hook.sh") {
		t.Error("stale hook should have been replaced")
	}
	if !containsHook(data, "path-guard.sh") {
		t.Error("new hook should be present")
	}
}

func TestDeployHooksToWorktree_UsesSettingsLocal(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToWorktree(dir, "standard", testScripts); err != nil {
		t.Fatalf("DeployHooksToWorktree: %v", err)
	}
	// settings.local.json should exist (standard mode writes overlay)
	localPath := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(localPath); err != nil {
		t.Errorf("settings.local.json should exist for worktree standard mode: %v", err)
	}
	// settings.json should NOT be created by worktree deploy
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		t.Error("settings.json should not be created for worktree deploy")
	}
	// Hook scripts should still exist
	if _, err := os.Stat(filepath.Join(dir, ".claude", "hooks", "path-guard.sh")); err != nil {
		t.Error("hook scripts should be deployed even for worktrees")
	}
}

func TestDeployHooksToProject_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, "strict", testScripts); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := DeployHooksToProject(dir, "strict", testScripts); err != nil {
		t.Fatalf("second call: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON after idempotent call: %v", err)
	}
}

func TestDeployHooksToProject_UnknownMode(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, "banana", testScripts); err == nil {
		t.Error("expected error for unknown hook_mode")
	}
}

func TestDeployHooksToWorktree_UnknownMode(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToWorktree(dir, "banana", testScripts); err == nil {
		t.Error("expected error for unknown hook_mode")
	}
}

func readSettingsLocal(t *testing.T, dir string) []byte {
	t.Helper()
	path := filepath.Join(dir, ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}
	return data
}

func containsHook(data []byte, hookName string) bool {
	return strings.Contains(string(data), hookName)
}
