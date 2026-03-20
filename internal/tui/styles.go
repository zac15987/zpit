package tui

import "github.com/charmbracelet/lipgloss"

// Color palette — ANSI 256 color codes.
var (
	colorAccent   = lipgloss.Color("170") // pink-purple — selected items, hotkeys
	colorText     = lipgloss.Color("252") // light gray — normal text
	colorMuted    = lipgloss.Color("241") // dark gray — secondary text
	colorTag      = lipgloss.Color("245") // mid gray — tags
	colorStatusFg = lipgloss.Color("229") // bright yellow — status bar text
	colorStatusBg = lipgloss.Color("236") // dark gray — status bar / header background
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
)
