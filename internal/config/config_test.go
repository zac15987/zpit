package config

import (
	"os"
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
	if len(cfg.Projects) != 7 {
		t.Fatalf("Projects = %d, want 7", len(cfg.Projects))
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

	// Agent models — testdata/config.toml explicitly sets all five
	if cfg.AgentModels.Clarifier != "opus[1m]" {
		t.Errorf("AgentModels.Clarifier = %q, want %q", cfg.AgentModels.Clarifier, "opus[1m]")
	}
	if cfg.AgentModels.Coding != "opus[1m]" {
		t.Errorf("AgentModels.Coding = %q, want %q", cfg.AgentModels.Coding, "opus[1m]")
	}
	if cfg.AgentModels.Reviewer != "opus[1m]" {
		t.Errorf("AgentModels.Reviewer = %q, want %q", cfg.AgentModels.Reviewer, "opus[1m]")
	}
	if cfg.AgentModels.TaskRunner != "sonnet" {
		t.Errorf("AgentModels.TaskRunner = %q, want %q", cfg.AgentModels.TaskRunner, "sonnet")
	}
	if cfg.AgentModels.Efficiency != "opus[1m]" {
		t.Errorf("AgentModels.Efficiency = %q, want %q", cfg.AgentModels.Efficiency, "opus[1m]")
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

func TestAgentModelsDefaults_Minimal(t *testing.T) {
	cfg, err := Load(testdataPath("config_minimal.toml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.AgentModels.Clarifier != defaultClarifierModel {
		t.Errorf("AgentModels.Clarifier = %q, want %q", cfg.AgentModels.Clarifier, defaultClarifierModel)
	}
	if cfg.AgentModels.Coding != defaultCodingModel {
		t.Errorf("AgentModels.Coding = %q, want %q", cfg.AgentModels.Coding, defaultCodingModel)
	}
	if cfg.AgentModels.Reviewer != defaultReviewerModel {
		t.Errorf("AgentModels.Reviewer = %q, want %q", cfg.AgentModels.Reviewer, defaultReviewerModel)
	}
	if cfg.AgentModels.TaskRunner != defaultTaskRunnerModel {
		t.Errorf("AgentModels.TaskRunner = %q, want %q", cfg.AgentModels.TaskRunner, defaultTaskRunnerModel)
	}
	if cfg.AgentModels.Efficiency != defaultEfficiencyModel {
		t.Errorf("AgentModels.Efficiency = %q, want %q", cfg.AgentModels.Efficiency, defaultEfficiencyModel)
	}
}

func TestAgentModelsPartialOverride(t *testing.T) {
	// Write a temp config with only `clarifier` overridden; the other four
	// fields must fall back to defaults.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[terminal]\n\n[agent_models]\nclarifier = \"custom-opus-id\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.AgentModels.Clarifier != "custom-opus-id" {
		t.Errorf("Clarifier override lost: %q", cfg.AgentModels.Clarifier)
	}
	if cfg.AgentModels.Coding != defaultCodingModel {
		t.Errorf("Coding = %q, want default %q", cfg.AgentModels.Coding, defaultCodingModel)
	}
	if cfg.AgentModels.Reviewer != defaultReviewerModel {
		t.Errorf("Reviewer = %q, want default %q", cfg.AgentModels.Reviewer, defaultReviewerModel)
	}
	if cfg.AgentModels.TaskRunner != defaultTaskRunnerModel {
		t.Errorf("TaskRunner = %q, want default %q", cfg.AgentModels.TaskRunner, defaultTaskRunnerModel)
	}
	if cfg.AgentModels.Efficiency != defaultEfficiencyModel {
		t.Errorf("Efficiency = %q, want default %q", cfg.AgentModels.Efficiency, defaultEfficiencyModel)
	}
}

func TestAgentModelsDiff_HotReload(t *testing.T) {
	base := &Config{}
	updated := &Config{AgentModels: AgentModelsConfig{Clarifier: "new"}}
	d := Diff(base, updated)
	found := false
	for _, f := range d.HotReload {
		if f == "agent_models" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent_models change should be hot-reloadable; got HotReload=%v", d.HotReload)
	}
}

// TestProjectAutoMergeDiff_HotReload guards the invariant that toggling
// per-project `auto_merge` / `merge_method` is picked up by the running loop
// without requiring a restart. Both fields flow through `projectsMetaEqual`
// and must surface under `HotReload` (via the `project_meta` tag), never
// under `RestartRequired`.
func TestProjectAutoMergeDiff_HotReload(t *testing.T) {
	base := &Config{
		Projects: []ProjectConfig{
			{ID: "p1", AutoMerge: false, MergeMethod: "squash"},
		},
	}
	cases := []struct {
		name    string
		updated *Config
	}{
		{
			name: "toggle_auto_merge",
			updated: &Config{
				Projects: []ProjectConfig{
					{ID: "p1", AutoMerge: true, MergeMethod: "squash"},
				},
			},
		},
		{
			name: "change_merge_method",
			updated: &Config{
				Projects: []ProjectConfig{
					{ID: "p1", AutoMerge: false, MergeMethod: "rebase"},
				},
			},
		},
		{
			name: "toggle_both",
			updated: &Config{
				Projects: []ProjectConfig{
					{ID: "p1", AutoMerge: true, MergeMethod: "merge"},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Diff(base, tc.updated)
			foundHot := false
			for _, f := range d.HotReload {
				if f == "project_meta" {
					foundHot = true
					break
				}
			}
			if !foundHot {
				t.Errorf("auto_merge/merge_method change should be hot-reloadable; got HotReload=%v", d.HotReload)
			}
			for _, f := range d.RestartRequired {
				if f == "project_meta" || f == "projects (added/removed)" {
					t.Errorf("auto_merge/merge_method change must NOT require restart; got RestartRequired=%v", d.RestartRequired)
				}
			}
		})
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
