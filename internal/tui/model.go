package tui

import (
	"context"
	"errors"
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

	"github.com/zac15987/zpit/internal/broker"
	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
	"github.com/zac15987/zpit/internal/worktree"
)

// errChannelClosed is a sentinel error returned by channelReadNextCmd when the
// EventBus channel is closed (normal shutdown after Unsubscribe).
var errChannelClosed = errors.New("channel closed")

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
	ViewChannel
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

	// Channel view state
	channelProjectID string

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
// Runs session scan and provider validation on the AppState.
func RunServerInit(state *AppState) {
	seenMissing := make(map[string]bool)
	var missingProviders []string

	for _, project := range state.projects {
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

	seenMissing := make(map[string]bool)
	var missingProviders []string

	for _, project := range m.state.projects {
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
	case ViewChannel:
		header = m.renderChannelHeader()
		footer = m.renderChannelFooter()
		m.viewport.SetContent(m.renderChannelScrollable())
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
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

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
		return m.handleExistingSessions(msg)

	case sessionFoundMsg:
		return m.handleSessionFound(msg)

	case watcherReadyMsg:
		return m.handleWatcherReady(msg)

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
		return m.handleSessionLost(msg)

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

	// Channel event messages
	case ChannelEventMsg:
		m.state.AppendChannelEvent(msg.ProjectID, msg.Event)
		m.state.logger.Printf("channel: event received project=%s type=%s", msg.ProjectID, msg.Event.Type)

		// Auto-scroll: if viewing channel and at bottom, follow new content (any project).
		autoScroll := m.currentView == ViewChannel && m.viewport.AtBottom()

		// Re-issue read cmd for the next event.
		m.state.RLock()
		ch, ok := m.state.channelSubs[msg.ProjectID]
		m.state.RUnlock()
		var nextCmd tea.Cmd
		if ok && ch != nil {
			nextCmd = channelReadNextCmd(msg.ProjectID, ch, m.state.logger)
		}

		if autoScroll {
			m.viewport.GotoBottom()
		}
		return m, nextCmd

	case ChannelSubscribedMsg:
		if msg.Err != nil && !errors.Is(msg.Err, errChannelClosed) {
			m.state.logger.Printf("channel: subscribe error project=%s err=%v", msg.ProjectID, msg.Err)
		}
		return m, nil

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
	case ViewChannel:
		bg = m.viewChannel()
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
		// Capture channel subscriptions for cleanup outside lock.
		capturedSubs := make(map[string]<-chan broker.Event, len(m.state.channelSubs))
		for pid, ch := range m.state.channelSubs {
			capturedSubs[pid] = ch
			delete(m.state.channelSubs, pid)
		}
		// Capture and nil-out broker reference under lock to prevent data race.
		brokerToClose := m.state.broker
		m.state.broker = nil
		m.state.Unlock()
		// Unsubscribe all channel listeners so SSE handlers return immediately.
		if brokerToClose != nil {
			for pid, ch := range capturedSubs {
				brokerToClose.Events().Unsubscribe(pid, ch)
			}
			brokerToClose.Close()
		}
		m.state.Unsubscribe(m.subscriberID)
		return m, tea.Quit
	}

	// View-specific keys
	switch m.currentView {
	case ViewProjects:
		return m.handleProjectsKey(msg)
	case ViewStatus:
		return m.handleStatusKey(msg)
	case ViewChannel:
		return m.handleChannelKey(msg)
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

	case key.Matches(msg, m.keys.PageUp):
		m.viewport.PageUp()

	case key.Matches(msg, m.keys.PageDown):
		m.viewport.PageDown()

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
			ls.Slots = make(map[string]*loop.Slot)
			// Clean up channel subscriptions under lock (own project + listen projects).
			capturedSubs := make(map[string]<-chan broker.Event)
			if ch, exists := m.state.channelSubs[p.ID]; exists {
				capturedSubs[p.ID] = ch
				delete(m.state.channelSubs, p.ID)
			}
			for _, lp := range p.ChannelListen {
				if ch, exists := m.state.channelSubs[lp]; exists {
					capturedSubs[lp] = ch
					delete(m.state.channelSubs, lp)
				}
			}
			m.state.NotifyAll()
			m.state.Unlock()
			// Unsubscribe channels outside lock (thread-safe).
			// Broker lifecycle is tied to the Zpit process, not individual loops.
			if m.state.broker != nil {
				bus := m.state.broker.Events()
				for pid, ch := range capturedSubs {
					bus.Unsubscribe(pid, ch)
				}
			}
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

	case key.Matches(msg, m.keys.Channel):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		m.focusedPanel = FocusProjects
		m.currentView = ViewChannel
		m.channelProjectID = p.ID
		m.viewport.GotoTop()
		return m, nil

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

	case key.Matches(msg, m.keys.PageUp):
		m.viewport.PageUp()

	case key.Matches(msg, m.keys.PageDown):
		m.viewport.PageDown()

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

func (m Model) handleChannelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.currentView = ViewProjects
		m.viewport.GotoTop()
		return m, nil

	case key.Matches(msg, m.keys.Up):
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case key.Matches(msg, m.keys.Down):
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case key.Matches(msg, m.keys.PageUp):
		m.viewport.PageUp()
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		m.viewport.PageDown()
		return m, nil
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
	// Check if channel subscription already exists while holding the lock.
	_, channelSubscribed := m.state.channelSubs[msg.ProjectID]
	// Snapshot listen project subscription status under lock.
	var unsubscribedListenProjects []string
	if !channelSubscribed {
		project := m.findProject(msg.ProjectID)
		if project != nil && project.ChannelEnabled && m.state.broker != nil {
			for _, lp := range project.ChannelListen {
				if _, exists := m.state.channelSubs[lp]; !exists && lp != project.ID {
					unsubscribedListenProjects = append(unsubscribedListenProjects, lp)
				}
			}
		}
	}
	m.state.NotifyAll()
	m.state.Unlock()

	cmds := []tea.Cmd{m.startWatcherDirCmdWithExcludes(trackKey, workDir, excludePIDs)}

	// Subscribe to broker EventBus if channel_enabled and not already subscribed.
	if !channelSubscribed {
		project := m.findProject(msg.ProjectID)
		if project != nil && project.ChannelEnabled && m.state.broker != nil {
			cmds = append(cmds, m.channelSubscribeCmd(msg.ProjectID))
			for _, lp := range unsubscribedListenProjects {
				cmds = append(cmds, m.channelSubscribeCmd(lp))
			}
		}
	}

	return m, tea.Batch(cmds...)
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
			if err := writeMCPConfig(projectPath, brokerAddr, project.ID, "0", zpitBin, channelListen); err != nil {
				logger.Printf("enter: failed to write .mcp.json for project=%s: %v", project.ID, err)
			} else {
				logger.Printf("enter: wrote .mcp.json to %s for project=%s", projectPath, project.ID)
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

func (m Model) findProject(id string) *config.ProjectConfig {
	for i := range m.state.projects {
		if m.state.projects[i].ID == id {
			return &m.state.projects[i]
		}
	}
	return nil
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// launchClarifierCmd opens a new terminal with claude --agent clarifier.
func (m Model) launchClarifierCmd() tea.Cmd {
	project := m.state.projects[m.cursor]
	cfg := m.state.cfg.Terminal
	return func() tea.Msg {
		args := []string{"--agent", "clarifier"}
		if project.ChannelEnabled {
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
			if err := writeMCPConfig(projectPath, brokerAddr, project.ID, "0", zpitBin, channelListen); err != nil {
				logger.Printf("%s: failed to write .mcp.json for project=%s: %v", agentName, project.ID, err)
			} else {
				logger.Printf("%s: wrote .mcp.json to %s for project=%s", agentName, projectPath, project.ID)
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
		args := []string{"--agent", "reviewer"}
		if project.ChannelEnabled {
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

// channelSubscribeCmd subscribes to the broker's EventBus for the given project.
// Stores the subscription channel in AppState.channelSubs and returns a cmd that
// reads the first event. Subsequent reads are triggered by channelReadNextCmd.
func (m Model) channelSubscribeCmd(projectID string) tea.Cmd {
	logger := m.state.logger
	brokerRef := m.state.broker
	if brokerRef == nil {
		return func() tea.Msg {
			return ChannelSubscribedMsg{ProjectID: projectID, Err: fmt.Errorf("broker not available")}
		}
	}
	bus := brokerRef.Events()
	ch := bus.Subscribe(projectID)

	m.state.Lock()
	m.state.channelSubs[projectID] = ch
	m.state.Unlock()

	logger.Printf("channel: subscribed to EventBus for project=%s", projectID)
	return channelReadNextCmd(projectID, ch, logger)
}

// channelReadNextCmd returns a tea.Cmd that blocks until the next event arrives on ch.
func channelReadNextCmd(projectID string, ch <-chan broker.Event, logger *log.Logger) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			logger.Printf("channel: EventBus channel closed for project=%s", projectID)
			return ChannelSubscribedMsg{ProjectID: projectID, Err: errChannelClosed}
		}
		return ChannelEventMsg{ProjectID: projectID, Event: event}
	}
}
