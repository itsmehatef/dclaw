// Package tui is the dclaw interactive dashboard. Entry points:
//
//   - Run(socketPath)                    — bare `dclaw` launch, ViewList.
//   - RunAttached(socketPath, name)      — `dclaw agent attach` alpha.2 path; ViewDetail.
//   - RunChatAttached(socketPath, name)  — `dclaw agent attach` alpha.3+ path; ViewChat.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/tui/views"
)

// NoMouse is set to true by cmd/dclaw/main.go when --no-mouse is passed.
var NoMouse bool

// Run launches the TUI on the default list view.
func Run(socketPath string) error {
	return runTUI(socketPath, nil)
}

// RunAttached launches the TUI on ViewDetail for agentName (alpha.2 compat).
func RunAttached(socketPath, agentName string) error {
	return runTUI(socketPath, &attachTarget{agentName: agentName, startChat: false})
}

// RunChatAttached launches the TUI on ViewChat for agentName (alpha.3+).
func RunChatAttached(socketPath, agentName string) error {
	return runTUI(socketPath, &attachTarget{agentName: agentName, startChat: true})
}

func runTUI(socketPath string, target *attachTarget) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rpc := client.NewRPCClient(socketPath)

	var startErr error
	if err := rpc.Dial(ctx); err != nil {
		startErr = err
	}

	m := NewModel(ctx, rpc, target)
	if startErr != nil {
		m.noDaemon.SetErr(startErr)
		m.current = views.ViewNoDaemon
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if !NoMouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(m, opts...)
	_, err := p.Run()
	return err
}
