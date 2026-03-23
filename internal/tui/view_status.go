package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/zac15987/zpit/internal/tracker"
)

func (m Model) viewStatus() string {
	var b strings.Builder
	b.WriteString(m.renderStatusHeader())
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	b.WriteString(m.renderStatusFooter())
	return b.String()
}

// renderStatusHeader returns the fixed header for the status view.
func (m Model) renderStatusHeader() string {
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")
	return b.String()
}

// renderStatusScrollable returns the scrollable content for the status view.
func (m Model) renderStatusScrollable() string {
	var b strings.Builder

	projectName := m.projectName(m.statusProjectID)
	b.WriteString(sectionTitleStyle.Render(fmt.Sprintf("Issues — %s", projectName)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 60) + "\n\n")

	if m.statusLoading {
		b.WriteString("  Loading issues...\n")
	} else if m.statusError != "" {
		b.WriteString(fmt.Sprintf("  Error: %s\n", m.statusError))
	} else if len(m.statusIssues) == 0 {
		b.WriteString("  No issues found.\n")
	} else {
		for i, issue := range m.statusIssues {
			cursor := "   "
			if i == m.statusCursor {
				cursor = cursorMarker
			}

			badge := renderStatusBadge(issue.Status)
			title := issue.Title
			if i == m.statusCursor {
				title = selectedStyle.Render(title)
			} else {
				title = normalStyle.Render(title)
			}

			b.WriteString(fmt.Sprintf("%s#%-4s %s %s\n", cursor, issue.ID, badge, title))
		}
	}

	return b.String()
}

// renderStatusFooter returns the fixed footer for the status view.
func (m Model) renderStatusFooter() string {
	var b strings.Builder
	hotkeys := []struct{ key, desc string }{
		{"y", "Confirm (pending→todo)"},
		{"p", "Open in browser"},
		{"Esc", "Back"},
	}
	for _, h := range hotkeys {
		b.WriteString("  ")
		b.WriteString(hotkeyLabelStyle.Render(fmt.Sprintf("[%s]", h.key)))
		b.WriteString(" ")
		b.WriteString(hotkeyDescStyle.Render(h.desc))
	}
	b.WriteString("\n\n")
	if m.statusMessage != "" && time.Now().Before(m.statusExpiry) {
		b.WriteString(statusBarStyle.Render(" " + m.statusMessage + " "))
	}
	return b.String()
}

func renderStatusBadge(status string) string {
	switch status {
	case tracker.StatusPendingConfirm:
		return waitingStyle.Render("[pending]")
	case tracker.StatusTodo:
		return normalStyle.Render("[todo]   ")
	case tracker.StatusInProgress:
		return workingStyle.Render("[wip]    ")
	case tracker.StatusAIReview:
		return detailStyle.Render("[review] ")
	case tracker.StatusWaitingReview:
		return detailStyle.Render("[review] ")
	case tracker.StatusNeedsVerify:
		return waitingStyle.Render("[verify] ")
	case tracker.StatusDone:
		return detailStyle.Render("[done]   ")
	case "open":
		return normalStyle.Render("[open]   ")
	default:
		return detailStyle.Render(fmt.Sprintf("[%-8s]", status))
	}
}
