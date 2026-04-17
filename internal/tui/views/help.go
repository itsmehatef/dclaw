package views

import "strings"

// HelpModel is a full-screen modal help overlay. It is toggled by the '?'
// key from any view. When active, it replaces the entire terminal output until
// the user presses '?' or 'esc' again.
type HelpModel struct {
	active bool
}

// NewHelpModel returns a help model in the inactive state.
func NewHelpModel() HelpModel { return HelpModel{} }

// Toggle flips the overlay on/off.
func (m *HelpModel) Toggle() { m.active = !m.active }

// Active reports whether the overlay is currently visible.
func (m *HelpModel) Active() bool { return m.active }

// View renders the full-screen help text.
func (m *HelpModel) View(width, height int) string {
	var b strings.Builder
	b.WriteString("dclaw TUI — Keybinding Reference\n")
	b.WriteString("Press '?' or 'esc' to close this overlay\n")
	b.WriteString("\n")
	b.WriteString("Navigation\n")
	b.WriteString("  j / ↓        move cursor down\n")
	b.WriteString("  k / ↑        move cursor up\n")
	b.WriteString("  enter        open detail view for selected agent\n")
	b.WriteString("  esc / ←      return to previous view (list ← detail ← describe)\n")
	b.WriteString("\n")
	b.WriteString("Views\n")
	b.WriteString("  (list view)   default — press 'enter' to drill in\n")
	b.WriteString("  (detail view) per-agent info — press 'd' to describe\n")
	b.WriteString("  (describe)    container inspect data — press 'esc' to go back\n")
	b.WriteString("\n")
	b.WriteString("Actions\n")
	b.WriteString("  r            force-refresh data from daemon\n")
	b.WriteString("  d            open describe view (from detail view)\n")
	b.WriteString("  ?            toggle this help overlay\n")
	b.WriteString("  q / ctrl+c   quit\n")
	b.WriteString("\n")
	b.WriteString("Coming in alpha.3\n")
	b.WriteString("  c            open chat view for the selected agent\n")
	b.WriteString("\n")
	b.WriteString("Coming in beta.1\n")
	b.WriteString("  l            open live log tail for the selected agent\n")
	b.WriteString("  :            enter command mode (:q :refresh :help)\n")
	b.WriteString("\n")
	b.WriteString("Flags\n")
	b.WriteString("  dclaw --no-mouse    disable mouse support (macOS Terminal.app fix)\n")
	return b.String()
}
