package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the TUI.
type KeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Clarify key.Binding
	Loop    key.Binding
	Review  key.Binding
	Status  key.Binding
	Open    key.Binding
	Tracker    key.Binding
	Add        key.Binding
	EditConfig key.Binding
	Channel    key.Binding
	Undeploy   key.Binding
	Help       key.Binding
	Quit        key.Binding
	Back        key.Binding
	Confirm     key.Binding
	FocusSwitch key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "launch Claude Code")),
		Clarify: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clarify")),
		Loop:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "loop")),
		Review:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "review")),
		Status:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "status")),
		Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open folder")),
		Tracker:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "tracker")),
		Add:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add project")),
		EditConfig: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit config")),
		Channel:    key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "channel")),
		Undeploy:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "undeploy")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
		Confirm:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm")),
		FocusSwitch: key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "switch focus")),
	}
}
