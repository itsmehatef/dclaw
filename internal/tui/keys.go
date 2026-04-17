package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all key bindings for the dclaw TUI.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Describe key.Binding
	Chat     key.Binding // alpha.3: open chat view
	Refresh  key.Binding
	Help     key.Binding
	Quit     key.Binding
}

// DefaultKeys returns the shared global keymap.
func DefaultKeys() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open detail")),
		Back:     key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
		Describe: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "describe")),
		Chat:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "chat")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp returns the abbreviated help row.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Back, k.Describe, k.Chat, k.Refresh, k.Help, k.Quit}
}

// FullHelp returns all bindings in one row.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
