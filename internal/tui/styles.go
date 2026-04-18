package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Catppuccin Mocha palette — truecolor hex.
var (
	mochaBase     = lipgloss.Color("#1e1e2e")
	mochaMantle   = lipgloss.Color("#181825")
	mochaCrust    = lipgloss.Color("#11111b")
	mochaSurface0 = lipgloss.Color("#313244")
	mochaSurface1 = lipgloss.Color("#45475a")
	mochaSurface2 = lipgloss.Color("#585b70")
	mochaOverlay0 = lipgloss.Color("#6c7086")
	mochaOverlay1 = lipgloss.Color("#7f849c")
	mochaSubtext0 = lipgloss.Color("#a6adc8")
	mochaSubtext1 = lipgloss.Color("#bac2de")
	mochaText     = lipgloss.Color("#cdd6f4")
	mochaMauve    = lipgloss.Color("#cba6f7")
	mochaTeal     = lipgloss.Color("#94e2d5")
	mochaGreen    = lipgloss.Color("#a6e3a1")
	mochaYellow   = lipgloss.Color("#f9e2af")
	mochaPeach    = lipgloss.Color("#fab387")
	mochaRed      = lipgloss.Color("#f38ba8")
	mochaSky      = lipgloss.Color("#89dceb")
)

// Semantic aliases — keep existing names so call sites stay unchanged.
var (
	colorAccent   = mochaMauve   // selected, focused title, focus bar
	colorText     = mochaText
	colorMuted    = mochaOverlay0
	colorTag      = mochaSubtext0
	colorStatusFg = mochaText
	colorStatusBg = mochaMantle
	colorWorking  = mochaGreen  // agent working
	colorWaiting  = mochaPeach  // agent waiting
	colorError    = mochaRed
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

	// Dock panel chrome (ViewProjects).
	panelTitleFocusedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(mochaMauve)

	panelTitleBlurredStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(mochaOverlay0)

	panelCountStyle = lipgloss.NewStyle().
			Foreground(mochaOverlay0)

	panelRuleStyle = lipgloss.NewStyle().
			Foreground(mochaSurface1)

	focusBarStyle = lipgloss.NewStyle().
			Foreground(mochaMauve)

	permissionStyle = lipgloss.NewStyle().
			Foreground(mochaYellow).
			Bold(true)

	endedStyle = lipgloss.NewStyle().
			Foreground(mochaOverlay0)
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
