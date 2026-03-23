package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/zac15987/zpit/internal/loop"
)

// Default values for config fields.
const (
	defaultWindowsMode     = "new_tab"
	defaultTmuxMode        = "new_window"
	defaultMaxPerProject   = 5
	defaultReRemindMinutes = 15
	defaultDirFormat       = "{project_id}/{issue_id}--{slug}"
	defaultBaseBranch      = "dev"
)

// Config is the top-level configuration loaded from config.toml.
// ProfileConfig holds agent-relevant metadata per project type.
type ProfileConfig struct {
	LogPolicy string `toml:"log_policy"` // "strict" | "standard" | "minimal"
}

type Config struct {
	Terminal     TerminalConfig            `toml:"terminal"`
	Notification NotificationConfig        `toml:"notification"`
	Worktree     WorktreeConfig            `toml:"worktree"`
	Providers    ProvidersConfig           `toml:"providers"`
	Profiles     map[string]ProfileConfig  `toml:"profiles"`
	Projects     []ProjectConfig           `toml:"projects"`
}

type TerminalConfig struct {
	WindowsMode string `toml:"windows_mode"` // "new_tab" | "new_window"
	TmuxMode    string `toml:"tmux_mode"`    // "new_window" | "new_pane"
}

type NotificationConfig struct {
	TUIAlert        bool `toml:"tui_alert"`
	WindowsToast    bool `toml:"windows_toast"`
	Sound           bool `toml:"sound"`
	ReRemindMinutes int  `toml:"re_remind_minutes"`
}

type WorktreeConfig struct {
	BaseDirWindows  string `toml:"base_dir_windows"`
	BaseDirWSL      string `toml:"base_dir_wsl"`
	DirFormat       string `toml:"dir_format"`
	AutoCleanup     bool   `toml:"auto_cleanup"`
	MaxPerProject   int    `toml:"max_per_project"`
	MaxReviewRounds int    `toml:"max_review_rounds"`
	PollSeconds     int    `toml:"poll_seconds"`    // todo issue polling interval
	PRPollSeconds   int    `toml:"pr_poll_seconds"` // PR merge polling interval
}

type ProvidersConfig struct {
	Tracker map[string]ProviderEntry `toml:"tracker"`
	Git     map[string]ProviderEntry `toml:"git"`
}

type ProviderEntry struct {
	Type     string `toml:"type"`
	URL      string `toml:"url"`
	TokenEnv string `toml:"token_env"`
}

type ProjectConfig struct {
	Name           string            `toml:"name"`
	ID             string            `toml:"id"`
	Profile        string            `toml:"profile"`
	HookMode       string            `toml:"hook_mode"`
	Tracker        string            `toml:"tracker"`
	TrackerProject string            `toml:"tracker_project"`
	Git            string            `toml:"git"`
	Repo           string            `toml:"repo"`
	SharedCore     bool              `toml:"shared_core"`
	LogLevel       string            `toml:"log_level"`
	BaseBranch     string            `toml:"base_branch"`
	Tags           []string          `toml:"tags"`
	Path           ProjectPathConfig `toml:"path"`
}

type ProjectPathConfig struct {
	Windows string `toml:"windows"`
	WSL     string `toml:"wsl"`
}

// BaseDir returns the Zpit data directory (~/.zpit/).
func BaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".zpit"), nil
}

func DefaultConfigPath() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "config.toml"), nil
}

const configTemplate = `# Zpit Configuration
# Docs: https://github.com/zac15987/zpit

[terminal]
windows_mode = "new_tab"    # new_tab | new_window
tmux_mode = "new_window"    # new_window | new_pane

[notification]
tui_alert = true
windows_toast = true
sound = true
re_remind_minutes = 15

[worktree]
base_dir_windows = ""       # e.g. "D:/worktrees"
base_dir_wsl = ""           # e.g. "/mnt/d/worktrees"
max_per_project = 5
# poll_seconds = 15         # todo issue polling interval (seconds)
# pr_poll_seconds = 30      # PR merge polling interval (seconds)

# --- Providers ---
# Uncomment and fill in your tracker provider(s).

# [providers.tracker.my-forgejo]
# type = "forgejo_issues"
# url = "https://your-forgejo.example.com"
# token_env = "FORGEJO_TOKEN"

# [providers.tracker.my-github]
# type = "github_issues"
# token_env = "GITHUB_TOKEN"

# --- Profiles ---

# [profiles.default]
# log_policy = "standard"   # strict | standard | minimal

# --- Projects ---
# Add at least one project to get started.

# [[projects]]
# name = "My Project"
# id = "my-project"
# profile = "default"
# hook_mode = "standard"    # strict | standard | relaxed
# tracker = "my-github"
# repo = "owner/repo"
# base_branch = "dev"
# tags = ["go"]
#
# [projects.path]
# windows = "D:/Projects/my-project"
# wsl = "/mnt/d/Projects/my-project"
`

// WriteTemplate creates a config file with a starter template.
func WriteTemplate(path string) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	return os.WriteFile(path, []byte(configTemplate), 0o644)
}

// Load reads and parses the TOML config file.
func Load(path string) (*Config, error) {
	var cfg Config
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, fmt.Errorf("loading config from %s: %w", path, err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Terminal.WindowsMode == "" {
		cfg.Terminal.WindowsMode = defaultWindowsMode
	}
	if cfg.Terminal.TmuxMode == "" {
		cfg.Terminal.TmuxMode = defaultTmuxMode
	}
	if cfg.Worktree.MaxPerProject == 0 {
		cfg.Worktree.MaxPerProject = defaultMaxPerProject
	}
	if cfg.Notification.ReRemindMinutes == 0 {
		cfg.Notification.ReRemindMinutes = defaultReRemindMinutes
	}
	if cfg.Worktree.DirFormat == "" {
		cfg.Worktree.DirFormat = defaultDirFormat
	}
	if cfg.Worktree.MaxReviewRounds == 0 {
		cfg.Worktree.MaxReviewRounds = loop.DefaultMaxReviewRounds
	}
	if cfg.Worktree.PollSeconds == 0 {
		cfg.Worktree.PollSeconds = loop.DefaultPollSeconds
	}
	if cfg.Worktree.PRPollSeconds == 0 {
		cfg.Worktree.PRPollSeconds = loop.DefaultPRPollSeconds
	}
	for i := range cfg.Projects {
		if cfg.Projects[i].BaseBranch == "" {
			cfg.Projects[i].BaseBranch = defaultBaseBranch
		}
	}
}
