package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/tui/components"
	"github.com/itsmehatef/dclaw/internal/tui/views"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

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
	logs     views.LogsModel

	width  int
	height int

	selected string // name of the currently selected agent

	streaming          bool
	streamCancel       context.CancelFunc           // non-nil when a chat stream is active
	streamCh           <-chan client.ChatChunkEvent // active chat stream channel; nil when not streaming
	chatHistoryLoading bool
	chatHistoryLoadID  int

	logFollowing bool
	logCancel    context.CancelFunc
	logCh        <-chan client.LogLineEvent
	logStreamID  int

	toasts components.Stack

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
		logs:     views.NewLogsModel(),
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
	cmds := []tea.Cmd{fetchAgents(m.ctx, m.rpc), tickPoll()}
	if m.current == views.ViewChat && m.selected != "" && m.rpc != nil {
		m.chatHistoryLoadID++
		m.chatHistoryLoading = true
		cmds = append(cmds, fetchChatHistory(m.ctx, m.rpc, m.selected, m.chatHistoryLoadID))
	}
	return tea.Batch(cmds...)
}

// Update dispatches incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chat.SetSize(msg.Width, m.chatHeight())
		m.logs.SetSize(msg.Width, m.logsHeight())
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
		// While in stream-backed views, skip fetching but keep the tick alive so
		// polling resumes normally when the user leaves.
		if m.current == views.ViewChat || m.current == views.ViewLogs {
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
		m.cancelLogStream()
		m.noDaemon.SetErr(msg.err)
		m.current = views.ViewNoDaemon
		return m, m.pushToast("warning", "daemon disconnected: "+msg.err.Error())

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
		var cmd tea.Cmd
		if msg.chunk.Role == "error" {
			cmd = m.pushToast("error", msg.chunk.Text)
		}
		if msg.chunk.Final {
			m.streaming = false
			m.streamCh = nil
			m.streamCancel = nil
			m.chat.SetStreaming(false)
			return m, cmd
		}
		return m, tea.Batch(cmd, drainChatStream(m.streamCh))

	case chatErrorMsg:
		if m.streamCancel != nil {
			m.streamCancel()
			m.streamCancel = nil
		}
		m.streaming = false
		m.streamCh = nil
		m.chat.SetStreaming(false)
		m.chat.AppendError(msg.err)
		return m, m.pushToast("error", msg.err.Error())

	case chatStreamClosedMsg:
		m.streaming = false
		m.streamCh = nil
		m.chat.SetStreaming(false)
		return m, nil

	case chatAckedMsg:
		return m, nil // reserved for beta.1 persistence hook

	case chatHistoryLoadedMsg:
		if msg.agentName == m.selected && msg.loadID == m.chatHistoryLoadID && m.current == views.ViewChat {
			m.chatHistoryLoading = false
			m.chat.LoadHistory(msg.messages)
		}
		return m, nil

	case chatHistoryErrorMsg:
		if msg.agentName != m.selected || msg.loadID != m.chatHistoryLoadID || m.current != views.ViewChat {
			return m, nil
		}
		m.chatHistoryLoading = false
		if isMethodNotFound(msg.err) {
			return m, nil
		}
		m.chat.AppendError(msg.err)
		return m, m.pushToast("warning", msg.err.Error())

	// logs events

	case logsStreamOpenedMsg:
		if msg.streamID != m.logStreamID {
			return m, nil
		}
		m.logFollowing = true
		m.logCh = msg.lines
		m.logs.SetFollowing(true)
		return m, drainLogStream(msg.streamID, m.logCh)

	case logLineMsg:
		if msg.streamID != m.logStreamID {
			return m, nil
		}
		m.logs.AppendLine(msg.line)
		if m.logCh == nil {
			return m, nil
		}
		return m, drainLogStream(msg.streamID, m.logCh)

	case logsErrorMsg:
		if msg.streamID != m.logStreamID {
			return m, nil
		}
		m.cancelLogStream()
		m.logs.AppendError(msg.err)
		return m, m.pushToast("error", msg.err.Error())

	case logsStreamClosedMsg:
		if msg.streamID != m.logStreamID {
			return m, nil
		}
		m.logFollowing = false
		m.logCh = nil
		m.logCancel = nil
		m.logs.SetFollowing(false)
		return m, nil

	case toastMsg:
		return m, m.pushToast(msg.level, msg.message)

	case toastTickMsg:
		m.toasts.Tick(msg.now)
		return m, nil
	}

	// Forward non-key msgs to chat textarea when in ViewChat.
	if m.current == views.ViewChat {
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	}
	if m.current == views.ViewLogs {
		var cmd tea.Cmd
		m.logs, cmd = m.logs.Update(msg)
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
	case views.ViewLogs:
		viewName = fmt.Sprintf("logs: %s", m.selected)
		main = m.logs.View(m.width, m.height-2)
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
	if m.current == views.ViewLogs && m.logFollowing {
		topContent += "  following..."
	}
	top := TopBarStyle.Width(m.width).Render(topContent)

	var hints []string
	switch m.current {
	case views.ViewList:
		hints = []string{"↑↓/jk:nav", "enter:open", "c:chat", "l:logs", "r:refresh", "?:help", "q:quit"}
	case views.ViewDetail:
		hints = []string{"c:chat", "l:logs", "d:describe", "r:refresh", "esc:back", "?:help", "q:quit"}
	case views.ViewDescribe:
		hints = []string{"esc:back", "r:refresh", "?:help", "q:quit"}
	case views.ViewNoDaemon:
		hints = []string{"r:retry", "?:help", "q:quit"}
	case views.ViewChat:
		if m.chatHistoryLoading {
			hints = []string{"loading history...", "?:help", "esc:back", "q:quit"}
		} else if m.streaming {
			hints = []string{"ctrl+c:cancel stream", "?:help", "esc:blocked while streaming"}
		} else {
			hints = []string{"enter:send", "shift+enter:newline", "?:help", "esc:back", "q:quit"}
		}
	case views.ViewLogs:
		hints = []string{"?:help", "esc:back", "q:quit"}
	default:
		hints = []string{"?:help", "q:quit"}
	}
	bottom := BottomBarStyle.Width(m.width).Render(strings.Join(hints, "  "))
	base := lipgloss.JoinVertical(lipgloss.Left, top, main, bottom)
	return overlayToastLayer(base, m.toasts.Render(m.width, m.height-2), m.width)
}

// chatHeight returns height available for the chat sub-model.
func (m *Model) chatHeight() int {
	h := m.height - 2
	if h < 4 {
		return 4
	}
	return h
}

func (m *Model) logsHeight() int {
	h := m.height - 2
	if h < 3 {
		return 3
	}
	return h
}

// handleKey dispatches keyboard events.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "t" {
		m.toasts.DismissTop()
		return m, nil
	}

	if m.help.Active() {
		switch msg.String() {
		case "?", "esc":
			m.help.Toggle()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	if msg.String() == "?" {
		m.help.Toggle()
		return m, nil
	}

	if m.current == views.ViewChat {
		return m.handleChatKey(msg)
	}
	if m.current == views.ViewLogs {
		return m.handleLogsKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
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
	case "l":
		if name := m.list.SelectedName(); name != "" {
			return m.openLogs(name, views.ViewList)
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
	case "l":
		if m.selected != "" {
			return m.openLogs(m.selected, views.ViewDetail)
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
	if m.chatHistoryLoading {
		switch msg.String() {
		case "esc", "backspace", "q", "ctrl+c":
		default:
			return m, nil
		}
	}
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
		m.chatHistoryLoading = false
		m.chatHistoryLoadID++
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

func (m *Model) handleLogsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.cancelLogStream()
		m.logs.Reset()
		m.current = m.prevView
		switch m.prevView {
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		default:
			return m, fetchAgents(m.ctx, m.rpc)
		}
		return m, nil
	case "q", "ctrl+c":
		m.cancelLogStream()
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.logs, cmd = m.logs.Update(msg)
		return m, cmd
	}
}

func (m *Model) pushToast(level, message string) tea.Cmd {
	m.toasts.Push(level, message, time.Now())
	return tea.Tick(components.ToastDuration, func(t time.Time) tea.Msg {
		return toastTickMsg{now: t}
	})
}

func overlayToastLayer(base, layer string, width int) string {
	if layer == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	layerLines := strings.Split(layer, "\n")
	for len(layerLines) > 0 && layerLines[len(layerLines)-1] == "" {
		layerLines = layerLines[:len(layerLines)-1]
	}
	offset := len(baseLines) - 1 - len(layerLines)
	if offset < 0 {
		offset = 0
	}
	for i, line := range layerLines {
		if strings.TrimSpace(stripANSI(line)) == "" {
			continue
		}
		idx := offset + i
		if idx >= len(baseLines) {
			baseLines = append(baseLines, line)
			continue
		}
		baseLines[idx] = overlayLineRight(baseLines[idx], line, width)
	}
	return strings.Join(baseLines, "\n")
}

func overlayLineRight(baseLine, overlay string, width int) string {
	if width <= 0 {
		return overlay
	}
	overlayWidth := lipgloss.Width(overlay)
	if overlayWidth >= width {
		return overlay
	}
	leftWidth := width - overlayWidth
	left := xansi.Truncate(baseLine, leftWidth, "")
	if pad := leftWidth - lipgloss.Width(left); pad > 0 {
		left += strings.Repeat(" ", pad)
	}
	return left + overlay
}

func stripANSI(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

func isMethodNotFound(err error) bool {
	rpcErr, ok := err.(*protocol.RPCError)
	return ok && rpcErr.Code == protocol.ErrMethodNotFound
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
	if m.rpc == nil {
		m.chatHistoryLoading = false
		return m, nil
	}
	m.chatHistoryLoadID++
	m.chatHistoryLoading = true
	return m, fetchChatHistory(m.ctx, m.rpc, agentName, m.chatHistoryLoadID)
}

func fetchChatHistory(ctx context.Context, rpc *client.RPCClient, agentName string, loadID int) tea.Cmd {
	return func() tea.Msg {
		messages, err := rpc.ChatHistoryList(ctx, agentName, 0)
		if err != nil {
			return chatHistoryErrorMsg{agentName: agentName, loadID: loadID, err: err}
		}
		return chatHistoryLoadedMsg{agentName: agentName, loadID: loadID, messages: messages}
	}
}

// openLogs transitions to ViewLogs for the given agent and starts following.
func (m *Model) openLogs(agentName string, from views.View) (tea.Model, tea.Cmd) {
	m.cancelLogStream()
	m.selected = agentName
	m.prevView = from
	m.current = views.ViewLogs
	m.logs.Reset()
	m.logs.SetAgent(agentName)
	m.logs.SetSize(m.width, m.logsHeight())
	if m.rpc == nil {
		m.logs.AppendError(fmt.Errorf("no daemon connection"))
		return m, nil
	}

	streamCtx, cancel := context.WithCancel(m.ctx)
	m.logCancel = cancel
	m.logStreamID++
	streamID := m.logStreamID
	return m, func() tea.Msg {
		ch, err := m.rpc.LogsStream(streamCtx, agentName, 0)
		if err != nil {
			cancel()
			return logsErrorMsg{agentName: agentName, streamID: streamID, err: err}
		}
		return logsStreamOpenedMsg{agentName: agentName, streamID: streamID, lines: ch}
	}
}

func (m *Model) cancelLogStream() {
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
	m.logFollowing = false
	m.logCh = nil
	m.logs.SetFollowing(false)
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

func drainLogStream(streamID int, ch <-chan client.LogLineEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return logsStreamClosedMsg{streamID: streamID}
		}
		if event.Err != nil {
			return logsErrorMsg{agentName: event.Name, streamID: streamID, err: event.Err}
		}
		return logLineMsg{streamID: streamID, line: event}
	}
}
