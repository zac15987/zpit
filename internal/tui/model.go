package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
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
	"github.com/zac15987/zpit/internal/sessionsync"
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
	ViewEditConfig
	ViewGitStatus
	ViewHistory
)

// EditConfigSub represents the sub-view within the edit config screen.
type EditConfigSub int

const (
	EditConfigMenu       EditConfigSub = iota // main 3-option menu
	EditConfigListenList                       // channel_listen multi-select
)

// editConfigListenItem represents one row in the channel_listen multi-select.
type editConfigListenItem struct {
	Key     string // project ID or "_global"
	Name    string // display name
	Checked bool
}

// FocusedPanel indicates which panel has keyboard focus in ViewProjects.
type FocusedPanel int

const (
	FocusProjects  FocusedPanel = iota
	FocusTerminals
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

	// Git status view state
	spinner      spinner.Model
	gitOp        string   // "" | "fetch" | "pull" | "refresh"
	gitData      *GitData // loaded branch + graph data, nil when loading or error
	gitProjectID string   // project ID currently shown in git status view
	gitError     string   // first-line error message, cleared on successful load

	// Edit config sub-menu state
	editConfigProjectID    string                // project being edited
	editConfigSub          EditConfigSub         // current sub-view
	editConfigListenCursor int                   // cursor for channel_listen list
	editConfigListenItems  []editConfigListenItem // items in multi-select

	// History (Session Browser) view state
	historyFolders             []sessionsync.FolderInfo
	historySessions            []sessionsync.SessionInfo
	historyActiveIDs           []string         // session IDs alive in current drilled folder
	historyActivePIDsByFolder  map[string]bool  // folder name → has-active-PID flag
	historyFolderCursor        int              // 0 = "[+] Import bundle..." pseudo-row
	historySessionCursor       int              // index into historySessions
	historySelected            map[string]bool  // session ID → selected
	historyDrilledFolder       string           // empty when in folder list; folder name when in session list
	historyExportIncludeMemory bool
	historyExportOutputPath    string
	historyExportStep          int              // 0=hidden, 1=active warning, 2=confirm modal, 3=running
	historyActivePending       []string         // active session IDs detected in current export selection
	historyExportAllPending    bool             // set by [E] in folder list, consumed when sessions scan completes
	historyExportFlowActive    bool             // user is in the middle of an export wizard
	historyImportBundlePath    string
	historyImportManifest      *sessionsync.Manifest
	historyImportSelection     map[string]bool
	historyImportDestPath      string
	historyImportSessionCursor int
	historyImportStep          int              // 0=path entry, 1=preview, 2=dest entry, 3=final preview, 4=running, 5=summary
	// Destination autocomplete suggestions for import wizard step 2.
	// Populated from m.state.projects via platform.ResolvePath when entering
	// step 2. The cursor selects between the freeform textinput (-1) and one
	// of the suggestion entries (0..len-1). Free-form input is always accepted.
	historyImportDestSuggestions      []string
	historyImportDestSuggestionCursor int
	historyImportFlowActive    bool             // user is in the middle of an import wizard
	historyCollisionQueue      []string         // pending session-collision IDs
	historyCollisionMemory     bool             // memory dir collision pending
	historyCollisionDecisions  map[string]sessionsync.CollisionDecision
	historyMemoryDecision      sessionsync.CollisionDecision
	historyImportResult        *sessionsync.UnpackResult

	// textinput widgets for History import/export path entry
	historyImportPathInput textinput.Model
	historyExportPathInput textinput.Model
	historyImportDestInput textinput.Model

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
	termCursor     int
	focusProjectID string

	// Viewport for scrollable content (used by ViewStatus/Channel/EditConfig/GitStatus).
	viewport viewport.Model

	// Dock layout viewports (ViewProjects only). Each panel is independently scrollable.
	projectsVP  viewport.Model
	terminalsVP viewport.Model
	loopVP      viewport.Model
	hotkeysVP   viewport.Model

	// Per-panel cumulative line offsets, rebuilt each sync; used by
	// ensureCursorInPanel for variable-stride cursors (terminals/loop).
	termLineStarts []int
	loopLineStarts []int

	// Last computed dock rects — used by mouse-wheel hit-testing.
	lastDockRects dockRects

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
	mkVP := func() viewport.Model {
		v := viewport.New(0, 0)
		v.MouseWheelEnabled = true
		v.MouseWheelDelta = 3
		v.KeyMap = viewport.KeyMap{} // disable all keyboard bindings — we handle keys ourselves
		return v
	}
	vp := mkVP()
	projectsVP := mkVP()
	terminalsVP := mkVP()
	loopVP := mkVP()
	hotkeysVP := mkVP()

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(colorWorking)

	id, ch := appState.Subscribe()

	return Model{
		state:        appState,
		isRemote:     isRemote,
		keys:         DefaultKeyMap(),
		currentView:  ViewProjects,
		spinner:      sp,
		viewport:     vp,
		projectsVP:   projectsVP,
		terminalsVP:  terminalsVP,
		loopVP:       loopVP,
		hotkeysVP:    hotkeysVP,
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
	cmds := []tea.Cmd{tickCmd(), m.waitForStateRefresh(), tea.SetWindowTitle("Zpit")}

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
		contentH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
		if contentH < 1 {
			contentH = 1
		}
		m.layoutDockPanels(m.width, contentH)
		// For ViewProjects, width/height are applied per-panel in layoutDockPanels;
		// the legacy m.viewport stays idle. Return before the shared width/height block.
		return
	case ViewStatus:
		header = m.renderStatusHeader()
		footer = m.renderStatusFooter()
		m.viewport.SetContent(m.renderStatusScrollable())
	case ViewChannel:
		header = m.renderChannelHeader()
		footer = m.renderChannelFooter()
		m.viewport.SetContent(m.renderChannelScrollable())
	case ViewEditConfig:
		header = m.renderEditConfigHeader()
		footer = m.renderEditConfigFooter()
		m.viewport.SetContent(m.renderEditConfigScrollable())
	case ViewGitStatus:
		header = m.renderGitStatusHeader()
		footer = m.renderGitStatusFooter()
		m.viewport.SetContent(m.renderGitStatusScrollable())
	case ViewHistory:
		header = m.renderHistoryHeader()
		footer = m.renderHistoryFooter()
		m.viewport.SetContent(m.renderHistoryScrollable())
	}
	h := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if h < 1 {
		h = 1
	}
	m.viewport.Width = m.width
	m.viewport.Height = h
}

// ensureCursorVisible adjusts the legacy viewport offset so the given line is on screen.
// Used by views that still share m.viewport (Status/Channel/EditConfig/GitStatus).
func (m *Model) ensureCursorVisible(cursorLine int) {
	m.ensureCursorInPanel(&m.viewport, cursorLine)
}

// ensureCursorInPanel adjusts a specific panel viewport's YOffset so cursorLine is visible.
// Used by dock panels (projectsVP, terminalsVP, loopVP) with per-panel cursor tracking.
func (m *Model) ensureCursorInPanel(vp *viewport.Model, cursorLine int) {
	if cursorLine < vp.YOffset {
		vp.SetYOffset(cursorLine)
	} else if cursorLine >= vp.YOffset+vp.Height {
		vp.SetYOffset(cursorLine - vp.Height + 1)
	}
}

// focusedVP returns a pointer to the viewport owned by the currently focused dock panel.
// Falls back to projectsVP for unknown focus states.
func (m *Model) focusedVP() *viewport.Model {
	switch m.focusedPanel {
	case FocusTerminals:
		return &m.terminalsVP
	case FocusLoopSlots:
		return &m.loopVP
	default:
		return &m.projectsVP
	}
}

// hitTestDockPanel returns a pointer to the viewport under the given terminal
// coordinates (in screen space), or nil if the point lies outside any dock panel.
// Rect coordinates are content-relative; this offsets the mouse Y by the rendered
// header height so the test uses the same frame as m.lastDockRects.
func (m *Model) hitTestDockPanel(x, y int) *viewport.Model {
	headerH := lipgloss.Height(m.renderProjectsHeader())
	ry := y - headerH
	if ry < 0 {
		return nil
	}
	inside := func(r panelRect) bool {
		if r.w == 0 || r.h == 0 {
			return false
		}
		return x >= r.x && x < r.x+r.w && ry >= r.y && ry < r.y+r.h
	}
	r := m.lastDockRects
	switch {
	case inside(r.projects):
		return &m.projectsVP
	case inside(r.terminals):
		return &m.terminalsVP
	case inside(r.loop):
		return &m.loopVP
	case inside(r.hotkeys):
		return &m.hotkeysVP
	}
	return nil
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
		if m.currentView == ViewProjects {
			if vp := m.hitTestDockPanel(msg.X, msg.Y); vp != nil {
				*vp, cmd = vp.Update(msg)
				return m, cmd
			}
			// No hit → fall through to projectsVP as a sensible default.
			m.projectsVP, cmd = m.projectsVP.Update(msg)
			return m, cmd
		}
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

	case KillTerminalMsg:
		return m.handleKillTerminal(msg)

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
	case LoopAutoMergeMsg:
		return m.handleLoopAutoMerge(msg)
	case LoopCleanupMsg:
		return m.handleLoopCleanup(msg)
	case LoopOpenPRsMsg:
		return m.handleLoopOpenPRs(msg)
	case loopPollTickMsg:
		return m.handleLoopPollTick(msg)
	case loopPRPollTickMsg:
		return m.handleLoopPRPollTick(msg)
	case LoopLabelPollMsg:
		return m.handleLoopLabelPoll(msg)
	case loopLabelPollTickMsg:
		return m.handleLoopLabelPollTick(msg)

	// Channel event messages
	case ChannelEventMsg:
		return m.handleChannelEvent(msg)

	case ChannelSubscribedMsg:
		if msg.Err != nil && !errors.Is(msg.Err, errChannelClosed) {
			m.state.logger.Printf("channel: subscribe error project=%s err=%v", msg.ProjectID, msg.Err)
		}
		return m, nil

	// History (Session Browser) messages
	case HistoryFoldersScannedMsg:
		return m.handleHistoryFoldersScanned(msg)
	case HistorySessionsScannedMsg:
		model, cmd := m.handleHistorySessionsScanned(msg)
		if mdl, ok := model.(Model); ok && mdl.historyExportAllPending {
			// Pre-select all sessions and immediately enter export flow.
			if mdl.historySelected == nil {
				mdl.historySelected = make(map[string]bool)
			}
			for _, s := range mdl.historySessions {
				mdl.historySelected[s.SessionID] = true
			}
			mdl.historyExportAllPending = false
			return mdl.beginExportFlow()
		}
		return model, cmd
	case ExportCompletedMsg:
		return m.handleExportCompleted(msg)
	case ImportCompletedMsg:
		return m.handleImportCompleted(msg)
	case manifestLoadedMsg:
		return m.handleManifestLoaded(msg)
	case sessionCollisionsMsg:
		// T10 takes over collision dispatch fully (sessions.go's handler is a stub
		// that calls startImportRun which itself is a no-op stub — both are superseded here).
		return m.handleSessionCollisionsT10(msg)

	// Git status messages
	case GitDataLoadedMsg:
		m, cmd := m.onGitDataLoaded(msg)
		return m, cmd
	case GitFetchResultMsg:
		m, cmd := m.onGitFetchResult(msg)
		return m, cmd
	case GitPullResultMsg:
		m, cmd := m.onGitPullResult(msg)
		return m, cmd
	case spinner.TickMsg:
		m, cmd := m.handleSpinnerTick(msg)
		return m, cmd

	// Edit config messages
	case EditorFinishedMsg:
		return m.handleEditorFinished(msg)

	case ConfigReloadedMsg:
		return m.handleConfigReloaded(msg)

	case ChannelToggledMsg:
		// Channel toggle is handled synchronously in handleEditConfigMenuKey.
		// This msg type is reserved for future async toggle flows.
		return m, nil

	case ChannelListenUpdatedMsg:
		// Channel listen update is handled synchronously in handleEditConfigListenKey.
		// This msg type is reserved for future async update flows.
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
	case ViewEditConfig:
		bg = m.viewEditConfig()
	case ViewGitStatus:
		bg = m.viewGitStatus()
	case ViewHistory:
		bg = m.viewHistory()
	default:
		bg = "Unknown view"
	}
	if m.currentView == ViewHistory {
		var modal string
		if m.historyImportFlowActive && m.historyImportStep == 0 {
			modal = m.renderHistoryStep0Modal()
		} else {
			modal = m.renderHistoryModal()
		}
		if modal != "" {
			fg := confirmOverlayStyle.Render(modal)
			return overlay.Composite(fg, bg, overlay.Center, overlay.Center, 0, 0)
		}
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
		// AC-1: in ViewHistory's folder-list and session-list (no modal up,
		// no wizard active), [q] quits zpit — matching the footer hint
		// "[q] quit" and the AC's "pressing q quits zpit" wording. When a
		// History modal/wizard is active (textinput focused, etc.) [q] still
		// routes back to the dock so the user can recover without losing the
		// process. Other non-Projects views keep the universal "q goes back
		// to dock" convention.
		quitsHere := m.currentView == ViewProjects ||
			(m.currentView == ViewHistory && !m.historyHasActiveModal())
		if !quitsHere {
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
	case ViewEditConfig:
		return m.handleEditConfigKey(msg)
	case ViewGitStatus:
		return m.handleGitStatusKey(msg)
	case ViewHistory:
		return m.handleHistoryKey(msg)
	}
	return m, nil
}

func (m Model) handleProjectsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Tab: toggle focus between project list and loop slots.
	if key.Matches(msg, m.keys.FocusSwitch) {
		return m.handleFocusSwitch()
	}

	// If focused on terminals, delegate key handling.
	if m.focusedPanel == FocusTerminals {
		return m.handleTerminalsKey(msg)
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
		m.ensureCursorInPanel(&m.projectsVP, m.cursor*3) // each project = 3 lines (name + detail + blank)

	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.state.projects)-1 {
			m.cursor++
		}
		m.ensureCursorInPanel(&m.projectsVP, m.cursor*3)

	case key.Matches(msg, m.keys.PageUp):
		m.projectsVP.PageUp()

	case key.Matches(msg, m.keys.PageDown):
		m.projectsVP.PageDown()

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
		if !m.checkConfig("[i]", *p, valTrackerURL) {
			return m, nil
		}
		return m, m.openTrackerCmd()

	case key.Matches(msg, m.keys.OpenPR):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[p]", *p, valTrackerURL) {
			return m, nil
		}
		return m, m.openPRCmd()

	case key.Matches(msg, m.keys.Lazygit):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[G]", *p, valPath) {
			return m, nil
		}
		return m, m.launchLazygitCmd()

	case key.Matches(msg, m.keys.ClaudeUpdate):
		return m, m.launchClaudeUpdateCmd()

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

	case key.Matches(msg, m.keys.Efficiency):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[f]", *p, valPath) {
			return m, nil
		}
		agentPath := filepath.Join(
			platform.ResolvePath(p.Path.Windows, p.Path.WSL),
			".claude", "agents", "efficiency.md",
		)
		if _, err := os.Stat(agentPath); err != nil {
			m.showEfficiencyDeployConfirm()
			return m, m.initConfirmForm()
		}
		return m, m.launchEfficiencyCmd()

	case key.Matches(msg, m.keys.Undeploy):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[u]", *p, valPath) {
			return m, nil
		}
		m.showUndeployConfirm(*p)
		return m, m.initConfirmForm()

	case key.Matches(msg, m.keys.Redeploy):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		if !m.checkConfig("[d]", *p, valPath) {
			return m, nil
		}
		m.showRedeployConfirm()
		return m, m.initConfirmForm()

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

	case key.Matches(msg, m.keys.History):
		m.focusedPanel = FocusProjects
		m.currentView = ViewHistory
		m.historyDrilledFolder = ""
		m.historyFolderCursor = 0
		m.historySessionCursor = 0
		m.historyExportStep = 0
		m.historyImportStep = 0
		m.historyImportFlowActive = false
		m.historyExportFlowActive = false
		if m.historyActivePIDsByFolder == nil {
			m.historyActivePIDsByFolder = make(map[string]bool)
		}
		m.viewport.GotoTop()
		return m, m.scanHistoryFoldersCmd()

	case key.Matches(msg, m.keys.GitStatus):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		var cmd tea.Cmd
		m, cmd = m.enterGitStatus(p.ID)
		return m, cmd

	case key.Matches(msg, m.keys.Add):
		m.setStatus(locale.T(locale.KeyAddProjectStub))

	case key.Matches(msg, m.keys.EditConfig):
		p := m.selectedProject()
		if p == nil {
			return m, nil
		}
		m.currentView = ViewEditConfig
		m.editConfigProjectID = p.ID
		m.editConfigSub = EditConfigMenu
		m.viewport.GotoTop()
		return m, nil

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
		if !m.checkConfig("[i]", *project, valTrackerURL) {
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
		WorktreeBranch: msg.WorktreeBranch,
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
			// Reset cooldown so the next waiting notification is not suppressed
			// by the permission notification's cooldown window.
			m.state.notifier.Reset(msg.ProjectID)
		}

		if ev.State == watcher.StateWaiting {
			// Detect new question: even in Waiting→Waiting transitions (e.g. clarifier
			// consecutive end_turn without tool_use), a different question text means
			// the agent asked something new and the user should be notified.
			isNewQuestion := at.LastQuestion != ev.QuestionText
			at.LastQuestion = ev.QuestionText

			if oldState != watcher.StateWaiting || isNewQuestion {
				if isNewQuestion {
					m.state.notifier.Reset(msg.ProjectID)
				}
				projectName := m.projectName(msg.ProjectID)
				if m.state.notifier.NotifyWaiting(msg.ProjectID, projectName, ev.QuestionText) {
					m.state.logger.Printf("notification sent: key=%s", msg.ProjectID)
				} else {
					m.state.logger.Printf("notification suppressed by cooldown: key=%s", msg.ProjectID)
				}
				if w := m.state.notifier.ConsumeWarning(); w != "" {
					m.setStatus(fmt.Sprintf(locale.T(locale.KeySoundFileNotFound), m.state.cfg.Notification.SoundFile))
				}
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

func (m Model) handleKillTerminal(msg KillTerminalMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyKillFailed), msg.Err))
		return m, nil
	}

	m.state.Lock()
	at, ok := m.state.activeTerminals[msg.TrackingKey]
	var pid int
	if ok {
		pid = at.SessionPID
		if at.Watcher != nil {
			at.Watcher.Stop()
		}
		at.State = watcher.StateEnded
		at.StateChangedAt = time.Now()
		m.state.NotifyAll()
	}
	m.state.Unlock()

	if ok {
		displayName := m.projectName(msg.TrackingKey)
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyKillTerminal), displayName, pid))
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

// === History (Session Browser) handlers and helpers ===

// initHistoryInputs lazily initialises the three textinput models the History
// view uses. Called once when entering the import or export flow.
func (m *Model) initHistoryInputs() {
	if m.historyImportPathInput.Placeholder == "" {
		ti := textinput.New()
		ti.Placeholder = "/path/to/bundle.zip"
		ti.CharLimit = 1024
		m.historyImportPathInput = ti
	}
	if m.historyExportPathInput.Placeholder == "" {
		ti := textinput.New()
		ti.Placeholder = "/path/to/output.zip"
		ti.CharLimit = 1024
		m.historyExportPathInput = ti
	}
	if m.historyImportDestInput.Placeholder == "" {
		ti := textinput.New()
		ti.Placeholder = "/abs/path/to/project"
		ti.CharLimit = 1024
		m.historyImportDestInput = ti
	}
}

// historyHasActiveModal returns true when the History view has any modal,
// wizard step, or textinput-driven flow currently up. Used by the global [q]
// handler so that pressing 'q' inside a path input does not quit zpit by
// mistake (AC-1: 'q' quits from the folder/session list, not from a modal).
func (m Model) historyHasActiveModal() bool {
	if m.historyImportStep != 0 || m.historyImportFlowActive {
		return true
	}
	if m.historyExportStep != 0 || m.historyExportFlowActive {
		return true
	}
	if len(m.historyCollisionQueue) > 0 || m.historyCollisionMemory {
		return true
	}
	return false
}

// collectImportDestSuggestions snapshots configured project paths from
// AppState and resolves each one to the OS-appropriate absolute path via
// platform.ResolvePath. Empty paths are skipped, and duplicates are removed
// while preserving project order. The returned slice is safe to retain on the
// Model after the function returns (the AppState lock is released).
func (m Model) collectImportDestSuggestions() []string {
	m.state.RLock()
	defer m.state.RUnlock()
	seen := make(map[string]bool, len(m.state.projects))
	out := make([]string, 0, len(m.state.projects))
	for _, p := range m.state.projects {
		path := platform.ResolvePath(p.Path.Windows, p.Path.WSL)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// renderHistoryStep0Modal returns the bundle-path-entry modal body for import
// step 0. view_sessions.go's renderHistoryModal returns "" for step 0; this
// method covers the gap so the user can type the bundle path into a textinput.
func (m Model) renderHistoryStep0Modal() string {
	title := selectedStyle.Render(locale.T(locale.KeyHistoryImportPathLabel))
	return strings.Join([]string{
		title,
		"",
		"  " + m.historyImportPathInput.View(),
		"",
		"  Press Enter to load manifest, Esc to cancel",
	}, "\n")
}

// handleHistoryKey dispatches keys for the History (Session Browser) view.
// Modal dispatch takes precedence: when any modal is active, keys go to the
// modal handler; otherwise they go to the folder-list or session-list handler.
func (m Model) handleHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.historyImportStep != 0 || m.historyImportFlowActive ||
		m.historyExportStep != 0 ||
		len(m.historyCollisionQueue) > 0 || m.historyCollisionMemory {
		return m.handleHistoryModalKey(msg)
	}
	if m.historyDrilledFolder == "" {
		return m.handleHistoryFolderListKey(msg)
	}
	return m.handleHistorySessionListKey(msg)
}

// handleHistoryFolderListKey handles keys when the user is browsing the
// top-level folder list (Encoded Folders view).
func (m Model) handleHistoryFolderListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rowCount := len(m.historyFolders) + 1 // +1 for the import pseudo-row
	switch {
	case key.Matches(msg, m.keys.Back):
		m.currentView = ViewProjects
		m.viewport.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.historyFolderCursor > 0 {
			m.historyFolderCursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.historyFolderCursor < rowCount-1 {
			m.historyFolderCursor++
		}
	case key.Matches(msg, m.keys.Enter):
		if m.historyFolderCursor == 0 {
			return m.beginImportFlow()
		}
		idx := m.historyFolderCursor - 1
		if idx < 0 || idx >= len(m.historyFolders) {
			return m, nil
		}
		folder := m.historyFolders[idx]
		m.viewport.GotoTop()
		return m, m.scanHistorySessionsCmd(folder.Name)
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'E':
				// [E] export-all on the focused folder: drill in, scan, then auto-select all.
				if m.historyFolderCursor == 0 {
					return m, nil
				}
				idx := m.historyFolderCursor - 1
				if idx < 0 || idx >= len(m.historyFolders) {
					return m, nil
				}
				folder := m.historyFolders[idx]
				m.historyExportAllPending = true
				m.viewport.GotoTop()
				return m, m.scanHistorySessionsCmd(folder.Name)
			case 'i':
				return m.beginImportFlow()
			}
		}
	}
	return m, nil
}

// handleHistorySessionListKey handles keys in the drilled-in session list.
func (m Model) handleHistorySessionListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.historyDrilledFolder = ""
		m.historyActiveIDs = nil
		m.historySessionCursor = 0
		m.viewport.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.historySessionCursor > 0 {
			m.historySessionCursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.historySessionCursor < len(m.historySessions)-1 {
			m.historySessionCursor++
		}
	case key.Matches(msg, m.keys.Space):
		if m.historySessionCursor < 0 || m.historySessionCursor >= len(m.historySessions) {
			return m, nil
		}
		id := m.historySessions[m.historySessionCursor].SessionID
		if m.historySelected == nil {
			m.historySelected = make(map[string]bool)
		}
		m.historySelected[id] = !m.historySelected[id]
	case key.Matches(msg, m.keys.Enter):
		m.setStatus(locale.T(locale.KeyHistoryDetailNotImplemented))
		return m, nil
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'a':
				// Toggle all-on if any unselected; otherwise toggle all-off.
				anyUnselected := false
				for _, s := range m.historySessions {
					if !m.historySelected[s.SessionID] {
						anyUnselected = true
						break
					}
				}
				if m.historySelected == nil {
					m.historySelected = make(map[string]bool)
				}
				if anyUnselected {
					for _, s := range m.historySessions {
						m.historySelected[s.SessionID] = true
					}
				} else {
					for _, s := range m.historySessions {
						m.historySelected[s.SessionID] = false
					}
				}
			case 'e':
				return m.beginExportFlow()
			}
		}
	}
	return m, nil
}

// handleHistoryModalKey routes keys based on which modal is currently up.
func (m Model) handleHistoryModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Collision modals take precedence.
	if len(m.historyCollisionQueue) > 0 {
		return m.handleSessionCollisionModalKey(msg)
	}
	if m.historyCollisionMemory {
		return m.handleMemoryCollisionModalKey(msg)
	}
	// Export wizard.
	if m.historyExportStep > 0 {
		return m.handleExportModalKey(msg)
	}
	// Import wizard (any active step or flow flag).
	if m.historyImportFlowActive || m.historyImportStep > 0 {
		return m.handleImportModalKey(msg)
	}
	return m, nil
}

// handleSessionCollisionModalKey handles keys for a per-session collision prompt.
// Keys: [o] Overwrite, [s] Skip, [c] Cancel All, Esc = Cancel All.
func (m Model) handleSessionCollisionModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.historyCollisionQueue) == 0 {
		return m, nil
	}
	front := m.historyCollisionQueue[0]
	if m.historyCollisionDecisions == nil {
		m.historyCollisionDecisions = make(map[string]sessionsync.CollisionDecision)
	}
	advance := func() (tea.Model, tea.Cmd) {
		m.historyCollisionQueue = m.historyCollisionQueue[1:]
		if len(m.historyCollisionQueue) == 0 && !m.historyCollisionMemory {
			return m.runImportPass2()
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'o', 'O':
			m.historyCollisionDecisions[front] = sessionsync.DecisionOverwrite
			return advance()
		case 's', 'S':
			m.historyCollisionDecisions[front] = sessionsync.DecisionSkip
			return advance()
		case 'c', 'C':
			m.historyCollisionDecisions[front] = sessionsync.DecisionCancelAll
			return m.runImportPass2()
		}
	}
	if key.Matches(msg, m.keys.Back) {
		m.historyCollisionDecisions[front] = sessionsync.DecisionCancelAll
		return m.runImportPass2()
	}
	return m, nil
}

// handleMemoryCollisionModalKey handles keys for the memory-directory collision prompt.
// Keys: [o] Overwrite, [s] Skip, [c] Cancel All, Esc = Cancel All.
func (m Model) handleMemoryCollisionModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'o', 'O':
			m.historyMemoryDecision = sessionsync.DecisionOverwrite
			m.historyCollisionMemory = false
			return m.runImportPass2()
		case 's', 'S':
			m.historyMemoryDecision = sessionsync.DecisionSkip
			m.historyCollisionMemory = false
			return m.runImportPass2()
		case 'c', 'C':
			m.historyMemoryDecision = sessionsync.DecisionCancelAll
			m.historyCollisionMemory = false
			return m.runImportPass2()
		}
	}
	if key.Matches(msg, m.keys.Back) {
		m.historyMemoryDecision = sessionsync.DecisionCancelAll
		m.historyCollisionMemory = false
		return m.runImportPass2()
	}
	return m, nil
}

// handleExportModalKey handles keys for export-wizard modals (steps 1 and 2).
func (m Model) handleExportModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) {
		m.historyExportStep = 0
		m.historyExportFlowActive = false
		m.historyActivePending = nil
		return m, nil
	}
	switch m.historyExportStep {
	case 1:
		// Active-session warning: Enter continues, Esc cancels (handled above).
		if key.Matches(msg, m.keys.Enter) {
			m.historyExportStep = 2
		}
	case 2:
		// Confirm modal: Tab toggles focus between memory checkbox and path input;
		// Space toggles memory when checkbox is focused; Enter fires export and
		// reads the path from the textinput when focused, otherwise from
		// historyExportOutputPath.
		if key.Matches(msg, m.keys.FocusSwitch) {
			if m.historyExportPathInput.Focused() {
				m.historyExportPathInput.Blur()
			} else {
				m.historyExportPathInput.Focus()
			}
			return m, nil
		}
		if m.historyExportPathInput.Focused() {
			// Path input is focused — Enter commits the value and fires export.
			if key.Matches(msg, m.keys.Enter) {
				if v := strings.TrimSpace(m.historyExportPathInput.Value()); v != "" {
					m.historyExportOutputPath = v
				}
				return m.runExport()
			}
			var cmd tea.Cmd
			m.historyExportPathInput, cmd = m.historyExportPathInput.Update(msg)
			return m, cmd
		}
		// Path input not focused — Space toggles checkbox, Enter fires export.
		if key.Matches(msg, m.keys.Space) {
			m.historyExportIncludeMemory = !m.historyExportIncludeMemory
			return m, nil
		}
		if key.Matches(msg, m.keys.Enter) {
			return m.runExport()
		}
	}
	return m, nil
}

// runExport fires the export command using the currently selected sessions and
// the resolved output path. Called from both the confirm-modal Enter handler
// and the path-input-focused Enter handler.
func (m Model) runExport() (tea.Model, tea.Cmd) {
	ids := make([]string, 0, len(m.historySelected))
	for _, s := range m.historySessions {
		if m.historySelected[s.SessionID] {
			ids = append(ids, s.SessionID)
		}
	}
	outPath := m.historyExportOutputPath
	include := m.historyExportIncludeMemory
	folder := m.historyDrilledFolder
	m.historyExportStep = 3 // running
	m.historyExportPathInput.Blur()
	return m, m.exportSessionsCmd(folder, ids, include, outPath)
}

// handleImportModalKey handles keys for all import-wizard modal steps (0–5).
func (m Model) handleImportModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) {
		// Cancel (or dismiss if at step 5 summary) the wizard.
		m.historyImportStep = 0
		m.historyImportFlowActive = false
		m.historyImportPathInput.Blur()
		m.historyImportDestInput.Blur()
		// Clear summary state on any cancellation path.
		m.historyImportManifest = nil
		m.historyImportSelection = nil
		m.historyImportResult = nil
		m.historyCollisionDecisions = nil
		return m, nil
	}
	switch m.historyImportStep {
	case 0:
		// Bundle path entry (textinput).
		if key.Matches(msg, m.keys.Enter) {
			path := strings.TrimSpace(m.historyImportPathInput.Value())
			if path == "" {
				return m, nil
			}
			m.historyImportBundlePath = path
			m.historyImportPathInput.Blur()
			return m, m.loadHistoryManifestCmd(path)
		}
		var cmd tea.Cmd
		m.historyImportPathInput, cmd = m.historyImportPathInput.Update(msg)
		return m, cmd
	case 1:
		// Preview: Up/Down navigates, Space toggles, [a] toggles all, Enter advances.
		if key.Matches(msg, m.keys.Up) {
			if m.historyImportSessionCursor > 0 {
				m.historyImportSessionCursor--
			}
			return m, nil
		}
		if key.Matches(msg, m.keys.Down) {
			if m.historyImportManifest != nil &&
				m.historyImportSessionCursor < len(m.historyImportManifest.Sessions)-1 {
				m.historyImportSessionCursor++
			}
			return m, nil
		}
		if key.Matches(msg, m.keys.Space) {
			if m.historyImportManifest != nil &&
				m.historyImportSessionCursor < len(m.historyImportManifest.Sessions) {
				id := m.historyImportManifest.Sessions[m.historyImportSessionCursor]
				if m.historyImportSelection == nil {
					m.historyImportSelection = make(map[string]bool)
				}
				m.historyImportSelection[id] = !m.historyImportSelection[id]
			}
			return m, nil
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'a' {
			if m.historyImportManifest != nil {
				anyUnselected := false
				for _, id := range m.historyImportManifest.Sessions {
					if !m.historyImportSelection[id] {
						anyUnselected = true
						break
					}
				}
				if m.historyImportSelection == nil {
					m.historyImportSelection = make(map[string]bool)
				}
				for _, id := range m.historyImportManifest.Sessions {
					m.historyImportSelection[id] = anyUnselected
				}
			}
			return m, nil
		}
		if key.Matches(msg, m.keys.Enter) {
			m.historyImportStep = 2
			m.historyImportDestInput.SetValue("")
			m.historyImportDestInput.Focus()
			m.historyImportDestSuggestions = m.collectImportDestSuggestions()
			m.historyImportDestSuggestionCursor = -1
			return m, textinput.Blink
		}
	case 2:
		// Destination path entry: textinput + autocomplete suggestion list.
		// Cursor -1 = textinput focused (typing); 0..N-1 = a suggestion focused.
		// Up/Down navigates between input and suggestions. Enter on a suggestion
		// fills the input with that path; Enter on the input commits the typed
		// path. Free-form absolute paths are always accepted (AC-8).
		//
		// IMPORTANT: gate navigation on msg.Type == tea.KeyUp/KeyDown rather than
		// key.Matches(m.keys.Up/Down). The Up/Down bindings include 'k'/'j' aliases
		// (see keymap.go) — using key.Matches here would intercept literal 'j'/'k'
		// runes typed into the focused textinput, breaking paths like /home/jeff/...
		// or C:\Users\jack\... (regression from round 1; see PR #103 review round 1).
		// When the cursor is already on a suggestion (input blurred), allow the
		// j/k aliases for vim-like navigation as well.
		isUp := msg.Type == tea.KeyUp || (m.historyImportDestSuggestionCursor != -1 && key.Matches(msg, m.keys.Up))
		isDown := msg.Type == tea.KeyDown || (m.historyImportDestSuggestionCursor != -1 && key.Matches(msg, m.keys.Down))
		if isUp {
			if m.historyImportDestSuggestionCursor > -1 {
				m.historyImportDestSuggestionCursor--
				if m.historyImportDestSuggestionCursor == -1 {
					m.historyImportDestInput.Focus()
					return m, textinput.Blink
				}
				m.historyImportDestInput.Blur()
			}
			return m, nil
		}
		if isDown {
			if m.historyImportDestSuggestionCursor < len(m.historyImportDestSuggestions)-1 {
				m.historyImportDestSuggestionCursor++
				m.historyImportDestInput.Blur()
			}
			return m, nil
		}
		if key.Matches(msg, m.keys.Enter) {
			// Enter on a suggestion: copy it into the input and advance.
			if m.historyImportDestSuggestionCursor >= 0 &&
				m.historyImportDestSuggestionCursor < len(m.historyImportDestSuggestions) {
				path := m.historyImportDestSuggestions[m.historyImportDestSuggestionCursor]
				if !filepath.IsAbs(path) {
					m.setStatus(locale.T(locale.KeyHistoryDestMustBeAbs))
					return m, nil
				}
				m.historyImportDestInput.SetValue(path)
				m.historyImportDestPath = path
				m.historyImportDestInput.Blur()
				m.historyImportStep = 3
				return m, nil
			}
			// Enter on freeform input: validate the typed value.
			path := strings.TrimSpace(m.historyImportDestInput.Value())
			if path == "" || !filepath.IsAbs(path) {
				m.setStatus(locale.T(locale.KeyHistoryDestMustBeAbs))
				return m, nil
			}
			m.historyImportDestPath = path
			m.historyImportDestInput.Blur()
			m.historyImportStep = 3
			return m, nil
		}
		// Other keys go to the textinput only when it is focused.
		if m.historyImportDestSuggestionCursor == -1 {
			var cmd tea.Cmd
			m.historyImportDestInput, cmd = m.historyImportDestInput.Update(msg)
			return m, cmd
		}
		return m, nil
	case 3:
		// Final preview: Enter starts collision detection + import.
		if key.Matches(msg, m.keys.Enter) {
			destEncoded := watcher.EncodeCwd(m.historyImportDestPath)
			claudeHome, err := watcher.ClaudeHome()
			if err != nil {
				m.setStatus(fmt.Sprintf(locale.T(locale.KeyHistoryClaudeHomeError), err))
				return m, nil
			}
			destDir := filepath.Join(claudeHome, "projects", destEncoded)
			ids := make([]string, 0, len(m.historyImportSelection))
			if m.historyImportManifest != nil {
				for _, id := range m.historyImportManifest.Sessions {
					if m.historyImportSelection[id] {
						ids = append(ids, id)
					}
				}
			}
			include := m.historyImportManifest != nil && m.historyImportManifest.IncludeMemory
			m.historyImportStep = 4 // running (pre-collision check)
			return m, m.detectImportCollisionsCmd(destDir, ids, include)
		}
	case 4:
		// Running — ignore most keys; nothing to handle.
	case 5:
		// Summary: Enter or Esc dismisses.
		if key.Matches(msg, m.keys.Enter) || key.Matches(msg, m.keys.Back) {
			m.historyImportStep = 0
			m.historyImportFlowActive = false
			m.historyImportManifest = nil
			m.historyImportSelection = nil
			m.historyImportResult = nil
			m.historyCollisionDecisions = nil
			return m, nil
		}
	}
	return m, nil
}

// beginExportFlow opens the export wizard. Detects active sessions in the current
// selection; if any, opens the active-warning modal first (step 1), otherwise
// jumps directly to the export confirm modal (step 2).
func (m Model) beginExportFlow() (tea.Model, tea.Cmd) {
	selectedIDs := make([]string, 0, len(m.historySelected))
	for _, s := range m.historySessions {
		if m.historySelected[s.SessionID] {
			selectedIDs = append(selectedIDs, s.SessionID)
		}
	}
	if len(selectedIDs) == 0 {
		m.setStatus(locale.T(locale.KeyHistoryNoSelection))
		return m, nil
	}
	activeSet := make(map[string]bool, len(m.historyActiveIDs))
	for _, id := range m.historyActiveIDs {
		activeSet[id] = true
	}
	var pending []string
	for _, id := range selectedIDs {
		if activeSet[id] {
			pending = append(pending, id)
		}
	}
	m.historyActivePending = pending
	// Pre-fill output path if not already set.
	if m.historyExportOutputPath == "" {
		if path, err := defaultExportOutputPath(m.historyDrilledFolder); err == nil {
			m.historyExportOutputPath = path
		}
	}
	m.initHistoryInputs()
	// Seed the export path textinput with the resolved default so the user can
	// edit it in the confirm modal before pressing Enter (AC-4).
	m.historyExportPathInput.SetValue(m.historyExportOutputPath)
	m.historyExportPathInput.Blur() // start with checkbox focused; Tab focuses path input
	m.historyExportFlowActive = true
	if len(pending) > 0 {
		m.historyExportStep = 1
	} else {
		m.historyExportStep = 2
	}
	return m, nil
}

// beginImportFlow opens the import wizard at step 0 (bundle path entry).
func (m Model) beginImportFlow() (tea.Model, tea.Cmd) {
	m.initHistoryInputs()
	m.historyImportFlowActive = true
	m.historyImportStep = 0
	m.historyImportPathInput.SetValue("")
	m.historyImportPathInput.Focus()
	if m.historyImportSelection == nil {
		m.historyImportSelection = make(map[string]bool)
	}
	if m.historyCollisionDecisions == nil {
		m.historyCollisionDecisions = make(map[string]sessionsync.CollisionDecision)
	}
	return m, textinput.Blink
}

// handleSessionCollisionsT10 takes over collision dispatch from sessions.go's stub.
// When there are no collisions, it immediately proceeds to the import run.
// When collisions exist, it stores them in the model for the collision modal loop.
func (m Model) handleSessionCollisionsT10(msg sessionCollisionsMsg) (tea.Model, tea.Cmd) {
	m.historyCollisionQueue = msg.SessionCollisions
	m.historyCollisionMemory = msg.MemoryCollision
	if len(msg.SessionCollisions) == 0 && !msg.MemoryCollision {
		return m.runImportPass2()
	}
	// Collision prompts exist; the modal loop (handleSessionCollisionModalKey /
	// handleMemoryCollisionModalKey) will resolve them one at a time before
	// calling runImportPass2.
	return m, nil
}

// runImportPass2 executes the actual Unpack with pre-resolved collision decisions.
// Called after all collision prompts have been answered (or CancelAll was chosen).
func (m Model) runImportPass2() (tea.Model, tea.Cmd) {
	if m.historyImportManifest == nil {
		return m, nil
	}
	destEncoded := watcher.EncodeCwd(m.historyImportDestPath)
	claudeHome, err := watcher.ClaudeHome()
	if err != nil {
		m.setStatus(fmt.Sprintf(locale.T(locale.KeyHistoryClaudeHomeError), err))
		return m, nil
	}
	destDir := filepath.Join(claudeHome, "projects", destEncoded)
	// Deep-copy maps so the closure doesn't capture mutable Model references.
	selection := make(map[string]bool, len(m.historyImportSelection))
	for k, v := range m.historyImportSelection {
		selection[k] = v
	}
	decisions := make(map[string]sessionsync.CollisionDecision, len(m.historyCollisionDecisions))
	for k, v := range m.historyCollisionDecisions {
		decisions[k] = v
	}
	memDecision := m.historyMemoryDecision
	bundlePath := m.historyImportBundlePath
	destCwd := m.historyImportDestPath
	// Set step 4 (running) before dispatching the cmd.
	m.historyImportStep = 4
	m.historyCollisionQueue = nil
	m.historyCollisionMemory = false
	return m, m.importBundleCmd(bundlePath, destDir, destCwd, selection, decisions, memDecision)
}
