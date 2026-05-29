package worktree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testScripts = HookScripts{
	PathGuard:        []byte("#!/bin/bash\n# path-guard"),
	BashFirewall:     []byte("#!/bin/bash\n# bash-firewall"),
	PwshFirewall:     []byte("#!/bin/bash\n# pwsh-firewall"),
	GitGuard:         []byte("#!/bin/bash\n# git-guard"),
	NotifyPermission: []byte("#!/bin/bash\n# notify-permission"),
	WorktreeCreate:   []byte("#!/bin/bash\n# worktree-create"),
}

// --- DeployHooksToProject tests ---

func TestDeployHooksToProject_ScriptsWritten(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, testScripts); err != nil {
		t.Fatalf("DeployHooksToProject: %v", err)
	}
	for _, name := range []string{"path-guard.sh", "bash-firewall.sh", "pwsh-firewall.sh", "git-guard.sh", "notify-permission.sh", "worktree-create.sh"} {
		p := filepath.Join(dir, ".claude", "hooks", name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("hook %s not found: %v", name, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "hooks", "path-guard.sh"))
	if string(data) != string(testScripts.PathGuard) {
		t.Error("path-guard.sh content mismatch")
	}
}

func TestDeployHooksToProject_MergeSettings_NewFile(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, testScripts); err != nil {
		t.Fatalf("DeployHooksToProject: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	for _, hook := range []string{"path-guard.sh", "bash-firewall.sh", "pwsh-firewall.sh", "git-guard.sh", "notify-permission.sh", "worktree-create.sh"} {
		if !containsHook(data, hook) {
			t.Errorf("settings.json should include %s", hook)
		}
	}
}

func TestDeployHooksToProject_MergeSettings_PreservesExistingKeys(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	_ = os.MkdirAll(claudeDir, 0o755)
	existing := `{"enabledPlugins":{"csharp-lsp@claude-plugins-official":true}}`
	_ = os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(existing), 0o644)

	if err := DeployHooksToProject(dir, testScripts); err != nil {
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

	if err := DeployHooksToProject(dir, testScripts); err != nil {
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

func TestDeployHooksToProject_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, testScripts); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := DeployHooksToProject(dir, testScripts); err != nil {
		t.Fatalf("second call: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON after idempotent call: %v", err)
	}
}

// --- DeployHooksToWorktree tests ---

// TestDeployHooksToWorktree_WritesBothSettingsFiles is the regression test
// for the Issue #39 bug — strict mode used to skip settings entirely,
// disabling every hook inside the worktree because Claude Code resolves
// project settings against process CWD (linked worktree), not git toplevel.
func TestDeployHooksToWorktree_WritesBothSettingsFiles(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToWorktree(dir, testScripts); err != nil {
		t.Fatalf("DeployHooksToWorktree: %v", err)
	}
	for _, name := range []string{"settings.json", "settings.local.json"} {
		path := filepath.Join(dir, ".claude", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s should exist in worktree: %v", name, err)
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("%s: invalid JSON: %v", name, err)
		}
		if !containsHook(data, "worktree-create.sh") {
			t.Errorf("%s: WorktreeCreate hook missing — [P] parallel batches will fall back to Claude Code default origin/main fork", name)
		}
		if !containsHook(data, "path-guard.sh") {
			t.Errorf("%s: path-guard hook missing", name)
		}
		if !containsHook(data, "bash-firewall.sh") {
			t.Errorf("%s: bash-firewall hook missing", name)
		}
	}
}

func TestDeployHooksToWorktree_ScriptsDeployed(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToWorktree(dir, testScripts); err != nil {
		t.Fatalf("DeployHooksToWorktree: %v", err)
	}
	for _, name := range []string{"path-guard.sh", "bash-firewall.sh", "pwsh-firewall.sh", "git-guard.sh", "notify-permission.sh", "worktree-create.sh"} {
		p := filepath.Join(dir, ".claude", "hooks", name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("hook %s not deployed: %v", name, err)
		}
	}
}

func TestDeployHooksToWorktree_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToWorktree(dir, testScripts); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := DeployHooksToWorktree(dir, testScripts); err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Both files still parseable JSON
	for _, name := range []string{"settings.json", "settings.local.json"} {
		data, _ := os.ReadFile(filepath.Join(dir, ".claude", name))
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("%s: invalid JSON after idempotent call: %v", name, err)
		}
	}
}

func TestDeployHooksToProject_NotifyPermissionScriptDeployed(t *testing.T) {
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, testScripts); err != nil {
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
	dir := t.TempDir()
	if err := DeployHooksToProject(dir, testScripts); err != nil {
		t.Fatalf("DeployHooksToProject: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	if !containsHook(data, "notify-permission.sh") {
		t.Error("settings.json should include notify-permission.sh in Notification hook")
	}
	if !strings.Contains(string(data), "Notification") {
		t.Error("settings.json should include Notification hook section")
	}
}

// --- EnsureGitignore tests ---

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
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".mcp.json\n"), 0o644)

	EnsureGitignore(dir)

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	if strings.Count(content, ".mcp.json") != 1 {
		t.Errorf(".mcp.json duplicated:\n%s", content)
	}
	for _, rule := range zpitIgnoreRules {
		if !strings.Contains(content, rule) {
			t.Errorf("missing rule: %s", rule)
		}
	}
}

func TestEnsureGitignore_AllExist(t *testing.T) {
	dir := t.TempDir()
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
	EnsureGitignore(dir)

	path := filepath.Join(dir, ".gitignore")
	data, _ := os.ReadFile(path)
	trimmed := strings.Replace(string(data), ".mcp.json\n", "", 1)
	os.WriteFile(path, []byte(trimmed), 0o644)

	EnsureGitignore(dir)

	data, _ = os.ReadFile(path)
	if strings.Count(string(data), "# Zpit auto-deploy") != 1 {
		t.Errorf("duplicate header:\n%s", string(data))
	}
	if !strings.Contains(string(data), ".mcp.json") {
		t.Error("missing rule was not re-added")
	}
}

func containsHook(data []byte, hookName string) bool {
	return strings.Contains(string(data), hookName)
}

func TestZpitIgnoreRules_ContainsAllDeployedArtifacts(t *testing.T) {
	rules := strings.Join(zpitIgnoreRules, "\n")

	for _, d := range ZpitDeployedDirs {
		pattern := ".claude/" + d + "/"
		if !strings.Contains(rules, pattern) {
			t.Errorf("ZpitDeployedDirs entry %q missing from zpitIgnoreRules", d)
		}
	}
	for _, f := range ZpitDeployedFiles {
		if !strings.Contains(rules, f) {
			t.Errorf("ZpitDeployedFiles entry %q missing from zpitIgnoreRules", f)
		}
	}
	if !strings.Contains(rules, ".claude/settings.json") {
		t.Error("settings.json missing from zpitIgnoreRules — committing it creates a latent fresh-clone bug because the hook commands it references live in gitignored dirs")
	}
	if strings.Contains(rules, ".claude/settings.local.json") {
		t.Error("settings.local.json should NOT be in zpitIgnoreRules — it causes user-project .gitignore drift on every launch")
	}
	if strings.Contains(rules, ".zpit-children/") {
		t.Error(".zpit-children/ must NOT be in zpitIgnoreRules — child worktrees now live under $HOME/.zpit/children/, outside the project")
	}
}

// --- CleanSettingsHooks tests ---

func TestCleanSettingsHooks_RemovesHooksKey(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	os.MkdirAll(claudeDir, 0o755)

	settings := `{
  "hooks": { "PreToolUse": [] },
  "allowedTools": ["Read"]
}
`
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644)

	if !CleanSettingsHooks(dir) {
		t.Fatal("expected CleanSettingsHooks to return true")
	}

	data, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json should still exist: %v", err)
	}
	if strings.Contains(string(data), "hooks") {
		t.Errorf("hooks key should be removed:\n%s", string(data))
	}
	if !strings.Contains(string(data), "allowedTools") {
		t.Errorf("other keys should be preserved:\n%s", string(data))
	}
}

func TestCleanSettingsHooks_DeletesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	os.MkdirAll(claudeDir, 0o755)

	settings := `{ "hooks": { "PreToolUse": [] } }
`
	settingsPath := filepath.Join(claudeDir, "settings.json")
	os.WriteFile(settingsPath, []byte(settings), 0o644)

	if !CleanSettingsHooks(dir) {
		t.Fatal("expected CleanSettingsHooks to return true")
	}

	if _, err := os.Stat(settingsPath); err == nil {
		t.Error("settings.json should be deleted when empty after hooks removal")
	}
}

func TestCleanSettingsHooks_NoHooksKey(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	os.MkdirAll(claudeDir, 0o755)

	settings := `{ "allowedTools": ["Read"] }
`
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644)

	if CleanSettingsHooks(dir) {
		t.Error("expected CleanSettingsHooks to return false when no hooks key")
	}
}

func TestCleanSettingsHooks_NoFile(t *testing.T) {
	dir := t.TempDir()
	if CleanSettingsHooks(dir) {
		t.Error("expected CleanSettingsHooks to return false when no settings.json")
	}
}
