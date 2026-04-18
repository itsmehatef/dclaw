package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/itsmehatef/dclaw/internal/client"
)

// ListModel is the agent-list view. It maintains a sorted-by-name slice and a
// cursor position. It does not own a bubbletea list.Model because our row
// format (five columns including a coloured STATUS badge) benefits from a
// hand-rolled renderer over the list bubble's opinionated delegate system.
// Beta.1 can migrate to list.Model if the row count grows large enough to
// warrant virtualised scrolling.
type ListModel struct {
	items  []client.Agent
	cursor int
}

// NewListModel returns an empty list model.
func NewListModel() ListModel { return ListModel{} }

// SetAgents replaces the backing slice and clamps the cursor.
func (m *ListModel) SetAgents(items []client.Agent) {
	m.items = items
	if m.cursor >= len(items) {
		m.cursor = max(0, len(items)-1)
	}
}

// Items returns the current agent slice (read-only view for rendering).
func (m *ListModel) Items() []client.Agent { return m.items }

// Up moves the cursor one row up (wraps at 0).
func (m *ListModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}

// Down moves the cursor one row down (clamps at len-1).
func (m *ListModel) Down() {
	if m.cursor < len(m.items)-1 {
		m.cursor++
	}
}

// SelectedName returns the name of the highlighted agent, or "" if the list is
// empty.
func (m *ListModel) SelectedName() string {
	if len(m.items) == 0 {
		return ""
	}
	return m.items[m.cursor].Name
}

// View renders the list into width × height. The header occupies one row;
// rows beyond height-1 are clipped (no scrolling — beta.1 adds it).
func (m *ListModel) View(width, height int) string {
	var b strings.Builder

	// Header row
	header := fmt.Sprintf("%-24s %-10s %-28s %-10s",
		"NAME", "STATUS", "IMAGE", "CREATED")
	b.WriteString(header + "\n")
	b.WriteString(strings.Repeat("─", min(width, len(header)+4)) + "\n")

	available := height - 2 // two rows for header + divider
	for i, a := range m.items {
		if i >= available {
			break
		}
		marker := "  "
		line := fmt.Sprintf("%-24s %-10s %-28s %-10s",
			truncate(a.Name, 22),
			a.Status,
			truncate(a.Image, 26),
			humanAge(a),
		)
		if i == m.cursor {
			marker = "> "
			line = marker + line
		} else {
			line = marker + line
		}
		b.WriteString(line + "\n")
	}
	if len(m.items) == 0 {
		b.WriteString("  (no agents — run: dclaw agent create <name> --image=<img>)\n")
	}
	return b.String()
}

// helpers

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func humanAge(a client.Agent) string {
	// client.Agent does not expose CreatedAt directly; we surface Status as a
	// proxy. Beta.1 adds CreatedAt to the client.Agent type and renders age.
	// For alpha.2 this column shows the status, which is already in the STATUS
	// column — the CREATED column is left as "-" until the field is wired.
	_ = a
	return "-"
}

// humanDuration formats a time.Duration for display in the age column.
// Exported for use in tests.
func HumanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
