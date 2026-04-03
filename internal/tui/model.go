package tui

import (
	"errors"
	"fmt"
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
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
)

const statusDisplayDuration = 5 * time.Second

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
		return m.handleTick()

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
		return m.handleLabelCheckResult(msg)

	case LabelsEnsuredMsg:
		return m.handleLabelsEnsured(msg)

	case IssuesLoadedMsg:
		return m.handleIssuesLoaded(msg)

	case IssueConfirmedMsg:
		return m.handleIssueConfirmed(msg)

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
		return m.handleChannelEvent(msg)

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

func (m Model) handleLaunchResult(msg LaunchResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Launch failed: %s", msg.Err))
		return m, nil
	}

	m.setStatus(fmt.Sprintf("Launched! %s", msg.Result.SwitchHint))

	// Log and display any non-fatal warnings (e.g. WT profile resolution failures).
	for _, w := range msg.Result.Warnings {
		m.state.logger.Printf("launch warning: project=%s %s", msg.ProjectID, w)
		m.setStatus(fmt.Sprintf("Warning: %s", w))
	}

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

func (m Model) findProject(id string) *config.ProjectConfig {
	for i := range m.state.projects {
		if m.state.projects[i].ID == id {
			return &m.state.projects[i]
		}
	}
	return nil
}


