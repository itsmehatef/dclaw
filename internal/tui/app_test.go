package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/tui/views"
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

// TestTUIChatOpenFromList verifies that pressing 'c' from the list view
// with a selected agent transitions to ViewChat.
func TestTUIChatOpenFromList(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "alice"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.current != views.ViewChat {
		t.Fatalf("expected ViewChat after 'c', got %v", m.current)
	}
	if m.selected != "alice" {
		t.Fatalf("expected selected=alice, got %q", m.selected)
	}
}

// TestTUIChatEscReturns verifies that pressing 'esc' from a non-streaming
// chat view returns to the previous view.
func TestTUIChatEscReturns(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "bob"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.current != views.ViewChat {
		t.Fatalf("expected ViewChat, got %v", m.current)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.current != views.ViewList {
		t.Fatalf("expected ViewList after esc, got %v", m.current)
	}
}

// Suppress unused import warning for teatest in files that only use the
// inline test model approach above. Remove this line if teatest is already
// used elsewhere in the file.
var _ = teatest.WithFinalTimeout
