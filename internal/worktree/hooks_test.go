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
	if err := SetupHookMode(dir, "strict"); err != nil {
		t.Fatalf("SetupHookMode(strict): %v", err)
	}
	// No file should be written.
	path := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(path); err == nil {
		t.Error("settings.local.json should not exist for strict mode")
	}
}

func TestSetupHookMode_Standard(t *testing.T) {
	dir := t.TempDir()
	if err := SetupHookMode(dir, "standard"); err != nil {
		t.Fatalf("SetupHookMode(standard): %v", err)
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
	if err := SetupHookMode(dir, "relaxed"); err != nil {
		t.Fatalf("SetupHookMode(relaxed): %v", err)
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

func TestSetupHookMode_Unknown(t *testing.T) {
	dir := t.TempDir()
	if err := SetupHookMode(dir, "banana"); err == nil {
		t.Error("expected error for unknown hook_mode")
	}
}

func TestSetupHookMode_ValidJSON(t *testing.T) {
	for _, mode := range []string{"standard", "relaxed"} {
		dir := t.TempDir()
		if err := SetupHookMode(dir, mode); err != nil {
			t.Fatalf("SetupHookMode(%s): %v", mode, err)
		}
		data := readSettingsLocal(t, dir)
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("%s: invalid JSON: %v", mode, err)
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
