package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/zac15987/zpit/internal/loop"
	"github.com/zac15987/zpit/internal/watcher"
)

// UI symbols.
const (
	iconMachine  = "⚙️ "
	iconWeb      = "🌐 "
	iconDesktop  = "🖥️ "
	iconAndroid  = "📱 "
	iconWorking  = "🟢"
	iconWaiting  = "🟡"
	iconEnded    = "⚫"
	cursorMarker = " › "
	boxHoriz     = "─"
	boxVert      = "│"
)

var profileIcons = map[string]string{
	"machine": iconMachine,
	"web":     iconWeb,
	"desktop": iconDesktop,
	"android": iconAndroid,
}

func (m Model) viewProjects() string {
	var b strings.Builder
	b.WriteString(m.renderProjectsHeader())
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	b.WriteString(m.renderProjectsFooter())
	return b.String()
}

// renderProjectsHeader returns the fixed header above the scrollable area.
func (m Model) renderProjectsHeader() string {
	return m.renderHeader() + "\n\n"
}

// renderProjectsScrollable returns the scrollable content for the projects view.
func (m Model) renderProjectsScrollable() string {
	var b strings.Builder

	// Two-column: project list + hotkeys
	left := m.renderProjectList()
	right := m.renderHotkeys()
	columns := lipgloss.JoinHorizontal(lipgloss.Top, left, "    ", right)
	b.WriteString(columns)

	// Active terminals (if any)
	if len(m.activeTerminals) > 0 {
		b.WriteString("\n\n")
		b.WriteString(m.renderActiveTerminals())
	}

	// Loop status (if any)
	loopStatus := m.renderLoopStatus()
	if loopStatus != "" {
		b.WriteString("\n\n")
		b.WriteString(loopStatus)
	}

	return b.String()
}

// renderProjectsFooter returns the fixed footer below the scrollable area.
func (m Model) renderProjectsFooter() string {
	var b strings.Builder
	if m.statusMessage != "" && time.Now().Before(m.statusExpiry) {
		b.WriteString(statusBarStyle.Render(" " + m.statusMessage + " "))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Press ? for help, q to quit"))
	return b.String()
}

func (m Model) renderHeader() string {
	now := time.Now().Format("01/02 15:04")
	env := m.env.String()
	title := fmt.Sprintf(" Zpit v0.1                                    %s  %s ", now, env)
	return headerBoxStyle.Render(title)
}

func (m Model) renderProjectList() string {
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Projects"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 32) + "\n\n")

	for i, p := range m.projects {
		icon := profileIcons[p.Profile]
		if icon == "" {
			icon = "  "
		}

		cursor := "   "
		if i == m.cursor {
			cursor = cursorMarker
		}

		name := p.Name
		if i == m.cursor {
			name = selectedStyle.Render(name)
		} else {
			name = normalStyle.Render(name)
		}

		tags := ""
		if len(p.Tags) > 0 {
			tags = tagStyle.Render(strings.Join(p.Tags, ", "))
		}

		b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, icon, name))
		b.WriteString(fmt.Sprintf("     %s",
			detailStyle.Render(p.Profile)))
		if tags != "" {
			b.WriteString(fmt.Sprintf(" %s %s", boxVert, tags))
		}
		b.WriteString("\n\n")
	}

	return b.String()
}

func (m Model) renderHotkeys() string {
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Hotkeys"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 26) + "\n\n")

	hotkeys := []struct {
		key  string
		desc string
		sep  bool // insert blank line before this entry
	}{
		{"Enter", "Launch Claude Code", false},
		{"c", "Clarify requirement", false},
		{"l", "Loop auto-implement", false},
		{"r", "Review changes", false},
		{"s", "Status overview", false},
		{"o", "Open project folder", false},
		{"p", "Open Issue Tracker", false},
		{"a", "Add project", true},
		{"e", "Edit config", false},
		{"?", "Help", true},
		{"q", "Quit", false},
	}

	for _, h := range hotkeys {
		if h.sep {
			b.WriteString("\n")
		}
		k := hotkeyLabelStyle.Render(fmt.Sprintf("[%s]", h.key))
		d := hotkeyDescStyle.Render(h.desc)
		b.WriteString(fmt.Sprintf("  %s %s\n", k, d))
	}

	return b.String()
}

func (m Model) projectName(id string) string {
	for _, p := range m.projects {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

func (m Model) renderActiveTerminals() string {
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Active Terminals"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat(boxHoriz, 50) + "\n")

	i := 1
	for projectID, at := range m.activeTerminals {
		// Status icon and text.
		statusIcon, statusText := renderAgentStatus(at)

		// Elapsed time since last state change.
		elapsed := formatElapsed(time.Since(at.StateChangedAt))

		b.WriteString(fmt.Sprintf("  [%d] %s %s %s %s\n",
			i,
			selectedStyle.Render(m.projectName(projectID)),
			boxVert,
			statusText,
			detailStyle.Render(elapsed),
		))

		// Question preview when waiting.
		if at.LastQuestion != "" && statusIcon == iconWaiting {
			preview := truncate(at.LastQuestion, 80)
			b.WriteString(fmt.Sprintf("      %s %s\n",
				detailStyle.Render("Q:"),
				questionStyle.Render(preview),
			))
		}

		// Switch hint.
		if at.LaunchResult != nil && at.LaunchResult.SwitchHint != "" {
			b.WriteString(fmt.Sprintf("      %s\n",
				detailStyle.Render(at.LaunchResult.SwitchHint),
			))
		}
		i++
	}

	return b.String()
}

func renderAgentStatus(at *ActiveTerminal) (string, string) {
	switch at.State {
	case watcher.StateEnded:
		return iconEnded, detailStyle.Render(iconEnded + " Session ended")
	case watcher.StateWaiting:
		return iconWaiting, waitingStyle.Render(iconWaiting + " Waiting for input")
	case watcher.StateWorking:
		return iconWorking, workingStyle.Render(iconWorking + " Working")
	case watcher.StateStreaming:
		return iconWorking, workingStyle.Render(iconWorking + " Working")
	default:
		return iconWorking, detailStyle.Render(iconWorking + " Launched")
	}
}

func formatElapsed(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

func (m Model) renderLoopStatus() string {
	var b strings.Builder
	hasContent := false

	// Sort project IDs for stable render order.
	projectIDs := make([]string, 0, len(m.loops))
	for pid := range m.loops {
		projectIDs = append(projectIDs, pid)
	}
	sort.Strings(projectIDs)

	for _, projectID := range projectIDs {
		ls := m.loops[projectID]
		if !ls.Active && len(ls.Slots) == 0 {
			continue
		}
		if !hasContent {
			b.WriteString(sectionTitleStyle.Render("Loop Status"))
			b.WriteString("\n")
			b.WriteString("  " + strings.Repeat(boxHoriz, 50) + "\n")
			hasContent = true
		}

		projectName := m.projectName(projectID)
		status := "running"
		if !ls.Active {
			status = "stopping"
		}
		b.WriteString(fmt.Sprintf("  %s (%s)\n",
			selectedStyle.Render(projectName),
			detailStyle.Render(status),
		))

		if len(ls.Slots) == 0 {
			b.WriteString(fmt.Sprintf("    %s\n", detailStyle.Render("polling for issues...")))
			continue
		}

		// Sort slot keys for stable render order.
		slotKeys := make([]string, 0, len(ls.Slots))
		for k := range ls.Slots {
			slotKeys = append(slotKeys, k)
		}
		sort.Strings(slotKeys)

		for _, key := range slotKeys {
			slot := ls.Slots[key]
			icon := iconWorking
			switch slot.State {
			case loop.SlotError:
				icon = "🔴"
			case loop.SlotCheckingReview:
				icon = "🔍"
			case loop.SlotNeedsHuman:
				icon = "🟠"
			case loop.SlotWaitingPRMerge:
				icon = iconWaiting
			case loop.SlotDone:
				icon = "✅"
			}
			stateText := slot.State.String()
			if slot.ReviewRound > 0 {
				stateText += fmt.Sprintf(" (round %d/%d)", slot.ReviewRound, m.cfg.Worktree.MaxReviewRounds)
			}
			b.WriteString(fmt.Sprintf("    %s #%s %s  %s\n",
				icon, slot.IssueID,
				truncate(slot.IssueTitle, 35),
				detailStyle.Render(stateText),
			))
			if slot.Error != nil {
				b.WriteString(fmt.Sprintf("      %s\n",
					detailStyle.Render(slot.Error.Error()),
				))
			}
		}
	}

	return b.String()
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
