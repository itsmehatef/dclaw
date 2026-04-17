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
// ViewNoDaemon on the first error and stays there until a manual retry succeeds.
type daemonErrMsg struct {
	err error
}

// retryMsg is injected by the 'r' key handler to kick off a reconnection attempt.
type retryMsg struct{}

// ---------- chat messages (alpha.3) ----------

// chatStreamOpenedMsg is emitted when ChatSend has been accepted by the daemon
// and the chunk channel is ready to drain.
type chatStreamOpenedMsg struct {
	agentName string
	messageID string
	chunks    <-chan client.ChatChunkEvent
}

// chatDeltaMsg is emitted for each chunk received from the daemon stream.
type chatDeltaMsg struct {
	chunk client.ChatChunkEvent
}

// chatAckedMsg is emitted when a message stream reaches final=true.
// The complete assembled text is included for future persistence hooks.
type chatAckedMsg struct {
	agentName string
	messageID string
	fullText  string
}

// chatErrorMsg is emitted when the stream breaks before final=true.
type chatErrorMsg struct {
	agentName string
	err       error
}

// chatStreamClosedMsg signals the drain goroutine has exited (channel closed).
type chatStreamClosedMsg struct{}
