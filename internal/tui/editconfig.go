package tui

// editconfig.go — Edit config sub-menu key handlers and commands.
//
// Lock protocol:
//   - handleEditConfigKey: no locks (reads per-connection UI state only)
//   - sub-handlers that mutate AppState: delegate to AppState methods that acquire their own locks

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/config"
	"github.com/zac15987/zpit/internal/locale"
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
		// Toggle channel_enabled for current project.
		projectID := m.editConfigProjectID
		newEnabled, subCmds, err := m.state.ToggleChannel(projectID)
		if err != nil {
			m.setStatus(fmt.Sprintf(locale.T(locale.KeyChannelBrokerStartFail), err))
			return m, nil
		}
		// Persist to config.toml asynchronously.
		cmds := append(subCmds, writeChannelEnabledCmd(m.state.logger, projectID, newEnabled))
		if newEnabled {
			m.setStatus(fmt.Sprintf(locale.T(locale.KeyChannelToggleOn), projectID))
		} else {
			m.setStatus(fmt.Sprintf(locale.T(locale.KeyChannelToggleOff), projectID))
		}
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.Option2):
		// Build the multi-select list from all other projects + _global.
		m.state.RLock()
		var currentListen []string
		for _, p := range m.state.projects {
			if p.ID == m.editConfigProjectID {
				currentListen = p.ChannelListen
				break
			}
		}
		var items []editConfigListenItem
		for _, p := range m.state.projects {
			if p.ID == m.editConfigProjectID {
				continue
			}
			items = append(items, editConfigListenItem{
				Key:     p.ID,
				Name:    p.Name,
				Checked: stringSliceContains(currentListen, p.ID),
			})
		}
		m.state.RUnlock()

		// Insert _global at the beginning.
		globalItem := editConfigListenItem{
			Key:     "_global",
			Name:    "Global",
			Checked: stringSliceContains(currentListen, "_global"),
		}
		items = append([]editConfigListenItem{globalItem}, items...)

		m.editConfigListenItems = items
		m.editConfigListenCursor = 0
		m.editConfigSub = EditConfigListenList
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
		// Cancel — discard changes, return to menu.
		m.editConfigSub = EditConfigMenu
		m.setStatus(locale.T(locale.KeyChannelListenNoChange))
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.editConfigListenCursor > 0 {
			m.editConfigListenCursor--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.editConfigListenCursor < len(m.editConfigListenItems)-1 {
			m.editConfigListenCursor++
		}
		return m, nil

	case key.Matches(msg, m.keys.Space):
		if m.editConfigListenCursor >= 0 && m.editConfigListenCursor < len(m.editConfigListenItems) {
			m.editConfigListenItems[m.editConfigListenCursor].Checked =
				!m.editConfigListenItems[m.editConfigListenCursor].Checked
		}
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		// Confirm — collect checked items and apply.
		var newListen []string
		for _, item := range m.editConfigListenItems {
			if item.Checked {
				newListen = append(newListen, item.Key)
			}
		}
		projectID := m.editConfigProjectID
		m.editConfigSub = EditConfigMenu

		// Update in-memory config under write lock.
		m.state.Lock()
		for i := range m.state.projects {
			if m.state.projects[i].ID == projectID {
				m.state.projects[i].ChannelListen = newListen
				m.state.cfg.Projects[i].ChannelListen = newListen
				break
			}
		}
		m.state.NotifyAll()
		m.state.Unlock()

		if len(newListen) > 0 {
			m.setStatus(fmt.Sprintf(locale.T(locale.KeyChannelListenUpdated), strings.Join(newListen, ", ")))
		} else {
			m.setStatus(locale.T(locale.KeyChannelListenNoChange))
		}

		// Persist to config.toml asynchronously.
		return m, writeChannelListenCmd(m.state.logger, projectID, newListen)
	}
	return m, nil
}

// configPath returns the config file path from $ZPIT_CONFIG or the default location.
func configPath() string {
	if p := os.Getenv("ZPIT_CONFIG"); p != "" {
		return p
	}
	p, err := config.DefaultConfigPath()
	if err != nil {
		return ""
	}
	return p
}

// writeChannelEnabledCmd persists channel_enabled to config.toml asynchronously.
func writeChannelEnabledCmd(logger interface{ Printf(string, ...interface{}) }, projectID string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		cfgPath := configPath()
		if cfgPath == "" {
			return nil
		}
		if err := config.UpdateProjectField(cfgPath, projectID, "channel_enabled", enabled); err != nil {
			logger.Printf("config: write channel_enabled failed project=%s: %v", projectID, err)
		} else {
			logger.Printf("config: wrote channel_enabled=%v for project=%s", enabled, projectID)
		}
		return nil
	}
}

// writeChannelListenCmd persists channel_listen to config.toml asynchronously.
func writeChannelListenCmd(logger interface{ Printf(string, ...interface{}) }, projectID string, listen []string) tea.Cmd {
	return func() tea.Msg {
		cfgPath := configPath()
		if cfgPath == "" {
			return nil
		}
		if err := config.UpdateProjectField(cfgPath, projectID, "channel_listen", listen); err != nil {
			logger.Printf("config: write channel_listen failed project=%s: %v", projectID, err)
		} else {
			logger.Printf("config: wrote channel_listen=%v for project=%s", listen, projectID)
		}
		return nil
	}
}

// stringSliceContains returns true if the slice contains the given string.
func stringSliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
