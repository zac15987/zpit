package tui

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/platform"
	"github.com/zac15987/zpit/internal/terminal"
)

const statusDisplayDuration = 5 * time.Second

// View represents the current screen.
type View int

const (
	ViewProjects View = iota
	// Future: ViewStatus, ViewLoop, ViewHelp
)

// Model is the root Bubble Tea model.
type Model struct {
	cfg *config.Config
	env platform.Environment
	keys KeyMap

	width  int
	height int

	currentView View

	// Project list state
	projects      []config.ProjectConfig
	cursor        int
	statusMessage string
	statusExpiry  time.Time

	// Active terminals
	activeTerminals map[string]*terminal.LaunchResult
}

// NewModel creates the root TUI model.
func NewModel(cfg *config.Config) Model {
	return Model{
		cfg:             cfg,
		env:             platform.Detect(),
		keys:            DefaultKeyMap(),
		currentView:     ViewProjects,
		projects:        cfg.Projects,
		activeTerminals: make(map[string]*terminal.LaunchResult),
	}
}

func (m Model) Init() tea.Cmd {
	return nil
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
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("Launch failed: %s", msg.Err))
		} else {
			m.setStatus(fmt.Sprintf("Launched! %s", msg.Result.SwitchHint))
			m.activeTerminals[msg.ProjectID] = msg.Result
		}
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

	case key.Matches(msg, m.keys.Help):
		m.setStatus("[?] Help — coming soon")
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
