package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// TestTUISmoke exercises the basic key dispatch without a live daemon.
// The Model is constructed directly (no RPC dial) so this test has no
// external dependencies.
func TestTUISmoke(t *testing.T) {
	// Build a model with no RPC client (nil). The noDaemon view handles the
	// nil gracefully because Init() will receive a daemonErrMsg when the
	// first fetchAgents call returns a connection error, but since we inject
	// keys immediately the test does not wait for that cycle.
	m := NewModel(t.Context(), nil, nil)
	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(120, 30),
	)

	// Navigate: down, up — should not crash on an empty list.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})

	// Open and close help overlay.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// Quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// TestTUIListNav verifies cursor movement clamping on an empty list.
func TestTUIListNav(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	// Inject agents directly via the message path.
	_, _ = m.Update(agentsLoadedMsg{agents: nil})

	m.list.Down()
	m.list.Up()
	if m.list.SelectedName() != "" {
		t.Fatalf("expected empty selection, got %q", m.list.SelectedName())
	}
}
