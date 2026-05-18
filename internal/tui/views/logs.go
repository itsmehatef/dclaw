package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/itsmehatef/dclaw/internal/client"
)

const MaxLogLines = 5000

// LogsModel is the TUI model for live per-agent container logs.
type LogsModel struct {
	agentName string
	lines     []string

	vp     viewport.Model
	width  int
	height int

	following bool
	dimStyle  lipgloss.Style
	errStyle  lipgloss.Style
}

// NewLogsModel returns an empty logs view.
func NewLogsModel() LogsModel {
	vp := viewport.New(80, 20)
	vp.SetContent("")
	return LogsModel{
		vp:       vp,
		dimStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		errStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
	}
}

// SetAgent configures the model for a specific agent.
func (m *LogsModel) SetAgent(name string) {
	m.agentName = name
}

// SetSize recalculates viewport dimensions.
func (m *LogsModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	if height < 3 {
		height = 3
	}
	m.vp.Width = width
	m.vp.Height = height
	m.rebuildViewport()
}

// SetFollowing marks whether a live stream is active.
func (m *LogsModel) SetFollowing(f bool) {
	m.following = f
	m.rebuildViewport()
}

// AppendLine appends one streamed log line.
func (m *LogsModel) AppendLine(event client.LogLineEvent) {
	line := event.Line
	if event.Stream != "" && event.Stream != "stdout" {
		line = fmt.Sprintf("[%s] %s", event.Stream, line)
	}
	m.lines = append(m.lines, line)
	if len(m.lines) > MaxLogLines {
		m.lines = append([]string(nil), m.lines[len(m.lines)-MaxLogLines:]...)
	}
	m.rebuildViewport()
	m.vp.GotoBottom()
}

// AppendError appends a visible stream error line.
func (m *LogsModel) AppendError(err error) {
	m.lines = append(m.lines, m.errStyle.Render("! "+err.Error()))
	m.rebuildViewport()
	m.vp.GotoBottom()
}

// Reset clears log scrollback.
func (m *LogsModel) Reset() {
	m.lines = nil
	m.following = false
	m.vp.SetContent("")
}

// Update forwards tea.Msgs to the viewport sub-model.
func (m LogsModel) Update(msg tea.Msg) (LogsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// View renders the logs pane.
func (m *LogsModel) View(width, height int) string {
	if m.width != width || m.height != height {
		m.SetSize(width, height)
	}
	return m.vp.View()
}

func (m *LogsModel) rebuildViewport() {
	var b strings.Builder
	if len(m.lines) == 0 {
		msg := "waiting for log lines"
		if !m.following {
			msg = "no log lines"
		}
		b.WriteString(m.dimStyle.Render(msg))
		b.WriteString("\n")
	} else {
		for _, line := range m.lines {
			b.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				b.WriteString("\n")
			}
		}
	}
	if m.following {
		b.WriteString(m.dimStyle.Render("following "))
		b.WriteString(m.dimStyle.Render(time.Now().Format("15:04:05")))
	}
	m.vp.SetContent(b.String())
}
