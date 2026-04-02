package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/zac15987/zpit/internal/broker"
	"github.com/zac15987/zpit/internal/locale"
)

// viewChannel renders the channel event timeline view.
// Follows the same header + viewport + footer structure as viewStatus.
func (m Model) viewChannel() string {
	header := m.renderChannelHeader()
	footer := m.renderChannelFooter()
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if contentHeight < 1 {
		contentHeight = 1
	}
	content := lipgloss.NewStyle().Width(m.width).Height(contentHeight).Render(m.viewport.View())
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// renderChannelHeader returns the fixed header for the channel view.
func (m Model) renderChannelHeader() string {
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")
	return b.String()
}

// renderChannelScrollable returns the scrollable content for the channel view.
func (m Model) renderChannelScrollable() string {
	var b strings.Builder

	projectName := m.projectName(m.channelProjectID)
	b.WriteString(sectionTitleStyle.Render(fmt.Sprintf(locale.T(locale.KeyChannelTitle), projectName)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 60) + "\n\n")

	p := m.findProject(m.channelProjectID)
	if p != nil && !p.ChannelEnabled {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyChannelDisabled)) + "\n")
		return b.String()
	}

	events := m.state.channelEvents[m.channelProjectID]
	if len(events) == 0 {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyChannelNoActivity)) + "\n")
		return b.String()
	}

	for _, ev := range events {
		b.WriteString("  ")
		b.WriteString(formatChannelEvent(ev))
		b.WriteString("\n")
	}

	return b.String()
}

// renderChannelFooter returns the fixed footer for the channel view.
func (m Model) renderChannelFooter() string {
	var b strings.Builder
	hotkeys := []struct{ key, desc string }{
		{"↑/↓", locale.T(locale.KeyChannelScroll)},
		{"Esc", locale.T(locale.KeyChannelBack)},
	}
	for _, h := range hotkeys {
		b.WriteString("  ")
		b.WriteString(hotkeyLabelStyle.Render(fmt.Sprintf("[%s]", h.key)))
		b.WriteString(" ")
		b.WriteString(hotkeyDescStyle.Render(h.desc))
	}
	b.WriteString("\n")
	return b.String()
}

// formatChannelEvent formats a single broker event as a timeline line.
// Format: {HH:MM:SS}  {icon} #{issueID} {action}: {content_preview}
func formatChannelEvent(ev broker.Event) string {
	icon := "📦"
	if ev.Type == "message" {
		icon = "💬"
	}

	var ts time.Time
	var issueID, action, content string

	switch ev.Type {
	case "artifact":
		var art broker.Artifact
		if err := json.Unmarshal(ev.Payload, &art); err == nil {
			ts = art.Timestamp
			issueID = art.IssueID
			action = art.Type
			content = art.Content
		}
	case "message":
		var msg broker.Message
		if err := json.Unmarshal(ev.Payload, &msg); err == nil {
			ts = msg.Timestamp
			issueID = msg.From
			action = fmt.Sprintf("→ #%s", msg.To)
			content = msg.Content
		}
	}

	timeStr := ts.Format("15:04:05")
	preview := truncateChannel(content, 120)

	return fmt.Sprintf("%s  %s #%s %s: %s",
		detailStyle.Render(timeStr),
		icon,
		issueID,
		detailStyle.Render(action),
		normalStyle.Render(preview),
	)
}

// truncateChannel truncates a string to maxLen runes, appending "..." if truncated.
func truncateChannel(s string, maxLen int) string {
	// Collapse whitespace to single line.
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
