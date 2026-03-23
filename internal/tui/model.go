package tui

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/huh"

	"github.com/zac15987/zpit/internal/config"
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
	livenessCheckInterval  = 10 * time.Second
	endedDisplayDuration   = 10 * time.Second
)

// View represents the current screen.
type View int

const (
	ViewProjects View = iota
	ViewStatus
)

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

	// Confirm dialog (huh)
	confirmForm   *huh.Form
	confirmResult *bool          // heap-allocated: shared across Bubble Tea value copies
	confirmAction func() tea.Cmd

	// Embedded agent templates
	clarifierMD []byte
	reviewerMD  []byte

	// Loop engine state
	loops     map[string]*loop.LoopState
	wtManager *worktree.Manager
}

// NewModel creates the root TUI model. logWriter may be nil (uses io.Discard).
func NewModel(cfg *config.Config, clarifierMD, reviewerMD []byte, logWriter io.Writer) Model {
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
		clarifierMD:     clarifierMD,
		reviewerMD:      reviewerMD,
		loops:           make(map[string]*loop.LoopState),
		wtManager:       worktree.NewManager(cfg.Worktree),
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}

	seenTracker := make(map[string]bool)
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

		// Ensure required labels exist (background, non-blocking).
		if project.Tracker == "" || project.Repo == "" {
			continue
		}
		if _, ok := m.clients[project.Tracker]; !ok {
			if !seenMissing[project.Tracker] {
				seenMissing[project.Tracker] = true
				missingProviders = append(missingProviders, project.Tracker)
			}
			continue
		}
		key := project.Tracker + ":" + project.Repo
		if seenTracker[key] {
			continue
		}
		seenTracker[key] = true
		cmds = append(cmds, m.ensureLabelsCmd(project.ID))
	}

	if len(missingProviders) > 0 {
		msg := fmt.Sprintf("Tracker unavailable (token not set?): %s", strings.Join(missingProviders, ", "))
		m.logger.Println(msg)
		m.statusMessage = msg
		m.statusExpiry = time.Now().Add(10 * time.Second)
	}

	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, nil
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case LaunchResultMsg:
		return m.handleLaunchResult(msg)

	case AgentEventMsg:
		return m.handleAgentEvent(msg)

	case sessionFoundMsg:
		if at, ok := m.activeTerminals[msg.ProjectID]; ok {
			at.SessionPID = msg.PID
		}
		return m, waitForLogCmd(msg.ProjectID, msg.PID, msg.LogPath)

	case watcherReadyMsg:
		if at, ok := m.activeTerminals[msg.ProjectID]; ok {
			at.Watcher = msg.Watcher
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

	case StatusMsg:
		m.setStatus(msg.Text)
		return m, nil

	case LabelsEnsuredMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("⚠ %s: label sync failed: %s", m.projectName(msg.ProjectID), msg.Err))
		} else if len(msg.Created) > 0 {
			m.setStatus(fmt.Sprintf("Created labels for %s: %s",
				m.projectName(msg.ProjectID), strings.Join(msg.Created, ", ")))
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
	if m.confirmForm != nil {
		return m.confirmForm.View()
	}
	switch m.currentView {
	case ViewProjects:
		return m.viewProjects()
	case ViewStatus:
		return m.viewStatus()
	default:
		return "Unknown view"
	}
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
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}

	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.projects)-1 {
			m.cursor++
		}

	case key.Matches(msg, m.keys.Enter):
		return m, m.launchClaudeCmd()

	case key.Matches(msg, m.keys.Open):
		return m, m.openFolderCmd()

	case key.Matches(msg, m.keys.Tracker):
		return m, m.openTrackerCmd()

	case key.Matches(msg, m.keys.Clarify):
		project := m.projects[m.cursor]
		if project.Tracker == "" {
			m.setStatus("No tracker configured for this project")
			return m, nil
		}
		agentPath := filepath.Join(
			platform.ResolvePath(project.Path.Windows, project.Path.WSL),
			".claude", "agents", "clarifier.md",
		)
		if _, err := os.Stat(agentPath); err != nil {
			// Agent not deployed — show confirm dialog
			m.showDeployConfirm()
			return m, m.confirmForm.Init()
		}
		return m, m.launchClarifierCmd()

	case key.Matches(msg, m.keys.Loop):
		project := m.projects[m.cursor]
		if project.Tracker == "" {
			m.setStatus("No tracker configured for this project")
			return m, nil
		}
		if _, ok := m.clients[project.Tracker]; !ok {
			m.setStatus("Tracker token not set")
			return m, nil
		}
		// Toggle loop on/off
		if ls, ok := m.loops[project.ID]; ok && ls.Active {
			ls.Active = false
			m.setStatus(fmt.Sprintf("Loop stopped for %s", project.Name))
			return m, nil
		}
		ls := &loop.LoopState{
			Active: true,
			Slots:  make(map[string]*loop.Slot),
		}
		m.loops[project.ID] = ls
		m.setStatus(fmt.Sprintf("Loop started for %s", project.Name))
		return m, tea.Batch(
			m.loopCleanupMergedCmd(project.ID),
			m.loopPollCmd(project.ID),
		)

	case key.Matches(msg, m.keys.Review):
		project := m.projects[m.cursor]
		if project.Tracker == "" {
			m.setStatus("No tracker configured for this project")
			return m, nil
		}
		agentPath := filepath.Join(
			platform.ResolvePath(project.Path.Windows, project.Path.WSL),
			".claude", "agents", "reviewer.md",
		)
		if _, err := os.Stat(agentPath); err != nil {
			m.showReviewerDeployConfirm()
			return m, m.confirmForm.Init()
		}
		return m, m.launchReviewerCmd()

	case key.Matches(msg, m.keys.Status):
		project := m.projects[m.cursor]
		if project.Tracker == "" {
			m.setStatus("No tracker configured for this project")
			return m, nil
		}
		m.currentView = ViewStatus
		m.statusProjectID = project.ID
		m.statusIssues = nil
		m.statusCursor = 0
		m.statusLoading = true
		m.statusError = ""
		return m, m.loadIssuesCmd()

	case key.Matches(msg, m.keys.Add):
		m.setStatus("[a] Add Project — coming in M5")

	case key.Matches(msg, m.keys.EditConfig):
		m.setStatus("[e] Edit Config — coming in M5")

	case key.Matches(msg, m.keys.Help):
		m.setStatus("[?] Help — coming soon")
	}

	return m, nil
}

func (m Model) handleStatusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.currentView = ViewProjects
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.statusCursor > 0 {
			m.statusCursor--
		}

	case key.Matches(msg, m.keys.Down):
		if m.statusCursor < len(m.statusIssues)-1 {
			m.statusCursor++
		}

	case key.Matches(msg, m.keys.Confirm):
		return m, m.confirmIssueCmd()

	case key.Matches(msg, m.keys.Tracker):
		return m, m.openIssueURLCmd()
	}

	return m, nil
}

func (m Model) handleLaunchResult(msg LaunchResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("Launch failed: %s", msg.Err))
		return m, nil
	}

	m.setStatus(fmt.Sprintf("Launched! %s", msg.Result.SwitchHint))

	at := &ActiveTerminal{
		LaunchResult:   msg.Result,
		State:          watcher.StateUnknown,
		StateChangedAt: time.Now(),
	}
	m.activeTerminals[msg.ProjectID] = at

	// Try to start watching the session log.
	return m, m.startWatcherCmd(msg.ProjectID)
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
			cmd = exec.Command("explorer", path)
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
	sessionRetryMax      = 15 // 15 * 2s = 30s max wait
	logWaitMax           = 60 // 60 * 2s = 120s max wait for JSONL
)

// startWatcherCmd phase 1: find the session PID, return immediately.
func (m Model) startWatcherCmd(projectID string) tea.Cmd {
	project := m.findProject(projectID)
	if project == nil {
		return nil
	}
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)

	return func() tea.Msg {
		claudeHome, err := watcher.ClaudeHome()
		if err != nil {
			return WatcherErrorMsg{ProjectID: projectID, Err: err}
		}

		// Retry: Claude Code needs time to start after wt.exe opens.
		var sessions []watcher.SessionInfo
		for attempt := range sessionRetryMax {
			sessions, err = watcher.FindActiveSessions(claudeHome, projectPath)
			if err != nil {
				return WatcherErrorMsg{ProjectID: projectID, Err: err}
			}
			if len(sessions) > 0 {
				break
			}
			if attempt < sessionRetryMax-1 {
				time.Sleep(sessionRetryInterval)
			}
		}

		if len(sessions) == 0 {
			return StatusMsg{Text: fmt.Sprintf("No active session found for %s (waited 30s)", projectID)}
		}

		// Use the most recently started session.
		latest := sessions[0]
		for _, s := range sessions[1:] {
			if s.StartedAt > latest.StartedAt {
				latest = s
			}
		}

		logPath := watcher.LogFilePath(claudeHome, projectPath, latest.SessionID)

		// Return PID immediately so liveness check works right away.
		// Phase 2 (waitForLogCmd) will handle waiting for the JSONL file.
		return sessionFoundMsg{ProjectID: projectID, PID: latest.PID, LogPath: logPath}
	}
}

// sessionFoundMsg is sent when the session PID is discovered but JSONL may not exist yet.
type sessionFoundMsg struct {
	ProjectID string
	PID       int
	LogPath   string
}

// waitForLogCmd phase 2: wait for the JSONL file to be created, then start the watcher.
func waitForLogCmd(projectID string, pid int, logPath string) tea.Cmd {
	return func() tea.Msg {
		for attempt := range logWaitMax {
			// Check if session process is still alive.
			if !watcher.IsProcessAlive(pid) {
				return StatusMsg{Text: fmt.Sprintf("Session ended before log created for %s", projectID)}
			}
			if _, err := os.Stat(logPath); err == nil {
				// File exists — create watcher.
				w, err := watcher.New(projectID, logPath)
				if err != nil {
					return WatcherErrorMsg{ProjectID: projectID, Err: err}
				}
				return watcherReadyMsg{ProjectID: projectID, Watcher: w}
			}
			if attempt < logWaitMax-1 {
				time.Sleep(sessionRetryInterval)
			}
		}
		return StatusMsg{Text: fmt.Sprintf("Session log not created for %s (waited 120s)", projectID)}
	}
}

// watcherReadyMsg is an internal message to attach a watcher to an ActiveTerminal.
type watcherReadyMsg struct {
	ProjectID string
	Watcher   *watcher.Watcher
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
				delete(m.activeTerminals, projectID)
			}
			continue
		}

		if at.SessionPID <= 0 {
			continue
		}
		if !watcher.IsProcessAlive(at.SessionPID) {
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
				Title("Clarifier agent 未部署至此專案，是否部署？").
				Affirmative("部署並啟動").
				Negative("取消").
				Value(confirmed),
		),
	)
	m.confirmAction = func() tea.Cmd {
		return m.deployAndLaunchClarifier()
	}
}

// deployAndLaunchClarifier deploys clarifier.md to the project and launches it.
func (m Model) deployAndLaunchClarifier() tea.Cmd {
	project := m.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	clarifierMD := m.clarifierMD
	cfg := m.cfg.Terminal

	deployTracker := func() error { return m.deployTrackerDoc(projectPath, &project) }

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
		// Deploy tracker.md
		if err := deployTracker(); err != nil {
			return StatusMsg{Text: fmt.Sprintf("Deploy tracker doc failed: %s", err)}
		}

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
			return StatusMsg{Text: "No tracker configured for this project"}
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
	".claude/docs/tracker.md",
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

// ensureLabelsCmd checks and creates missing required labels for a project's tracker.
func (m Model) ensureLabelsCmd(projectID string) tea.Cmd {
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
		created, err := tracker.EnsureLabels(ctx, lm, repo, tracker.RequiredLabels)
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
				Title("Reviewer agent 未部署至此專案，是否部署？").
				Affirmative("部署並啟動").
				Negative("取消").
				Value(confirmed),
		),
	)
	m.confirmAction = func() tea.Cmd {
		return m.deployAndLaunchReviewer()
	}
}

// deployAndLaunchReviewer deploys reviewer.md to the project and launches it.
func (m Model) deployAndLaunchReviewer() tea.Cmd {
	project := m.projects[m.cursor]
	projectPath := platform.ResolvePath(project.Path.Windows, project.Path.WSL)
	reviewerMD := m.reviewerMD
	cfg := m.cfg.Terminal
	deployTracker := func() error { return m.deployTrackerDoc(projectPath, &project) }

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

		result, err := terminal.LaunchClaude(project, cfg, "--agent", "reviewer")
		return LaunchResultMsg{
			ProjectID: project.ID,
			Result:    result,
			Err:       err,
		}
	}
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
	content := tracker.BuildTrackerDoc(provider.Type, provider.URL, project.Repo, provider.TokenEnv)
	return os.WriteFile(filepath.Join(docsDir, "tracker.md"), []byte(content), 0o644)
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

