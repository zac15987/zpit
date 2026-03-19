package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level configuration loaded from config.toml.
type Config struct {
	Terminal     TerminalConfig     `toml:"terminal"`
	Notification NotificationConfig `toml:"notification"`
	Worktree     WorktreeConfig     `toml:"worktree"`
	Providers    ProvidersConfig    `toml:"providers"`
	Projects     []ProjectConfig    `toml:"projects"`
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
	BaseDirWindows string `toml:"base_dir_windows"`
	BaseDirWSL     string `toml:"base_dir_wsl"`
	DirFormat      string `toml:"dir_format"`
	AutoCleanup    bool   `toml:"auto_cleanup"`
	MaxPerProject  int    `toml:"max_per_project"`
}

type ProvidersConfig struct {
	Tracker map[string]ProviderEntry `toml:"tracker"`
	Git     map[string]ProviderEntry `toml:"git"`
}

type ProviderEntry struct {
	Type      string `toml:"type"`
	URL       string `toml:"url"`
	APIKeyEnv string `toml:"api_key_env"`
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
	Tags           []string          `toml:"tags"`
	Path           ProjectPathConfig `toml:"path"`
}

type ProjectPathConfig struct {
	Windows string `toml:"windows"`
	WSL     string `toml:"wsl"`
}

// DefaultConfigPath returns ~/.config/zpit/config.toml.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zpit", "config.toml")
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
		cfg.Terminal.WindowsMode = "new_tab"
	}
	if cfg.Terminal.TmuxMode == "" {
		cfg.Terminal.TmuxMode = "new_window"
	}
	if cfg.Worktree.MaxPerProject == 0 {
		cfg.Worktree.MaxPerProject = 5
	}
	if cfg.Notification.ReRemindMinutes == 0 {
		cfg.Notification.ReRemindMinutes = 15
	}
	if cfg.Worktree.DirFormat == "" {
		cfg.Worktree.DirFormat = "{project_id}/{issue_id}--{slug}"
	}
}
