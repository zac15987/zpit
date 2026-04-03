package terminal

import (
	"fmt"
	"strings"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
)

// LaunchResult contains info about a launched terminal session.
type LaunchResult struct {
	Env        platform.Environment
	Command    string
	Args       []string
	SwitchHint string
	Warnings   []string // non-fatal warnings (e.g. WT profile resolution failures)
}

// LaunchClaude opens a new terminal window/tab and runs claude in the project directory.
func LaunchClaude(project config.ProjectConfig, cfg config.TerminalConfig, extraArgs ...string) (*LaunchResult, error) {
	env := platform.Detect()
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)

	switch env {
	case platform.EnvWindowsTerminal:
		return launchWindows(project, cfg, projectPath, extraArgs)
	case platform.EnvWSLTmux, platform.EnvLinuxTmux:
		return launchTmux(project, cfg, projectPath, extraArgs)
	default:
		return nil, fmt.Errorf("unsupported environment: %s", env)
	}
}

// LaunchClaudeInDir opens Claude Code in a new terminal with a custom working directory.
// Used by the loop engine to launch agents in worktree directories.
func LaunchClaudeInDir(workDir, tabTitle string, cfg config.TerminalConfig, extraArgs ...string) (*LaunchResult, error) {
	env := platform.Detect()

	switch env {
	case platform.EnvWindowsTerminal:
		return launchWindowsInDir(tabTitle, cfg, workDir, extraArgs)
	case platform.EnvWSLTmux, platform.EnvLinuxTmux:
		return launchTmuxInDir(tabTitle, cfg, workDir, extraArgs)
	default:
		return nil, fmt.Errorf("unsupported environment: %s", env)
	}
}

// buildClaudeArgs returns "claude" followed by any extra arguments as separate elements.
// If --channel-enabled is present, it is removed from the args and
// --dangerously-load-development-channels server:zpit-channel is injected instead.
func buildClaudeArgs(extraArgs []string) []string {
	filtered, channelEnabled := filterChannelFlag(extraArgs)
	args := append([]string{"claude"}, filtered...)
	if channelEnabled {
		args = append(args, "--dangerously-load-development-channels", "server:zpit-channel")
	}
	return args
}

// filterChannelFlag removes --channel-enabled from the args slice.
// Returns the filtered args and whether the flag was found.
func filterChannelFlag(args []string) ([]string, bool) {
	found := false
	var filtered []string
	for _, arg := range args {
		if arg == "--channel-enabled" {
			found = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, found
}

// hasAgentFlag returns true if extraArgs contains "--agent".
func hasAgentFlag(extraArgs []string) bool {
	for _, arg := range extraArgs {
		if arg == "--agent" {
			return true
		}
	}
	return false
}

// BuildWindowsArgs constructs wt.exe arguments for testing without exec.
// When profile is non-empty, -p "ProfileName" is inserted before the -- separator.
// When extraArgs contains "--agent", the command is wrapped with the appropriate
// env wrapper based on shell type to inject ZPIT_AGENT=1.
// shell should be "cmd", "pwsh", or "powershell" (empty defaults to "cmd").
func BuildWindowsArgs(projectName, projectPath, mode, profile, shell string, extraArgs []string) []string {
	var base []string
	switch mode {
	case "new_window":
		base = []string{"-w", "new"}
	default: // "new_tab"
		base = []string{"new-tab"}
	}

	if profile != "" {
		base = append(base, "-p", profile)
	}
	base = append(base, "-d", projectPath, "--title", projectName, "--")

	if hasAgentFlag(extraArgs) {
		wrapper := buildEnvWrapper(shell)
		return append(append(base, wrapper...), buildClaudeArgs(extraArgs)...)
	}
	return append(base, buildClaudeArgs(extraArgs)...)
}

// buildEnvWrapper returns the command prefix for the ZPIT_AGENT=1 env wrapper
// based on the detected shell type.
func buildEnvWrapper(shell string) []string {
	switch shell {
	case "pwsh":
		return []string{"pwsh", "-NoProfile", "-File", ".claude\\hooks\\zpit-env.ps1"}
	case "powershell":
		return []string{"powershell", "-NoProfile", "-File", ".claude\\hooks\\zpit-env.ps1"}
	default: // "cmd" or empty
		return []string{"cmd", "/c", ".claude\\hooks\\zpit-env.cmd"}
	}
}

// BuildTmuxArgs constructs tmux arguments for testing without exec.
// When extraArgs contains "--agent", ZPIT_AGENT=1 is prefixed to the command
// so hook scripts can detect agent sessions.
func BuildTmuxArgs(projectID, projectPath, mode string, extraArgs []string) []string {
	claudeCmd := strings.Join(buildClaudeArgs(extraArgs), " ")
	if hasAgentFlag(extraArgs) {
		claudeCmd = "ZPIT_AGENT=1 " + claudeCmd
	}
	switch mode {
	case "new_pane":
		return []string{"split-window", "-h", "-c", projectPath, claudeCmd}
	default: // "new_window"
		return []string{"new-window", "-n", projectID, "-c", projectPath, claudeCmd}
	}
}
