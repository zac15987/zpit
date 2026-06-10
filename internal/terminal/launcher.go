package terminal

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/zplex"
)

// LaunchResult contains info about a launched terminal session.
type LaunchResult struct {
	Env            platform.Environment
	Command        string
	Args           []string
	SwitchHint     string
	Warnings       []string // non-fatal warnings (e.g. WT profile resolution failures)
	ZplexSessionID string   // non-empty only on the zplex backend path
}

// SessionMeta carries loop/project metadata passed to the zplex POST body.
type SessionMeta struct {
	ProjectID string
	IssueID   string
}

// zplexClient probes the zplex backend and returns a ready client, or nil.
// It makes AT MOST one HTTP call (Health) and ZERO when port is 0.
// zplex failures are best-effort: callers fall back to wt/tmux on nil.
func zplexClient(cfg config.TerminalConfig) *zplex.Client {
	if cfg.ZplexPort <= 0 {
		return nil
	}
	c := zplex.New(cfg.ZplexPort)
	if c.Health() != nil {
		return nil
	}
	return c
}

// LaunchClaude opens a new terminal window/tab and runs claude in the project directory.
func LaunchClaude(project config.ProjectConfig, cfg config.TerminalConfig, extraArgs ...string) (*LaunchResult, error) {
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)

	if c := zplexClient(cfg); c != nil {
		return launchClaudeZplex(c, cfg, project.Name, projectPath,
			SessionMeta{ProjectID: project.ID}, extraArgs)
	}

	env := platform.Detect()
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
// meta carries project/issue IDs for zplex session metadata; pass SessionMeta{} for non-loop launches.
func LaunchClaudeInDir(workDir, tabTitle string, cfg config.TerminalConfig, meta SessionMeta, extraArgs ...string) (*LaunchResult, error) {
	if c := zplexClient(cfg); c != nil {
		return launchClaudeZplex(c, cfg, tabTitle, workDir, meta, extraArgs)
	}

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

// LaunchLazygit opens lazygit in a new terminal with a custom working directory.
// Does NOT go through the ZPIT_AGENT hook wrapper — lazygit is a plain user tool.
func LaunchLazygit(workDir, tabTitle string, cfg config.TerminalConfig) (*LaunchResult, error) {
	if c := zplexClient(cfg); c != nil {
		return launchLazygitZplex(c, cfg, tabTitle, workDir)
	}

	env := platform.Detect()
	switch env {
	case platform.EnvWindowsTerminal:
		return launchLazygitWindows(tabTitle, cfg, workDir)
	case platform.EnvWSLTmux, platform.EnvLinuxTmux:
		return launchLazygitTmux(tabTitle, cfg, workDir)
	default:
		return nil, fmt.Errorf("unsupported environment: %s", env)
	}
}

// LaunchClaudeUpdate runs `claude update` in a new terminal and keeps the window
// open after the command exits so the user can read the result.
func LaunchClaudeUpdate(cfg config.TerminalConfig) (*LaunchResult, error) {
	if c := zplexClient(cfg); c != nil {
		return launchClaudeUpdateZplex(c, cfg)
	}

	env := platform.Detect()
	switch env {
	case platform.EnvWindowsTerminal:
		return launchClaudeUpdateWindows(cfg)
	case platform.EnvWSLTmux, platform.EnvLinuxTmux:
		return launchClaudeUpdateTmux(cfg)
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

// needsAgentEnv returns true if the agent session requires ZPIT_AGENT=1.
// The efficiency agent works directly in the project directory under user control,
// so hook enforcement is intentionally skipped. The desktop agent runs in $HOME
// without per-project hooks (safety is enforced by the proxy policy instead), so
// it must also skip the wrapper — otherwise wt would invoke a non-existent
// .claude/hooks/zpit-env.{cmd,ps1} and the tab would die before Claude starts.
func needsAgentEnv(extraArgs []string) bool {
	for i, arg := range extraArgs {
		if arg == "--agent" && i+1 < len(extraArgs) {
			role := extraArgs[i+1]
			return role != "efficiency" && role != "desktop"
		}
	}
	return false
}

// getAgentRole extracts the role name from "--agent <role>".
// Returns empty string if not present.
func getAgentRole(extraArgs []string) string {
	for i, arg := range extraArgs {
		if arg == "--agent" && i+1 < len(extraArgs) {
			return extraArgs[i+1]
		}
	}
	return ""
}

// BuildWindowsArgs constructs wt.exe arguments for testing without exec.
// When profile is non-empty, -p "ProfileName" is inserted before the -- separator.
// All launches are wrapped with a shell-aware script so Windows Terminal closes the
// tab on exit (closeOnExit: "graceful" closes on exit 0):
//   - Agent sessions (except efficiency): zpit-env wrapper (sets ZPIT_AGENT=1, exits 0)
//   - Non-agent / efficiency sessions: zpit-exit wrapper (exits 0, no ZPIT_AGENT)
//
// shell should be "cmd", "pwsh", or "powershell" (empty defaults to "cmd").
func BuildWindowsArgs(projectName, projectPath, mode, profile, shell string, extraArgs []string) []string {
	base := buildWindowsBase(mode)
	if profile != "" {
		base = append(base, "-p", profile)
	}
	base = append(base, "-d", projectPath, "--title", projectName, "--")

	if needsAgentEnv(extraArgs) {
		wrapper := buildEnvWrapper(shell)
		role := getAgentRole(extraArgs)
		// Pass role as first arg to the wrapper so it can export ZPIT_AGENT_TYPE.
		withRole := append([]string{role}, buildClaudeArgs(extraArgs)...)
		return append(append(base, wrapper...), withRole...)
	}
	wrapper := buildCleanExitWrapper(shell)
	return append(append(base, wrapper...), buildClaudeArgs(extraArgs)...)
}

// buildCleanExitWrapper returns the command prefix for a clean-exit wrapper
// (no ZPIT_AGENT) based on the detected shell type. This ensures the WT tab
// closes on exit (closeOnExit: "graceful" closes on exit 0).
func buildCleanExitWrapper(shell string) []string {
	return buildShellWrapper(shell, "zpit-exit")
}

// buildEnvWrapper returns the command prefix for the ZPIT_AGENT=1 env wrapper
// based on the detected shell type.
func buildEnvWrapper(shell string) []string {
	return buildShellWrapper(shell, "zpit-env")
}

// buildShellWrapper returns a shell-aware command prefix that invokes the named
// script from .claude/hooks/. cmd shells use .cmd extension; pwsh/powershell use .ps1.
//
// PowerShell exes are resolved to their absolute paths via exec.LookPath. WT's
// packaged-app child processes have shown intermittent failures resolving bare
// "pwsh" against PATH (CreateProcess returns 0x80070002), so we hand WT a fully
// qualified path. If the lookup fails (PowerShell not installed at all), we
// silently fall back to the cmd wrapper.
func buildShellWrapper(shell, scriptBase string) []string {
	switch shell {
	case "pwsh":
		if exe, ok := resolveShellExe("pwsh"); ok {
			return []string{exe, "-NoProfile", "-File", ".claude\\hooks\\" + scriptBase + ".ps1"}
		}
	case "powershell":
		if exe, ok := resolveShellExe("powershell"); ok {
			return []string{exe, "-NoProfile", "-File", ".claude\\hooks\\" + scriptBase + ".ps1"}
		}
	}
	return []string{"cmd", "/c", ".claude\\hooks\\" + scriptBase + ".cmd"}
}

// exePathLookup is the lookup function used by resolveShellExe.
// Tests override this to make resolution deterministic.
var exePathLookup = exec.LookPath

// exePathCache memoizes lookup results across launches.
var exePathCache sync.Map // map[string]resolvedExe

type resolvedExe struct {
	path string
	ok   bool
}

// resolveShellExe returns the absolute path to the named exe, or ("", false)
// if the lookup fails. Results are cached for the process lifetime — if the
// user installs PowerShell after zpit starts, they need to restart to pick it
// up. Acceptable trade-off given how rarely PATH changes mid-session.
func resolveShellExe(name string) (string, bool) {
	if v, loaded := exePathCache.Load(name); loaded {
		r := v.(resolvedExe)
		return r.path, r.ok
	}
	p, err := exePathLookup(name)
	r := resolvedExe{path: p, ok: err == nil}
	exePathCache.Store(name, r)
	return r.path, r.ok
}

// ShellResolutionWarning returns a non-empty warning when the caller requested
// pwsh or powershell but the executable could not be located on PATH, in which
// case buildShellWrapper falls back to the cmd wrapper. Launch sites should
// append the result to LaunchResult.Warnings so the TUI surfaces the silent
// downgrade — otherwise a user with shell="pwsh" configured has no way to know
// their config was ignored.
func ShellResolutionWarning(shell string) string {
	switch shell {
	case "pwsh", "powershell":
		if _, ok := resolveShellExe(shell); !ok {
			return fmt.Sprintf("%s not found on PATH; falling back to cmd shell wrapper", shell)
		}
	}
	return ""
}

// BuildLazygitWindowsArgs constructs wt.exe arguments for lazygit in a new tab/window.
// No wrapper is used — WT's default closeOnExit closes the tab when lazygit exits.
func BuildLazygitWindowsArgs(tabTitle, workDir, mode, profile string) []string {
	base := buildWindowsBase(mode)
	if profile != "" {
		base = append(base, "-p", profile)
	}
	base = append(base, "-d", workDir, "--title", tabTitle, "--", "lazygit")
	return base
}

// BuildLazygitTmuxArgs constructs tmux arguments for lazygit.
func BuildLazygitTmuxArgs(tabTitle, workDir, mode string) []string {
	switch mode {
	case "new_pane":
		return []string{"split-window", "-h", "-c", workDir, "lazygit"}
	default:
		return []string{"new-window", "-n", tabTitle, "-c", workDir, "lazygit"}
	}
}

// BuildClaudeUpdateWindowsArgs constructs wt.exe arguments for `claude update`
// with a trailing `pause` so the tab stays open after the command finishes.
func BuildClaudeUpdateWindowsArgs(mode, profile string) []string {
	base := buildWindowsBase(mode)
	if profile != "" {
		base = append(base, "-p", profile)
	}
	base = append(base, "--title", "claude update", "--",
		"cmd", "/c", "claude update & pause")
	return base
}

// BuildClaudeUpdateTmuxArgs constructs tmux arguments for `claude update`
// with a trailing `read` so the window stays open for the user to read output.
func BuildClaudeUpdateTmuxArgs(mode string) []string {
	cmd := `claude update; read -n1 -r -p "Press any key to close..."`
	switch mode {
	case "new_pane":
		return []string{"split-window", "-h", cmd}
	default:
		return []string{"new-window", "-n", "claude-update", cmd}
	}
}

// buildWindowsBase returns the leading wt.exe args for the requested mode.
func buildWindowsBase(mode string) []string {
	switch mode {
	case "new_window":
		return []string{"-w", "new"}
	default:
		return []string{"new-tab"}
	}
}

// BuildTmuxArgs constructs tmux arguments for testing without exec.
// When extraArgs contains "--agent" (except "efficiency"), ZPIT_AGENT=1 and
// ZPIT_AGENT_TYPE=<role> are prefixed to the command so hook scripts can
// detect agent sessions and apply role-aware enforcement.
func BuildTmuxArgs(projectID, projectPath, mode string, extraArgs []string) []string {
	claudeCmd := strings.Join(buildClaudeArgs(extraArgs), " ")
	if needsAgentEnv(extraArgs) {
		role := getAgentRole(extraArgs)
		claudeCmd = "ZPIT_AGENT=1 ZPIT_AGENT_TYPE=" + role + " " + claudeCmd
	}
	switch mode {
	case "new_pane":
		return []string{"split-window", "-h", "-c", projectPath, claudeCmd}
	default: // "new_window"
		return []string{"new-window", "-n", projectID, "-c", projectPath, claudeCmd}
	}
}
