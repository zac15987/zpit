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
	if cfg.Notification.ReRemindMinutes != 15 {
		t.Errorf("ReRemindMinutes = %d, want 15", cfg.Notification.ReRemindMinutes)
	}

	// Worktree
	if cfg.Worktree.MaxPerProject != 5 {
		t.Errorf("MaxPerProject = %d, want 5", cfg.Worktree.MaxPerProject)
	}
	if cfg.Worktree.BaseDirWindows != "D:/Projects/.worktrees" {
		t.Errorf("BaseDirWindows = %q", cfg.Worktree.BaseDirWindows)
	}

	// Providers
	if len(cfg.Providers.Tracker) != 4 {
		t.Errorf("Tracker providers = %d, want 4", len(cfg.Providers.Tracker))
	}
	plane, ok := cfg.Providers.Tracker["plane-local"]
	if !ok {
		t.Fatal("plane-local tracker not found")
	}
	if plane.Type != "plane" {
		t.Errorf("plane-local type = %q, want %q", plane.Type, "plane")
	}
	if plane.APIKeyEnv != "PLANE_API_KEY" {
		t.Errorf("plane-local api_key_env = %q", plane.APIKeyEnv)
	}

	if len(cfg.Providers.Git) != 2 {
		t.Errorf("Git providers = %d, want 2", len(cfg.Providers.Git))
	}

	// Projects
	if len(cfg.Projects) != 4 {
		t.Fatalf("Projects = %d, want 4", len(cfg.Projects))
	}

	ase := cfg.Projects[0]
	if ase.ID != "ase-inspection" {
		t.Errorf("Projects[0].ID = %q", ase.ID)
	}
	if ase.Profile != "machine" {
		t.Errorf("Projects[0].Profile = %q", ase.Profile)
	}
	if ase.HookMode != "strict" {
		t.Errorf("Projects[0].HookMode = %q", ase.HookMode)
	}
	if !ase.SharedCore {
		t.Error("Projects[0].SharedCore should be true")
	}
	if len(ase.Tags) != 3 {
		t.Errorf("Projects[0].Tags = %v", ase.Tags)
	}
	if ase.Path.Windows != "D:/Projects/ASE_Inspection" {
		t.Errorf("Projects[0].Path.Windows = %q", ase.Path.Windows)
	}
	if ase.Path.WSL != "/mnt/d/Projects/ASE_Inspection" {
		t.Errorf("Projects[0].Path.WSL = %q", ase.Path.WSL)
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
