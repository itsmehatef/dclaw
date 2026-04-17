package views

import (
	"fmt"
	"strings"

	"github.com/itsmehatef/dclaw/internal/client"
)

// DescribeModel renders a kubectl-describe-style verbose view for a single
// agent. Alpha.2 populates it from client.Agent (which is the result of
// agent.get). Beta.1 upgrades the data source to agent.describe (which
// includes the SQLite events table) once the daemon exposes that method via
// the RPC client.
type DescribeModel struct {
	agent client.Agent
	name  string
}

// NewDescribeModel returns an empty describe model.
func NewDescribeModel() DescribeModel { return DescribeModel{} }

// SetAgent replaces the backing record.
func (m *DescribeModel) SetAgent(a client.Agent) {
	m.agent = a
	m.name = a.Name
}

// View renders the describe pane into width × height.
func (m *DescribeModel) View(width, height int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Describe: %s\n", m.name))
	b.WriteString(strings.Repeat("─", min(width, 60)) + "\n\n")

	b.WriteString("Container\n")
	b.WriteString(fmt.Sprintf("  Image:     %s\n", m.agent.Image))
	b.WriteString(fmt.Sprintf("  Status:    %s\n", m.agent.Status))
	b.WriteString(fmt.Sprintf("  Workspace: %s\n", m.agent.Workspace))

	if len(m.agent.Labels) > 0 {
		b.WriteString("\nLabels\n")
		for k, v := range m.agent.Labels {
			b.WriteString(fmt.Sprintf("  %s = %s\n", k, v))
		}
	}

	if len(m.agent.Env) > 0 {
		b.WriteString("\nEnvironment\n")
		for k, v := range m.agent.Env {
			b.WriteString(fmt.Sprintf("  %s = %s\n", k, v))
		}
	}

	b.WriteString("\nMounts\n")
	if m.agent.Workspace != "" {
		b.WriteString(fmt.Sprintf("  /workspace ← %s (bind)\n", m.agent.Workspace))
	} else {
		b.WriteString("  (none)\n")
	}

	b.WriteString("\nNetwork\n")
	b.WriteString("  bridge (default)\n")

	b.WriteString("\nEvents\n")
	b.WriteString("  (events from daemon.describe — wired in beta.1)\n")

	b.WriteString("\n  Press 'esc' to return to detail view.\n")
	return b.String()
}
