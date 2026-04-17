package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsmehatef/dclaw/internal/client"
)

const pollInterval = 2 * time.Second
const rpcTimeout = 5 * time.Second

// tickPoll schedules the next poll tick. Called at the end of every successful
// agentsLoadedMsg handling to keep the 2s cadence running.
func tickPoll() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return pollTickMsg(t)
	})
}

// fetchAgents is a tea.Cmd that calls agent.list on the daemon and returns
// either agentsLoadedMsg (success) or daemonErrMsg (failure). The command
// creates its own timeout-scoped context so it does not leak on view changes.
func fetchAgents(ctx context.Context, rpc *client.RPCClient) tea.Cmd {
	return func() tea.Msg {
		if rpc == nil {
			return daemonErrMsg{err: fmt.Errorf("no rpc client")}
		}
		cctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		agents, err := rpc.AgentList(cctx)
		if err != nil {
			return daemonErrMsg{err: err}
		}
		return agentsLoadedMsg{agents: agents}
	}
}

// fetchAgent is a tea.Cmd that calls agent.get for the named agent and returns
// either agentFetchedMsg or daemonErrMsg.
func fetchAgent(ctx context.Context, rpc *client.RPCClient, name string) tea.Cmd {
	return func() tea.Msg {
		if rpc == nil {
			return daemonErrMsg{err: fmt.Errorf("no rpc client")}
		}
		cctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		a, err := rpc.AgentGet(cctx, name)
		if err != nil {
			return daemonErrMsg{err: err}
		}
		return agentFetchedMsg{agent: a}
	}
}

// retryDial is a tea.Cmd issued by the noDaemon view's 'r' key. It attempts a
// fresh Dial on the existing RPCClient (Dial is idempotent; it re-opens the
// connection if closed). On success it returns agentsLoadedMsg to restore the
// list view.
func retryDial(ctx context.Context, rpc *client.RPCClient) tea.Cmd {
	return func() tea.Msg {
		if rpc == nil {
			return daemonErrMsg{err: fmt.Errorf("no rpc client")}
		}
		cctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		if err := rpc.Dial(cctx); err != nil {
			return daemonErrMsg{err: err}
		}
		agents, err := rpc.AgentList(cctx)
		if err != nil {
			return daemonErrMsg{err: err}
		}
		return agentsLoadedMsg{agents: agents}
	}
}
