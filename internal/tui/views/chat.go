package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/itsmehatef/dclaw/internal/client"
)

// ChatMessage is one in-memory message in the conversation.
// Alpha.3: stored in RAM only. Beta.1 persists to SQLite.
type ChatMessage struct {
	Role      string    // "user" | "agent" | "system" | "error"
	AgentName string    // agent name for display (agent messages only)
	Content   string    // complete text (accumulated from deltas)
	Streaming bool      // true while this message is still receiving chunks
	MessageID string    // content-addressed ID; set when Final=true
	Timestamp time.Time
}

// ChatModel is the TUI model for the interactive chat view.
//
// Layout inside the available content area (terminal height - 2 chrome rows):
//
//	+--------------------------------------+
//	|         message history (viewport)   |  ~80% of height
//	+--------------------------------------+
//	|   --------------- separator          |  1 row
//	|   > input (textarea, 3-5 lines)      |  3-5 rows
//	+--------------------------------------+
type ChatModel struct {
	agentName string
	messages  []ChatMessage
	lastMsgID string // ID of last complete message (used as parent_id)

	vp        viewport.Model
	ta        textarea.Model
	width     int
	height    int
	streaming bool

	userStyle   lipgloss.Style
	agentStyle  lipgloss.Style
	systemStyle lipgloss.Style
	errorStyle  lipgloss.Style
	dimStyle    lipgloss.Style
}

// NewChatModel returns a ChatModel with sensible defaults.
func NewChatModel() ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (enter to send, shift+enter for newline)"
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	ta.KeyMap.InsertNewline.SetKeys("shift+enter")
	ta.Focus()

	vp := viewport.New(80, 20)
	vp.SetContent("")

	return ChatModel{
		ta: ta,
		vp: vp,

		userStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
		agentStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true),
		systemStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		errorStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		dimStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
}

// SetAgent configures the model for a specific agent.
func (m *ChatModel) SetAgent(name string) {
	m.agentName = name
}

// SetSize recalculates viewport and textarea dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	taLines := 3
	if height > 20 {
		taLines = 5
	}
	vpHeight := height - taLines - 2 // 1 separator + 1 padding
	if vpHeight < 3 {
		vpHeight = 3
	}

	m.vp.Width = width
	m.vp.Height = vpHeight
	m.ta.SetWidth(width)
	m.ta.SetHeight(taLines)
	m.rebuildViewport()
}

// SetStreaming marks whether a stream is active.
func (m *ChatModel) SetStreaming(s bool) {
	m.streaming = s
	m.rebuildViewport()
}

// AppendUserMessage adds the user's outgoing message to history immediately.
func (m *ChatModel) AppendUserMessage(content string) {
	m.messages = append(m.messages, ChatMessage{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	m.rebuildViewport()
	m.vp.GotoBottom()
}

// AppendChunk handles an incoming ChatChunkEvent from the daemon stream.
func (m *ChatModel) AppendChunk(chunk client.ChatChunkEvent) {
	if chunk.Err != nil {
		m.AppendError(chunk.Err)
		return
	}
	// Append to in-progress agent message if one exists.
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		if last.Role == "agent" && last.Streaming {
			last.Content += chunk.Text
			last.Streaming = !chunk.Final
			if chunk.Final {
				last.MessageID = chunk.MessageID
				m.lastMsgID = chunk.MessageID
			}
			m.rebuildViewport()
			m.vp.GotoBottom()
			return
		}
	}
	// Start a new agent message.
	m.messages = append(m.messages, ChatMessage{
		Role:      "agent",
		AgentName: m.agentName,
		Content:   chunk.Text,
		Streaming: !chunk.Final,
		MessageID: chunk.MessageID,
		Timestamp: time.Now(),
	})
	if chunk.Final {
		m.lastMsgID = chunk.MessageID
	}
	m.rebuildViewport()
	m.vp.GotoBottom()
}

// AppendError adds a system-level error message to history.
func (m *ChatModel) AppendError(err error) {
	m.messages = append(m.messages, ChatMessage{
		Role:      "error",
		Content:   err.Error(),
		Timestamp: time.Now(),
	})
	m.rebuildViewport()
	m.vp.GotoBottom()
}

// SubmitInput returns and clears the textarea content. Returns "" if empty.
func (m *ChatModel) SubmitInput() string {
	val := strings.TrimSpace(m.ta.Value())
	m.ta.Reset()
	return val
}

// LastMessageID returns the content-addressed ID of the last complete message.
func (m *ChatModel) LastMessageID() string {
	return m.lastMsgID
}

// Reset clears history and textarea. Called when leaving the chat view.
func (m *ChatModel) Reset() {
	m.messages = nil
	m.lastMsgID = ""
	m.ta.Reset()
	m.vp.SetContent("")
}

// Update forwards tea.Msgs to the textarea sub-model.
func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

// View renders the chat pane (history + separator + textarea).
func (m *ChatModel) View(width, height int) string {
	if m.width != width || m.height != height {
		m.SetSize(width, height)
	}
	sep := m.dimStyle.Render(strings.Repeat("-", m.width))
	return lipgloss.JoinVertical(lipgloss.Left, m.vp.View(), sep, m.ta.View())
}

// rebuildViewport re-renders all messages into the viewport content.
func (m *ChatModel) rebuildViewport() {
	var b strings.Builder
	for _, msg := range m.messages {
		b.WriteString(m.renderMessage(msg))
		b.WriteString("\n")
	}
	m.vp.SetContent(b.String())
}

// renderMessage renders one ChatMessage as a styled string.
func (m *ChatModel) renderMessage(msg ChatMessage) string {
	ts := m.dimStyle.Render(chatRelTime(msg.Timestamp))
	var label string
	switch msg.Role {
	case "user":
		label = m.userStyle.Render("you")
	case "agent":
		name := msg.AgentName
		if name == "" {
			name = m.agentName
		}
		label = m.agentStyle.Render(name)
	case "system":
		label = m.systemStyle.Render("system")
	case "error":
		label = m.errorStyle.Render("! error")
	default:
		label = msg.Role
	}
	glyph := ""
	if msg.Streaming {
		glyph = m.dimStyle.Render(" ...")
	}
	header := fmt.Sprintf("%s  %s%s", label, ts, glyph)
	body := msg.Content
	if body == "" && msg.Streaming {
		body = m.dimStyle.Render("(streaming)")
	}
	indented := strings.ReplaceAll(body, "\n", "\n  ")
	return header + "\n  " + indented
}

// chatRelTime formats a time.Time as a relative duration string.
func chatRelTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
