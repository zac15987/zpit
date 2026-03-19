package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			PaddingLeft(1)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	detailStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	tagStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			PaddingLeft(2)

	headerBoxStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("252")).
				PaddingLeft(2)

	hotkeyLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("170")).
				Bold(true)

	hotkeyDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
)
