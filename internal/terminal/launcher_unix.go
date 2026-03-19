//go:build !windows

package terminal

import (
	"fmt"
	"os/exec"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
)

func launchWindows(project config.ProjectConfig, cfg config.TerminalConfig, path string, extraArgs []string) (*LaunchResult, error) {
	return nil, fmt.Errorf("Windows Terminal not available on this platform")
}

func launchTmux(project config.ProjectConfig, cfg config.TerminalConfig, path string, extraArgs []string) (*LaunchResult, error) {
	args := BuildTmuxArgs(project.ID, path, cfg.TmuxMode, extraArgs)

	cmd := exec.Command("tmux", args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launching tmux: %w", err)
	}

	hint := fmt.Sprintf("tmux select-window -t %s", project.ID)
	if cfg.TmuxMode == "new_pane" {
		hint = "Visible in split pane"
	}

	return &LaunchResult{
		Env:        platform.Detect(),
		Command:    "tmux",
		Args:       args,
		SwitchHint: hint,
	}, nil
}
