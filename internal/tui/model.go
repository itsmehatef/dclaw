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

// Model is the root bubbletea.Model for the dclaw TUI. It owns the entire
// application state and dispatches all messages to the appropriate sub-model.
// The single-model design avoids nested Update loops and makes state
// transitions explicit.
type Model struct {
	ctx context.Context
	rpc *client.RPCClient

	// current view
	current views.View

	// sub-models
	list     views.ListModel
	detail   views.DetailModel
	desc     views.DescribeModel
	help     views.HelpModel
	noDaemon views.NoDaemonModel

	// chrome
	width  int
	height int

	// selection: the name of the currently selected agent
	selected string

	// keys
	keys KeyMap
}

// attachTarget carries an optional pre-selected agent for RunAttached().
// If non-nil, the TUI starts on ViewDetail for that agent instead of ViewList.
type attachTarget struct {
	agentName string
}

// NewModel constructs the root Model. target is nil for a bare TUI launch.
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
		keys:     DefaultKeys(),
	}
	if target != nil {
		m.selected = target.agentName
		m.current = views.ViewDetail
	}
	return m
}

// Init sends the first fetch and schedules the poll timer.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		fetchAgents(m.ctx, m.rpc),
		tickPoll(),
	)
}

// Update dispatches incoming messages to the appropriate handler.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agentsLoadedMsg:
		m.list.SetAgents(msg.agents)
		// If we're on ViewNoDaemon and a load succeeded, restore ViewList.
		if m.current == views.ViewNoDaemon {
			m.current = views.ViewList
		}
		return m, tickPoll()

	case agentFetchedMsg:
		m.detail.SetAgent(msg.agent)
		m.desc.SetAgent(msg.agent)
		return m, tickPoll()

	case pollTickMsg:
		// Route poll to the appropriate fetch based on current view.
		switch m.current {
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		case views.ViewDescribe:
			// Describe is one-shot: no background refresh.
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
	}

	return m, nil
}

// View renders the full terminal output: top bar + main pane + bottom bar.
// When the help overlay is active it replaces everything.
func (m *Model) View() string {
	if m.help.Active() {
		return m.renderChrome("help", m.help.View(m.width, m.height-2))
	}

	var main string
	var viewName string

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
	default:
		viewName = "agents"
		main = m.list.View(m.width, m.height-2)
	}

	return m.renderChrome(viewName, main)
}

// renderChrome wraps main content with the top bar and bottom bar.
func (m *Model) renderChrome(viewName, main string) string {
	agentCount := len(m.list.Items())
	topContent := fmt.Sprintf("[%s]  daemon:ok  agents:%d", viewName, agentCount)
	if m.current == views.ViewNoDaemon {
		topContent = fmt.Sprintf("[%s]  daemon:DOWN", viewName)
	}
	top := TopBarStyle.Width(m.width).Render(topContent)

	var hintParts []string
	switch m.current {
	case views.ViewList:
		hintParts = []string{"↑↓/jk:nav", "enter:open", "r:refresh", "?:help", "q:quit"}
	case views.ViewDetail:
		hintParts = []string{"d:describe", "r:refresh", "esc:back", "?:help", "q:quit"}
	case views.ViewDescribe:
		hintParts = []string{"esc:back", "r:refresh", "?:help", "q:quit"}
	case views.ViewNoDaemon:
		hintParts = []string{"r:retry", "?:help", "q:quit"}
	default:
		hintParts = []string{"?:help", "q:quit"}
	}
	bottom := BottomBarStyle.Width(m.width).Render(strings.Join(hintParts, "  "))

	return lipgloss.JoinVertical(lipgloss.Left, top, main, bottom)
}

// handleKey dispatches keyboard events to the right view handler.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Help overlay: any key except another '?' closes it on esc; '?' toggles it.
	if m.help.Active() {
		switch msg.String() {
		case "?", "esc":
			m.help.Toggle()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	// Global keys active in all non-help views.
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help.Toggle()
		return m, nil
	case "r":
		// Manual refresh: schedule an immediate fetch regardless of view.
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

	// Per-view keys.
	switch m.current {
	case views.ViewList:
		return m.handleListKey(msg)
	case views.ViewDetail:
		return m.handleDetailKey(msg)
	case views.ViewDescribe:
		return m.handleDescribeKey(msg)
	case views.ViewNoDaemon:
		// No additional keys beyond global r/q/?
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
		name := m.list.SelectedName()
		if name != "" {
			m.selected = name
			m.current = views.ViewDetail
			// Kick an immediate fetch so the detail view is populated.
			return m, fetchAgent(m.ctx, m.rpc, name)
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
