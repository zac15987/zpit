package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Color palette — ANSI 256 color codes.
var (
	colorAccent   = lipgloss.Color("170") // pink-purple — selected items, hotkeys
	colorText     = lipgloss.Color("252") // light gray — normal text
	colorMuted    = lipgloss.Color("241") // dark gray — secondary text
	colorTag      = lipgloss.Color("245") // mid gray — tags
	colorStatusFg = lipgloss.Color("229") // bright yellow — status bar text
	colorStatusBg = lipgloss.Color("236") // dark gray — status bar / header background
	colorWorking  = lipgloss.Color("78")  // green — agent working
	colorWaiting  = lipgloss.Color("220") // bright yellow-orange — agent waiting
	colorError    = lipgloss.Color("203") // red — config error overlay border
)

var (
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	normalStyle = lipgloss.NewStyle().
			Foreground(colorText)

	detailStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	tagStyle = lipgloss.NewStyle().
			Foreground(colorTag)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorStatusFg).
			Background(colorStatusBg).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(2)

	headerBoxStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText).
			Background(colorStatusBg).
			Padding(0, 1)

	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorText).
				PaddingLeft(2)

	hotkeyLabelStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	hotkeyDescStyle = lipgloss.NewStyle().
			Foreground(colorText)

	workingStyle = lipgloss.NewStyle().
			Foreground(colorWorking)

	waitingStyle = lipgloss.NewStyle().
			Foreground(colorWaiting).
			Bold(true)

	questionStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	confirmOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Padding(1, 2)

	errorOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorError).
				Padding(1, 2)
)

// styledHotkeys highlights [key] patterns in s with hotkeyLabelStyle,
// leaving the rest in hotkeyDescStyle.
func styledHotkeys(s string) string {
	var b strings.Builder
	for {
		open := strings.IndexByte(s, '[')
		if open == -1 {
			b.WriteString(hotkeyDescStyle.Render(s))
			break
		}
		close := strings.IndexByte(s[open:], ']')
		if close == -1 {
			b.WriteString(hotkeyDescStyle.Render(s))
			break
		}
		close += open // absolute index
		if open > 0 {
			b.WriteString(hotkeyDescStyle.Render(s[:open]))
		}
		b.WriteString(hotkeyLabelStyle.Render(s[open : close+1]))
		s = s[close+1:]
	}
	return b.String()
}
