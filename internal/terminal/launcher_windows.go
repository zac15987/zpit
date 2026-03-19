//go:build windows

package terminal

import (
	"fmt"
	"os/exec"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
)

func launchWindows(project config.ProjectConfig, cfg config.TerminalConfig, path string, extraArgs []string) (*LaunchResult, error) {
	args := BuildWindowsArgs(project.Name, path, cfg.WindowsMode, extraArgs)

	cmd := exec.Command("wt.exe", args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launching Windows Terminal: %w", err)
	}

	return &LaunchResult{
		Env:        platform.EnvWindowsTerminal,
		Command:    "wt.exe",
		Args:       args,
		SwitchHint: fmt.Sprintf("Tab: %s", project.Name),
	}, nil
}

func launchTmux(project config.ProjectConfig, cfg config.TerminalConfig, path string, extraArgs []string) (*LaunchResult, error) {
	return nil, fmt.Errorf("tmux not available on Windows")
}
