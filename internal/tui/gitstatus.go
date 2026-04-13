package tui

// gitstatus.go — Git Status page: message handlers, tea.Cmd functions, key handler, spinner integration.
//
// This file depends on the following Model fields (added in T7):
//   spinner       spinner.Model     // Bubble Tea spinner for async operation indicator
//   gitOp         string            // "" | "fetch" | "pull" | "refresh"
//   gitData       *GitData          // loaded branch + graph data, nil when loading or error
//   gitProjectID  string            // project ID currently shown in git status view
//   gitError      string            // first-line error message, cleared on successful load
//
// And the following View enum constant (added in T7):
//   ViewGitStatus View
//
// Lock protocol:
//   - enterGitStatus: acquires Lock for view transition + field init, calls NotifyAll.
//   - handleGitStatusKey: acquires Lock for op start / view exit, calls NotifyAll.
//   - onGitDataLoaded, onGitFetchResult, onGitPullResult: acquire Lock for state mutation, call NotifyAll.
//   - loadGitDataCmd, fetchCmd, pullCmd: pure tea.Cmd factories, capture logger before closure.
//   - handleSpinnerTick: no lock (reads per-connection gitOp only).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/git"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/platform"
)

const gitOpTimeout = 30 * time.Second

// GitData bundles the result of loading branch info and commit graph for a project.
// Used by the git status view renderer (T6) and populated by onGitDataLoaded.
type GitData struct {
	Branches git.BranchInfo
	Graph    string
}

// firstLine returns everything before the first newline, or the full string if no newline.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// enterGitStatus transitions the model to the Git Status view for the given project.
// Called by handleProjectsKey when [g] is pressed (T7 wires the dispatch).
func (m Model) enterGitStatus(projectID string) (Model, tea.Cmd) {
	project := m.findProject(projectID)
	if project == nil {
		m.state.logger.Printf("[git-status] project not found id=%s", projectID)
		return m, nil
	}

	p := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	if p == "" {
		m.errorOverlay = locale.T(locale.KeyGitStatusPathNotConfigured)
		m.state.logger.Printf("[git-status] path not configured project=%s", projectID)
		return m, nil
	}

	if _, err := os.Stat(filepath.Join(p, ".git")); err != nil {
		m.errorOverlay = fmt.Sprintf(locale.T(locale.KeyGitStatusNotGitRepo), p)
		m.state.logger.Printf("[git-status] not a git repo project=%s path=%s err=%v", projectID, p, err)
		return m, nil
	}

	m.state.Lock()
	m.currentView = ViewGitStatus
	m.gitProjectID = projectID
	m.gitOp = "refresh"
	m.gitData = nil
	m.gitError = ""
	m.state.NotifyAll()
	m.state.Unlock()

	m.viewport.GotoTop()

	return m, tea.Batch(m.loadGitDataCmd(projectID, p), m.spinner.Tick)
}

// loadGitDataCmd creates a tea.Cmd that loads branches and commit graph for a project.
// Uses copy-before-closure for the logger reference.
func (m Model) loadGitDataCmd(projectID, cwd string) tea.Cmd {
	logger := m.state.logger
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
		defer cancel()

		logger.Printf("[git-status] load project=%s cwd=%s", projectID, cwd)

		info, err := git.Branches(ctx, cwd)
		if err != nil {
			logger.Printf("[git-status] load branches err project=%s: %v", projectID, err)
			return GitDataLoadedMsg{ProjectID: projectID, Err: err}
		}

		graph, err := git.LogGraph(ctx, cwd)
		if err != nil {
			logger.Printf("[git-status] load graph err project=%s: %v", projectID, err)
			return GitDataLoadedMsg{ProjectID: projectID, Branches: info, Err: err}
		}

		return GitDataLoadedMsg{ProjectID: projectID, Branches: info, Graph: graph}
	}
}

// fetchCmd creates a tea.Cmd that runs git fetch --all --prune for the project.
func (m Model) fetchCmd(projectID, cwd string) tea.Cmd {
	logger := m.state.logger
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
		defer cancel()

		logger.Printf("[git-status] fetch project=%s cwd=%s", projectID, cwd)

		stdout, stderr, err := git.Fetch(ctx, cwd)
		return GitFetchResultMsg{
			ProjectID: projectID,
			Stdout:    stdout,
			Stderr:    stderr,
			Err:       err,
		}
	}
}

// pullCmd creates a tea.Cmd that runs git pull --ff-only for the project.
func (m Model) pullCmd(projectID, cwd string) tea.Cmd {
	logger := m.state.logger
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
		defer cancel()

		logger.Printf("[git-status] pull project=%s cwd=%s", projectID, cwd)

		stdout, stderr, err := git.Pull(ctx, cwd)
		return GitPullResultMsg{
			ProjectID: projectID,
			Stdout:    stdout,
			Stderr:    stderr,
			Err:       err,
		}
	}
}

// resolveGitProjectCwd resolves the working directory for the current git status project.
// Returns ("", nil) if the project is not found or path is empty.
func (m Model) resolveGitProjectCwd() (string, *config.ProjectConfig) {
	project := m.findProject(m.gitProjectID)
	if project == nil {
		return "", nil
	}
	cwd := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	return cwd, project
}

// handleGitStatusKey dispatches key presses while the Git Status view is active.
// Called from handleKey in model.go (T7 wires the dispatch).
func (m Model) handleGitStatusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		// Return to project list and clear git status state.
		m.state.Lock()
		m.currentView = ViewProjects
		m.gitData = nil
		m.gitOp = ""
		m.gitProjectID = ""
		m.gitError = ""
		m.state.NotifyAll()
		m.state.Unlock()
		m.viewport.GotoTop()
		return m, nil

	case key.Matches(msg, m.keys.GitFetch):
		return m.startGitOp("fetch")

	case key.Matches(msg, m.keys.GitPull):
		return m.startGitOp("pull")

	case key.Matches(msg, m.keys.GitRefresh):
		return m.startGitOp("refresh")

	default:
		// Delegate to viewport for scrolling (up/down/pgup/pgdn).
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
}

// startGitOp begins a git operation (fetch/pull/refresh) if no operation is in progress.
func (m Model) startGitOp(op string) (tea.Model, tea.Cmd) {
	// Reject if an operation is already running.
	if m.gitOp != "" {
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyGitStatusOpInProgress), m.gitOp))
		return m, nil
	}

	cwd, _ := m.resolveGitProjectCwd()
	if cwd == "" {
		m.state.logger.Printf("[git-status] cannot resolve cwd for op=%s project=%s", op, m.gitProjectID)
		return m, nil
	}

	pid := m.gitProjectID

	m.state.Lock()
	m.gitOp = op
	m.state.NotifyAll()
	m.state.Unlock()

	var cmd tea.Cmd
	switch op {
	case "fetch":
		cmd = m.fetchCmd(pid, cwd)
	case "pull":
		cmd = m.pullCmd(pid, cwd)
	case "refresh":
		cmd = m.loadGitDataCmd(pid, cwd)
	}

	return m, tea.Batch(cmd, m.spinner.Tick)
}

// --- Message handlers (called from Update in model.go, wired by T7) ---

// onGitDataLoaded handles GitDataLoadedMsg after branch+graph data is loaded.
func (m Model) onGitDataLoaded(msg GitDataLoadedMsg) (Model, tea.Cmd) {
	// Drop stale message if the view has changed or project differs.
	if m.currentView != ViewGitStatus || m.gitProjectID != msg.ProjectID {
		return m, nil
	}

	m.state.Lock()
	m.gitOp = ""
	if msg.Err != nil {
		m.gitData = nil
		m.gitError = firstLine(msg.Err.Error())
		m.state.logger.Printf("[git-status] load failed project=%s err=%v", msg.ProjectID, msg.Err)
	} else {
		m.gitData = &GitData{Branches: msg.Branches, Graph: msg.Graph}
		m.gitError = ""
	}
	m.state.NotifyAll()
	m.state.Unlock()

	return m, nil
}

// onGitFetchResult handles GitFetchResultMsg after a fetch operation completes.
func (m Model) onGitFetchResult(msg GitFetchResultMsg) (Model, tea.Cmd) {
	if m.currentView != ViewGitStatus || m.gitProjectID != msg.ProjectID {
		return m, nil
	}

	m.state.Lock()
	m.gitOp = ""
	m.state.NotifyAll()
	m.state.Unlock()

	if msg.Err != nil {
		errText := extractErrorText(msg.Stderr, msg.Err)
		m.state.logger.Printf("[git-status] fetch failed project=%s stderr=%s err=%v", msg.ProjectID, msg.Stderr, msg.Err)
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyGitStatusFetchFailed), firstLine(errText)))
		if len(errText) > 100 {
			m.errorOverlay = errText
		}
		return m, nil
	}

	m.state.logger.Printf("[git-status] fetch OK project=%s", msg.ProjectID)
	m.setStatus(locale.T(locale.KeyGitStatusFetchOK))

	// Re-load branch data after successful fetch.
	cwd, _ := m.resolveGitProjectCwd()
	if cwd == "" {
		return m, nil
	}

	m.state.Lock()
	m.gitOp = "refresh"
	m.state.NotifyAll()
	m.state.Unlock()

	return m, tea.Batch(m.loadGitDataCmd(msg.ProjectID, cwd), m.spinner.Tick)
}

// onGitPullResult handles GitPullResultMsg after a pull operation completes.
func (m Model) onGitPullResult(msg GitPullResultMsg) (Model, tea.Cmd) {
	if m.currentView != ViewGitStatus || m.gitProjectID != msg.ProjectID {
		return m, nil
	}

	m.state.Lock()
	m.gitOp = ""
	m.state.NotifyAll()
	m.state.Unlock()

	if msg.Err != nil {
		errText := extractErrorText(msg.Stderr, msg.Err)
		m.state.logger.Printf("[git-status] pull failed project=%s stderr=%s err=%v", msg.ProjectID, msg.Stderr, msg.Err)
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyGitStatusPullFailed), firstLine(errText)))
		if len(errText) > 100 {
			m.errorOverlay = errText
		}
		return m, nil
	}

	m.state.logger.Printf("[git-status] pull OK project=%s", msg.ProjectID)
	m.setStatus(locale.T(locale.KeyGitStatusPullOK))

	// Re-load branch data after successful pull.
	cwd, _ := m.resolveGitProjectCwd()
	if cwd == "" {
		return m, nil
	}

	m.state.Lock()
	m.gitOp = "refresh"
	m.state.NotifyAll()
	m.state.Unlock()

	return m, tea.Batch(m.loadGitDataCmd(msg.ProjectID, cwd), m.spinner.Tick)
}

// handleSpinnerTick is called from Update() on spinner.TickMsg.
// Only returns a continuation tick if a git op is in progress.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	if m.gitOp == "" {
		return m, nil // stop ticking when no operation is active
	}
	return m, cmd
}

// --- Helpers ---

// extractErrorText returns the most informative error string from stderr and err.
// Prefers stderr if non-empty; falls back to err.Error().
func extractErrorText(stderr string, err error) string {
	s := strings.TrimSpace(stderr)
	if s != "" {
		return s
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}
