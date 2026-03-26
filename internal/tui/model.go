package tui

import (
	"context"
	"fmt"
	"io"
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
	"github.com/zac15987/zpit/internal/notify"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/tracker"
	"github.com/zac15987/zpit/internal/watcher"
	"github.com/zac15987/zpit/internal/worktree"
)

const (
	statusDisplayDuration  = 5 * time.Second
	tickInterval           = 1 * time.Second
	livenessCheckInterval  = 5 * time.Second
	endedDisplayDuration   = 3 * time.Second
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
	LaunchResult   *terminal.LaunchResult
	SessionPID     int
	State          watcher.AgentState
	LastQuestion   string
	StateChangedAt time.Time
	Watcher        *watcher.Watcher
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg      *config.Config
	env      platform.Environment
	keys     KeyMap
	notifier *notify.Notifier
	logger   *log.Logger

	width  int
	height int

	currentView View

	// Project list state
	projects      []config.ProjectConfig
	cursor        int
	statusMessage string
	statusExpiry  time.Time

	// Active terminals
	activeTerminals    map[string]*ActiveTerminal
	lastLivenessCheck  time.Time

	// TrackerClients for [s] status and [y] confirm (keyed by provider name)
	clients map[string]tracker.TrackerClient

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

	// Embedded agent templates
	clarifierMD []byte
	reviewerMD  []byte

	// Embedded static docs (deployed to .claude/docs/)
	agentGuidelinesMD            []byte
	codeConstructionPrinciplesMD []byte

	// Loop engine state
	loops     map[string]*loop.LoopState
	wtManager *worktree.Manager

	// Focus panel state (loop slot selection)
	focusedPanel   FocusedPanel
	loopCursor     int
	focusProjectID string

	// Viewport for scrollable content
	viewport viewport.Model
}

// NewModel creates the root TUI model. logWriter may be nil (uses io.Discard).
func NewModel(cfg *config.Config, clarifierMD, reviewerMD, agentGuidelinesMD, codeConstructionPrinciplesMD []byte, logWriter io.Writer) Model {
	if logWriter == nil {
		logWriter = io.Discard
	}
	logger := log.New(logWriter, "", log.LstdFlags)
	logger.Println("zpit started")

	clients := make(map[string]tracker.TrackerClient)
	for name, provider := range cfg.Providers.Tracker {
		client, err := tracker.NewClient(provider.Type, provider.URL, provider.TokenEnv)
		if err != nil {
			logger.Printf("tracker client %q init failed: %v", name, err)
			continue
		}
		clients[name] = client
	}
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	vp.KeyMap = viewport.KeyMap{} // disable all keyboard bindings — we handle keys ourselves

	return Model{
		cfg:             cfg,
		env:             platform.Detect(),
		keys:            DefaultKeyMap(),
		notifier:        notify.NewNotifier(cfg.Notification),
		logger:          logger,
		currentView:     ViewProjects,
		projects:        cfg.Projects,
		activeTerminals: make(map[string]*ActiveTerminal),
		clients:         clients,
		clarifierMD:                  clarifierMD,
		reviewerMD:                   reviewerMD,
		agentGuidelinesMD:            agentGuidelinesMD,
		codeConstructionPrinciplesMD: codeConstructionPrinciplesMD,
		loops:           make(map[string]*loop.LoopState),
		wtManager:       worktree.NewManager(cfg.Worktree),
		viewport:        vp,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}

	seenPath := make(map[string]bool)
	seenMissing := make(map[string]bool)
	var missingProviders []string

	for _, project := range m.projects {
		// Ensure .gitignore has Zpit-deployed paths (sync, fast).
		projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
		if projectPath != "" && !seenPath[projectPath] {
			seenPath[projectPath] = true
			ensureGitignore(projectPath)
		}

		// Log missing tracker providers for awareness.
		if project.Tracker == "" || project.Repo == "" {
			continue
		}
		if _, ok := m.clients[project.Tracker]; !ok {
			if !seenMissing[project.Tracker] {
				seenMissing[project.Tracker] = true
				missingProviders = append(missingProviders, project.Tracker)
			}
		}
	}

	if len(missingProviders) > 0 {
		msg := fmt.Sprintf("Tracker unavailable (token not set?): %s", strings.Join(missingProviders, ", "))
		m.logger.Println(msg)
		cmds = append(cmds, func() tea.Msg {
			return StatusMsg{Text: msg}
		})
	}

	// Scan for already-running Claude Code sessions.
	cmds = append(cmds, m.scanExistingSessionsCmd())

	return tea.Batch(cmds...)
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
func (m *Model) syncViewportContent() {
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
			m.checkSessionLiveness()
			return m, tickCmd()
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
			m.checkSessionLiveness()
			return m, tickCmd()
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

	case LaunchResultMsg:
		return m.handleLaunchResult(msg)

	case AgentEventMsg:
		return m.handleAgentEvent(msg)

	case existingSessionsMsg:
		m.logger.Printf("startup scan: found %d session(s)", len(msg.Entries))
		var cmds []tea.Cmd
		for _, entry := range msg.Entries {
			key := m.nextTrackingKey(entry.ProjectID)
			m.logger.Printf("  attach: key=%s PID=%d", key, entry.PID)
			m.activeTerminals[key] = &ActiveTerminal{
				State:          watcher.StateUnknown,
				SessionPID:     entry.PID,
				StateChangedAt: time.Now(),
			}
			cmds = append(cmds, waitForLogCmd(key, entry.PID, entry.LogPath))
		}
		return m, tea.Batch(cmds...)

	case sessionFoundMsg:
		m.logger.Printf("session found: key=%s PID=%d", msg.ProjectID, msg.PID)
		if at, ok := m.activeTerminals[msg.ProjectID]; ok {
			at.SessionPID = msg.PID
		}
		return m, waitForLogCmd(msg.ProjectID, msg.PID, msg.LogPath)

	case watcherReadyMsg:
		if at, ok := m.activeTerminals[msg.ProjectID]; ok {
			at.Watcher = msg.Watcher
			if at.State == watcher.StateUnknown && msg.LogPath != "" {
				state, question := watcher.ReadLastState(msg.LogPath)
				if state != watcher.StateUnknown {
					at.State = state
					at.LastQuestion = question
					at.StateChangedAt = time.Now()
				}
			}
			m.logger.Printf("watcher ready: key=%s state=%s", msg.ProjectID, at.State)
			return m, watchNextCmd(msg.ProjectID, msg.Watcher)
		}
		msg.Watcher.Stop()
		return m, nil

	case TickMsg:
		m.checkSessionLiveness()
		return m, tickCmd()

	case WatcherErrorMsg:
		m.setStatus(fmt.Sprintf("Watcher error (%s): %s", msg.ProjectID, msg.Err))
		return m, nil

	case sessionLostMsg:
		m.logger.Printf("session lost: %s — %s", msg.ProjectID, msg.Text)
		if at, ok := m.activeTerminals[msg.ProjectID]; ok {
			at.State = watcher.StateEnded
			at.StateChangedAt = time.Now()
			if at.Watcher != nil {
				at.Watcher.Stop()
			}
		}
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
	case LoopAgentExitedMsg:
		return m.handleLoopAgentExited(msg)
	case LoopReviewResultMsg:
		return m.handleLoopReviewResult(msg)
	case LoopPRStatusMsg:
		return m.handleLoopPRStatus(msg)
	case LoopCleanupMsg:
		return m.handleLoopCleanup(msg)
	case LoopOpenPRsMsg:
		return m.handleLoopOpenPRs(msg)
	case loopPollTickMsg:
		if ls, ok := m.loops[msg.ProjectID]; ok && ls.Active {
			return m, m.loopPollCmd(msg.ProjectID)
		}
		return m, nil
	case loopPRPollTickMsg:
		return m, m.loopPollPRCmd(msg.ProjectID, msg.IssueID)

	}

	return m, nil
}

func (m Model) View() string {
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
	m.logger.Println(text)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys
	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.currentView != ViewProjects {
			m.currentView = ViewProjects
			return m, nil
		}
		for _, at := range m.activeTerminals {
			if at.Watcher != nil {
				at.Watcher.Stop()
			}
		}
		for _, ls := range m.loops {
			ls.Active = false
		}
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
		if m.cursor < len(m.projects)-1 {
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
		if ls, ok := m.loops[p.ID]; ok && ls.Active {
			ls.Active = false
			m.setStatus(fmt.Sprintf("Loop stopped for %s", p.Name))
			return m, nil
		}
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
	project := m.projects[m.cursor]
	keys := m.sortedSlotKeys(project.ID)
	if len(keys) == 0 {
		return m, nil
	}
	m.focusedPanel = FocusLoopSlots
	m.focusProjectID = project.ID
	m.loopCursor = 0
	return m, nil
}

func (m Model) handleLoopSlotsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	keys := m.sortedSlotKeys(m.focusProjectID)
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
	ls, ok := m.loops[m.focusProjectID]
	if !ok {
		return m, nil
	}
	slot, ok := ls.Slots[slotKey]
	if !ok || slot.WorktreePath == "" {
		m.setStatus(locale.T(locale.KeyNoWorktreePath))
		return m, nil
	}
	if !launchableSlotStates[slot.State] {
		m.setStatus(locale.T(locale.KeyCannotLaunch))
		return m, nil
	}
	if _, err := os.Stat(slot.WorktreePath); err != nil {
		m.logger.Printf("launch check failed [focus] worktree path missing: %s", slot.WorktreePath)
		m.showErrorOverlay([]string{locale.T(locale.KeyErrWorktreeMissing)})
		return m, nil
	}

	cfg := m.cfg.Terminal
	tabTitle := fmt.Sprintf("Focus #%s", slot.IssueID)
	wtPath := slot.WorktreePath
	trackingKey := "focus:" + m.focusProjectID + ":" + slot.IssueID

	return m, func() tea.Msg {
		result, err := terminal.LaunchClaudeInDir(wtPath, tabTitle, cfg)
		return LaunchResultMsg{
			ProjectID:   m.focusProjectID,
			TrackingKey: trackingKey,
			WorkDir:     wtPath,
			Result:      result,
			Err:         err,
		}
	}
}

func (m Model) sortedSlotKeys(projectID string) []string {
	ls, ok := m.loops[projectID]
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

	trackKey := msg.ProjectID
	if msg.TrackingKey != "" {
		trackKey = msg.TrackingKey
	}
	trackKey = m.nextTrackingKey(trackKey)
	m.logger.Printf("launch: key=%s (PID pending)", trackKey)

	at := &ActiveTerminal{
		LaunchResult:   msg.Result,
		State:          watcher.StateUnknown,
		StateChangedAt: time.Now(),
	}
	m.activeTerminals[trackKey] = at

	// Try to start watching the session log.
	if msg.WorkDir == "" {
		project := m.findProject(msg.ProjectID)
		if project == nil {
			return m, nil
		}
		msg.WorkDir = platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	}
	return m, m.startWatcherDirCmd(trackKey, msg.WorkDir)
}

func (m Model) handleAgentEvent(msg AgentEventMsg) (tea.Model, tea.Cmd) {
	at, ok := m.activeTerminals[msg.ProjectID]
	if !ok {
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

		if ev.State == watcher.StateWaiting {
			at.LastQuestion = ev.QuestionText
			// Notify on transition to waiting.
			if oldState != watcher.StateWaiting {
				projectName := m.projectName(msg.ProjectID)
				m.notifier.NotifyWaiting(msg.ProjectID, projectName, ev.QuestionText)
			}
		} else if ev.State == watcher.StateWorking {
			// User responded — reset notification cooldown.
			m.notifier.Reset(msg.ProjectID)
			at.LastQuestion = ""
		}
	}

	// Continue watching.
	if at.Watcher != nil {
		return m, watchNextCmd(msg.ProjectID, at.Watcher)
	}
	return m, nil
}

func (m Model) launchClaudeCmd() tea.Cmd {
	project := m.projects[m.cursor]
	cfg := m.cfg.Terminal
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
	project := m.projects[m.cursor]
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
	sessionRetryMax      = 8 // 8 * 2s = 16s max wait
)

// scanExistingSessionsCmd scans all projects for already-running Claude Code sessions at startup.
func (m Model) scanExistingSessionsCmd() tea.Cmd {
	type projectInfo struct {
		id   string
		path string
	}
	seen := make(map[string]bool)
	var projects []projectInfo
	for _, p := range m.projects {
		path := platform.ResolvePath(p.Path.Windows, p.Path.WSL)
		if path == "" {
			m.logger.Printf("session scan: skipping project %q (empty path)", p.ID)
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
			return existingSessionsMsg{}
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
					LogPath:   logPath,
				})
			}
		}
		return existingSessionsMsg{Entries: entries}
	}
}

// startWatcherDirCmd starts session discovery for a given tracking key and work directory.
func (m Model) startWatcherDirCmd(trackingKey, workDir string) tea.Cmd {
	// Snapshot already-tracked PIDs so we can exclude them when picking a session.
	excludePIDs := m.trackedPIDs()
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
		return sessionFoundMsg{ProjectID: trackingKey, PID: latest.PID, LogPath: logPath}
	}
}

// sessionFoundMsg is sent when the session PID is discovered but JSONL may not exist yet.
type sessionFoundMsg struct {
	ProjectID string
	PID       int
	LogPath   string
}

// existingSessionEntry represents a session found during startup scan.
type existingSessionEntry struct {
	ProjectID string
	PID       int
	LogPath   string
}

// nextTrackingKey returns a unique key for activeTerminals.
// First session uses baseKey as-is; subsequent ones get "#2", "#3", etc.
func (m Model) nextTrackingKey(baseKey string) string {
	if _, exists := m.activeTerminals[baseKey]; !exists {
		return baseKey
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s#%d", baseKey, i)
		if _, exists := m.activeTerminals[candidate]; !exists {
			return candidate
		}
	}
}

// trackedPIDs returns all PIDs currently tracked in activeTerminals.
func (m Model) trackedPIDs() map[int]bool {
	pids := make(map[int]bool, len(m.activeTerminals))
	for _, at := range m.activeTerminals {
		if at.SessionPID != 0 {
			pids[at.SessionPID] = true
		}
	}
	return pids
}

// existingSessionsMsg carries results of scanning for already-running sessions at startup.
type existingSessionsMsg struct {
	Entries []existingSessionEntry
}

// waitForLogCmd phase 2: wait for the JSONL file to be created, then start the watcher.
func waitForLogCmd(projectID string, pid int, logPath string) tea.Cmd {
	return func() tea.Msg {
		for {
			if !watcher.IsProcessAlive(pid) {
				return sessionLostMsg{ProjectID: projectID, Text: "session ended before log created"}
			}
			if _, err := os.Stat(logPath); err == nil {
				w, err := watcher.New(projectID, logPath)
				if err != nil {
					return WatcherErrorMsg{ProjectID: projectID, Err: err}
				}
				return watcherReadyMsg{ProjectID: projectID, Watcher: w, LogPath: logPath}
			}
			time.Sleep(sessionRetryInterval)
		}
	}
}

// watcherReadyMsg is an internal message to attach a watcher to an ActiveTerminal.
type watcherReadyMsg struct {
	ProjectID string
	Watcher   *watcher.Watcher
	LogPath   string
}

func (m *Model) checkSessionLiveness() {
	now := time.Now()
	if now.Sub(m.lastLivenessCheck) < livenessCheckInterval {
		return
	}
	m.lastLivenessCheck = now

	for projectID, at := range m.activeTerminals {
		// Clean up ended sessions after display duration.
		if at.State == watcher.StateEnded {
			if now.Sub(at.StateChangedAt) >= endedDisplayDuration {
				m.logger.Printf("session removed: key=%s", projectID)
				delete(m.activeTerminals, projectID)
			}
			continue
		}

		if at.SessionPID <= 0 {
			continue
		}
		if !watcher.IsProcessAlive(at.SessionPID) {
			m.logger.Printf("session PID %d ended: %s", at.SessionPID, projectID)
			at.State = watcher.StateEnded
			at.StateChangedAt = now
			at.LastQuestion = ""
			if at.Watcher != nil {
				at.Watcher.Stop()
			}
		}
	}
}

func (m Model) findProject(id string) *config.ProjectConfig {
	for i := range m.projects {
		if m.projects[i].ID == id {
			return &m.projects[i]
		}
	}
	return nil
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
	project := m.projects[m.cursor]
	cfg := m.cfg.Terminal
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
	project := m.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	clarifierMD := injectLangInstruction(m.clarifierMD)
	cfg := m.cfg.Terminal

	deployTracker := func() error { return m.deployTrackerDoc(projectPath, &project) }
	agentGuidelines := m.agentGuidelinesMD
	codeConstructionPrinciples := m.codeConstructionPrinciplesMD

	return func() tea.Msg {
		// Deploy: create .claude/agents/ and write clarifier.md
		agentDir := filepath.Join(projectPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		agentPath := filepath.Join(agentDir, "clarifier.md")
		if err := os.WriteFile(agentPath, clarifierMD, 0o644); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		// Deploy docs
		if err := deployTracker(); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy tracker doc failed: %s", err)}
		}
		deployStaticDocs(projectPath, agentGuidelines, codeConstructionPrinciples)

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
	project := m.projects[m.cursor]
	provider, ok := m.cfg.Providers.Tracker[project.Tracker]
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
	project := m.projects[m.cursor]
	client, ok := m.clients[project.Tracker]
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
	client, ok := m.clients[project.Tracker]
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
		project := m.projects[op.ProjectIndex]
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
		project := m.projects[op.ProjectIndex]
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
		project := m.projects[op.ProjectIndex]
		ls := &loop.LoopState{
			Active: true,
			Slots:  make(map[string]*loop.Slot),
		}
		m.loops[project.ID] = ls
		m.setStatus(fmt.Sprintf("Loop started for %s", project.Name))
		return m, tea.Batch(
			m.loopCleanupMergedCmd(project.ID),
			m.loopScanOpenPRsCmd(project.ID),
			m.loopPollCmd(project.ID),
		)

	case PendingConfirmIssue:
		m.pendingOp = nil
		return m, m.confirmIssueCmd()
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
	client, ok := m.clients[project.Tracker]
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
	client, ok := m.clients[project.Tracker]
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
	provider, ok := m.cfg.Providers.Tracker[project.Tracker]
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
	project := m.projects[m.cursor]
	cfg := m.cfg.Terminal
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

// deployAndLaunchReviewer deploys reviewer.md to the project and launches it.
func (m Model) deployAndLaunchReviewer() tea.Cmd {
	project := m.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	reviewerMD := injectLangInstruction(m.reviewerMD)
	cfg := m.cfg.Terminal
	deployTracker := func() error { return m.deployTrackerDoc(projectPath, &project) }
	agentGuidelines := m.agentGuidelinesMD
	codeConstructionPrinciples := m.codeConstructionPrinciplesMD

	return func() tea.Msg {
		agentDir := filepath.Join(projectPath, ".claude", "agents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		agentPath := filepath.Join(agentDir, "reviewer.md")
		if err := os.WriteFile(agentPath, reviewerMD, 0o644); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy failed: %s", err)}
		}
		_ = deployTracker()
		deployStaticDocs(projectPath, agentGuidelines, codeConstructionPrinciples)

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

// deployTrackerDoc writes .claude/docs/tracker.md to the target directory.
func (m Model) deployTrackerDoc(targetPath string, project *config.ProjectConfig) error {
	provider, ok := m.cfg.Providers.Tracker[project.Tracker]
	if !ok {
		return nil // no tracker configured, skip silently
	}
	docsDir := filepath.Join(targetPath, ".claude", "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude/docs: %w", err)
	}
	content := tracker.BuildTrackerDoc(provider.Type, provider.URL, project.Repo, provider.TokenEnv, project.BaseBranch)
	return os.WriteFile(filepath.Join(docsDir, "tracker.md"), []byte(content), 0o644)
}

// deployStaticDocs writes embedded agent-guidelines.md and code-construction-principles.md to .claude/docs/.
func deployStaticDocs(targetPath string, agentGuidelines, codeConstructionPrinciples []byte) {
	docsDir := filepath.Join(targetPath, ".claude", "docs")
	_ = os.MkdirAll(docsDir, 0o755)
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

