package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the TUI.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Enter   key.Binding
	Clarify key.Binding
	Loop    key.Binding
	Review     key.Binding
	Efficiency key.Binding
	Status     key.Binding
	Open    key.Binding
	Tracker      key.Binding
	OpenPR       key.Binding
	Lazygit      key.Binding
	ClaudeUpdate key.Binding
	Add        key.Binding
	EditConfig key.Binding
	Channel    key.Binding
	Undeploy   key.Binding
	Redeploy   key.Binding
	Help       key.Binding
	Quit        key.Binding
	Back        key.Binding
	Confirm     key.Binding
	FocusSwitch key.Binding
	Kill        key.Binding

	// Git status page keys
	GitStatus  key.Binding
	GitFetch   key.Binding
	GitPull    key.Binding
	GitRefresh key.Binding

	// Edit config sub-menu keys
	Option1 key.Binding
	Option2 key.Binding
	Option3 key.Binding
	Space   key.Binding
	Reload  key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("PgDn", "page down")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "launch Claude Code")),
		Clarify: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clarify")),
		Loop:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "loop")),
		Review:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "review")),
		Efficiency: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "efficiency")),
		Status:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "status")),
		Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open folder")),
		Tracker:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "tracker")),
		OpenPR:       key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "open PR")),
		Lazygit:      key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "lazygit")),
		ClaudeUpdate: key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "claude update")),
		Add:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add project")),
		EditConfig: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit config")),
		Channel:    key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "channel")),
		Undeploy:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "undeploy")),
		Redeploy:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "redeploy")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
		Confirm:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm")),
		FocusSwitch: key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "switch focus")),
		Kill:        key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "kill terminal")),

		GitStatus:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "git status")),
		GitFetch:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fetch")),
		GitPull:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull")),
		GitRefresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),

		Option1: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "option 1")),
		Option2: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "option 2")),
		Option3: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "option 3")),
		Space:   key.NewBinding(key.WithKeys(" "), key.WithHelp("Space", "toggle")),
		Reload:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
	}
}
