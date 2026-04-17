package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/tui/views"
)

// Model is the root bubbletea.Model for the dclaw TUI.
type Model struct {
	ctx context.Context
	rpc *client.RPCClient

	current  views.View
	prevView views.View // view to return to when leaving ViewChat

	list     views.ListModel
	detail   views.DetailModel
	desc     views.DescribeModel
	help     views.HelpModel
	noDaemon views.NoDaemonModel
	chat     views.ChatModel

	width  int
	height int

	selected string // name of the currently selected agent

	streaming    bool
	streamCancel context.CancelFunc                // non-nil when a stream is active
	streamCh     <-chan client.ChatChunkEvent      // active stream channel; nil when not streaming

	keys KeyMap
}

// attachTarget carries an optional pre-selected agent for Run* entry points.
type attachTarget struct {
	agentName string
	startChat bool // if true, open ViewChat; otherwise ViewDetail
}

// NewModel constructs the root Model.
func NewModel(ctx context.Context, rpc *client.RPCClient, target *attachTarget) *Model {
	m := &Model{
		ctx:      ctx,
		rpc:      rpc,
		current:  views.ViewList,
		list:     views.NewListModel(),
		detail:   views.NewDetailModel(),
		desc:     views.NewDescribeModel(),
		help:     views.NewHelpModel(),
		noDaemon: views.NewNoDaemonModel(nil),
		chat:     views.NewChatModel(),
		keys:     DefaultKeys(),
	}
	if target != nil {
		m.selected = target.agentName
		if target.startChat {
			m.current = views.ViewChat
			m.prevView = views.ViewList
			m.chat.SetAgent(target.agentName)
		} else {
			m.current = views.ViewDetail
		}
	}
	return m
}

// Init sends the first fetch and schedules the poll timer.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(fetchAgents(m.ctx, m.rpc), tickPoll())
}

// Update dispatches incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chat.SetSize(msg.Width, m.chatHeight())
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agentsLoadedMsg:
		m.list.SetAgents(msg.agents)
		if m.current == views.ViewNoDaemon {
			m.current = views.ViewList
		}
		return m, tickPoll()

	case agentFetchedMsg:
		m.detail.SetAgent(msg.agent)
		m.desc.SetAgent(msg.agent)
		return m, tickPoll()

	case pollTickMsg:
		// While in ViewChat, skip fetching but keep the tick alive so
		// polling resumes normally when the user leaves.
		if m.current == views.ViewChat {
			return m, tickPoll()
		}
		switch m.current {
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		case views.ViewDescribe:
			return m, nil
		default:
			return m, fetchAgents(m.ctx, m.rpc)
		}
		return m, fetchAgents(m.ctx, m.rpc)

	case daemonErrMsg:
		m.noDaemon.SetErr(msg.err)
		m.current = views.ViewNoDaemon
		return m, nil

	case retryMsg:
		return m, retryDial(m.ctx, m.rpc)

	// chat events

	case chatStreamOpenedMsg:
		m.streaming = true
		m.streamCh = msg.chunks
		m.chat.SetStreaming(true)
		return m, drainChatStream(m.streamCh)

	case chatDeltaMsg:
		m.chat.AppendChunk(msg.chunk)
		if msg.chunk.Final {
			m.streaming = false
			m.streamCh = nil
			m.streamCancel = nil
			m.chat.SetStreaming(false)
			return m, nil
		}
		return m, drainChatStream(m.streamCh)

	case chatErrorMsg:
		if m.streamCancel != nil {
			m.streamCancel()
			m.streamCancel = nil
		}
		m.streaming = false
		m.streamCh = nil
		m.chat.SetStreaming(false)
		m.chat.AppendError(msg.err)
		return m, nil

	case chatStreamClosedMsg:
		m.streaming = false
		m.streamCh = nil
		m.chat.SetStreaming(false)
		return m, nil

	case chatAckedMsg:
		return m, nil // reserved for beta.1 persistence hook
	}

	// Forward non-key msgs to chat textarea when in ViewChat.
	if m.current == views.ViewChat {
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the full terminal output.
func (m *Model) View() string {
	if m.help.Active() {
		return m.renderChrome("help", m.help.View(m.width, m.height-2))
	}

	var main, viewName string
	switch m.current {
	case views.ViewList:
		viewName = "agents"
		main = m.list.View(m.width, m.height-2)
	case views.ViewDetail:
		viewName = fmt.Sprintf("detail: %s", m.selected)
		main = m.detail.View(m.width, m.height-2)
	case views.ViewDescribe:
		viewName = fmt.Sprintf("describe: %s", m.selected)
		main = m.desc.View(m.width, m.height-2)
	case views.ViewNoDaemon:
		viewName = "no-daemon"
		main = m.noDaemon.View(m.width, m.height-2)
	case views.ViewChat:
		viewName = fmt.Sprintf("chat: %s", m.selected)
		main = m.chat.View(m.width, m.height-2)
	default:
		viewName = "agents"
		main = m.list.View(m.width, m.height-2)
	}
	return m.renderChrome(viewName, main)
}

// renderChrome wraps main content with top/bottom bars.
func (m *Model) renderChrome(viewName, main string) string {
	agentCount := len(m.list.Items())
	topContent := fmt.Sprintf("[%s]  daemon:ok  agents:%d", viewName, agentCount)
	if m.current == views.ViewNoDaemon {
		topContent = fmt.Sprintf("[%s]  daemon:DOWN", viewName)
	}
	if m.current == views.ViewChat && m.streaming {
		topContent += "  streaming..."
	}
	top := TopBarStyle.Width(m.width).Render(topContent)

	var hints []string
	switch m.current {
	case views.ViewList:
		hints = []string{"↑↓/jk:nav", "enter:open", "c:chat", "r:refresh", "?:help", "q:quit"}
	case views.ViewDetail:
		hints = []string{"c:chat", "d:describe", "r:refresh", "esc:back", "?:help", "q:quit"}
	case views.ViewDescribe:
		hints = []string{"esc:back", "r:refresh", "?:help", "q:quit"}
	case views.ViewNoDaemon:
		hints = []string{"r:retry", "?:help", "q:quit"}
	case views.ViewChat:
		if m.streaming {
			hints = []string{"ctrl+c:cancel stream", "esc:blocked while streaming"}
		} else {
			hints = []string{"enter:send", "shift+enter:newline", "esc:back", "q:quit"}
		}
	default:
		hints = []string{"?:help", "q:quit"}
	}
	bottom := BottomBarStyle.Width(m.width).Render(strings.Join(hints, "  "))
	return lipgloss.JoinVertical(lipgloss.Left, top, main, bottom)
}

// chatHeight returns height available for the chat sub-model.
func (m *Model) chatHeight() int {
	h := m.height - 2
	if h < 4 {
		return 4
	}
	return h
}

// handleKey dispatches keyboard events.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.help.Active() {
		switch msg.String() {
		case "?", "esc":
			m.help.Toggle()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	if m.current == views.ViewChat {
		return m.handleChatKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help.Toggle()
		return m, nil
	case "r":
		switch m.current {
		case views.ViewNoDaemon:
			return m, func() tea.Msg { return retryMsg{} }
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		default:
			return m, fetchAgents(m.ctx, m.rpc)
		}
	}

	switch m.current {
	case views.ViewList:
		return m.handleListKey(msg)
	case views.ViewDetail:
		return m.handleDetailKey(msg)
	case views.ViewDescribe:
		return m.handleDescribeKey(msg)
	}
	return m, nil
}

func (m *Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.list.Down()
	case "k", "up":
		m.list.Up()
	case "enter":
		if name := m.list.SelectedName(); name != "" {
			m.selected = name
			m.current = views.ViewDetail
			return m, fetchAgent(m.ctx, m.rpc, name)
		}
	case "c":
		if name := m.list.SelectedName(); name != "" {
			return m.openChat(name, views.ViewList)
		}
	}
	return m, nil
}

func (m *Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.current = views.ViewList
		return m, fetchAgents(m.ctx, m.rpc)
	case "d":
		if m.selected != "" {
			m.current = views.ViewDescribe
		}
	case "c":
		if m.selected != "" {
			return m.openChat(m.selected, views.ViewDetail)
		}
	}
	return m, nil
}

func (m *Model) handleDescribeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.current = views.ViewDetail
	}
	return m, nil
}

func (m *Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.streaming {
			if m.streamCancel != nil {
				m.streamCancel()
				m.streamCancel = nil
			}
			m.streaming = false
			m.chat.SetStreaming(false)
			return m, nil
		}
		return m, tea.Quit

	case "esc", "backspace":
		if m.streaming {
			return m, nil // blocked while streaming
		}
		m.current = m.prevView
		m.chat.Reset()
		switch m.prevView {
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		default:
			return m, fetchAgents(m.ctx, m.rpc)
		}
		return m, nil

	case "q":
		if m.streaming {
			return m, nil // swallow q while streaming
		}
		return m, tea.Quit

	case "enter":
		return m.handleChatEnter()

	default:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	}
}

// handleChatEnter fires when enter is pressed in the chat textarea.
func (m *Model) handleChatEnter() (tea.Model, tea.Cmd) {
	if m.streaming {
		return m, nil
	}
	content := m.chat.SubmitInput()
	if content == "" {
		return m, nil
	}
	if m.rpc == nil {
		m.chat.AppendError(fmt.Errorf("no daemon connection"))
		return m, nil
	}
	parentID := m.chat.LastMessageID()
	agentName := m.selected

	streamCtx, cancel := context.WithCancel(m.ctx)
	m.streamCancel = cancel
	m.chat.AppendUserMessage(content)

	return m, func() tea.Msg {
		ch, err := m.rpc.ChatSend(streamCtx, agentName, content, parentID)
		if err != nil {
			cancel()
			return chatErrorMsg{agentName: agentName, err: err}
		}
		return chatStreamOpenedMsg{agentName: agentName, chunks: ch}
	}
}

// openChat transitions to ViewChat for the given agent.
func (m *Model) openChat(agentName string, from views.View) (tea.Model, tea.Cmd) {
	m.selected = agentName
	m.prevView = from
	m.current = views.ViewChat
	m.chat.SetAgent(agentName)
	m.chat.SetSize(m.width, m.chatHeight())
	return m, nil
}

// drainChatStream returns a tea.Cmd that reads exactly one chunk from ch.
// The caller (Update) stores the channel on m.streamCh and re-calls this
// for each chunk until Final=true or the channel closes.
func drainChatStream(ch <-chan client.ChatChunkEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return chatStreamClosedMsg{}
		}
		if event.Err != nil {
			return chatErrorMsg{err: event.Err}
		}
		return chatDeltaMsg{chunk: event}
	}
}
