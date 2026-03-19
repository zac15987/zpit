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

// buildClaudeCommand constructs the claude command string with optional extra arguments.
func buildClaudeCommand(extraArgs []string) string {
	if len(extraArgs) == 0 {
		return "claude"
	}
	parts := append([]string{"claude"}, extraArgs...)
	return strings.Join(parts, " ")
}

// BuildWindowsArgs constructs wt.exe arguments for testing without exec.
func BuildWindowsArgs(projectName, projectPath, mode string, extraArgs []string) []string {
	claudeCmd := buildClaudeCommand(extraArgs)
	switch mode {
	case "new_window":
		return []string{"-w", "new", "-d", projectPath, "--title", projectName, "--", claudeCmd}
	default: // "new_tab"
		return []string{"new-tab", "-d", projectPath, "--title", projectName, "--", claudeCmd}
	}
}

// BuildTmuxArgs constructs tmux arguments for testing without exec.
func BuildTmuxArgs(projectID, projectPath, mode string, extraArgs []string) []string {
	claudeCmd := buildClaudeCommand(extraArgs)
	switch mode {
	case "new_pane":
		return []string{"split-window", "-h", "-c", projectPath, claudeCmd}
	default: // "new_window"
		return []string{"new-window", "-n", projectID, "-c", projectPath, claudeCmd}
	}
}
