package tui

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/notify"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/terminal"
	"github.com/zac15987/zpit/internal/watcher"
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
	// Future: ViewStatus, ViewLoop, ViewHelp
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
}

// NewModel creates the root TUI model.
func NewModel(cfg *config.Config) Model {
	return Model{
		cfg:             cfg,
		env:             platform.Detect(),
		keys:            DefaultKeyMap(),
		notifier:        notify.NewNotifier(cfg.Notification),
		currentView:     ViewProjects,
		projects:        cfg.Projects,
		activeTerminals: make(map[string]*ActiveTerminal),
	}
}

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	}

	return m, nil
}

func (m Model) View() string {
	switch m.currentView {
	case ViewProjects:
		return m.viewProjects()
	default:
		return "Unknown view"
	}
}

func (m *Model) setStatus(text string) {
	m.statusMessage = text
	m.statusExpiry = time.Now().Add(statusDisplayDuration)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		// Stop all watchers on quit.
		for _, at := range m.activeTerminals {
			if at.Watcher != nil {
				at.Watcher.Stop()
			}
		}
		return m, tea.Quit

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
		m.setStatus("[p] Open Tracker — coming in M3")

	case key.Matches(msg, m.keys.Clarify):
		m.setStatus("[c] Clarify — coming in M3")

	case key.Matches(msg, m.keys.Loop):
		m.setStatus("[l] Loop — coming in M4")

	case key.Matches(msg, m.keys.Review):
		m.setStatus("[r] Review — coming in M4")

	case key.Matches(msg, m.keys.Status):
		m.setStatus("[s] Status — coming in M3")

	case key.Matches(msg, m.keys.Add):
		m.setStatus("[a] Add Project — coming in M5")

	case key.Matches(msg, m.keys.EditConfig):
		m.setStatus("[e] Edit Config — coming in M5")

	case key.Matches(msg, m.keys.Help):
		m.setStatus("[?] Help — coming soon")
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
