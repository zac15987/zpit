package tui

// confirm.go — Confirm dialogs: deploy confirm, undeploy confirm, issue confirm, executePendingOp.
//
// Lock protocol:
//   - Handler methods (executePendingOp): acquires Lock only for PendingLoop case
//     (mutates loops, channelSubs). Other cases mutate per-connection Model fields only.
//   - show*Confirm methods: mutate per-connection Model fields (confirmForm, confirmResult,
//     confirmAction) — no AppState lock needed.
//   - Free function (undeployFiles): stateless filesystem operation, no lock.

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/worktree"
)

// showDeployConfirm displays a huh confirm dialog for deploying the clarifier agent.
func (m *Model) showDeployConfirm() {
	confirmed := new(bool)
	m.confirmResult = confirmed
	m.confirmForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(locale.T(locale.KeyClarifierNotDeployed)).
				Affirmative(locale.T(locale.KeyDeployAndLaunch)).
				Negative(locale.T(locale.KeyCancel)).
				Value(confirmed),
		),
	).WithWidth(50)
	m.confirmAction = func() tea.Cmd {
		return m.deployAndLaunchAgent("clarifier", injectLangInstruction(m.state.clarifierMD))
	}
}

// showReviewerDeployConfirm displays a huh confirm dialog for deploying the reviewer agent.
func (m *Model) showReviewerDeployConfirm() {
	confirmed := new(bool)
	m.confirmResult = confirmed
	m.confirmForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(locale.T(locale.KeyReviewerNotDeployed)).
				Affirmative(locale.T(locale.KeyDeployAndLaunch)).
				Negative(locale.T(locale.KeyCancel)).
				Value(confirmed),
		),
	).WithWidth(50)
	m.confirmAction = func() tea.Cmd {
		return m.deployAndLaunchAgent("reviewer", injectLangInstruction(m.state.reviewerMD))
	}
}

// showUndeployConfirm displays a huh confirm dialog for removing deployed files.
func (m *Model) showUndeployConfirm(project config.ProjectConfig) {
	confirmed := new(bool)
	m.confirmResult = confirmed
	m.confirmForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(locale.T(locale.KeyUndeployConfirm)).
				Affirmative(locale.T(locale.KeyUndeployButton)).
				Negative(locale.T(locale.KeyCancel)).
				Value(confirmed),
		),
	).WithWidth(60)
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	projectName := project.Name
	logger := m.state.logger
	m.confirmAction = func() tea.Cmd {
		return func() tea.Msg {
			count := undeployFiles(projectPath)
			logger.Printf("[undeploy] removed %d items from %s", count, projectName)
			if count == 0 {
				return StatusMsg{Text: fmt.Sprintf(locale.T(locale.KeyUndeployNoop), projectName)}
			}
			return StatusMsg{Text: fmt.Sprintf(locale.T(locale.KeyUndeployDone), count, projectName)}
		}
	}
}

// showIssueConfirm displays an overlay confirm dialog before changing an issue from pending to todo.
func (m *Model) showIssueConfirm(issueID, issueTitle string) {
	title := fmt.Sprintf(locale.T(locale.KeyIssueConfirmTitle), issueID, issueTitle)
	confirmed := new(bool)
	m.confirmResult = confirmed
	m.confirmForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Affirmative(locale.T(locale.KeyIssueConfirmButton)).
				Negative(locale.T(locale.KeyCancel)).
				Value(confirmed),
		),
	).WithWidth(50)
	m.confirmAction = func() tea.Cmd {
		return m.confirmIssueCmd()
	}
}

// executePendingOp continues the original operation after labels are confirmed present.
func (m *Model) executePendingOp() (tea.Model, tea.Cmd) {
	op := m.pendingOp
	if op == nil {
		return m, nil
	}

	switch op.Kind {
	case PendingClarify:
		m.pendingOp = nil
		m.cursor = op.ProjectIndex // restore cursor for deploy confirm / launch
		project := m.state.projects[op.ProjectIndex]
		agentPath := filepath.Join(
			platform.ResolvePath(project.Path.Windows, project.Path.WSL),
			".claude", "agents", "clarifier.md",
		)
		if _, err := os.Stat(agentPath); err != nil {
			m.showDeployConfirm()
			return m, m.confirmForm.Init()
		}
		return m, m.launchClarifierCmd()

	case PendingReview:
		m.pendingOp = nil
		m.cursor = op.ProjectIndex // restore cursor for deploy confirm / launch
		project := m.state.projects[op.ProjectIndex]
		agentPath := filepath.Join(
			platform.ResolvePath(project.Path.Windows, project.Path.WSL),
			".claude", "agents", "reviewer.md",
		)
		if _, err := os.Stat(agentPath); err != nil {
			m.showReviewerDeployConfirm()
			return m, m.confirmForm.Init()
		}
		return m, m.launchReviewerCmd()

	case PendingLoop:
		m.pendingOp = nil
		project := m.state.projects[op.ProjectIndex]

		m.state.Lock()
		ls := &loop.LoopState{
			Active: true,
			Slots:  make(map[string]*loop.Slot),
		}
		m.state.loops[project.ID] = ls
		// Snapshot listen subscription status under lock.
		var unsubscribedListenProjects []string
		if project.ChannelEnabled && m.state.broker != nil {
			for _, lp := range project.ChannelListen {
				if _, exists := m.state.channelSubs[lp]; !exists && lp != project.ID {
					unsubscribedListenProjects = append(unsubscribedListenProjects, lp)
				}
			}
		}
		m.state.NotifyAll()
		m.state.Unlock()
		m.setStatus(fmt.Sprintf("Loop started for %s", project.Name))
		cmds := []tea.Cmd{
			m.loopCleanupMergedCmd(project.ID),
			m.loopScanOpenPRsCmd(project.ID),
			m.loopPollCmd(project.ID),
		}
		if project.ChannelEnabled && m.state.broker != nil {
			cmds = append(cmds, m.channelSubscribeCmd(project.ID))
			for _, lp := range unsubscribedListenProjects {
				cmds = append(cmds, m.channelSubscribeCmd(lp))
			}
		}
		return m, tea.Batch(cmds...)

	case PendingConfirmIssue:
		m.pendingOp = nil
		if m.statusCursor < len(m.statusIssues) {
			issue := m.statusIssues[m.statusCursor]
			m.showIssueConfirm(issue.ID, issue.Title)
			return m, m.confirmForm.Init()
		}
		return m, nil
	}

	m.pendingOp = nil
	return m, nil
}

// undeployFiles removes all Zpit-deployed artifacts from a project:
// .claude/{agents,docs,hooks}/ dirs, .mcp.json, settings.local.json,
// and the "hooks" key from settings.json.
func undeployFiles(projectPath string) int {
	claudeDir := filepath.Join(projectPath, ".claude")
	removed := 0

	for _, dir := range worktree.ZpitDeployedDirs {
		target := filepath.Join(claudeDir, dir)
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			os.RemoveAll(target)
			removed++
		}
	}

	for _, file := range worktree.ZpitDeployedFiles {
		target := filepath.Join(projectPath, file)
		if _, err := os.Stat(target); err == nil {
			os.Remove(target)
			removed++
		}
	}

	// Remove settings.local.json (worktree overlay, fully Zpit-created).
	localSettings := filepath.Join(claudeDir, "settings.local.json")
	if _, err := os.Stat(localSettings); err == nil {
		os.Remove(localSettings)
		removed++
	}

	// Strip Zpit-injected "hooks" key from settings.json (preserves other keys).
	if worktree.CleanSettingsHooks(projectPath) {
		removed++
	}

	return removed
}
