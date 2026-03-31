package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/charmbracelet/huh"
	overlay "github.com/rmhubbert/bubbletea-overlay"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
	"github.com/zac15987/zpit/internal/worktree"
)

const (
	statusDisplayDuration      = 5 * time.Second
	tickInterval               = 1 * time.Second
	livenessCheckInterval      = 5 * time.Second
	permissionCheckInterval    = 2 * time.Second
	endedDisplayDuration       = 3 * time.Second
	sessionScanInterval        = 10 * time.Second
)

// View represents the current screen.
type View int

const (
	ViewProjects View = iota
	ViewStatus
)

// FocusedPanel indicates which panel has keyboard focus in ViewProjects.
type FocusedPanel int

const (
	FocusProjects  FocusedPanel = iota
	FocusLoopSlots
)

// PendingOpKind identifies the type of pending operation waiting for label readiness.
type PendingOpKind int

const (
	PendingNone PendingOpKind = iota
	PendingClarify
	PendingReview
	PendingLoop
	PendingConfirmIssue
)

// PendingOp captures the context of an operation that requires label readiness.
type PendingOp struct {
	Kind         PendingOpKind
	ProjectID    string
	ProjectIndex int                // snapshot of m.cursor at key press time
	Required     []tracker.LabelDef // labels this operation needs
}

// ActiveTerminal tracks a launched terminal and its agent state.
type ActiveTerminal struct {
	LaunchResult      *terminal.LaunchResult
	SessionPID        int
	SessionID         string // current session ID (for /resume change detection)
	WorkDir           string // project work directory (needed to recompute logPath on session switch)
	State             watcher.AgentState
	LastQuestion      string
	PermissionMessage string // message from permission signal (e.g., "Claude needs your permission to use Bash")
	StateChangedAt    time.Time
	Watcher           *watcher.Watcher
}

// Model is the root Bubble Tea model for a single connected client session.
// Shared application state is accessed via the state *AppState pointer, enabling
// multiple tea.Program instances (local TUI + SSH remote) to share the same state.
type Model struct {
	// Shared application state (common across all connected clients)
	state *AppState

	// isRemote is true for SSH sessions. When true, pressing q only closes this
	// session (tea.Quit) without stopping watchers or deactivating loops.
	isRemote bool

	// Per-connection UI state
	keys KeyMap

	width  int
	height int

	currentView View

	// Project list state (cursor position is per-connection)
	cursor        int
	statusMessage string
	statusExpiry  time.Time

	// Status view state
	statusProjectID string
	statusIssues    []tracker.Issue
	statusCursor    int
	statusLoading   bool
	statusError     string

	// Error overlay (dismissible with Esc/Enter)
	errorOverlay string

	// Confirm dialog (huh)
	confirmForm   *huh.Form
	confirmResult *bool          // heap-allocated: shared across Bubble Tea value copies
	confirmAction func() tea.Cmd

	// Label check state
	pendingOp *PendingOp

	// Focus panel state (loop slot selection)
	focusedPanel   FocusedPanel
	loopCursor     int
	focusProjectID string

	// Viewport for scrollable content
	viewport viewport.Model

	// Subscriber for cross-client state refresh broadcast
	subscriberID int
	subscriberCh <-chan struct{}
}

// NewModelWithState creates a per-connection Model backed by the given shared AppState.
// Each connected client (local terminal, SSH session) gets its own Model instance
// with independent UI state while sharing the same AppState.
// When isRemote is true, pressing q only closes this session without stopping
// shared watchers or deactivating loops.
func NewModelWithState(appState *AppState, isRemote bool) Model {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	vp.KeyMap = viewport.KeyMap{} // disable all keyboard bindings — we handle keys ourselves

	id, ch := appState.Subscribe()

	return Model{
		state:        appState,
		isRemote:     isRemote,
		keys:         DefaultKeyMap(),
		currentView:  ViewProjects,
		viewport:     vp,
		subscriberID: id,
		subscriberCh: ch,
	}
}

// NewModel creates a local (non-remote) Model backed by the given AppState.
// Equivalent to NewModelWithState(appState, false).
func NewModel(appState *AppState) Model {
	return NewModelWithState(appState, false)
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd(), m.waitForStateRefresh()}

	// SSH remote sessions skip server-init (session scan, .gitignore, provider check).
	// These are run once at zpit serve startup via RunServerInit.
	if m.isRemote {
		return tea.Batch(cmds...)
	}

	cmds = append(cmds, m.serverInitCmds()...)
	return tea.Batch(cmds...)
}

// waitForStateRefresh returns a tea.Cmd that blocks on the subscriber channel.
// When a signal arrives (from NotifyAll), it sends a StateRefreshMsg to trigger re-render.
func (m Model) waitForStateRefresh() tea.Cmd {
	ch := m.subscriberCh
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil // channel closed, subscriber removed
		}
		return StateRefreshMsg{}
	}
}

// RunServerInit performs server-init logic synchronously (for zpit serve startup).
// Runs session scan, .gitignore check, and provider validation on the AppState.
func RunServerInit(state *AppState) {
	seenPath := make(map[string]bool)
	seenMissing := make(map[string]bool)
	var missingProviders []string

	for _, project := range state.projects {
		projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
		if projectPath != "" && !seenPath[projectPath] {
			seenPath[projectPath] = true
			ensureGitignore(projectPath)
		}
		if project.Tracker == "" || project.Repo == "" {
			continue
		}
		if _, ok := state.clients[project.Tracker]; !ok {
			if !seenMissing[project.Tracker] {
				seenMissing[project.Tracker] = true
				missingProviders = append(missingProviders, project.Tracker)
			}
		}
	}
	if len(missingProviders) > 0 {
		state.logger.Printf("Tracker unavailable (token not set?): %s", strings.Join(missingProviders, ", "))
	}
}

// serverInitCmds returns tea.Cmd slices for server-init tasks (used by local TUI Init).
func (m Model) serverInitCmds() []tea.Cmd {
	var cmds []tea.Cmd

	seenPath := make(map[string]bool)
	seenMissing := make(map[string]bool)
	var missingProviders []string

	for _, project := range m.state.projects {
		projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
		if projectPath != "" && !seenPath[projectPath] {
			seenPath[projectPath] = true
			ensureGitignore(projectPath)
		}
		if project.Tracker == "" || project.Repo == "" {
			continue
		}
		if _, ok := m.state.clients[project.Tracker]; !ok {
			if !seenMissing[project.Tracker] {
				seenMissing[project.Tracker] = true
				missingProviders = append(missingProviders, project.Tracker)
			}
		}
	}

	if len(missingProviders) > 0 {
		msg := fmt.Sprintf("Tracker unavailable (token not set?): %s", strings.Join(missingProviders, ", "))
		m.state.logger.Println(msg)
		cmds = append(cmds, func() tea.Msg {
			return StatusMsg{Text: msg}
		})
	}

	// Scan for already-running Claude Code sessions.
	cmds = append(cmds, m.scanExistingSessionsCmd())

	return cmds
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	if mdl, ok := model.(Model); ok {
		mdl.syncViewportContent()
		return mdl, cmd
	}
	return model, cmd
}

// syncViewportContent re-renders the scrollable area into the viewport.
// Acquires RLock to safely read mutable shared fields (activeTerminals, loops)
// used by render methods. Called after handlers release their write locks.
func (m *Model) syncViewportContent() {
	m.state.RLock()
	defer m.state.RUnlock()

	var header, footer string
	switch m.currentView {
	case ViewProjects:
		header = m.renderProjectsHeader()
		footer = m.renderProjectsFooter()
		m.viewport.SetContent(m.renderProjectsScrollable())
	case ViewStatus:
		header = m.renderStatusHeader()
		footer = m.renderStatusFooter()
		m.viewport.SetContent(m.renderStatusScrollable())
	}
	h := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if h < 1 {
		h = 1
	}
	m.viewport.Width = m.width
	m.viewport.Height = h
}

// ensureCursorVisible adjusts the viewport offset so the given line is on screen.
func (m *Model) ensureCursorVisible(cursorLine int) {
	if cursorLine < m.viewport.YOffset {
		m.viewport.SetYOffset(cursorLine)
	} else if cursorLine >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(cursorLine - m.viewport.Height + 1)
	}
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If error overlay is showing, only allow dismiss (Esc/Enter) and tick.
	if m.errorOverlay != "" {
		if _, ok := msg.(TickMsg); ok {
			cmds := m.checkSessionLiveness()
			cmds = append(cmds, tickCmd())
			return m, tea.Batch(cmds...)
		}
		if msg, ok := msg.(tea.KeyMsg); ok {
			if key.Matches(msg, m.keys.Enter) || key.Matches(msg, m.keys.Back) {
				m.errorOverlay = ""
			}
		}
		return m, nil
	}

	// If confirm dialog is active, route messages to it (but keep tick alive).
	if m.confirmForm != nil {
		// Let tick through so the UI stays responsive after confirm closes.
		if _, ok := msg.(TickMsg); ok {
			cmds := m.checkSessionLiveness()
			cmds = append(cmds, tickCmd())
			return m, tea.Batch(cmds...)
		}
		form, cmd := m.confirmForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.confirmForm = f
		}
		if m.confirmForm.State == huh.StateCompleted {
			action := m.confirmAction
			confirmed := m.confirmResult != nil && *m.confirmResult
			m.confirmForm = nil
			m.confirmAction = nil
			m.confirmResult = nil
			if confirmed && action != nil {
				return m, action()
			}
			// User cancelled — clear any pending operation.
			m.pendingOp = nil
			return m, nil
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case StateRefreshMsg:
		// Another client changed shared state — re-render by returning model,
		// and re-subscribe for the next notification.
		return m, m.waitForStateRefresh()

	case LaunchResultMsg:
		return m.handleLaunchResult(msg)

	case AgentEventMsg:
		return m.handleAgentEvent(msg)

	case existingSessionsMsg:
		m.state.Lock()
		m.state.logger.Printf("session scan [%s]: found %d session(s)", msg.Source, len(msg.Entries))
		var cmds []tea.Cmd
		for _, entry := range msg.Entries {
			key := m.nextTrackingKey(entry.ProjectID)
			m.state.logger.Printf("  attach: key=%s PID=%d sessionID=%s", key, entry.PID, entry.SessionID)
			m.state.activeTerminals[key] = &ActiveTerminal{
				State:          watcher.StateUnknown,
				SessionPID:     entry.PID,
				SessionID:      entry.SessionID,
				WorkDir:        entry.WorkDir,
				StateChangedAt: time.Now(),
			}
			cmds = append(cmds, waitForLogCmd(key, entry.PID, entry.SessionID, entry.LogPath, entry.WorkDir, m.state.logger))
		}
		m.state.NotifyAll()
		m.state.Unlock()
		return m, tea.Batch(cmds...)

	case sessionFoundMsg:
		m.state.Lock()
		m.state.logger.Printf("session found: key=%s PID=%d sessionID=%s", msg.ProjectID, msg.PID, msg.SessionID)
		at, ok := m.state.activeTerminals[msg.ProjectID]
		if !ok {
			m.state.Unlock()
			return m, nil
		}
		at.SessionPID = msg.PID
		at.SessionID = msg.SessionID
		workDir := at.WorkDir
		m.state.NotifyAll()
		m.state.Unlock()
		return m, waitForLogCmd(msg.ProjectID, msg.PID, msg.SessionID, msg.LogPath, workDir, m.state.logger)

	case watcherReadyMsg:
		m.state.Lock()
		if at, ok := m.state.activeTerminals[msg.ProjectID]; ok {
			// Stale guard: if session has already switched past this msg, discard.
			if at.SessionID != "" && msg.SessionID != "" && at.SessionID != msg.SessionID {
				m.state.logger.Printf("watcher ready: key=%s STALE (at=%s, msg=%s), discarding",
					msg.ProjectID, at.SessionID, msg.SessionID)
				m.state.Unlock()
				msg.Watcher.Stop()
				return m, nil
			}
			at.SessionID = msg.SessionID
			at.Watcher = msg.Watcher
			if at.State == watcher.StateUnknown && msg.LogPath != "" {
				state, question := watcher.ReadLastState(msg.LogPath)
				if state != watcher.StateUnknown {
					at.State = state
					at.LastQuestion = question
					at.StateChangedAt = time.Now()
				}
			}
			m.state.logger.Printf("watcher ready: key=%s state=%s", msg.ProjectID, at.State)
			m.state.NotifyAll()
			m.state.Unlock()
			return m, watchNextCmd(msg.ProjectID, msg.Watcher)
		}
		m.state.Unlock()
		msg.Watcher.Stop()
		return m, nil

	case TickMsg:
		cmds := m.checkSessionLiveness()
		m.checkPermissionSignals()
		if scanCmd := m.checkNewSessions(); scanCmd != nil {
			cmds = append(cmds, scanCmd)
		}
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case WatcherErrorMsg:
		m.state.logger.Printf("watcher error: key=%s err=%v", msg.ProjectID, msg.Err)
		m.setStatus(fmt.Sprintf("Watcher error (%s): %s", msg.ProjectID, msg.Err))
		return m, nil

	case sessionLostMsg:
		m.state.Lock()
		m.state.logger.Printf("session lost: %s — %s", msg.ProjectID, msg.Text)
		if at, ok := m.state.activeTerminals[msg.ProjectID]; ok {
			at.State = watcher.StateEnded
			at.StateChangedAt = time.Now()
			if at.Watcher != nil {
				at.Watcher.Stop()
			}
			m.state.NotifyAll()
		}
		m.state.Unlock()
		return m, nil

	case StatusMsg:
		m.setStatus(msg.Text)
		return m, nil

	case LabelCheckResultMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("Label check failed: %s", msg.Err))
			m.pendingOp = nil
			return m, nil
		}
		if len(msg.Missing) == 0 {
			return m.executePendingOp()
		}
		m.showLabelConfirm(msg.ProjectID, msg.Missing)
		return m, m.confirmForm.Init()

	case LabelsEnsuredMsg:
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

	case IssuesLoadedMsg:
		if msg.ProjectID == m.statusProjectID {
			m.statusLoading = false
			if msg.Err != nil {
				m.statusError = msg.Err.Error()
			} else {
				m.statusIssues = msg.Issues
			}
		}
		return m, nil

	case IssueConfirmedMsg:
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

	// Loop engine messages
	case LoopPollMsg:
		return m.handleLoopPoll(msg)
	case LoopWorktreeCreatedMsg:
		return m.handleLoopWorktreeCreated(msg)
	case LoopAgentWrittenMsg:
		return m.handleLoopAgentWritten(msg)
	case LoopAgentLaunchedMsg:
		return m.handleLoopAgentLaunched(msg)
	case LoopPRStatusMsg:
		return m.handleLoopPRStatus(msg)
	case LoopCleanupMsg:
		return m.handleLoopCleanup(msg)
	case LoopOpenPRsMsg:
		return m.handleLoopOpenPRs(msg)
	case loopPollTickMsg:
		m.state.RLock()
		ls, ok := m.state.loops[msg.ProjectID]
		active := ok && ls.Active
		m.state.RUnlock()
		if active {
			return m, m.loopPollCmd(msg.ProjectID)
		}
		return m, nil
	case loopPRPollTickMsg:
		// loopPollPRCmd acquires its own RLock internally.
		return m, m.loopPollPRCmd(msg.ProjectID, msg.IssueID)
	case LoopLabelPollMsg:
		return m.handleLoopLabelPoll(msg)
	case loopLabelPollTickMsg:
		return m, m.loopPollLabelsCmd(msg.ProjectID, msg.IssueID)

	}

	return m, nil
}

func (m Model) View() string {
	m.state.RLock()
	defer m.state.RUnlock()

	var bg string
	switch m.currentView {
	case ViewProjects:
		bg = m.viewProjects()
	case ViewStatus:
		bg = m.viewStatus()
	default:
		bg = "Unknown view"
	}
	if m.errorOverlay != "" {
		fg := errorOverlayStyle.Render(m.errorOverlay)
		return overlay.Composite(fg, bg, overlay.Center, overlay.Center, 0, 0)
	}
	if m.confirmForm != nil {
		fg := confirmOverlayStyle.Render(m.confirmForm.View())
		return overlay.Composite(fg, bg, overlay.Center, overlay.Center, 0, 0)
	}
	return bg
}

func (m *Model) setStatus(text string) {
	m.statusMessage = text
	m.statusExpiry = time.Now().Add(statusDisplayDuration)
	m.state.logger.Println(text)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys
	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.currentView != ViewProjects {
			m.currentView = ViewProjects
			return m, nil
		}
		// Remote sessions: only close this session, don't stop shared state.
		if m.isRemote {
			m.state.logger.Println("SSH session quit (remote)")
			m.state.Unsubscribe(m.subscriberID)
			return m, tea.Quit
		}
		// Local TUI: stop all watchers and loops before exiting.
		m.state.Lock()
		for _, at := range m.state.activeTerminals {
			if at.Watcher != nil {
				at.Watcher.Stop()
			}
		}
		for _, ls := range m.state.loops {
			ls.Active = false
		}
		m.state.Unlock()
		m.state.Unsubscribe(m.subscriberID)
		return m, tea.Quit
	}

	// View-specific keys
	switch m.currentView {
	case ViewProjects:
		return m.handleProjectsKey(msg)
	case ViewStatus:
		return m.handleStatusKey(msg)
	}
	return m, nil
}

func (m Model) handleProjectsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Tab: toggle focus between project list and loop slots.
	if key.Matches(msg, m.keys.FocusSwitch) {
		return m.handleFocusSwitch()
	}

	// If focused on loop slots, delegate key handling.
	if m.focusedPanel == FocusLoopSlots {
		return m.handleLoopSlotsKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		m.ensureCursorVisible(m.cursor * 3) // each project = 3 lines (name + detail + blank)

	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.state.projects)-1 {
			m.cursor++
		}
		m.ensureCursorVisible(m.cursor * 3)

	case key.Matches(msg, m.keys.Enter):
		if m.selectedProject() == nil {
			return m, nil
		}
		return m, m.launchClaudeCmd()

	case key.Matches(msg, m.keys.Open):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[o]", *p, valPath) {
			return m, nil
		}
		return m, m.openFolderCmd()

	case key.Matches(msg, m.keys.Tracker):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[p]", *p, valTrackerURL) {
			return m, nil
		}
		return m, m.openTrackerCmd()

	case key.Matches(msg, m.keys.Clarify):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[c]", *p, valPath, valTracker) {
			return m, nil
		}
		return m.startWithLabelCheck(PendingClarify, *p, tracker.RequiredLabels)

	case key.Matches(msg, m.keys.Loop):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		// Toggle loop off — no config check needed.
		m.state.Lock()
		if ls, ok := m.state.loops[p.ID]; ok && ls.Active {
			ls.Active = false
			m.state.NotifyAll()
			m.state.Unlock()
			m.setStatus(fmt.Sprintf("Loop stopped for %s", p.Name))
			return m, nil
		}
		m.state.Unlock()
		if !m.checkConfig("[l]", *p, valPath, valTracker, valWorktree) {
			return m, nil
		}
		return m.startWithLabelCheck(PendingLoop, *p, tracker.RequiredLabels)

	case key.Matches(msg, m.keys.Review):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[r]", *p, valPath, valTracker) {
			return m, nil
		}
		return m.startWithLabelCheck(PendingReview, *p, tracker.RequiredLabels)

	case key.Matches(msg, m.keys.Undeploy):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[u]", *p, valPath) {
			return m, nil
		}
		m.showUndeployConfirm(*p)
		return m, m.confirmForm.Init()

	case key.Matches(msg, m.keys.Status):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[s]", *p, valTracker) {
			return m, nil
		}
		m.focusedPanel = FocusProjects
		m.currentView = ViewStatus
		m.statusProjectID = p.ID
		m.statusIssues = nil
		m.statusCursor = 0
		m.statusLoading = true
		m.statusError = ""
		m.viewport.GotoTop()
		return m, m.loadIssuesCmd()

	case key.Matches(msg, m.keys.Add):
		m.setStatus(locale.T(locale.KeyAddProjectStub))

	case key.Matches(msg, m.keys.EditConfig):
		m.setStatus(locale.T(locale.KeyEditConfigStub))

	case key.Matches(msg, m.keys.Help):
		m.setStatus(locale.T(locale.KeyHelpStub))
	}

	return m, nil
}

func (m Model) handleStatusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.currentView = ViewProjects
		m.viewport.GotoTop()
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.statusCursor > 0 {
			m.statusCursor--
		}
		m.ensureCursorVisible(m.statusCursor + 3) // +3 for title + separator + blank line

	case key.Matches(msg, m.keys.Down):
		if m.statusCursor < len(m.statusIssues)-1 {
			m.statusCursor++
		}
		m.ensureCursorVisible(m.statusCursor + 3)

	case key.Matches(msg, m.keys.Confirm):
		// Validate before setting pendingOp.
		if m.statusCursor >= len(m.statusIssues) {
			return m, nil
		}
		issue := m.statusIssues[m.statusCursor]
		if issue.Status != tracker.StatusPendingConfirm {
			m.setStatus(fmt.Sprintf("Issue #%s is %s, not pending_confirm", issue.ID, issue.Status))
			return m, nil
		}
		project := m.findProject(m.statusProjectID)
		if project == nil {
			m.setStatus(fmt.Sprintf("project not found: %s", m.statusProjectID))
			return m, nil
		}
		if !m.checkConfig("[y]", *project, valTracker) {
			return m, nil
		}
		m.pendingOp = &PendingOp{
			Kind:         PendingConfirmIssue,
			ProjectID:    m.statusProjectID,
			ProjectIndex: m.statusCursor,
			Required:     tracker.RequiredLabels,
		}
		m.setStatus(locale.T(locale.KeyCheckingLabels))
		return m, m.checkLabelsCmd(m.statusProjectID, tracker.RequiredLabels)

	case key.Matches(msg, m.keys.Tracker):
		project := m.findProject(m.statusProjectID)
		if project == nil {
			return m, nil
		}
		if !m.checkConfig("[p]", *project, valTrackerURL) {
			return m, nil
		}
		return m, m.openIssueURLCmd()
	}

	return m, nil
}

// --- Focus panel: loop slot selection ---

func (m Model) handleFocusSwitch() (tea.Model, tea.Cmd) {
	if m.focusedPanel == FocusLoopSlots {
		m.focusedPanel = FocusProjects
		return m, nil
	}
	project := m.state.projects[m.cursor]
	m.state.RLock()
	keys := m.sortedSlotKeys(project.ID)
	m.state.RUnlock()
	if len(keys) == 0 {
		return m, nil
	}
	m.focusedPanel = FocusLoopSlots
	m.focusProjectID = project.ID
	m.loopCursor = 0
	return m, nil
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

	case key.Matches(msg, m.keys.Enter):
		return m.launchFocusClaudeCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.Open):
		return m.openSlotFolderCmd(keys[m.loopCursor])

	case key.Matches(msg, m.keys.Tracker):
		return m.openSlotIssueCmd(keys[m.loopCursor])
	}

	return m, nil
}

// launchableSlotStates defines which slot states allow manual Claude launch.
var launchableSlotStates = map[loop.SlotState]bool{
	loop.SlotCoding:        true,
	loop.SlotReviewing:     true,
	loop.SlotWaitingPRMerge: true,
	loop.SlotNeedsHuman:    true,
	loop.SlotError:         true,
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
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg)
		return LaunchResultMsg{
			ProjectID:   focusProjectID,
			TrackingKey: trackingKey,
			WorkDir:     wtPath,
			Result:      result,
			Err:         err,
		}
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

func (m Model) handleLaunchResult(msg LaunchResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Launch failed: %s", msg.Err))
		return m, nil
	}

	m.setStatus(fmt.Sprintf("Launched! %s", msg.Result.SwitchHint))

	// Resolve WorkDir before creating ActiveTerminal so it's always stored.
	workDir := msg.WorkDir
	if workDir == "" {
		project := m.findProject(msg.ProjectID)
		if project == nil {
			return m, nil
		}
		workDir = platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	}

	m.state.Lock()
	trackKey := msg.ProjectID
	if msg.TrackingKey != "" {
		trackKey = msg.TrackingKey
	}
	trackKey = m.nextTrackingKey(trackKey)
	m.state.logger.Printf("launch: key=%s (PID pending)", trackKey)

	at := &ActiveTerminal{
		LaunchResult:   msg.Result,
		WorkDir:        workDir,
		State:          watcher.StateUnknown,
		StateChangedAt: time.Now(),
	}
	m.state.activeTerminals[trackKey] = at
	// Snapshot tracked PIDs while still holding the lock.
	excludePIDs := m.trackedPIDs()
	m.state.NotifyAll()
	m.state.Unlock()

	return m, m.startWatcherDirCmdWithExcludes(trackKey, workDir, excludePIDs)
}

func (m Model) handleAgentEvent(msg AgentEventMsg) (tea.Model, tea.Cmd) {
	m.state.Lock()
	at, ok := m.state.activeTerminals[msg.ProjectID]
	if !ok {
		m.state.Unlock()
		return m, nil
	}

	// Process events and track the latest meaningful state.
	for _, ev := range msg.Events {
		if ev.Type != "assistant" {
			continue
		}

		oldState := at.State
		at.State = ev.State
		at.StateChangedAt = time.Now()

		// Clean up permission state on any transition out.
		if oldState == watcher.StatePermission && ev.State != watcher.StatePermission {
			at.PermissionMessage = ""
			deletePermissionSignal(at.SessionID)
		}

		if ev.State == watcher.StateWaiting {
			at.LastQuestion = ev.QuestionText
			// Notify on transition to waiting.
			if oldState != watcher.StateWaiting {
				projectName := m.projectName(msg.ProjectID)
				m.state.notifier.NotifyWaiting(msg.ProjectID, projectName, ev.QuestionText)
			}
		} else if ev.State == watcher.StateWorking {
			// User responded — reset notification cooldown.
			m.state.notifier.Reset(msg.ProjectID)
			at.LastQuestion = ""
		}
	}

	// Capture watcher ref before releasing lock.
	var w *watcher.Watcher
	if at.Watcher != nil {
		w = at.Watcher
	}
	m.state.NotifyAll()
	m.state.Unlock()

	// Continue watching.
	if w != nil {
		return m, watchNextCmd(msg.ProjectID, w)
	}
	return m, nil
}

func (m Model) launchClaudeCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	cfg := m.state.cfg.Terminal
	return func() tea.Msg {
		result, err := terminal.LaunchClaude(project, cfg)
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
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

const (
	sessionRetryInterval = 2 * time.Second
	sessionRetryMax      = 8  // 8 * 2s = 16s max wait
	logWaitWarnAfter     = 15 // log a warning after 15 * 2s = 30s, but keep waiting
)

// scanExistingSessionsCmd scans all projects for already-running Claude Code sessions at startup.
func (m Model) scanExistingSessionsCmd() tea.Cmd {
	type projectInfo struct {
		id   string
		path string
	}
	seen := make(map[string]bool)
	var projects []projectInfo
	for _, p := range m.state.projects {
		path := platform.ResolvePath(p.Path.Windows, p.Path.WSL)
		if path == "" {
			m.state.logger.Printf("session scan: skipping project %q (empty path)", p.ID)
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		projects = append(projects, projectInfo{id: p.ID, path: path})
	}

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			return existingSessionsMsg{Source: "startup"}
		}
		var entries []existingSessionEntry
		for _, p := range projects {
			sessions, err := watcher.FindActiveSessions(claudeHome, p.path)
			if err != nil || len(sessions) == 0 {
				continue
			}
			// Track ALL active sessions per project, not just the latest.
			for _, s := range sessions {
				logPath := watcher.LogFilePath(claudeHome, p.path, s.SessionID)
				entries = append(entries, existingSessionEntry{
					ProjectID: p.id,
					PID:       s.PID,
					SessionID: s.SessionID,
					WorkDir:   p.path,
					LogPath:   logPath,
				})
			}
		}
		return existingSessionsMsg{Source: "startup", Entries: entries}
	}
}

// startWatcherDirCmd starts session discovery for a given tracking key and work directory.
// Acquires RLock to snapshot tracked PIDs. Must NOT be called while holding a write lock.
func (m Model) startWatcherDirCmd(trackingKey, workDir string) tea.Cmd {
	// Snapshot already-tracked PIDs so we can exclude them when picking a session.
	m.state.RLock()
	excludePIDs := m.trackedPIDs()
	m.state.RUnlock()
	return m.startWatcherDirCmdWithExcludes(trackingKey, workDir, excludePIDs)
}

// startWatcherDirCmdWithExcludes starts session discovery with pre-computed exclude PIDs.
// Used when the caller already holds a lock and has snapshotted the PIDs.
func (m Model) startWatcherDirCmdWithExcludes(trackingKey, workDir string, excludePIDs map[int]bool) tea.Cmd {
	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			return WatcherErrorMsg{ProjectID: trackingKey, Err: err}
		}

		var candidates []watcher.SessionInfo
		for attempt := range sessionRetryMax {
			sessions, err := watcher.FindActiveSessions(claudeHome, workDir)
			if err != nil {
				return WatcherErrorMsg{ProjectID: trackingKey, Err: err}
			}
			// Filter out sessions already tracked by other ActiveTerminals.
			candidates = candidates[:0]
			for _, s := range sessions {
				if !excludePIDs[s.PID] {
					candidates = append(candidates, s)
				}
			}
			if len(candidates) > 0 {
				break
			}
			// If all sessions are tracked but some exist, a new one may appear soon.
			if attempt < sessionRetryMax-1 {
				time.Sleep(sessionRetryInterval)
			}
		}

		if len(candidates) == 0 {
			return sessionLostMsg{ProjectID: trackingKey, Text: "no active session found (waited 30s)"}
		}

		latest := candidates[0]
		for _, s := range candidates[1:] {
			if s.StartedAt > latest.StartedAt {
				latest = s
			}
		}

		logPath := watcher.LogFilePath(claudeHome, workDir, latest.SessionID)
		return sessionFoundMsg{ProjectID: trackingKey, PID: latest.PID, SessionID: latest.SessionID, LogPath: logPath}
	}
}

// sessionFoundMsg is sent when the session PID is discovered but JSONL may not exist yet.
type sessionFoundMsg struct {
	ProjectID string
	PID       int
	SessionID string
	LogPath   string
}

// existingSessionEntry represents a session found during startup scan.
type existingSessionEntry struct {
	ProjectID string
	PID       int
	SessionID string
	WorkDir   string
	LogPath   string
}

// nextTrackingKey returns a unique key for activeTerminals.
// First session uses baseKey as-is; subsequent ones get "#2", "#3", etc.
func (m Model) nextTrackingKey(baseKey string) string {
	if _, exists := m.state.activeTerminals[baseKey]; !exists {
		return baseKey
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s#%d", baseKey, i)
		if _, exists := m.state.activeTerminals[candidate]; !exists {
			return candidate
		}
	}
}

// trackedPIDs returns all PIDs currently tracked in activeTerminals.
func (m Model) trackedPIDs() map[int]bool {
	pids := make(map[int]bool, len(m.state.activeTerminals))
	for _, at := range m.state.activeTerminals {
		if at.SessionPID != 0 {
			pids[at.SessionPID] = true
		}
	}
	return pids
}

// existingSessionsMsg carries results of scanning for already-running sessions.
// Source distinguishes "startup" (initial scan) from "periodic" (tick-driven scan).
type existingSessionsMsg struct {
	Source  string // "startup" or "periodic"
	Entries []existingSessionEntry
}

// waitForLogCmd phase 2: wait for the JSONL file to be created, then start the watcher.
// Re-reads {pid}.json each iteration to detect /resume session switches.
func waitForLogCmd(projectID string, pid int, sessionID, logPath, workDir string, logger *log.Logger) tea.Cmd {
	return func() tea.Msg {
		logger.Printf("waitForLog: key=%s pid=%d sessionID=%s path=%s", projectID, pid, sessionID, logPath)

		claudeHome, _ := watcher.ClaudeHome() // best-effort; empty means skip re-check
		warned := false

		for attempt := 0; ; attempt++ {
			if !watcher.IsProcessAlive(pid) {
				logger.Printf("waitForLog: key=%s pid=%d died at attempt %d", projectID, pid, attempt)
				return sessionLostMsg{ProjectID: projectID, Text: "session ended before log created"}
			}

			// Re-check session file for /resume detection.
			// On switch, return sessionFoundMsg to let update loop sync AT.SessionID first.
			if claudeHome != "" && workDir != "" {
				if info, err := watcher.ReadSessionByPID(claudeHome, pid); err == nil {
					if info.SessionID != sessionID {
						newLogPath := watcher.LogFilePath(claudeHome, workDir, info.SessionID)
						logger.Printf("waitForLog: key=%s session switched %s → %s",
							projectID, sessionID, info.SessionID)
						return sessionFoundMsg{
							ProjectID: projectID,
							PID:       pid,
							SessionID: info.SessionID,
							LogPath:   newLogPath,
						}
					}
				} else {
					logger.Printf("waitForLog: key=%s ReadSessionByPID failed: %v", projectID, err)
				}
			} else {
				logger.Printf("waitForLog: key=%s skip re-check (claudeHome=%q workDir=%q)", projectID, claudeHome, workDir)
			}

			if _, err := os.Stat(logPath); err == nil {
				logger.Printf("waitForLog: key=%s file found at attempt %d (sessionID=%s)", projectID, attempt, sessionID)
				w, err := watcher.New(projectID, logPath)
				if err != nil {
					logger.Printf("waitForLog: key=%s watcher creation failed: %v", projectID, err)
					return WatcherErrorMsg{ProjectID: projectID, Err: err}
				}
				return watcherReadyMsg{ProjectID: projectID, SessionID: sessionID, Watcher: w, LogPath: logPath}
			}

			if !warned && attempt >= logWaitWarnAfter {
				logger.Printf("waitForLog: key=%s still waiting after %d attempts, PID %d alive — continuing",
					projectID, attempt, pid)
				warned = true
			}
			time.Sleep(sessionRetryInterval)
		}
	}
}

// watcherReadyMsg is an internal message to attach a watcher to an ActiveTerminal.
type watcherReadyMsg struct {
	ProjectID string
	SessionID string
	Watcher   *watcher.Watcher
	LogPath   string
}

func (m *Model) checkSessionLiveness() []tea.Cmd {
	m.state.Lock()
	now := time.Now()
	if now.Sub(m.state.lastLivenessCheck) < livenessCheckInterval {
		m.state.Unlock()
		return nil
	}
	m.state.lastLivenessCheck = now

	claudeHome, _ := watcher.ClaudeHome() // best-effort; empty means skip /resume detection

	var cmds []tea.Cmd
	changed := false

	for projectID, at := range m.state.activeTerminals {
		// Clean up ended sessions after display duration.
		if at.State == watcher.StateEnded {
			if now.Sub(at.StateChangedAt) >= endedDisplayDuration {
				m.state.logger.Printf("session removed: key=%s", projectID)
				delete(m.state.activeTerminals, projectID)
				changed = true
			}
			continue
		}

		if at.SessionPID <= 0 {
			continue
		}
		if !watcher.IsProcessAlive(at.SessionPID) {
			m.state.logger.Printf("session PID %d ended: %s", at.SessionPID, projectID)
			at.State = watcher.StateEnded
			at.StateChangedAt = now
			at.LastQuestion = ""
			at.PermissionMessage = ""
			deletePermissionSignal(at.SessionID)
			if at.Watcher != nil {
				at.Watcher.Stop()
			}
			changed = true
			continue
		}

		// /resume detection: re-read {pid}.json and check if sessionId changed.
		if claudeHome != "" && at.SessionID != "" && at.WorkDir != "" {
			if info, err := watcher.ReadSessionByPID(claudeHome, at.SessionPID); err == nil {
				if info.SessionID != at.SessionID {
					newLogPath := watcher.LogFilePath(claudeHome, at.WorkDir, info.SessionID)
					m.state.logger.Printf("session switch detected: key=%s old=%s new=%s",
						projectID, at.SessionID, info.SessionID)

					// Stop old watcher and restart for new session.
					if at.Watcher != nil {
						at.Watcher.Stop()
						at.Watcher = nil
					}
					at.SessionID = info.SessionID
					at.State = watcher.StateUnknown
					at.StateChangedAt = now
					at.LastQuestion = ""
					changed = true

					cmds = append(cmds, waitForLogCmd(
						projectID, at.SessionPID, info.SessionID, newLogPath, at.WorkDir, m.state.logger))
				}
			} else {
				m.state.logger.Printf("liveness: key=%s ReadSessionByPID(%d) failed: %v", projectID, at.SessionPID, err)
			}
		} else if claudeHome != "" {
			m.state.logger.Printf("liveness: key=%s skip resume check (sessionID=%q workDir=%q)", projectID, at.SessionID, at.WorkDir)
		}
	}

	if changed {
		m.state.NotifyAll()
	}
	m.state.Unlock()
	return cmds
}

// checkNewSessions checks if sessionScanInterval has elapsed and, if so, returns a tea.Cmd
// that scans for externally-launched Claude Code sessions not yet tracked in activeTerminals.
// Follows the same tick + interval + lock pattern as checkSessionLiveness.
func (m *Model) checkNewSessions() tea.Cmd {
	m.state.Lock()
	now := time.Now()
	if now.Sub(m.state.lastSessionScan) < sessionScanInterval {
		m.state.Unlock()
		return nil
	}
	m.state.lastSessionScan = now
	trackedPIDs := m.trackedPIDs()
	m.state.Unlock()

	// Build project list (same logic as scanExistingSessionsCmd).
	type projectInfo struct {
		id   string
		path string
	}
	seen := make(map[string]bool)
	var projects []projectInfo
	m.state.RLock()
	for _, p := range m.state.projects {
		path := platform.ResolvePath(p.Path.Windows, p.Path.WSL)
		if path == "" {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		projects = append(projects, projectInfo{id: p.ID, path: path})
	}
	logger := m.state.logger
	m.state.RUnlock()

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			logger.Printf("periodic session scan: ClaudeHome error: %v", err)
			return nil
		}
		var entries []existingSessionEntry
		for _, p := range projects {
			sessions, err := watcher.FindActiveSessions(claudeHome, p.path)
			if err != nil || len(sessions) == 0 {
				continue
			}
			for _, s := range sessions {
				if trackedPIDs[s.PID] {
					continue
				}
				logPath := watcher.LogFilePath(claudeHome, p.path, s.SessionID)
				entries = append(entries, existingSessionEntry{
					ProjectID: p.id,
					PID:       s.PID,
					SessionID: s.SessionID,
					WorkDir:   p.path,
					LogPath:   logPath,
				})
			}
		}
		if len(entries) == 0 {
			return nil
		}
		logger.Printf("periodic session scan: found %d new session(s)", len(entries))
		return existingSessionsMsg{Source: "periodic", Entries: entries}
	}
}

func (m Model) findProject(id string) *config.ProjectConfig {
	for i := range m.state.projects {
		if m.state.projects[i].ID == id {
			return &m.state.projects[i]
		}
	}
	return nil
}

// signalDir returns the path to the permission signal directory (~/.zpit/signals/).
func signalDir() string {
	base, err := config.BaseDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "signals")
}

// deletePermissionSignal removes the permission signal file for the given session ID.
func deletePermissionSignal(sessionID string) {
	if sessionID == "" {
		return
	}
	dir := signalDir()
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, "permission-"+sessionID+".json"))
}

// permissionSignal is the parsed content of a permission signal file.
type permissionSignal struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// checkPermissionSignals scans ~/.zpit/signals/ for permission signal files
// and updates matching ActiveTerminals to StatePermission.
func (m *Model) checkPermissionSignals() {
	m.state.Lock()
	now := time.Now()
	if now.Sub(m.state.lastPermissionCheck) < permissionCheckInterval {
		m.state.Unlock()
		return
	}
	m.state.lastPermissionCheck = now
	m.state.Unlock()

	dir := signalDir()
	if dir == "" {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory may not exist yet
	}

	// Collect parsed signals from filesystem (no lock needed for I/O).
	type parsedSignal struct {
		sig  permissionSignal
		name string
	}
	var signals []parsedSignal
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "permission-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var sig permissionSignal
		if err := json.Unmarshal(data, &sig); err != nil || sig.SessionID == "" {
			continue
		}
		signals = append(signals, parsedSignal{sig: sig, name: name})
	}

	if len(signals) == 0 {
		return
	}

	// Match signals to active terminals under lock.
	m.state.Lock()
	changed := false
	var staleFiles []string
	for _, ps := range signals {
		matched := false
		for projectID, at := range m.state.activeTerminals {
			if at.SessionID != ps.sig.SessionID {
				continue
			}
			matched = true
			if at.State == watcher.StatePermission {
				break // already in permission state
			}
			m.state.logger.Printf("permission detected: key=%s session=%s msg=%q", projectID, ps.sig.SessionID, ps.sig.Message)
			at.State = watcher.StatePermission
			at.PermissionMessage = ps.sig.Message
			at.StateChangedAt = now
			changed = true
			projectName := m.projectName(projectID)
			m.state.notifier.NotifyWaiting(projectID, projectName, ps.sig.Message)
			break
		}
		if !matched {
			staleFiles = append(staleFiles, ps.name)
		}
	}
	if changed {
		m.state.NotifyAll()
	}
	m.state.Unlock()

	// Clean up stale signal files outside lock.
	for _, name := range staleFiles {
		os.Remove(filepath.Join(dir, name))
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func watchNextCmd(projectID string, w *watcher.Watcher) tea.Cmd {
	return func() tea.Msg {
		events, err := w.WatchOnce()
		if err != nil {
			return WatcherErrorMsg{ProjectID: projectID, Err: err}
		}
		if events == nil {
			return nil
		}
		return AgentEventMsg{ProjectID: projectID, Events: events}
	}
}

// launchClarifierCmd opens a new terminal with claude --agent clarifier.
func (m Model) launchClarifierCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	cfg := m.state.cfg.Terminal
	return func() tea.Msg {
		result, err := terminal.LaunchClaude(project, cfg, "--agent", "clarifier")
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
}

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
		return m.deployAndLaunchClarifier()
	}
}

// deployAndLaunchClarifier deploys clarifier.md to the project and launches it.
func (m Model) deployAndLaunchClarifier() tea.Cmd {
	project := m.state.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	clarifierMD := injectLangInstruction(m.state.clarifierMD)
	cfg := m.state.cfg.Terminal

	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD
	hookScripts := m.state.hookScripts
	hookMode := project.HookMode
	var trackerDocContent string
	if provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]; ok {
		trackerDocContent = tracker.BuildTrackerDoc(provider.Type, provider.URL, project.Repo, provider.TokenEnv, project.BaseBranch)
	}

	return func() tea.Msg {
		// Deploy hooks
		if err := worktree.DeployHooksToProject(projectPath, hookMode, hookScripts); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Hook deploy failed: %s", err)}
		}

		// Deploy agent
		agentDir := filepath.Join(projectPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		if err := os.WriteFile(filepath.Join(agentDir, "clarifier.md"), clarifierMD, 0o644); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		deployDocs(projectPath, trackerDocContent, agentGuidelines, codeConstructionPrinciples)

		// Launch
		result, err := terminal.LaunchClaude(project, cfg, "--agent", "clarifier")
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
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

// zpitIgnoreRules are .gitignore patterns for Zpit auto-deployed files.
var zpitIgnoreRules = []string{
	".claude/agents/",
	".claude/docs/",
	".claude/hooks/",
	".claude/settings.local.json",
}

// ensureGitignore appends missing Zpit gitignore rules to a project's .gitignore.
func ensureGitignore(projectPath string) {
	gitignorePath := filepath.Join(projectPath, ".gitignore")

	content, _ := os.ReadFile(gitignorePath)
	existing := make(map[string]bool)
	for _, line := range strings.Split(string(content), "\n") {
		existing[strings.TrimSpace(line)] = true
	}

	var missing []string
	for _, rule := range zpitIgnoreRules {
		if !existing[rule] {
			missing = append(missing, rule)
		}
	}
	if len(missing) == 0 {
		return
	}

	var buf strings.Builder
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		buf.WriteByte('\n')
	}
	buf.WriteString("\n# Zpit auto-deploy\n")
	for _, rule := range missing {
		buf.WriteString(rule)
		buf.WriteByte('\n')
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(buf.String())
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
		m.state.NotifyAll()
		m.state.Unlock()
		m.setStatus(fmt.Sprintf("Loop started for %s", project.Name))
		return m, tea.Batch(
			m.loopCleanupMergedCmd(project.ID),
			m.loopScanOpenPRsCmd(project.ID),
			m.loopPollCmd(project.ID),
		)

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

// launchReviewerCmd opens a new terminal with claude --agent reviewer.
func (m Model) launchReviewerCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	cfg := m.state.cfg.Terminal
	return func() tea.Msg {
		result, err := terminal.LaunchClaude(project, cfg, "--agent", "reviewer")
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
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
		return m.deployAndLaunchReviewer()
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

// undeployFiles removes all Zpit-deployed directories from a project's .claude/ directory.
func undeployFiles(projectPath string) int {
	claudeDir := filepath.Join(projectPath, ".claude")
	removed := 0

	for _, dir := range []string{"agents", "docs", "hooks"} {
		target := filepath.Join(claudeDir, dir)
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			os.RemoveAll(target)
			removed++
		}
	}

	return removed
}

// deployAndLaunchReviewer deploys reviewer.md to the project and launches it.
func (m Model) deployAndLaunchReviewer() tea.Cmd {
	project := m.state.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	reviewerMD := injectLangInstruction(m.state.reviewerMD)
	cfg := m.state.cfg.Terminal
	agentGuidelines := m.state.agentGuidelinesMD
	codeConstructionPrinciples := m.state.codeConstructionPrinciplesMD
	hookScripts := m.state.hookScripts
	hookMode := project.HookMode
	var trackerDocContent string
	if provider, ok := m.state.cfg.Providers.Tracker[project.Tracker]; ok {
		trackerDocContent = tracker.BuildTrackerDoc(provider.Type, provider.URL, project.Repo, provider.TokenEnv, project.BaseBranch)
	}

	return func() tea.Msg {
		// Deploy hooks
		if err := worktree.DeployHooksToProject(projectPath, hookMode, hookScripts); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Hook deploy failed: %s", err)}
		}

		agentDir := filepath.Join(projectPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		if err := os.WriteFile(filepath.Join(agentDir, "reviewer.md"), reviewerMD, 0o644); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		deployDocs(projectPath, trackerDocContent, agentGuidelines, codeConstructionPrinciples)

		result, err := terminal.LaunchClaude(project, cfg, "--agent", "reviewer")
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
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

