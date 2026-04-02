package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/zac15987/zpit/internal/broker"
	"github.com/zac15987/zpit/internal/locale"
)

// taggedEvent pairs a broker event with its source project for cross-project display.
type taggedEvent struct {
	source string
	event  broker.Event
}

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

	// Collect events from own project + listen projects.
	var all []taggedEvent
	for _, ev := range m.state.channelEvents[m.channelProjectID] {
		all = append(all, taggedEvent{source: "", event: ev}) // own project: no tag
	}
	if p != nil {
		for _, lp := range p.ChannelListen {
			for _, ev := range m.state.channelEvents[lp] {
				all = append(all, taggedEvent{source: lp, event: ev})
			}
		}
	}

	if len(all) == 0 {
		b.WriteString("  " + detailStyle.Render(locale.T(locale.KeyChannelNoActivity)) + "\n")
		return b.String()
	}

	// Sort by timestamp for chronological display.
	sort.Slice(all, func(i, j int) bool {
		return extractEventTimestamp(all[i].event).Before(extractEventTimestamp(all[j].event))
	})

	for _, te := range all {
		b.WriteString("  ")
		b.WriteString(formatChannelEvent(te.event, te.source))
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
// Format: {HH:MM:SS}  [source] {icon} #{issueID} {action}: {content_preview}
// sourceProject is shown as a prefix tag for cross-project events (empty for own project).
func formatChannelEvent(ev broker.Event, sourceProject string) string {
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

	projectTag := ""
	if sourceProject != "" {
		projectTag = detailStyle.Render(fmt.Sprintf("[%s] ", sourceProject))
	}

	return fmt.Sprintf("%s  %s%s #%s %s: %s",
		detailStyle.Render(timeStr),
		projectTag,
		icon,
		issueID,
		detailStyle.Render(action),
		normalStyle.Render(preview),
	)
}

// extractEventTimestamp extracts the timestamp from a broker event payload for sorting.
func extractEventTimestamp(ev broker.Event) time.Time {
	switch ev.Type {
	case "artifact":
		var art broker.Artifact
		if err := json.Unmarshal(ev.Payload, &art); err == nil {
			return art.Timestamp
		}
	case "message":
		var msg broker.Message
		if err := json.Unmarshal(ev.Payload, &msg); err == nil {
			return msg.Timestamp
		}
	}
	return time.Time{}
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
