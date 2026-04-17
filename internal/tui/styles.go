package tui

import "github.com/charmbracelet/lipgloss"

// Shared lipgloss styles. All views import this package to get consistent
// colours. If a terminal has NO_COLOR set, lipgloss degrades gracefully.

var (
	// TopBarStyle is the full-width header: view name + daemon state.
	TopBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1).
			Bold(true)

	// BottomBarStyle is the full-width keybinding hint row.
	BottomBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("240")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1)

	// ListHeaderStyle renders the column-header row in the list view.
	ListHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	// SelectedRowStyle highlights the cursor row in the list.
	SelectedRowStyle = lipgloss.NewStyle().
				Reverse(true).
				Bold(true)

	// DimStyle is used for secondary text (id-prefix, timestamps).
	DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// SectionHeaderStyle renders labelled sections inside detail/describe.
	SectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("213"))

	// ErrorStyle renders the noDaemon error box.
	ErrorStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1, 2)

	// HelpOverlayStyle is the full-screen help modal background.
	HelpOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62")).
				Padding(1, 2)
)

// StatusColor maps agent status strings to ANSI 256 colour codes for the
// list view's STATUS column.
func StatusColor(status string) lipgloss.Style {
	switch status {
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82")) // green
	case "stopped", "exited":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // orange
	case "created":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // blue
	case "dead", "oomkilled":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // grey
	}
}
