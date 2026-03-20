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

// buildClaudeArgs returns "claude" followed by any extra arguments as separate elements.
func buildClaudeArgs(extraArgs []string) []string {
	return append([]string{"claude"}, extraArgs...)
}

// BuildWindowsArgs constructs wt.exe arguments for testing without exec.
func BuildWindowsArgs(projectName, projectPath, mode string, extraArgs []string) []string {
	base := []string{}
	switch mode {
	case "new_window":
		base = []string{"-w", "new", "-d", projectPath, "--title", projectName, "--"}
	default: // "new_tab"
		base = []string{"new-tab", "-d", projectPath, "--title", projectName, "--"}
	}
	return append(base, buildClaudeArgs(extraArgs)...)
}

// BuildTmuxArgs constructs tmux arguments for testing without exec.
func BuildTmuxArgs(projectID, projectPath, mode string, extraArgs []string) []string {
	claudeCmd := strings.Join(buildClaudeArgs(extraArgs), " ")
	switch mode {
	case "new_pane":
		return []string{"split-window", "-h", "-c", projectPath, claudeCmd}
	default: // "new_window"
		return []string{"new-window", "-n", projectID, "-c", projectPath, claudeCmd}
	}
}
