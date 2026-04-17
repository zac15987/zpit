package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", name)
}

func TestLoad(t *testing.T) {
	cfg, err := Load(testdataPath("config.toml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Terminal
	if cfg.Terminal.WindowsMode != "new_tab" {
		t.Errorf("WindowsMode = %q, want %q", cfg.Terminal.WindowsMode, "new_tab")
	}
	if cfg.Terminal.TmuxMode != "new_window" {
		t.Errorf("TmuxMode = %q, want %q", cfg.Terminal.TmuxMode, "new_window")
	}

	// Notification
	if !cfg.Notification.TUIAlert {
		t.Error("TUIAlert should be true")
	}
	if cfg.Notification.ReRemindMinutes != 2 {
		t.Errorf("ReRemindMinutes = %d, want 2", cfg.Notification.ReRemindMinutes)
	}

	// Worktree
	if cfg.Worktree.MaxPerProject != 5 {
		t.Errorf("MaxPerProject = %d, want 5", cfg.Worktree.MaxPerProject)
	}

	// Providers
	if len(cfg.Providers.Tracker) != 2 {
		t.Errorf("Tracker providers = %d, want 2", len(cfg.Providers.Tracker))
	}
	forgejo, ok := cfg.Providers.Tracker["forgejo-leyu"]
	if !ok {
		t.Fatal("forgejo-leyu tracker not found")
	}
	if forgejo.Type != "forgejo_issues" {
		t.Errorf("forgejo-leyu type = %q, want %q", forgejo.Type, "forgejo_issues")
	}
	if forgejo.TokenEnv != "FORGEJO_TOKEN" {
		t.Errorf("forgejo-leyu token_env = %q, want %q", forgejo.TokenEnv, "FORGEJO_TOKEN")
	}

	if len(cfg.Providers.Git) != 2 {
		t.Errorf("Git providers = %d, want 2", len(cfg.Providers.Git))
	}

	// Projects
	if len(cfg.Projects) != 5 {
		t.Fatalf("Projects = %d, want 5", len(cfg.Projects))
	}

	first := cfg.Projects[0]
	if first.ID != "ai-inspection-cleaning" {
		t.Errorf("Projects[0].ID = %q", first.ID)
	}
	if first.Profile != "machine" {
		t.Errorf("Projects[0].Profile = %q", first.Profile)
	}
	if first.HookMode != "strict" {
		t.Errorf("Projects[0].HookMode = %q", first.HookMode)
	}
	if !first.SharedCore {
		t.Error("Projects[0].SharedCore should be true")
	}
	if len(first.Tags) != 3 {
		t.Errorf("Projects[0].Tags = %v", first.Tags)
	}
	if first.Path.Windows == "" {
		t.Error("Projects[0].Path.Windows should not be empty")
	}
	if first.Path.WSL == "" {
		t.Error("Projects[0].Path.WSL should not be empty")
	}
	if first.BaseBranch != "dev" {
		t.Errorf("Projects[0].BaseBranch = %q, want %q", first.BaseBranch, "dev")
	}
	if first.LogPolicy != "strict" {
		t.Errorf("Projects[0].LogPolicy = %q, want %q", first.LogPolicy, "strict")
	}

	// SSH
	if !cfg.SSH.AutoServe {
		t.Error("SSH.AutoServe should be true")
	}

	// log_policy is now per-project: verify a different project has a different policy
	var desktopProj *ProjectConfig
	for i := range cfg.Projects {
		if cfg.Projects[i].Profile == "desktop" {
			desktopProj = &cfg.Projects[i]
			break
		}
	}
	if desktopProj == nil {
		t.Fatal("no project with profile=desktop found in testdata")
	}
	if desktopProj.LogPolicy != "standard" {
		t.Errorf("desktop project LogPolicy = %q, want %q", desktopProj.LogPolicy, "standard")
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(testdataPath("config.toml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Worktree.DirFormat != "{project_id}/{issue_id}--{slug}" {
		t.Errorf("DirFormat = %q", cfg.Worktree.DirFormat)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("/nonexistent/config.toml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadMinimal_AppliesAllDefaults(t *testing.T) {
	cfg, err := Load(testdataPath("config_minimal.toml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Terminal.WindowsMode != defaultWindowsMode {
		t.Errorf("WindowsMode = %q, want %q", cfg.Terminal.WindowsMode, defaultWindowsMode)
	}
	if cfg.Terminal.TmuxMode != defaultTmuxMode {
		t.Errorf("TmuxMode = %q, want %q", cfg.Terminal.TmuxMode, defaultTmuxMode)
	}
	if cfg.Worktree.MaxPerProject != defaultMaxPerProject {
		t.Errorf("MaxPerProject = %d, want %d", cfg.Worktree.MaxPerProject, defaultMaxPerProject)
	}
	if cfg.Notification.ReRemindMinutes != defaultReRemindMinutes {
		t.Errorf("ReRemindMinutes = %d, want %d", cfg.Notification.ReRemindMinutes, defaultReRemindMinutes)
	}
	if cfg.Worktree.DirFormat != defaultDirFormat {
		t.Errorf("DirFormat = %q, want %q", cfg.Worktree.DirFormat, defaultDirFormat)
	}
}

func TestBaseBranchDefault(t *testing.T) {
	cfg, err := Load(testdataPath("config.toml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// Projects without explicit base_branch should default to "dev"
	for i, p := range cfg.Projects {
		if p.BaseBranch != "dev" {
			t.Errorf("Projects[%d].BaseBranch = %q, want %q", i, p.BaseBranch, "dev")
		}
	}
}

func TestLoadMinimal_ZeroProjects(t *testing.T) {
	cfg, err := Load(testdataPath("config_minimal.toml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Projects) != 0 {
		t.Errorf("Projects = %d, want 0", len(cfg.Projects))
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath failed: %v", err)
	}
	if path == "" {
		t.Error("DefaultConfigPath returned empty string")
	}
}
