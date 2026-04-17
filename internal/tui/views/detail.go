package views

import (
	"fmt"
	"strings"

	"github.com/itsmehatef/dclaw/internal/client"
)

// DetailModel renders the full agent record for a single selected agent. It
// uses a simple string builder rather than a viewport bubble because the
// content is short and static (re-rendered on every poll tick). Beta.1 can
// upgrade to viewport.Model if the field list grows.
type DetailModel struct {
	agent client.Agent
	name  string // name of the agent being viewed (held separately for the title)
}

// NewDetailModel returns an empty detail model.
func NewDetailModel() DetailModel { return DetailModel{} }

// SetAgent replaces the backing record. The name is stored separately so the
// title remains stable while a background refresh is in flight.
func (m *DetailModel) SetAgent(a client.Agent) {
	m.agent = a
	m.name = a.Name
}

// Name returns the agent name this model is currently tracking.
func (m *DetailModel) Name() string { return m.name }

// View renders the detail pane into width × height.
func (m *DetailModel) View(width, height int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Agent: %s\n", m.name))
	b.WriteString(strings.Repeat("─", min(width, 60)) + "\n")
	b.WriteString(fmt.Sprintf("  Status:    %s\n", m.agent.Status))
	b.WriteString(fmt.Sprintf("  Image:     %s\n", m.agent.Image))
	b.WriteString(fmt.Sprintf("  Workspace: %s\n", m.agent.Workspace))
	if len(m.agent.Labels) > 0 {
		b.WriteString("  Labels:\n")
		for k, v := range m.agent.Labels {
			b.WriteString(fmt.Sprintf("    %s = %s\n", k, v))
		}
	}
	if len(m.agent.Env) > 0 {
		b.WriteString("  Env:\n")
		for k, v := range m.agent.Env {
			b.WriteString(fmt.Sprintf("    %s = %s\n", k, v))
		}
	}
	b.WriteString("\n")
	b.WriteString("  Press 'd' to describe container, 'esc' to return to list.\n")
	return b.String()
}
