package tui

// editconfig.go — Edit config sub-menu key handlers and commands.
//
// Lock protocol:
//   - handleEditConfigKey: no locks (reads per-connection UI state only)
//   - sub-handlers that mutate AppState: delegate to AppState methods that acquire their own locks

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// handleEditConfigKey dispatches key events to the active sub-view handler.
func (m Model) handleEditConfigKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.editConfigSub {
	case EditConfigMenu:
		return m.handleEditConfigMenuKey(msg)
	case EditConfigListenList:
		return m.handleEditConfigListenKey(msg)
	}
	return m, nil
}

// handleEditConfigMenuKey handles keys in the main 3-option menu.
func (m Model) handleEditConfigMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.currentView = ViewProjects
		m.viewport.GotoTop()
		return m, nil

	case key.Matches(msg, m.keys.Option1):
		// Toggle channel_enabled for current project — implemented in T6.
		return m, nil

	case key.Matches(msg, m.keys.Option2):
		// Enter channel_listen multi-select — implemented in T6.
		return m, nil

	case key.Matches(msg, m.keys.Option3):
		// Open config in editor — implemented in T7.
		return m, nil

	case key.Matches(msg, m.keys.Reload):
		// Manual reload (for SSH remote mode) — implemented in T7.
		return m, nil
	}
	return m, nil
}

// handleEditConfigListenKey handles keys in the channel_listen multi-select sub-view.
func (m Model) handleEditConfigListenKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.editConfigSub = EditConfigMenu
		return m, nil
	}
	return m, nil
}
