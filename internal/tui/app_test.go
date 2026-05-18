package tui

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/tui/components"
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

func TestTUIChatHistoryLoaded(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "alice"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = m.Update(chatHistoryLoadedMsg{agentName: "alice", messages: []protocol.ChatMessage{
		{Role: "user", Content: "hello", MessageID: "m1", Timestamp: time.Now().Unix()},
		{Role: "agent", Content: "hi", ParentID: "m1", MessageID: "m2", Timestamp: time.Now().Unix()},
	}})
	got := m.View()
	if !strings.Contains(got, "hello") || !strings.Contains(got, "hi") {
		t.Fatalf("expected history in chat view, got %q", got)
	}
	if m.chat.LastMessageID() != "m2" {
		t.Fatalf("last message id=%q want m2", m.chat.LastMessageID())
	}
}

func TestTUIChatHistoryLoadedIgnoresStaleLoad(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "alice"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	m.chatHistoryLoadID = 2
	m.chatHistoryLoading = true
	_, _ = m.Update(chatHistoryLoadedMsg{agentName: "alice", loadID: 1, messages: []protocol.ChatMessage{
		{Role: "user", Content: "stale", MessageID: "m1", Timestamp: time.Now().Unix()},
	}})
	if got := m.View(); strings.Contains(got, "stale") {
		t.Fatalf("stale history load should not render, got %q", got)
	}
	if !m.chatHistoryLoading {
		t.Fatal("stale history load should not clear loading state")
	}
}

func TestTUIHelpOpensFromChatAndLogs(t *testing.T) {
	m := NewModel(t.Context(), client.NewRPCClient(filepath.Join(t.TempDir(), "unused.sock")), nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "alice"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !m.chatHistoryLoading {
		t.Fatal("expected chat history loading before help key")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !m.help.Active() {
		t.Fatal("expected help to open from chat view while history is loading")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	m.current = views.ViewList
	m.chatHistoryLoading = false
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !m.help.Active() {
		t.Fatal("expected help to open from logs view")
	}
}

func TestTUIOpenChatLoadsHistoryViaRPC(t *testing.T) {
	socket := serveTUIHistoryRPC(t, "alice", []protocol.ChatMessage{
		{Role: "user", Content: "hello from disk", MessageID: "m1", Timestamp: time.Now().Unix()},
		{Role: "agent", Content: "loaded reply", ParentID: "m1", MessageID: "m2", Timestamp: time.Now().Unix()},
	})
	rpc := client.NewRPCClient(socket)
	t.Cleanup(func() { _ = rpc.Close() })

	m := NewModel(t.Context(), rpc, nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "alice"}}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("expected history load command")
	}
	if !m.chatHistoryLoading {
		t.Fatal("expected chat history loading flag")
	}
	msg := cmd()
	_, _ = m.Update(msg)
	if m.chatHistoryLoading {
		t.Fatal("expected chat history loading to finish")
	}
	got := m.View()
	if !strings.Contains(got, "hello from disk") || !strings.Contains(got, "loaded reply") {
		t.Fatalf("expected RPC-loaded history in view, got %q", got)
	}
}

func TestTUIChatHistoryLoadingBlocksInput(t *testing.T) {
	m := NewModel(t.Context(), client.NewRPCClient(filepath.Join(t.TempDir(), "unused.sock")), nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "alice"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !m.chatHistoryLoading {
		t.Fatal("expected chat history loading flag")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fast prompt")})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.streaming {
		t.Fatal("should not start chat while history is loading")
	}
	if got := m.View(); strings.Contains(got, "fast prompt") {
		t.Fatalf("input should be blocked until history loads, got %q", got)
	}
}

func TestTUILogsOpenFromList(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "alice"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.current != views.ViewLogs {
		t.Fatalf("expected ViewLogs after 'l', got %v", m.current)
	}
	if m.selected != "alice" {
		t.Fatalf("expected selected=alice, got %q", m.selected)
	}
	if got := m.View(); !strings.Contains(got, "no daemon connection") {
		t.Fatalf("expected no daemon connection error in view, got %q", got)
	}
}

func TestTUILogsLineAppearsAndEscReturns(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "bob"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.current != views.ViewLogs {
		t.Fatalf("expected ViewLogs, got %v", m.current)
	}

	_, _ = m.Update(logLineMsg{streamID: m.logStreamID, line: client.LogLineEvent{Name: "bob", Line: "T24_PROBE_OK", Stream: "stdout"}})
	if got := m.View(); !strings.Contains(got, "T24_PROBE_OK") {
		t.Fatalf("expected streamed log line in view, got %q", got)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.current != views.ViewList {
		t.Fatalf("expected ViewList after esc, got %v", m.current)
	}
}

func TestToastPushedOnDaemonDisconnect(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	_, _ = m.Update(daemonErrMsg{err: errTestDaemonDown})
	if m.toasts.Len() != 1 {
		t.Fatalf("expected one toast, got %d", m.toasts.Len())
	}
	if got := m.View(); !strings.Contains(got, "daemon disconnected") {
		t.Fatalf("expected daemon disconnected toast in view, got %q", got)
	}
}

func TestToastOverlayKeepsBottomBar(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	_, _ = m.Update(toastMsg{level: "info", message: "hello"})
	got := m.View()
	if !strings.Contains(got, "hello") {
		t.Fatalf("expected toast in view, got %q", got)
	}
	if !strings.Contains(got, "q:quit") {
		t.Fatalf("expected bottom bar to remain visible, got %q", got)
	}
}

func TestToastTickExpiresEntries(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(toastMsg{level: "info", message: "hello"})
	if m.toasts.Len() != 1 {
		t.Fatalf("expected one toast, got %d", m.toasts.Len())
	}
	_, _ = m.Update(toastTickMsg{now: time.Now().Add(components.ToastDuration + time.Second)})
	if m.toasts.Len() != 0 {
		t.Fatalf("expected expired toast removed, got %d", m.toasts.Len())
	}
}

func TestToastDismissTop(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(toastMsg{level: "info", message: "hello"})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if m.toasts.Len() != 0 {
		t.Fatalf("expected toast dismissed, got %d", m.toasts.Len())
	}
}

// Suppress unused import warning for teatest in files that only use the
// inline test model approach above. Remove this line if teatest is already
// used elsewhere in the file.
var _ = teatest.WithFinalTimeout

var errTestDaemonDown = &testErr{s: "boom"}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

func serveTUIHistoryRPC(t *testing.T, expectedName string, messages []protocol.ChatMessage) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "dclaw.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)

		var hs protocol.Envelope
		if err := dec.Decode(&hs); err != nil {
			done <- err
			return
		}
		if err := enc.Encode(protocol.SuccessResponse(hs.ID, protocol.HandshakeResult{
			Accepted:          true,
			NegotiatedVersion: protocol.Version,
		})); err != nil {
			done <- err
			return
		}

		var req protocol.Envelope
		if err := dec.Decode(&req); err != nil {
			done <- err
			return
		}
		if req.Method != "agent.chat.history.list" {
			t.Errorf("method=%q want agent.chat.history.list", req.Method)
		}
		var params protocol.ChatHistoryListParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			done <- err
			return
		}
		if params.Name != expectedName || params.Limit != 0 {
			t.Errorf("params=%#v want name=%s limit=0", params, expectedName)
		}
		if err := enc.Encode(protocol.SuccessResponse(req.ID, protocol.ChatHistoryListResult{Messages: messages})); err != nil {
			done <- err
			return
		}
		done <- nil
	}()
	t.Cleanup(func() {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("fake TUI RPC server: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("fake TUI RPC server did not finish")
		}
	})
	return socket
}
