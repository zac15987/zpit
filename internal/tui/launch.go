package tui

// launch.go — Launch & Deploy: terminal launch commands, slot operations, deploy helpers.
//
// Lock protocol:
//   - Cmd factory methods (launchClaudeCmd, launchClarifierCmd, launchReviewerCmd,
//     launchEfficiencyCmd, deployAndLaunchAgent, deployAndLaunchAgentLite,
//     launchDesktopAgentCmd, openFolderCmd, openTrackerCmd): read-only access to
//     m.state.projects[m.cursor] and read-only config fields — no lock needed,
//     except launchDesktopAgentCmd which acquires RLock for the single-instance guard.
//   - Slot operation methods (launchFocusClaudeCmd, openSlotFolderCmd, openSlotIssueCmd):
//     acquire RLock to read loops/slots, release before I/O or returning cmd.
//   - sortedSlotKeys: caller must hold at least RLock.
//   - Free functions (openInBrowser, deployDocs, injectLangInstruction): stateless, no lock.

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
	"github.com/zac15987/zpit/internal/worktree"
)

// launchableSlotStates defines which slot states allow manual Claude launch.
var launchableSlotStates = map[loop.SlotState]bool{
	loop.SlotCoding:         true,
	loop.SlotReviewing:      true,
	loop.SlotWaitingPRMerge: true,
	loop.SlotAutoMerging:    true,
	loop.SlotNeedsHuman:     true,
	loop.SlotError:          true,
}

// generateAgentName returns a name like "clarifier-a3f7" using 2 random bytes (4 hex chars).
func generateAgentName(prefix string) string {
	b := make([]byte, 2)
	crypto_rand.Read(b)
	return fmt.Sprintf("%s-%04x", prefix, b)
}

// resolveAgentModel picks the --model value for a given agent role.
// Falls back to the Coding model for unknown roles (keeps behavior
// consistent with a generic coding session).
func (m Model) resolveAgentModel(agentName string) string {
	am := m.state.cfg.AgentModels
	switch agentName {
	case "clarifier":
		return am.Clarifier
	case "reviewer":
		return am.Reviewer
	case "efficiency":
		return am.Efficiency
	default:
		return am.Coding
	}
}

func (m Model) launchClaudeCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	cfg := m.state.cfg.Terminal
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	logger := m.state.logger

	// Capture broker info for .mcp.json (read-only after init).
	channelEnabled := project.ChannelEnabled
	channelListen := project.ChannelListen
	var brokerAddr string
	if channelEnabled && m.state.broker != nil {
		brokerAddr = m.state.broker.Addr()
	}
	zpitBin := m.state.cfg.ZpitBin

	return func() tea.Msg {
		// Write .mcp.json for channel communication (Enter launch uses issue_id "0" = lobby).
		if channelEnabled && brokerAddr != "" {
			agentName := generateAgentName("claude")
			if err := writeMCPConfig(projectPath, brokerAddr, project.ID, "0", zpitBin, agentName, "claude", channelListen); err != nil {
				logger.Printf("enter: failed to write .mcp.json for project=%s: %v", project.ID, err)
			} else {
				logger.Printf("enter: wrote .mcp.json to %s for project=%s agent=%s", projectPath, project.ID, agentName)
			}
		}

		var args []string
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaude(project, cfg, args...)
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
}

// launchClarifierCmd opens a new terminal with claude --agent clarifier.
func (m Model) launchClarifierCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	cfg := m.state.cfg.Terminal
	model := m.state.cfg.AgentModels.Clarifier
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	logger := m.state.logger

	// Capture broker info for .mcp.json (read-only after init).
	channelEnabled := project.ChannelEnabled
	channelListen := project.ChannelListen
	var brokerAddr string
	if channelEnabled && m.state.broker != nil {
		brokerAddr = m.state.broker.Addr()
	}
	zpitBin := m.state.cfg.ZpitBin

	return func() tea.Msg {
		// Write .mcp.json for channel communication with a fresh AgentName.
		if channelEnabled && brokerAddr != "" {
			agentName := generateAgentName("clarifier")
			if err := writeMCPConfig(projectPath, brokerAddr, project.ID, "0", zpitBin, agentName, "clarifier", channelListen); err != nil {
				logger.Printf("clarifier: failed to write .mcp.json for project=%s: %v", project.ID, err)
			} else {
				logger.Printf("clarifier: wrote .mcp.json to %s for project=%s agent=%s", projectPath, project.ID, agentName)
			}
		}

		args := []string{"--agent", "clarifier", "--model", model}
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaude(project, cfg, args...)
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
}

// launchReviewerCmd opens a new terminal with claude --agent reviewer.
func (m Model) launchReviewerCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	cfg := m.state.cfg.Terminal
	model := m.state.cfg.AgentModels.Reviewer
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	logger := m.state.logger

	// Capture broker info for .mcp.json (read-only after init).
	channelEnabled := project.ChannelEnabled
	channelListen := project.ChannelListen
	var brokerAddr string
	if channelEnabled && m.state.broker != nil {
		brokerAddr = m.state.broker.Addr()
	}
	zpitBin := m.state.cfg.ZpitBin

	return func() tea.Msg {
		// Write .mcp.json for channel communication with a fresh AgentName.
		if channelEnabled && brokerAddr != "" {
			agentName := generateAgentName("reviewer")
			if err := writeMCPConfig(projectPath, brokerAddr, project.ID, "0", zpitBin, agentName, "reviewer", channelListen); err != nil {
				logger.Printf("reviewer: failed to write .mcp.json for project=%s: %v", project.ID, err)
			} else {
				logger.Printf("reviewer: wrote .mcp.json to %s for project=%s agent=%s", projectPath, project.ID, agentName)
			}
		}

		args := []string{"--agent", "reviewer", "--model", model}
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaude(project, cfg, args...)
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
}

// launchEfficiencyCmd opens a new terminal with claude --agent efficiency.
// Unlike clarifier/reviewer, this does not redeploy — efficiency.md is already present.
func (m Model) launchEfficiencyCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	cfg := m.state.cfg.Terminal
	model := m.state.cfg.AgentModels.Efficiency
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	logger := m.state.logger

	// Capture broker info for .mcp.json (read-only after init).
	channelEnabled := project.ChannelEnabled
	channelListen := project.ChannelListen
	var brokerAddr string
	if channelEnabled && m.state.broker != nil {
		brokerAddr = m.state.broker.Addr()
	}
	zpitBin := m.state.cfg.ZpitBin

	return func() tea.Msg {
		// Write .mcp.json for channel communication with a fresh AgentName.
		if channelEnabled && brokerAddr != "" {
			agentName := generateAgentName("efficiency")
			if err := writeMCPConfig(projectPath, brokerAddr, project.ID, "0", zpitBin, agentName, "efficiency", channelListen); err != nil {
				logger.Printf("efficiency: failed to write .mcp.json for project=%s: %v", project.ID, err)
			} else {
				logger.Printf("efficiency: wrote .mcp.json to %s for project=%s agent=%s", projectPath, project.ID, agentName)
			}
		}

		args := []string{"--agent", "efficiency", "--model", model}
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaude(project, cfg, args...)
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
}

// writeDesktopMCPConfig writes ~/.zpit/.mcp.json for the desktop agent.
// The file registers a single "desktop-proxy" MCP server — no broker, no listen-projects.
// homeDir is the user's home directory; zpitBin is the absolute path to the zpit binary;
// agentName is the generated agent name (e.g. "desktop-a3f7").
func writeDesktopMCPConfig(homeDir, zpitBin, agentName string) error {
	zpitDir := filepath.Join(homeDir, ".zpit")
	if err := os.MkdirAll(zpitDir, 0o755); err != nil {
		return fmt.Errorf("create ~/.zpit dir: %w", err)
	}

	mcpConfig := map[string]any{
		"mcpServers": map[string]any{
			"desktop-proxy": map[string]any{
				"command": zpitBin,
				"args":    []string{"serve-desktop-proxy"},
				"env": map[string]string{
					"ZPIT_AGENT_NAME": agentName,
					"ZPIT_AGENT_TYPE": "desktop",
				},
			},
		},
	}

	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal desktop .mcp.json: %w", err)
	}

	return os.WriteFile(filepath.Join(zpitDir, ".mcp.json"), data, 0o644)
}

// evaluateDesktopLaunchGuards checks preconditions for launching the desktop agent.
// Returns (blockText, false) to block with the given user-facing message, or ("", true)
// to allow the launch to proceed.
// isAlive is called to check if a PID is still running (injectable for testing).
func evaluateDesktopLaunchGuards(state *AppState, isAlive func(int) bool) (string, bool) {
	// AC-11: Linux is not supported.
	if runtime.GOOS == "linux" {
		return locale.T(locale.KeyDesktopLinuxUnsupported), false
	}

	// AC-7: single-instance enforcement.
	state.RLock()
	da := state.activeDesktopAgent
	state.RUnlock()

	if da != nil {
		pid := da.SessionPID
		switch {
		case pid == 0:
			// Launch in flight — handleDesktopAgentLaunched set activeDesktopAgent
			// but the periodic session scan hasn't populated SessionPID yet (takes
			// up to ~5s). Treating this as stale would let a second [w] press race
			// past the lock and spawn a duplicate session.
			return locale.T(locale.KeyDesktopLaunching), false
		case isAlive(pid):
			return fmt.Sprintf(locale.T(locale.KeyDesktopAlreadyRunning), pid), false
		}
		// PID known but the process is gone — stale entry, allow overwrite.
	}

	return "", true
}

// launchDesktopAgentCmd returns a tea.Cmd that launches the desktop-control agent.
// Implements AC-6, AC-7, AC-11.
func (m Model) launchDesktopAgentCmd() tea.Cmd {
	logger := m.state.logger

	// Evaluate launch guards synchronously (before the cmd closure) so the block
	// message reaches the TUI immediately without waiting for async I/O.
	blockText, ok := evaluateDesktopLaunchGuards(m.state, watcher.IsClaudeProcess)
	if !ok {
		logger.Printf("desktop: launch blocked: %s", blockText)
		return func() tea.Msg {
			return DesktopAgentBlockedMsg{Text: blockText}
		}
	}

	agentName := generateAgentName("desktop")
	cfg := m.state.cfg.Terminal
	model := m.state.cfg.AgentModels.Desktop
	zpitBinOverride := m.state.cfg.ZpitBin
	desktopMD := m.state.desktopMD
	exitWrapperCMD := m.state.hookScripts.ExitWrapper
	exitWrapperPS1 := m.state.hookScripts.ExitWrapperPS1

	logger.Printf("desktop: preparing launch agent=%s model=%s", agentName, model)

	return func() tea.Msg {
		// Resolve home directory (cwd for the desktop agent per AC-6).
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return DesktopAgentBlockedMsg{Text: fmt.Sprintf("desktop: cannot resolve home directory: %s", err)}
		}

		// Resolve zpit binary path.
		zpitBin := zpitBinOverride
		if zpitBin == "" {
			zpitBin, err = os.Executable()
			if err != nil {
				return DesktopAgentBlockedMsg{Text: fmt.Sprintf("desktop: cannot resolve zpit binary path: %s", err)}
			}
		}

		// Write ~/.zpit/.mcp.json registering the desktop-proxy MCP server (AC-6).
		if err := writeDesktopMCPConfig(homeDir, zpitBin, agentName); err != nil {
			return DesktopAgentBlockedMsg{Text: fmt.Sprintf("desktop: failed to write .mcp.json: %s", err)}
		}
		logger.Printf("desktop: wrote ~/.zpit/.mcp.json agent=%s", agentName)

		// Deploy agents/desktop.md to ~/.claude/agents/ so Claude Code can invoke it.
		// Only desktop.md is written — no other agents are clobbered.
		if len(desktopMD) > 0 {
			claudeAgentsDir := filepath.Join(homeDir, ".claude", "agents")
			if err := os.MkdirAll(claudeAgentsDir, 0o755); err != nil {
				return DesktopAgentBlockedMsg{Text: fmt.Sprintf("desktop: cannot create ~/.claude/agents/: %s", err)}
			}
			// Desktop agent skips injectLangInstruction: it mirrors the user's input
			// language because it produces no commit/PR/Issue artifacts that would
			// justify the English-only rule.
			processed := injectFrontmatterModel(desktopMD, model)
			destPath := filepath.Join(claudeAgentsDir, "desktop.md")
			if err := os.WriteFile(destPath, processed, 0o644); err != nil {
				return DesktopAgentBlockedMsg{Text: fmt.Sprintf("desktop: failed to deploy desktop.md: %s", err)}
			}
			logger.Printf("desktop: deployed desktop.md to %s", destPath)
		}

		// Deploy zpit-exit.{cmd,ps1} to ~/.claude/hooks/ so the WT clean-exit
		// wrapper (BuildWindowsArgs → buildCleanExitWrapper) resolves. needsAgentEnv
		// excludes "desktop" so the ZPIT_AGENT-injection wrapper is skipped; the
		// clean-exit wrapper is still used by all Windows launches so the WT tab
		// closes gracefully on exit. No safety hooks (path-guard etc.) are deployed —
		// the proxy policy is the safety layer.
		if runtime.GOOS == "windows" && len(exitWrapperCMD) > 0 {
			claudeHooksDir := filepath.Join(homeDir, ".claude", "hooks")
			if err := os.MkdirAll(claudeHooksDir, 0o755); err != nil {
				return DesktopAgentBlockedMsg{Text: fmt.Sprintf("desktop: cannot create ~/.claude/hooks/: %s", err)}
			}
			if err := os.WriteFile(filepath.Join(claudeHooksDir, "zpit-exit.cmd"), exitWrapperCMD, 0o644); err != nil {
				return DesktopAgentBlockedMsg{Text: fmt.Sprintf("desktop: failed to deploy zpit-exit.cmd: %s", err)}
			}
			if len(exitWrapperPS1) > 0 {
				if err := os.WriteFile(filepath.Join(claudeHooksDir, "zpit-exit.ps1"), exitWrapperPS1, 0o644); err != nil {
					return DesktopAgentBlockedMsg{Text: fmt.Sprintf("desktop: failed to deploy zpit-exit.ps1: %s", err)}
				}
			}
			logger.Printf("desktop: deployed zpit-exit wrappers to %s", claudeHooksDir)
		}

		// Pass the .mcp.json explicitly — Claude Code resolves bare .mcp.json against
		// the cwd ($HOME), but we keep ours under ~/.zpit/ to avoid colonizing $HOME.
		mcpConfigPath := filepath.Join(homeDir, ".zpit", ".mcp.json")

		tabTitle := "Desktop Agent"
		args := []string{
			"--agent", "desktop",
			"--model", model,
			"--mcp-config", mcpConfigPath,
			"--allowedTools", "Read,Bash,Glob,Grep,mcp__desktop-proxy__*",
		}
		result, launchErr := terminal.LaunchClaudeInDir(homeDir, tabTitle, cfg, terminal.SessionMeta{}, args...)
		return DesktopAgentLaunchedMsg{
			AgentName: agentName,
			HomeDir:   homeDir,
			Result:    result,
			Err:       launchErr,
		}
	}
}

// handleDesktopAgentLaunched stores the new active desktop agent in AppState.
// Acquires the write lock; releases before returning.
func (m Model) handleDesktopAgentLaunched(msg DesktopAgentLaunchedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.state.logger.Printf("desktop: launch failed agent=%s err=%v", msg.AgentName, msg.Err)
		m.setStatus(fmt.Sprintf("Desktop agent launch failed: %s", msg.Err))
		return m, nil
	}

	trackingKey := "desktop:" + msg.AgentName

	m.state.Lock()
	at := &ActiveTerminal{
		LaunchResult:   msg.Result,
		WorkDir:        msg.HomeDir, // periodic session scan keys off WorkDir to find the spawned claude PID
		State:          watcher.StateUnknown,
		StateChangedAt: time.Now(),
	}
	if msg.Result != nil {
		at.ZplexSessionID = msg.Result.ZplexSessionID
	}
	m.state.activeTerminals[trackingKey] = at
	m.state.activeDesktopAgent = at
	m.state.NotifyAll()
	m.state.Unlock()

	if at.ZplexSessionID != "" {
		m.state.logger.Printf("zplex launch: key=%s role=%s session=%s", trackingKey, "desktop", at.ZplexSessionID)
	}
	m.state.logger.Printf("desktop: launched agent=%s (PID pending session discovery)", msg.AgentName)
	// Kick off active session discovery (same flow as project-scope launches).
	// Without this, a terminal closed before the 10s periodic scan would leave
	// the entry stuck with SessionPID=0 — neither the liveness sweep nor the
	// activeDesktopAgent clear-block can recover an entry that never reached
	// StateEnded.
	return m, m.startWatcherDirCmd(trackingKey, msg.HomeDir)
}

// handleDesktopAgentBlocked surfaces the block reason via the status bar.
func (m Model) handleDesktopAgentBlocked(msg DesktopAgentBlockedMsg) (tea.Model, tea.Cmd) {
	m.setStatus(msg.Text)
	return m, nil
}

// handleDesktopAgentExited is dispatched by the liveness check when the
// active desktop agent PID dies. AppState.activeDesktopAgent has already
// been cleared by the liveness check before this message reaches here.
// The cleanup is visible to the user via the entry disappearing from
// Active Terminals and [w] becoming available again — no toast needed,
// matching how project-scope agents announce their exit (logger only).
func (m Model) handleDesktopAgentExited(_ DesktopAgentExitedMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

// deployAndLaunchAgent deploys the named agent to the project and launches it.
// If the broker is available (channel_enabled), writes .mcp.json to the project root
// with ZPIT_ISSUE_ID = "0" (lobby) so the manual agent can use channel communication.
func (m Model) deployAndLaunchAgent(agentName string, agentMD []byte) tea.Cmd {
	project := m.state.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	cfg := m.state.cfg.Terminal
	model := m.resolveAgentModel(agentName)
	logger := m.state.logger

	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD
	hookScripts := m.state.hookScripts
	var trackerDocContent string
	if provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]; ok {
		trackerDocContent = tracker.BuildTrackerDoc(provider.Type, provider.URL, project.Repo, provider.TokenEnv, project.BaseBranch)
	}
	// Capture broker info for .mcp.json (read-only after init).
	channelEnabled := project.ChannelEnabled
	channelListen := project.ChannelListen
	var brokerAddr string
	if channelEnabled && m.state.broker != nil {
		brokerAddr = m.state.broker.Addr()
	}
	zpitBin := m.state.cfg.ZpitBin

	return func() tea.Msg {
		// Deploy hooks + gitignore
		worktree.EnsureGitignore(projectPath)
		if err := worktree.DeployHooksToProject(projectPath, hookScripts); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Hook deploy failed: %s", err)}
		}

		// Deploy agent
		agentDir := filepath.Join(projectPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		if err := os.WriteFile(filepath.Join(agentDir, agentName+".md"), agentMD, 0o644); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		deployDocs(projectPath, trackerDocContent, agentGuidelines, codeConstructionPrinciples)

		// Write .mcp.json for channel communication (manual agent uses issue_id "0" = lobby).
		if channelEnabled && brokerAddr != "" {
			channelAgentName := generateAgentName(agentName)
			if err := writeMCPConfig(projectPath, brokerAddr, project.ID, "0", zpitBin, channelAgentName, agentName, channelListen); err != nil {
				logger.Printf("%s: failed to write .mcp.json for project=%s: %v", agentName, project.ID, err)
			} else {
				logger.Printf("%s: wrote .mcp.json to %s for project=%s agent=%s", agentName, projectPath, project.ID, channelAgentName)
			}
		}

		// Launch
		args := []string{"--agent", agentName, "--model", model}
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaude(project, cfg, args...)
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
}

// deployAndLaunchAgentLite deploys the efficiency agent with minimal setup and launches it.
// Unlike deployAndLaunchAgent, this function:
//   - Does NOT deploy hooks (no path-guard, bash-firewall, git-guard)
//   - Does NOT set ZPIT_AGENT=1 environment variable
//   - Deploys: gitignore, gitattributes, agent MD, docs (including tracker.md)
//   - Writes .mcp.json if channel_enabled (with agent_type=efficiency)
func (m Model) deployAndLaunchAgentLite() tea.Cmd {
	project := m.state.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	cfg := m.state.cfg.Terminal
	model := m.state.cfg.AgentModels.Efficiency
	logger := m.state.logger

	agentMD := injectLangInstruction(m.state.efficiencyMD)
	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD
	var trackerDocContent string
	if provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]; ok {
		trackerDocContent = tracker.BuildTrackerDoc(provider.Type, provider.URL, project.Repo, provider.TokenEnv, project.BaseBranch)
	}
	// Capture broker info for .mcp.json (read-only after init).
	channelEnabled := project.ChannelEnabled
	channelListen := project.ChannelListen
	var brokerAddr string
	if channelEnabled && m.state.broker != nil {
		brokerAddr = m.state.broker.Addr()
	}
	zpitBin := m.state.cfg.ZpitBin

	return func() tea.Msg {
		// (a) Ensure gitignore
		worktree.EnsureGitignore(projectPath)

		// (b) Deploy agent — NO hooks deployed
		agentDir := filepath.Join(projectPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		if err := os.WriteFile(filepath.Join(agentDir, "efficiency.md"), agentMD, 0o644); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}

		// (c) Deploy docs (including tracker.md)
		deployDocs(projectPath, trackerDocContent, agentGuidelines, codeConstructionPrinciples)

		// (d) Write .mcp.json if channel_enabled (agent_type=efficiency)
		if channelEnabled && brokerAddr != "" {
			channelAgentName := generateAgentName("efficiency")
			if err := writeMCPConfig(projectPath, brokerAddr, project.ID, "0", zpitBin, channelAgentName, "efficiency", channelListen); err != nil {
				logger.Printf("efficiency: failed to write .mcp.json for project=%s: %v", project.ID, err)
			} else {
				logger.Printf("efficiency: wrote .mcp.json to %s for project=%s agent=%s", projectPath, project.ID, channelAgentName)
			}
		}

		// (e) NO DeployHooksToProject call
		// (f) NO ZPIT_AGENT=1 — launcher skips env injection for "efficiency" agent
		//     (see needsAgentEnv in terminal/launcher.go)

		// (g) Launch with --agent efficiency
		args := []string{"--agent", "efficiency", "--model", model}
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaude(project, cfg, args...)
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
}

// deployAllCmd wipes any existing Zpit deployment from the project and writes a
// fresh copy of every agent, hook, and doc. Does NOT launch Claude Code and does
// NOT write .mcp.json (that requires agent-name/issue-id decided at launch time).
// The set of files written here must stay in sync with the deployedFiles list in
// view_projects.go used by deployStatus for the list indicator.
func (m Model) deployAllCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	projectName := project.Name
	logger := m.state.logger

	clarifierMD := injectClarifierLangInstruction(m.state.clarifierMD)
	reviewerMD := injectLangInstruction(m.state.reviewerMD)
	taskRunnerMD := m.state.taskRunnerMD
	efficiencyMD := injectLangInstruction(m.state.efficiencyMD)
	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD
	hookScripts := m.state.hookScripts
	var trackerDocContent string
	if provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]; ok {
		trackerDocContent = tracker.BuildTrackerDoc(provider.Type, provider.URL, project.Repo, provider.TokenEnv, project.BaseBranch)
	}

	return func() tea.Msg {
		removed := undeployFiles(projectPath)
		logger.Printf("[redeploy] %s: cleared %d prior item(s)", projectName, removed)

		worktree.EnsureGitignore(projectPath)

		if err := worktree.DeployHooksToProject(projectPath, hookScripts); err != nil {
			logger.Printf("[redeploy] %s: hook deploy failed: %v", projectName, err)
			return StatusMsg{Text: fmt.Sprintf("Redeploy failed: %s", err)}
		}

		agentDir := filepath.Join(projectPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			logger.Printf("[redeploy] %s: mkdir agents failed: %v", projectName, err)
			return StatusMsg{Text: fmt.Sprintf("Redeploy failed: %s", err)}
		}
		agents := map[string][]byte{
			"clarifier.md":   clarifierMD,
			"reviewer.md":    reviewerMD,
			"task-runner.md": taskRunnerMD,
			"efficiency.md":  efficiencyMD,
		}
		for name, content := range agents {
			if err := os.WriteFile(filepath.Join(agentDir, name), content, 0o644); err != nil {
				logger.Printf("[redeploy] %s: write %s failed: %v", projectName, name, err)
				return StatusMsg{Text: fmt.Sprintf("Redeploy failed: %s", err)}
			}
		}

		deployDocs(projectPath, trackerDocContent, agentGuidelines, codeConstructionPrinciples)

		logger.Printf("[redeploy] %s: wrote %d agent(s), hooks, docs to %s", projectName, len(agents), projectPath)
		return StatusMsg{Text: fmt.Sprintf(locale.T(locale.KeyRedeployDone), projectName)}
	}
}

func (m Model) launchFocusClaudeCmd(slotKey string) (tea.Model, tea.Cmd) {
	m.state.RLock()
	ls, ok := m.state.loops[m.focusProjectID]
	if !ok {
		m.state.RUnlock()
		return m, nil
	}
	slot, ok := ls.Slots[slotKey]
	if !ok || slot.WorktreePath == "" {
		m.state.RUnlock()
		m.setStatus(locale.T(locale.KeyNoWorktreePath))
		return m, nil
	}
	if !launchableSlotStates[slot.State] {
		m.state.RUnlock()
		m.setStatus(locale.T(locale.KeyCannotLaunch))
		return m, nil
	}
	wtPath := slot.WorktreePath
	issueID := slot.IssueID
	branchName := slot.BranchName
	// Find project config to check channel_enabled (within existing RLock).
	var channelEnabled bool
	for i := range m.state.projects {
		if m.state.projects[i].ID == m.focusProjectID {
			channelEnabled = m.state.projects[i].ChannelEnabled
			break
		}
	}
	m.state.RUnlock()

	if _, err := os.Stat(wtPath); err != nil {
		m.state.logger.Printf("launch check failed [focus] worktree path missing: %s", wtPath)
		m.showErrorOverlay([]string{locale.T(locale.KeyErrWorktreeMissing)})
		return m, nil
	}

	cfg := m.state.cfg.Terminal
	tabTitle := fmt.Sprintf("Focus #%s", issueID)
	trackingKey := "focus:" + m.focusProjectID + ":" + issueID
	focusProjectID := m.focusProjectID

	return m, func() tea.Msg {
		var args []string
		if channelEnabled {
			args = append(args, "--channel-enabled")
		}
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg,
			terminal.SessionMeta{ProjectID: focusProjectID, IssueID: issueID}, args...)
		msg := LaunchResultMsg{
			ProjectID:      focusProjectID,
			TrackingKey:    trackingKey,
			WorkDir:        wtPath,
			WorktreeBranch: branchName,
			Result:         result,
			Err:            err,
		}
		if result != nil {
			msg.ZplexSessionID = result.ZplexSessionID
		}
		return msg
	}
}

func (m Model) openFolderCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	path := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	return func() tea.Msg {
		var cmd *exec.Cmd
		if platform.IsWindows() {
			cmd = exec.Command("explorer", strings.ReplaceAll(path, "/", `\`))
		} else {
			cmd = exec.Command("xdg-open", path)
		}
		if err := cmd.Start(); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Failed to open: %s", err)}
		}
		return StatusMsg{Text: fmt.Sprintf("Opened %s", path)}
	}
}

// openSlotFolderCmd opens the selected slot's worktree folder in the file manager.
func (m Model) openSlotFolderCmd(slotKey string) (tea.Model, tea.Cmd) {
	m.state.RLock()
	ls, ok := m.state.loops[m.focusProjectID]
	if !ok {
		m.state.RUnlock()
		return m, nil
	}
	slot, ok := ls.Slots[slotKey]
	if !ok || slot.WorktreePath == "" {
		m.state.RUnlock()
		m.setStatus(locale.T(locale.KeyNoWorktreePath))
		return m, nil
	}
	path := slot.WorktreePath
	m.state.RUnlock()

	if _, err := os.Stat(path); err != nil {
		m.showErrorOverlay([]string{locale.T(locale.KeyErrWorktreeMissing)})
		return m, nil
	}
	return m, func() tea.Msg {
		var cmd *exec.Cmd
		if platform.IsWindows() {
			cmd = exec.Command("explorer", strings.ReplaceAll(path, "/", `\`))
		} else {
			cmd = exec.Command("xdg-open", path)
		}
		if err := cmd.Start(); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Failed to open: %s", err)}
		}
		return StatusMsg{Text: fmt.Sprintf("Opened %s", path)}
	}
}

// openSlotIssueCmd opens the selected slot's issue page in the browser.
func (m Model) openSlotIssueCmd(slotKey string) (tea.Model, tea.Cmd) {
	m.state.RLock()
	ls, ok := m.state.loops[m.focusProjectID]
	if !ok {
		m.state.RUnlock()
		return m, nil
	}
	slot, ok := ls.Slots[slotKey]
	if !ok {
		m.state.RUnlock()
		return m, nil
	}
	issueID := slot.IssueID
	m.state.RUnlock()

	project := m.findProject(m.focusProjectID)
	if project == nil {
		return m, nil
	}
	provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]
	if !ok {
		m.setStatus(locale.T(locale.KeyNoTrackerConfigured))
		return m, nil
	}
	url := tracker.BuildIssueURL(provider, project.Repo, issueID)
	if url == "" {
		m.setStatus(fmt.Sprintf("Cannot build URL for tracker type: %s", provider.Type))
		return m, nil
	}
	return m, openInBrowser(url)
}

// sortedSlotKeys returns sorted slot keys for the given project.
// Caller must hold at least a read lock on state, or call from a context
// where mutable state is not concurrently modified.
func (m Model) sortedSlotKeys(projectID string) []string {
	ls, ok := m.state.loops[projectID]
	if !ok || len(ls.Slots) == 0 {
		return nil
	}
	keys := make([]string, 0, len(ls.Slots))
	for k := range ls.Slots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// openTrackerCmd opens the project's issue tracker in the browser.
func (m Model) openTrackerCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]
	if !ok {
		return func() tea.Msg {
			return StatusMsg{Text: locale.T(locale.KeyNoTrackerConfigured)}
		}
	}
	url := tracker.BuildTrackerURL(provider, project.Repo)
	if url == "" {
		return func() tea.Msg {
			return StatusMsg{Text: fmt.Sprintf("Unknown tracker type: %s", provider.Type)}
		}
	}
	return openInBrowser(url)
}

// openPRCmd opens the project's PR list page in the browser.
func (m Model) openPRCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]
	if !ok {
		return func() tea.Msg {
			return StatusMsg{Text: locale.T(locale.KeyNoTrackerConfigured)}
		}
	}
	url := tracker.BuildPRListURL(provider, project.Repo)
	if url == "" {
		return func() tea.Msg {
			return StatusMsg{Text: fmt.Sprintf("Unknown tracker type: %s", provider.Type)}
		}
	}
	return openInBrowser(url)
}

// openSlotPRCmd opens the slot's PR in the browser. If FindPRByBranch locates a
// specific PR, its direct URL is used; otherwise falls back to a filtered PR list
// URL (?head=<branch>).
func (m Model) openSlotPRCmd(slotKey string) (tea.Model, tea.Cmd) {
	m.state.RLock()
	ls, ok := m.state.loops[m.focusProjectID]
	if !ok {
		m.state.RUnlock()
		return m, nil
	}
	slot, ok := ls.Slots[slotKey]
	if !ok {
		m.state.RUnlock()
		return m, nil
	}
	branch := slot.BranchName
	m.state.RUnlock()

	project := m.findProject(m.focusProjectID)
	if project == nil {
		return m, nil
	}
	provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]
	if !ok {
		m.setStatus(locale.T(locale.KeyNoTrackerConfigured))
		return m, nil
	}
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		m.setStatus(locale.T(locale.KeyNoTrackerConfigured))
		return m, nil
	}
	fallback := tracker.BuildPRFilterURL(provider, project.Repo, branch)
	repo := project.Repo

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pr, err := client.FindPRByBranch(ctx, repo, branch)
		if err == nil && pr != nil && pr.URL != "" {
			return openInBrowser(pr.URL)()
		}
		if fallback == "" {
			return StatusMsg{Text: fmt.Sprintf("Cannot build PR URL for tracker type: %s", provider.Type)}
		}
		return openInBrowser(fallback)()
	}
}

// runLazygitCmd returns a tea.Cmd that spawns lazygit in a new terminal.
func runLazygitCmd(workDir, title string, cfg config.TerminalConfig) tea.Cmd {
	return func() tea.Msg {
		result, err := terminal.LaunchLazygit(workDir, title, cfg)
		if err != nil {
			return StatusMsg{Text: fmt.Sprintf("lazygit launch failed: %s", err)}
		}
		if result != nil && result.Env == platform.EnvZplex {
			return StatusMsg{Text: locale.T(locale.KeyZplexLaunched)}
		}
		return StatusMsg{Text: fmt.Sprintf("Opened lazygit in %s", workDir)}
	}
}

// launchLazygitCmd opens lazygit in a new terminal at the selected project root.
func (m Model) launchLazygitCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	workDir := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	title := fmt.Sprintf("lazygit — %s", project.Name)
	return runLazygitCmd(workDir, title, m.state.cfg.Terminal)
}

// launchSlotLazygitCmd opens lazygit in a new terminal at the slot's worktree.
func (m Model) launchSlotLazygitCmd(slotKey string) (tea.Model, tea.Cmd) {
	m.state.RLock()
	ls, ok := m.state.loops[m.focusProjectID]
	if !ok {
		m.state.RUnlock()
		return m, nil
	}
	slot, ok := ls.Slots[slotKey]
	if !ok || slot.WorktreePath == "" {
		m.state.RUnlock()
		m.setStatus(locale.T(locale.KeyNoWorktreePath))
		return m, nil
	}
	workDir := slot.WorktreePath
	issueID := slot.IssueID
	m.state.RUnlock()

	if _, err := os.Stat(workDir); err != nil {
		m.showErrorOverlay([]string{locale.T(locale.KeyErrWorktreeMissing)})
		return m, nil
	}
	title := fmt.Sprintf("lazygit — #%s", issueID)
	return m, runLazygitCmd(workDir, title, m.state.cfg.Terminal)
}

// launchClaudeUpdateCmd runs `claude update` in a new terminal. The terminal
// stays open after the command finishes so the user can read the result.
func (m Model) launchClaudeUpdateCmd() tea.Cmd {
	cfg := m.state.cfg.Terminal
	return func() tea.Msg {
		result, err := terminal.LaunchClaudeUpdate(cfg)
		if err != nil {
			return StatusMsg{Text: fmt.Sprintf("claude update launch failed: %s", err)}
		}
		if result != nil && result.Env == platform.EnvZplex {
			return StatusMsg{Text: locale.T(locale.KeyZplexLaunched)}
		}
		return StatusMsg{Text: "Launched claude update"}
	}
}

// openInBrowser opens a URL in the default browser.
func openInBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		if platform.IsWindows() {
			cmd = exec.Command("cmd", "/c", "start", url)
		} else {
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Failed to open: %s", err)}
		}
		return StatusMsg{Text: fmt.Sprintf("Opened %s", url)}
	}
}

// deployDocs writes tracker.md (if content non-empty), agent-guidelines.md, and
// code-construction-principles.md to .claude/docs/.
func deployDocs(targetPath, trackerDocContent string, agentGuidelines, codeConstructionPrinciples []byte) {
	docsDir := filepath.Join(targetPath, ".claude", "docs")
	_ = os.MkdirAll(docsDir, 0o755)
	if trackerDocContent != "" {
		_ = os.WriteFile(filepath.Join(docsDir, "tracker.md"), []byte(trackerDocContent), 0o644)
	}
	_ = os.WriteFile(filepath.Join(docsDir, "agent-guidelines.md"), agentGuidelines, 0o644)
	_ = os.WriteFile(filepath.Join(docsDir, "code-construction-principles.md"), codeConstructionPrinciples, 0o644)
}

// injectLangInstruction prepends the strict English language rule after the
// YAML frontmatter. Used for coding / reviewer / efficiency / task-runner —
// every agent whose artifacts must stay in English.
func injectLangInstruction(md []byte) []byte {
	return injectLangInstructionWith(md, locale.ResponseInstruction())
}

// injectClarifierLangInstruction prepends the clarifier-specific language rule.
// The clarifier may converse in the configured locale, but its Issue Spec
// artifacts and tracker labels still must be English.
func injectClarifierLangInstruction(md []byte) []byte {
	return injectLangInstructionWith(md, locale.ClarifierResponseInstruction())
}

// injectLangInstructionWith inserts the given instruction string after the
// YAML frontmatter block (the second `---` delimiter). Returns the input
// unchanged if the instruction is empty or the frontmatter is malformed.
func injectLangInstructionWith(md []byte, instruction string) []byte {
	if instruction == "" {
		return md
	}
	// Normalize CRLF → LF for reliable marker search, then restore original line endings.
	s := string(md)
	hasCRLF := strings.Contains(s, "\r\n")
	normalized := strings.ReplaceAll(s, "\r\n", "\n")

	const marker = "---\n"
	first := strings.Index(normalized, marker)
	if first < 0 {
		return md // no frontmatter found — return unchanged
	}
	second := strings.Index(normalized[first+len(marker):], marker)
	if second < 0 {
		return md // malformed frontmatter — return unchanged
	}
	insertPos := first + len(marker) + second + len(marker)
	result := normalized[:insertPos] + "\n" + instruction + normalized[insertPos:]
	if hasCRLF {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}
	return []byte(result)
}

// injectFrontmatterModel inserts or overwrites `model: <value>` in the YAML
// frontmatter block. Used to wire cfg.AgentModels.TaskRunner into
// task-runner.md at deploy time so subagents can use a different model from
// the orchestrator (Claude Code's Agent tool reads the frontmatter's model
// field as the subagent default).
//
// Behavior:
//   - Empty model → return md unchanged (let Claude Code inherit from parent).
//   - Existing `model:` line → overwrite the value.
//   - No `model:` line → insert before the closing `---`.
//   - Malformed/missing frontmatter → return md unchanged.
func injectFrontmatterModel(md []byte, model string) []byte {
	if model == "" {
		return md
	}
	s := string(md)
	hasCRLF := strings.Contains(s, "\r\n")
	normalized := strings.ReplaceAll(s, "\r\n", "\n")

	const marker = "---\n"
	first := strings.Index(normalized, marker)
	if first < 0 {
		return md
	}
	blockStart := first + len(marker)
	secondRel := strings.Index(normalized[blockStart:], marker)
	if secondRel < 0 {
		return md
	}
	blockEnd := blockStart + secondRel // index of closing `---\n`

	block := normalized[blockStart:blockEnd]
	modelLine := "model: " + model + "\n"

	// Overwrite existing model line if present.
	lines := strings.Split(block, "\n")
	replaced := false
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "model:") {
			lines[i] = "model: " + model
			replaced = true
			break
		}
	}
	var newBlock string
	if replaced {
		newBlock = strings.Join(lines, "\n")
	} else {
		newBlock = block + modelLine
	}

	result := normalized[:blockStart] + newBlock + normalized[blockEnd:]
	if hasCRLF {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}
	return []byte(result)
}

// --- Focus panel: loop slot selection ---

func (m Model) handleFocusSwitch() (tea.Model, tea.Cmd) {
	// Determine which panels are available.
	m.state.RLock()
	hasTerminals := len(m.state.activeTerminals) > 0
	m.state.RUnlock()

	project := m.state.projects[m.cursor]
	m.state.RLock()
	slotKeys := m.sortedSlotKeys(project.ID)
	m.state.RUnlock()
	hasSlots := len(slotKeys) > 0

	// Build ordered list of available panels: Projects -> Terminals -> LoopSlots.
	panels := []FocusedPanel{FocusProjects}
	if hasTerminals {
		panels = append(panels, FocusTerminals)
	}
	if hasSlots {
		panels = append(panels, FocusLoopSlots)
	}

	// If only Projects panel is available, do nothing.
	if len(panels) <= 1 {
		return m, nil
	}

	// Find current panel index and advance to next.
	current := 0
	for i, p := range panels {
		if p == m.focusedPanel {
			current = i
			break
		}
	}
	next := panels[(current+1)%len(panels)]

	m.focusedPanel = next
	switch next {
	case FocusTerminals:
		m.termCursor = 0
		m.terminalsVP.GotoTop()
	case FocusLoopSlots:
		m.focusProjectID = project.ID
		m.loopCursor = 0
		m.loopVP.GotoTop()
	case FocusProjects:
		m.projectsVP.GotoTop()
	}
	return m, nil
}

// --- Focus panel: terminal selection ---

func (m Model) handleTerminalsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	termKeys := m.sortedTerminalKeys()
	if len(termKeys) == 0 {
		m.focusedPanel = FocusProjects
		return m, nil
	}
	if m.termCursor >= len(termKeys) {
		m.termCursor = len(termKeys) - 1
	}

	switch {
	case key.Matches(msg, m.keys.Back):
		m.focusedPanel = FocusProjects
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.termCursor > 0 {
			m.termCursor--
		}
		if m.termCursor >= 0 && m.termCursor < len(m.termLineStarts) {
			m.ensureCursorInPanel(&m.terminalsVP, m.termLineStarts[m.termCursor])
		}

	case key.Matches(msg, m.keys.Down):
		if m.termCursor < len(termKeys)-1 {
			m.termCursor++
		}
		if m.termCursor >= 0 && m.termCursor < len(m.termLineStarts) {
			m.ensureCursorInPanel(&m.terminalsVP, m.termLineStarts[m.termCursor])
		}

	case key.Matches(msg, m.keys.PageUp):
		m.terminalsVP.PageUp()

	case key.Matches(msg, m.keys.PageDown):
		m.terminalsVP.PageDown()

	case key.Matches(msg, m.keys.Kill):
		trackingKey := termKeys[m.termCursor]
		m.state.RLock()
		at, ok := m.state.activeTerminals[trackingKey]
		if !ok {
			m.state.RUnlock()
			return m, nil
		}
		pid := at.SessionPID
		state := at.State
		displayName := m.projectName(trackingKey)
		m.state.RUnlock()

		if pid == 0 {
			m.setStatus(locale.T(locale.KeyTerminalNoPID))
			return m, nil
		}
		if state == watcher.StateEnded {
			m.setStatus(locale.T(locale.KeyTerminalAlreadyEnded))
			return m, nil
		}
		m.showKillTerminalConfirm(trackingKey, displayName, pid)
		return m, m.initConfirmForm()
	}

	return m, nil
}

// sortedTerminalKeys returns sorted activeTerminals keys.
// Caller must NOT hold any lock (acquires RLock internally).
func (m Model) sortedTerminalKeys() []string {
	m.state.RLock()
	defer m.state.RUnlock()
	return m.sortedTerminalKeysLocked()
}

// sortedTerminalKeysLocked returns sorted activeTerminals keys.
// Caller must already hold at least RLock.
func (m Model) sortedTerminalKeysLocked() []string {
	keys := make([]string, 0, len(m.state.activeTerminals))
	for k := range m.state.activeTerminals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (m Model) handleLoopSlotsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.state.RLock()
	keys := m.sortedSlotKeys(m.focusProjectID)
	m.state.RUnlock()
	if len(keys) == 0 {
		m.focusedPanel = FocusProjects
		return m, nil
	}
	if m.loopCursor >= len(keys) {
		m.loopCursor = len(keys) - 1
	}

	switch {
	case key.Matches(msg, m.keys.Back):
		m.focusedPanel = FocusProjects
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.loopCursor > 0 {
			m.loopCursor--
		}
		if m.loopCursor >= 0 && m.loopCursor < len(m.loopLineStarts) {
			m.ensureCursorInPanel(&m.loopVP, m.loopLineStarts[m.loopCursor])
		}

	case key.Matches(msg, m.keys.Down):
		if m.loopCursor < len(keys)-1 {
			m.loopCursor++
		}
		if m.loopCursor >= 0 && m.loopCursor < len(m.loopLineStarts) {
			m.ensureCursorInPanel(&m.loopVP, m.loopLineStarts[m.loopCursor])
		}

	case key.Matches(msg, m.keys.PageUp):
		m.loopVP.PageUp()

	case key.Matches(msg, m.keys.PageDown):
		m.loopVP.PageDown()

	case key.Matches(msg, m.keys.Enter):
		return m.launchFocusClaudeCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.Open):
		return m.openSlotFolderCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.Tracker):
		return m.openSlotIssueCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.OpenPR):
		return m.openSlotPRCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.Lazygit):
		return m.launchSlotLazygitCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.ClaudeUpdate):
		return m, m.launchClaudeUpdateCmd()
	}

	return m, nil
}
