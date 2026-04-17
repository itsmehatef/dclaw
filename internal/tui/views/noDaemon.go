package views

import "fmt"

// NoDaemonModel renders the "daemon not running" screen. This view is shown
// whenever the TUI cannot reach the daemon socket. The user can press 'r' to
// retry (see retryDial in poll.go) or 'q' to quit.
type NoDaemonModel struct {
	err string
}

// NewNoDaemonModel returns a model pre-populated with the dial error text.
func NewNoDaemonModel(err error) NoDaemonModel {
	msg := "daemon not running"
	if err != nil {
		msg = err.Error()
	}
	return NoDaemonModel{err: msg}
}

// SetErr replaces the error message (used when a retry fails with a new error).
func (m *NoDaemonModel) SetErr(err error) {
	if err != nil {
		m.err = err.Error()
	}
}

// View renders the no-daemon screen into the given terminal dimensions.
func (m *NoDaemonModel) View(width, height int) string {
	box := fmt.Sprintf(
		"  dclaw daemon is not running\n\n"+
			"  Error: %s\n\n"+
			"  Start the daemon:  dclaw daemon start\n\n"+
			"  Press 'r' to retry, 'q' to quit.",
		m.err,
	)
	// Centre the box vertically (rough approximation).
	padding := (height - 7) / 2
	if padding < 0 {
		padding = 0
	}
	out := ""
	for i := 0; i < padding; i++ {
		out += "\n"
	}
	out += box
	return out
}
