package tui

// view_editconfig.go — Edit config sub-menu rendering.
//
// Lock protocol:
//   All render methods in this file are called from syncViewportContent (which
//   holds RLock) or from View() (which also holds RLock). They must NOT acquire
//   locks themselves.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/zac15987/zpit/internal/locale"
)

// viewEditConfig renders the complete edit config screen (header + viewport + footer).
func (m Model) viewEditConfig() string {
	header := m.renderEditConfigHeader()
	footer := m.renderEditConfigFooter()
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if contentHeight < 1 {
		contentHeight = 1
	}
	content := lipgloss.NewStyle().Width(m.width).Height(contentHeight).Render(m.viewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// renderEditConfigHeader returns the fixed header for the edit config view.
// Called under existing RLock — must not acquire locks.
func (m Model) renderEditConfigHeader() string {
	projectName := m.editConfigProjectID
	for _, p := range m.state.projects {
		if p.ID == m.editConfigProjectID {
			projectName = p.Name
			break
		}
	}

	title := fmt.Sprintf(locale.T(locale.KeyEditConfigTitle), projectName)
	return fmt.Sprintf(" %s\n %s\n", title, strings.Repeat(boxHoriz, len(title)+2))
}

// renderEditConfigFooter returns the fixed footer for the edit config view.
// Called under existing RLock — must not acquire locks.
func (m Model) renderEditConfigFooter() string {
	switch m.editConfigSub {
	case EditConfigMenu:
		status := ""
		if m.statusMessage != "" && time.Now().Before(m.statusExpiry) {
			status = "  " + m.statusMessage
		}
		return fmt.Sprintf("\n %s%s", locale.T(locale.KeyEditConfigFooter), status)
	case EditConfigListenList:
		return fmt.Sprintf("\n %s", locale.T(locale.KeyChannelListenFooter))
	}
	return ""
}

// renderEditConfigScrollable returns the scrollable content for the edit config view.
// Called under existing RLock — must not acquire locks.
func (m Model) renderEditConfigScrollable() string {
	switch m.editConfigSub {
	case EditConfigMenu:
		return m.renderEditConfigMenuContent()
	case EditConfigListenList:
		return m.renderEditConfigListenContent()
	}
	return ""
}

// renderEditConfigMenuContent renders the main 3-option menu with current channel status.
func (m Model) renderEditConfigMenuContent() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s\n", locale.T(locale.KeyEditConfigOption1)))
	b.WriteString(fmt.Sprintf("  %s\n", locale.T(locale.KeyEditConfigOption2)))
	b.WriteString(fmt.Sprintf("  %s\n", locale.T(locale.KeyEditConfigOption3)))

	// Show current channel status for context.
	for _, p := range m.state.projects {
		if p.ID == m.editConfigProjectID {
			if p.ChannelEnabled {
				b.WriteString("\n  channel_enabled = true")
			} else {
				b.WriteString("\n  channel_enabled = false")
			}
			if len(p.ChannelListen) > 0 {
				b.WriteString(fmt.Sprintf("\n  channel_listen = %v", p.ChannelListen))
			}
			break
		}
	}
	return b.String()
}

// renderEditConfigListenContent renders the channel_listen multi-select list.
func (m Model) renderEditConfigListenContent() string {
	var b strings.Builder
	b.WriteString("\n")
	for i, item := range m.editConfigListenItems {
		cursor := "  "
		if i == m.editConfigListenCursor {
			cursor = " >"
		}
		check := "[ ]"
		if item.Checked {
			check = "[x]"
		}
		b.WriteString(fmt.Sprintf("%s %s %s (%s)\n", cursor, check, item.Name, item.Key))
	}
	return b.String()
}
