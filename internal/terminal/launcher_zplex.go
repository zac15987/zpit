// Package terminal — zplex backend launch functions.
// No build tags: pure HTTP, compiles on all OSes.
package terminal

import (
	"fmt"
	"os"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/zplex"
)

// launchClaudeZplex creates a claude session on the zplex daemon.
//
// The `env` field in the POST body is set to append(os.Environ(), "ZPIT_AGENT=1",
// "ZPIT_AGENT_TYPE=<role>") if and only if needsAgentEnv(extraArgs) == true.
// For all other sessions the env field is omitted (nil).
// The zpit-env / zpit-exit wrapper scripts are NOT referenced here.
func launchClaudeZplex(c *zplex.Client, cfg config.TerminalConfig, title, cwd string, meta SessionMeta, extraArgs []string) (*LaunchResult, error) {
	full := buildClaudeArgs(extraArgs) // ["claude", ...]; honors --channel-enabled rewrite
	shell := full[0]
	args := full[1:]

	role := getAgentRole(extraArgs)
	agentState := ""
	if role != "" {
		agentState = "active"
	}

	var env []string
	if needsAgentEnv(extraArgs) {
		env = append(os.Environ(), "ZPIT_AGENT=1", "ZPIT_AGENT_TYPE="+role)
	}

	req := zplex.CreateSessionRequest{
		Shell:      shell,
		Title:      title,
		Args:       args,
		Cwd:        cwd,
		Env:        env,
		Source:     "zpit",
		ProjectID:  meta.ProjectID,
		IssueID:    meta.IssueID,
		Role:       role,
		AgentState: agentState,
	}

	id, err := c.CreateSession(req)
	if err != nil {
		return nil, fmt.Errorf("zplex create session: %w", err)
	}

	return &LaunchResult{
		Env:            platform.EnvZplex,
		Command:        shell,
		Args:           args,
		ZplexSessionID: id,
		SwitchHint:     title,
	}, nil
}

// launchLazygitZplex creates a lazygit session on the zplex daemon.
// No env, no role/agent_state per AC-11.
func launchLazygitZplex(c *zplex.Client, _ config.TerminalConfig, title, workDir string) (*LaunchResult, error) {
	req := zplex.CreateSessionRequest{
		Shell:  "lazygit",
		Title:  title,
		Cwd:    workDir,
		Source: "zpit",
	}

	id, err := c.CreateSession(req)
	if err != nil {
		return nil, fmt.Errorf("zplex create session: %w", err)
	}

	return &LaunchResult{
		Env:            platform.EnvZplex,
		Command:        "lazygit",
		ZplexSessionID: id,
		SwitchHint:     title,
	}, nil
}

// launchClaudeUpdateZplex creates a claude update session on the zplex daemon.
// Uses the same command string as the wt/tmux variants per GOOS so the panel
// persists until keypress, then closes by process exit.
// No env, no role/agent_state per AC-11.
func launchClaudeUpdateZplex(c *zplex.Client, _ config.TerminalConfig) (*LaunchResult, error) {
	var shell string
	var args []string

	if platform.IsWindows() {
		shell = "cmd"
		args = []string{"/c", "claude update & pause"}
	} else {
		shell = "sh"
		args = []string{"-c", `claude update; read -n1 -r -p "Press any key to close..."`}
	}

	req := zplex.CreateSessionRequest{
		Shell:  shell,
		Title:  "claude update",
		Args:   args,
		Source: "zpit",
	}

	id, err := c.CreateSession(req)
	if err != nil {
		return nil, fmt.Errorf("zplex create session: %w", err)
	}

	return &LaunchResult{
		Env:            platform.EnvZplex,
		Command:        shell,
		Args:           args,
		ZplexSessionID: id,
		SwitchHint:     "claude update",
	}, nil
}
