package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/zac15987/zpit/internal/loop"
)

// Default values for config fields.
const (
	defaultWindowsMode     = "new_tab"
	defaultTmuxMode        = "new_window"
	defaultMaxPerProject   = 5
	defaultReRemindMinutes = 2
	defaultDirFormat       = "{project_id}/{issue_id}--{slug}"
	defaultBaseBranch      = "dev"

	defaultBrokerPort = 17731

	defaultSSHPort               = 2200
	defaultSSHHost               = "0.0.0.0"
	defaultSSHHostKeyPath        = "~/.zpit/ssh/host_ed25519"
	defaultSSHAuthorizedKeysPath = "~/.ssh/authorized_keys"
)

// Config is the top-level configuration loaded from config.toml.
// ProfileConfig holds agent-relevant metadata per project type.
type ProfileConfig struct {
	LogPolicy string `toml:"log_policy"` // "strict" | "standard" | "minimal"
}

type Config struct {
	Language     string                    `toml:"language"`
	BrokerPort   int                       `toml:"broker_port"`
	ZpitBin      string                    `toml:"zpit_bin"`
	Terminal     TerminalConfig            `toml:"terminal"`
	Notification NotificationConfig        `toml:"notification"`
	Worktree     WorktreeConfig            `toml:"worktree"`
	SSH          SSHConfig                 `toml:"ssh"`
	Providers    ProvidersConfig           `toml:"providers"`
	Profiles     map[string]ProfileConfig  `toml:"profiles"`
	Projects     []ProjectConfig           `toml:"projects"`
}

// SSHConfig holds settings for the Wish SSH server (zpit serve).
type SSHConfig struct {
	Port               int    `toml:"port"`
	Host               string `toml:"host"`
	HostKeyPath        string `toml:"host_key_path"`
	PasswordEnv        string `toml:"password_env"`
	AuthorizedKeysPath string `toml:"authorized_keys_path"`
}

type TerminalConfig struct {
	WindowsMode            string `toml:"windows_mode"`              // "new_tab" | "new_window"
	TmuxMode               string `toml:"tmux_mode"`                 // "new_window" | "new_pane"
	WindowsTerminalProfile string `toml:"windows_terminal_profile"`  // WT profile name for -p flag
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
	ChannelEnabled bool              `toml:"channel_enabled"`
	ChannelListen  []string          `toml:"channel_listen"`
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

# broker_port: TCP port for the cross-agent channel broker (default 17731).
# The broker starts automatically when any project has channel_enabled = true.
# broker_port = 17731

# zpit_bin: explicit path to the zpit executable for .mcp.json generation.
# Useful when running via "go run ." (temp binary path becomes invalid after exit).
# If omitted, falls back to os.Executable().
# zpit_bin = "/usr/local/bin/zpit"

[terminal]
windows_mode = "new_tab"    # new_tab | new_window
tmux_mode = "new_window"    # new_window | new_pane
# windows_terminal_profile = "PowerShell 7"  # WT profile name for -p flag and auto shell detection

[notification]
tui_alert = true
windows_toast = true
sound = true
re_remind_minutes = 2

[worktree]
base_dir_windows = ""       # e.g. "D:/worktrees"
base_dir_wsl = ""           # e.g. "/mnt/d/worktrees"
max_per_project = 5
# poll_seconds = 10         # todo issue polling interval (seconds)
# pr_poll_seconds = 10      # PR merge polling interval (seconds)

# --- SSH Server (zpit serve) ---
# [ssh]
# port = 2200
# host = "0.0.0.0"
# host_key_path = "~/.zpit/ssh/host_ed25519"
# authorized_keys_path = "~/.ssh/authorized_keys"
# password_env = ""               # env var name for password auth (e.g. "ZPIT_SSH_PASSWORD")

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
# channel_enabled = false  # enable cross-agent channel communication
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

// ResolveSSHPaths expands ~ in SSH config paths to the user's home directory.
func (s *SSHConfig) ResolveSSHPaths() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	expand := func(p string) string {
		if strings.HasPrefix(p, "~/") {
			return filepath.Join(home, p[2:])
		}
		return p
	}
	s.HostKeyPath = expand(s.HostKeyPath)
	s.AuthorizedKeysPath = expand(s.AuthorizedKeysPath)
	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.Language == "" {
		cfg.Language = "en"
	}
	if cfg.BrokerPort == 0 {
		cfg.BrokerPort = defaultBrokerPort
	}
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
	if cfg.SSH.Port == 0 {
		cfg.SSH.Port = defaultSSHPort
	}
	if cfg.SSH.Host == "" {
		cfg.SSH.Host = defaultSSHHost
	}
	if cfg.SSH.HostKeyPath == "" {
		cfg.SSH.HostKeyPath = defaultSSHHostKeyPath
	}
	if cfg.SSH.AuthorizedKeysPath == "" {
		cfg.SSH.AuthorizedKeysPath = defaultSSHAuthorizedKeysPath
	}

	for i := range cfg.Projects {
		if cfg.Projects[i].BaseBranch == "" {
			cfg.Projects[i].BaseBranch = defaultBaseBranch
		}
		if cfg.Projects[i].HookMode == "" {
			cfg.Projects[i].HookMode = "strict"
		}
	}
}
