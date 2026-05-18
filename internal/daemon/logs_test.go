package daemon_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/store"
)

type mockLogsDocker struct {
	lines []string
	err   error
}

func (m *mockLogsDocker) LogsFollow(context.Context, string, int) (<-chan string, <-chan error) {
	lines := make(chan string, len(m.lines))
	errs := make(chan error, 1)
	for _, line := range m.lines {
		lines <- line
	}
	close(lines)
	if m.err != nil {
		errs <- m.err
	}
	close(errs)
	return lines, errs
}

func TestLogStreamHandlerForwardsLines(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-alice")

	var received []*protocol.Envelope
	h := daemon.NewLogStreamHandler(slog.Default(), repo, &mockLogsDocker{lines: []string{"line one", "line two"}})
	params, err := json.Marshal(protocol.LogsStreamParams{Name: "alice", Follow: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := h.Handle(context.Background(), params, 7, sendCollector(&received)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(received) != 4 {
		t.Fatalf("expected ack + 2 line notifications + done, got %d", len(received))
	}
	if received[0].Error != nil || len(received[0].Result) == 0 {
		t.Fatalf("expected ack result, got %+v", received[0])
	}
	for i, want := range []string{"line one", "line two"} {
		env := received[i+1]
		if env.Method != "agent.log.line" {
			t.Fatalf("notification %d method=%q want agent.log.line", i, env.Method)
		}
		var line protocol.LogsStreamLineNotification
		if err := json.Unmarshal(env.Params, &line); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		if line.Name != "alice" || line.Line != want || line.Stream != "stdout" {
			t.Fatalf("line notification = %#v", line)
		}
	}
	if received[3].Method != "agent.log.done" {
		t.Fatalf("expected terminal agent.log.done notification, got %q", received[3].Method)
	}
}

func TestLogStreamHandlerAgentNotFound(t *testing.T) {
	repo := newTestRepo(t)

	var received []*protocol.Envelope
	h := daemon.NewLogStreamHandler(slog.Default(), repo, &mockLogsDocker{})
	params, err := json.Marshal(protocol.LogsStreamParams{Name: "missing", Follow: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := h.Handle(context.Background(), params, 8, sendCollector(&received)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("expected one error response, got %d", len(received))
	}
	if received[0].Error == nil || received[0].Error.Code != protocol.ErrAgentNotFound {
		t.Fatalf("expected ErrAgentNotFound, got %+v", received[0])
	}
}

func TestLogStreamHandlerAgentNotRunning(t *testing.T) {
	repo := newTestRepo(t)
	if err := repo.InsertAgent(context.Background(), store.AgentRecord{
		ID:        "test-id-no-container",
		Name:      "alice",
		Image:     "dclaw-agent:v0.1",
		Status:    "created",
		Env:       "{}",
		Labels:    "{}",
		CreatedAt: 1,
		UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	var received []*protocol.Envelope
	h := daemon.NewLogStreamHandler(slog.Default(), repo, &mockLogsDocker{})
	params, err := json.Marshal(protocol.LogsStreamParams{Name: "alice", Follow: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := h.Handle(context.Background(), params, 9, sendCollector(&received)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("expected one error response, got %d", len(received))
	}
	if received[0].Error == nil || received[0].Error.Code != protocol.ErrAgentNotRunning {
		t.Fatalf("expected ErrAgentNotRunning, got %+v", received[0])
	}
}
