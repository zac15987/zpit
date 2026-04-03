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
	PathGuard:        []byte("#!/bin/bash\n# path-guard"),
	BashFirewall:     []byte("#!/bin/bash\n# bash-firewall"),
	GitGuard:         []byte("#!/bin/bash\n# git-guard"),
	NotifyPermission: []byte("#!/bin/bash\n# notify-permission"),
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

func TestDeployHooksToProject_NotifyPermissionScriptDeployed(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, "strict", testScripts); err != nil {
		t.Fatalf("DeployHooksToProject: %v", err)
	}
	p := filepath.Join(dir, ".claude", "hooks", "notify-permission.sh")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("notify-permission.sh not found: %v", err)
	}
	if string(data) != string(testScripts.NotifyPermission) {
		t.Error("notify-permission.sh content mismatch")
	}
}

func TestDeployHooksToProject_NotificationHookInSettings(t *testing.T) {
	for _, mode := range []string{"strict", "standard", "relaxed"} {
		dir := t.TempDir()
		if err := DeployHooksToProject(dir, mode, testScripts); err != nil {
			t.Fatalf("DeployHooksToProject(%s): %v", mode, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
		if err != nil {
			t.Fatalf("read settings.json: %v", err)
		}
		if !containsHook(data, "notify-permission.sh") {
			t.Errorf("%s mode: settings.json should include notify-permission.sh in Notification hook", mode)
		}
		if !strings.Contains(string(data), "Notification") {
			t.Errorf("%s mode: settings.json should include Notification hook section", mode)
		}
	}
}

func TestEnsureGitignore_NewFile(t *testing.T) {
	dir := t.TempDir()
	EnsureGitignore(dir)

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, rule := range zpitIgnoreRules {
		if !strings.Contains(string(data), rule) {
			t.Errorf("missing rule: %s", rule)
		}
	}
	if !strings.Contains(string(data), "# Zpit auto-deploy") {
		t.Error("missing header comment")
	}
}

func TestEnsureGitignore_PartialExists(t *testing.T) {
	dir := t.TempDir()
	// Pre-populate with one rule already present.
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".mcp.json\n"), 0o644)

	EnsureGitignore(dir)

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	// .mcp.json should appear exactly once (the original).
	if strings.Count(content, ".mcp.json") != 1 {
		t.Errorf(".mcp.json duplicated:\n%s", content)
	}
	// Other rules should be present.
	for _, rule := range zpitIgnoreRules {
		if !strings.Contains(content, rule) {
			t.Errorf("missing rule: %s", rule)
		}
	}
}

func TestEnsureGitignore_AllExist(t *testing.T) {
	dir := t.TempDir()
	// Write all rules already.
	var buf strings.Builder
	buf.WriteString("# Zpit auto-deploy\n")
	for _, rule := range zpitIgnoreRules {
		buf.WriteString(rule + "\n")
	}
	initial := buf.String()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(initial), 0o644)

	EnsureGitignore(dir)

	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(data) != initial {
		t.Errorf("file was modified when all rules already existed:\n%s", string(data))
	}
}

func TestEnsureGitignore_NoDuplicateHeader(t *testing.T) {
	dir := t.TempDir()
	// First deploy: creates header + all rules.
	EnsureGitignore(dir)

	// Simulate a future zpitIgnoreRules addition by removing one rule from .gitignore.
	path := filepath.Join(dir, ".gitignore")
	data, _ := os.ReadFile(path)
	trimmed := strings.Replace(string(data), ".mcp.json\n", "", 1)
	os.WriteFile(path, []byte(trimmed), 0o644)

	// Second deploy: should add the missing rule without a second header.
	EnsureGitignore(dir)

	data, _ = os.ReadFile(path)
	if strings.Count(string(data), "# Zpit auto-deploy") != 1 {
		t.Errorf("duplicate header:\n%s", string(data))
	}
	if !strings.Contains(string(data), ".mcp.json") {
		t.Error("missing rule was not re-added")
	}
}

func TestEnsureGitattributes_NewFile(t *testing.T) {
	dir := t.TempDir()
	EnsureGitattributes(dir)

	data, err := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	for _, rule := range zpitGitattributesRules {
		if !strings.Contains(string(data), rule) {
			t.Errorf("missing rule: %s", rule)
		}
	}
	if !strings.Contains(string(data), "# Zpit auto-deploy") {
		t.Error("missing header comment")
	}
}

func TestEnsureGitattributes_PartialExists(t *testing.T) {
	dir := t.TempDir()
	// Pre-populate with an existing rule.
	os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.go text eol=lf\n"), 0o644)

	EnsureGitattributes(dir)

	data, err := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	content := string(data)
	// Original rule should remain.
	if !strings.Contains(content, "*.go text eol=lf") {
		t.Error("original rule was lost")
	}
	// Zpit rules should be present.
	for _, rule := range zpitGitattributesRules {
		if !strings.Contains(content, rule) {
			t.Errorf("missing rule: %s", rule)
		}
	}
}

func TestEnsureGitattributes_AllExist(t *testing.T) {
	dir := t.TempDir()
	var buf strings.Builder
	buf.WriteString("# Zpit auto-deploy\n")
	for _, rule := range zpitGitattributesRules {
		buf.WriteString(rule + "\n")
	}
	initial := buf.String()
	os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(initial), 0o644)

	EnsureGitattributes(dir)

	data, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if string(data) != initial {
		t.Errorf("file was modified when all rules already existed:\n%s", string(data))
	}
}

func TestEnsureGitattributes_NoDuplicateHeader(t *testing.T) {
	dir := t.TempDir()
	EnsureGitattributes(dir)

	// Simulate removing one rule.
	path := filepath.Join(dir, ".gitattributes")
	data, _ := os.ReadFile(path)
	trimmed := strings.Replace(string(data), zpitGitattributesRules[0]+"\n", "", 1)
	os.WriteFile(path, []byte(trimmed), 0o644)

	// Second deploy: should add missing rule without duplicating header.
	EnsureGitattributes(dir)

	data, _ = os.ReadFile(path)
	if strings.Count(string(data), "# Zpit auto-deploy") != 1 {
		t.Errorf("duplicate header:\n%s", string(data))
	}
	for _, rule := range zpitGitattributesRules {
		if !strings.Contains(string(data), rule) {
			t.Errorf("missing rule was not re-added: %s", rule)
		}
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
