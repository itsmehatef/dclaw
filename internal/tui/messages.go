package tui

import (
	"time"

	"github.com/itsmehatef/dclaw/internal/client"
)

// agentsLoadedMsg carries a fresh agent list from the daemon.
type agentsLoadedMsg struct {
	agents []client.Agent
}

// agentFetchedMsg carries a single agent record for the detail/describe view.
type agentFetchedMsg struct {
	agent client.Agent
}

// pollTickMsg fires on the 2-second polling cadence to trigger a fresh fetch.
type pollTickMsg time.Time

// daemonErrMsg is emitted when an RPC call fails. The TUI transitions to
// ViewNoDaemon on the first error and stays there until a manual retry
// (key 'r') succeeds.
type daemonErrMsg struct {
	err error
}

// retryMsg is injected by the 'r' key handler to kick off a reconnection
// attempt from the noDaemon view.
type retryMsg struct{}
