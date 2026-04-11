package tui

// launch.go — Launch & Deploy: terminal launch commands, slot operations, deploy helpers.
//
// Lock protocol:
//   - Cmd factory methods (launchClaudeCmd, launchClarifierCmd, launchReviewerCmd,
//     launchEfficiencyCmd, deployAndLaunchAgent, deployAndLaunchAgentLite,
//     openFolderCmd, openTrackerCmd): read-only access to
//     m.state.projects[m.cursor] and read-only config fields — no lock needed.
//   - Slot operation methods (launchFocusClaudeCmd, openSlotFolderCmd, openSlotIssueCmd):
//     acquire RLock to read loops/slots, release before I/O or returning cmd.
//   - sortedSlotKeys: caller must hold at least RLock.
//   - Free functions (openInBrowser, deployDocs, injectLangInstruction): stateless, no lock.

import (
	crypto_rand "crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

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
	loop.SlotNeedsHuman:     true,
	loop.SlotError:          true,
}

// generateAgentName returns a name like "clarifier-a3f7" using 2 random bytes (4 hex chars).
func generateAgentName(prefix string) string {
	b := make([]byte, 2)
	crypto_rand.Read(b)
	return fmt.Sprintf("%s-%04x", prefix, b)
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

		args := []string{"--agent", "clarifier"}
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

		args := []string{"--agent", "reviewer"}
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

		args := []string{"--agent", "efficiency"}
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

// deployAndLaunchAgent deploys the named agent to the project and launches it.
// If the broker is available (channel_enabled), writes .mcp.json to the project root
// with ZPIT_ISSUE_ID = "0" (lobby) so the manual agent can use channel communication.
func (m Model) deployAndLaunchAgent(agentName string, agentMD []byte) tea.Cmd {
	project := m.state.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	cfg := m.state.cfg.Terminal
	logger := m.state.logger

	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD
	hookScripts := m.state.hookScripts
	hookMode := project.HookMode
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
		// Deploy hooks + gitignore + gitattributes
		worktree.EnsureGitignore(projectPath)
		worktree.EnsureGitattributes(projectPath)
		if err := worktree.DeployHooksToProject(projectPath, hookMode, hookScripts); err != nil {
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
		args := []string{"--agent", agentName}
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
		// (a) Ensure gitignore + gitattributes
		worktree.EnsureGitignore(projectPath)
		worktree.EnsureGitattributes(projectPath)

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
		args := []string{"--agent", "efficiency"}
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
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg, args...)
		return LaunchResultMsg{
			ProjectID:      focusProjectID,
			TrackingKey:    trackingKey,
			WorkDir:        wtPath,
			WorktreeBranch: branchName,
			Result:         result,
			Err:            err,
		}
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

// injectLangInstruction prepends the locale response instruction after YAML frontmatter.
func injectLangInstruction(md []byte) []byte {
	instruction := locale.ResponseInstruction()
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
	case FocusLoopSlots:
		m.focusProjectID = project.ID
		m.loopCursor = 0
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

	case key.Matches(msg, m.keys.Down):
		if m.termCursor < len(termKeys)-1 {
			m.termCursor++
		}

	case key.Matches(msg, m.keys.PageUp):
		m.viewport.PageUp()

	case key.Matches(msg, m.keys.PageDown):
		m.viewport.PageDown()

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
		return m, m.confirmForm.Init()
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

	case key.Matches(msg, m.keys.Down):
		if m.loopCursor < len(keys)-1 {
			m.loopCursor++
		}

	case key.Matches(msg, m.keys.PageUp):
		m.viewport.PageUp()

	case key.Matches(msg, m.keys.PageDown):
		m.viewport.PageDown()

	case key.Matches(msg, m.keys.Enter):
		return m.launchFocusClaudeCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.Open):
		return m.openSlotFolderCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.Tracker):
		return m.openSlotIssueCmd(keys[m.loopCursor])
	}

	return m, nil
}
