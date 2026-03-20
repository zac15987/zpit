package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// UI symbols.
const (
	iconMachine  = "⚙️ "
	iconWeb      = "🌐 "
	iconDesktop  = "🖥️ "
	iconAndroid  = "📱 "
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

	// Header bar
	header := m.renderHeader()
	b.WriteString(header)
	b.WriteString("\n\n")

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

	// Status bar
	b.WriteString("\n\n")
	if m.statusMessage != "" && time.Now().Before(m.statusExpiry) {
		b.WriteString(statusBarStyle.Render(" "+m.statusMessage+" "))
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
	for projectID, result := range m.activeTerminals {
		b.WriteString(fmt.Sprintf("  [%d] %s %s %s\n",
			i,
			selectedStyle.Render(m.projectName(projectID)),
			boxVert,
			detailStyle.Render(result.SwitchHint),
		))
		i++
	}

	return b.String()
}
