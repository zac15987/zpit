//go:build windows

package terminal

import (
	"fmt"
	"os/exec"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
)

// resolveProfileAndShell resolves the WT profile to a shell type.
// Returns profile name, shell type, and any warnings.
// When profile is empty, returns empty values with no WT settings.json read.
func resolveProfileAndShell(profile string) (string, string, []string) {
	if profile == "" {
		return "", "", nil
	}
	result := ResolveWTProfile(profile)
	if result.Warning != "" {
		// Fall back to cmd on failure, still pass profile for -p flag visual appearance.
		return profile, "cmd", []string{result.Warning}
	}
	return profile, result.Shell, nil
}

func launchWindows(project config.ProjectConfig, cfg config.TerminalConfig, path string, extraArgs []string) (*LaunchResult, error) {
	profile, shell, warnings := resolveProfileAndShell(cfg.WindowsTerminalProfile)
	args := BuildWindowsArgs(project.Name, path, cfg.WindowsMode, profile, shell, extraArgs)

	cmd := exec.Command("wt.exe", args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launching Windows Terminal: %w", err)
	}

	return &LaunchResult{
		Env:        platform.EnvWindowsTerminal,
		Command:    "wt.exe",
		Args:       args,
		SwitchHint: fmt.Sprintf("Tab: %s", project.Name),
		Warnings:   warnings,
	}, nil
}

func launchTmux(_ config.ProjectConfig, _ config.TerminalConfig, _ string, _ []string) (*LaunchResult, error) {
	return nil, fmt.Errorf("tmux not available on Windows")
}

func launchWindowsInDir(tabTitle string, cfg config.TerminalConfig, path string, extraArgs []string) (*LaunchResult, error) {
	profile, shell, warnings := resolveProfileAndShell(cfg.WindowsTerminalProfile)
	args := BuildWindowsArgs(tabTitle, path, cfg.WindowsMode, profile, shell, extraArgs)

	cmd := exec.Command("wt.exe", args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launching Windows Terminal: %w", err)
	}

	return &LaunchResult{
		Env:        platform.EnvWindowsTerminal,
		Command:    "wt.exe",
		Args:       args,
		SwitchHint: fmt.Sprintf("Tab: %s", tabTitle),
		Warnings:   warnings,
	}, nil
}

func launchTmuxInDir(_ string, _ config.TerminalConfig, _ string, _ []string) (*LaunchResult, error) {
	return nil, fmt.Errorf("tmux not available on Windows")
}
