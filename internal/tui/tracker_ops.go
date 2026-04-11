package tui

// tracker_ops.go — Tracker & Label operations: label check/ensure, issue load/confirm, label check flow.
//
// Lock protocol:
//   - Cmd factory methods (checkLabelsCmd, ensureLabelsCmd, loadIssuesCmd,
//     confirmIssueCmd, openIssueURLCmd): read-only access to m.state.clients
//     and m.state.projects — no lock needed (read-only after init).
//   - Handler methods (startWithLabelCheck, showLabelConfirm): mutate per-connection
//     Model fields only (pendingOp, confirmForm) — no AppState lock needed.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/tracker"
)

func (m Model) handleLabelCheckResult(msg LabelCheckResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Label check failed: %s", msg.Err))
		m.pendingOp = nil
		return m, nil
	}
	if len(msg.Missing) == 0 {
		return m.executePendingOp()
	}
	m.showLabelConfirm(msg.ProjectID, msg.Missing)
	return m, m.initConfirmForm()
}

func (m Model) handleLabelsEnsured(msg LabelsEnsuredMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Label sync failed: %s", msg.Err))
		m.pendingOp = nil
		return m, nil
	}
	if len(msg.Created) > 0 {
		m.setStatus(fmt.Sprintf("Created labels: %s", strings.Join(msg.Created, ", ")))
	}
	if m.pendingOp != nil {
		return m.executePendingOp()
	}
	return m, nil
}

func (m Model) handleIssuesLoaded(msg IssuesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.ProjectID == m.statusProjectID {
		m.statusLoading = false
		if msg.Err != nil {
			m.statusError = msg.Err.Error()
		} else {
			m.statusIssues = msg.Issues
		}
	}
	return m, nil
}

func (m Model) handleIssueConfirmed(msg IssueConfirmedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Confirm failed: %s", msg.Err))
	} else {
		m.setStatus(fmt.Sprintf("Issue #%s confirmed → todo", msg.IssueID))
		for i, issue := range m.statusIssues {
			if issue.ID == msg.IssueID {
				m.statusIssues[i].Status = tracker.StatusTodo
				break
			}
		}
	}
	return m, nil
}

// loadIssuesCmd fetches issues from the tracker via TrackerClient.
func (m Model) loadIssuesCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return func() tea.Msg {
			return IssuesLoadedMsg{ProjectID: project.ID, Err: fmt.Errorf("tracker %q not configured or token missing", project.Tracker)}
		}
	}
	repo := project.Repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issues, err := client.ListIssues(ctx, repo)
		return IssuesLoadedMsg{ProjectID: project.ID, Issues: issues, Err: err}
	}
}

// checkLabelsCmd checks which required labels are missing (read-only, no creation).
func (m Model) checkLabelsCmd(projectID string, required []tracker.LabelDef) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return func() tea.Msg {
			return LabelCheckResultMsg{ProjectID: projectID, Err: fmt.Errorf("project not found: %s", projectID)}
		}
	}
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return func() tea.Msg {
			return LabelCheckResultMsg{ProjectID: projectID, Err: fmt.Errorf("%s", locale.T(locale.KeyTrackerTokenNotSet))}
		}
	}
	lm, ok := client.(tracker.LabelManager)
	if !ok {
		return func() tea.Msg {
			return LabelCheckResultMsg{ProjectID: projectID, Err: fmt.Errorf("%s", locale.T(locale.KeyTrackerLabelNotSupported))}
		}
	}
	repo := project.Repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		missing, err := tracker.CheckLabels(ctx, lm, repo, required)
		return LabelCheckResultMsg{ProjectID: projectID, Missing: missing, Err: err}
	}
}

// ensureLabelsCmd creates the specified missing labels for a project's tracker.
func (m Model) ensureLabelsCmd(projectID string, required []tracker.LabelDef) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	lm, ok := client.(tracker.LabelManager)
	if !ok {
		return nil
	}
	repo := project.Repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		created, err := tracker.EnsureLabels(ctx, lm, repo, required)
		return LabelsEnsuredMsg{ProjectID: projectID, Created: created, Err: err}
	}
}

// confirmIssueCmd changes the selected issue from pending_confirm to todo.
func (m Model) confirmIssueCmd() tea.Cmd {
	if m.statusCursor >= len(m.statusIssues) {
		return nil
	}
	issue := m.statusIssues[m.statusCursor]
	if issue.Status != tracker.StatusPendingConfirm {
		return func() tea.Msg {
			return StatusMsg{Text: fmt.Sprintf("Issue #%s is %s, not pending_confirm", issue.ID, issue.Status)}
		}
	}
	project := m.findProject(m.statusProjectID)
	if project == nil {
		return nil
	}
	client, ok := m.state.clients[project.Tracker]
	if !ok {
		return nil
	}
	repo := project.Repo
	issueID := issue.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := client.UpdateLabels(ctx, repo, issueID, []string{"todo"}, []string{"pending"})
		return IssueConfirmedMsg{ProjectID: project.ID, IssueID: issueID, Err: err}
	}
}

// openIssueURLCmd opens the selected issue in the browser.
func (m Model) openIssueURLCmd() tea.Cmd {
	if m.statusCursor >= len(m.statusIssues) {
		return nil
	}
	issue := m.statusIssues[m.statusCursor]
	project := m.findProject(m.statusProjectID)
	if project == nil {
		return nil
	}
	provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]
	if !ok {
		return nil
	}
	url := tracker.BuildIssueURL(provider, project.Repo, issue.ID)
	if url == "" {
		return func() tea.Msg {
			return StatusMsg{Text: "Cannot build URL for this tracker type"}
		}
	}
	return openInBrowser(url)
}

// startWithLabelCheck sets up pendingOp and fires an async label check.
func (m *Model) startWithLabelCheck(kind PendingOpKind, project config.ProjectConfig, required []tracker.LabelDef) (tea.Model, tea.Cmd) {
	m.pendingOp = &PendingOp{
		Kind:         kind,
		ProjectID:    project.ID,
		ProjectIndex: m.cursor,
		Required:     required,
	}
	m.setStatus(locale.T(locale.KeyCheckingLabels))
	return m, m.checkLabelsCmd(project.ID, required)
}

// showLabelConfirm displays an overlay confirm dialog listing missing labels.
func (m *Model) showLabelConfirm(projectID string, missing []tracker.LabelDef) {
	project := m.findProject(projectID)
	repo := ""
	if project != nil {
		repo = project.Repo
	}
	names := make([]string, len(missing))
	for i, ld := range missing {
		names[i] = "  • " + ld.Name
	}
	title := fmt.Sprintf(locale.T(locale.KeyLabelsMissing), repo, strings.Join(names, "\n"))

	confirmed := new(bool)
	m.confirmResult = confirmed
	m.confirmForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Affirmative(locale.T(locale.KeyCreateLabels)).
				Negative(locale.T(locale.KeyCancel)).
				Value(confirmed),
		),
	).WithWidth(50)
	m.confirmAction = func() tea.Cmd {
		return m.ensureLabelsCmd(projectID, missing)
	}
}
