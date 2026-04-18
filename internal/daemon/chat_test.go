package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/store"
)

// newTestRepo opens a temporary on-disk SQLite store with migrations applied.
// Returns the repo and arranges cleanup on test exit. An on-disk path under
// t.TempDir is used because modernc.org/sqlite's ":memory:" DSN does not share
// the DB across connections in the same process, whereas the goose migration
// runner and the test query may use different connections.
func newTestRepo(t *testing.T) *store.Repo {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dclaw-test.db")
	repo, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("repo.Migrate: %v", err)
	}
	return repo
}

// silentLogger returns an slog.Logger that discards all output.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// insertRunningAgent inserts a fake agent record into repo with the given name
// and containerID, status "running". Returns the inserted record's ID.
func insertRunningAgent(t *testing.T, repo *store.Repo, name, containerID string) {
	t.Helper()
	err := repo.InsertAgent(context.Background(), store.AgentRecord{
		ID:          "test-id-" + name,
		Name:        name,
		Image:       "dclaw-agent:v0.1",
		Status:      "running",
		ContainerID: containerID,
		Workspace:   "/tmp",
		Env:         "{}",
		Labels:      "{}",
		CreatedAt:   1000000,
		UpdatedAt:   1000000,
	})
	if err != nil {
		t.Fatalf("insertRunningAgent: %v", err)
	}
}

// ---------- mock DockerExecClient ----------

// mockDockerExec is a test double for sandbox.DockerExecClient. Fields control
// the return values from InspectStatus and ExecIn.
type mockDockerExec struct {
	// InspectStatus returns these.
	inspectStatus string
	inspectErr    error

	// ExecIn returns these.
	execStdout string
	execStderr string
	execCode   int
	execErr    error
}

func (m *mockDockerExec) InspectStatus(_ context.Context, _ string) (string, error) {
	return m.inspectStatus, m.inspectErr
}

func (m *mockDockerExec) ExecIn(_ context.Context, _ string, _ []string) (string, string, int, error) {
	return m.execStdout, m.execStderr, m.execCode, m.execErr
}

// ---------- helpers ----------

// sendCollector returns a send func that accumulates all envelopes into *received.
func sendCollector(received *[]*protocol.Envelope) func(*protocol.Envelope) error {
	return func(env *protocol.Envelope) error {
		*received = append(*received, env)
		return nil
	}
}

// chatParams marshals an AgentChatSendParams to json.RawMessage.
func chatParams(t *testing.T, name, content string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(protocol.AgentChatSendParams{Name: name, Content: content})
	if err != nil {
		t.Fatalf("chatParams marshal: %v", err)
	}
	return b
}

// ---------- tests ----------

// TestChatHandlerAgentNotFound verifies that a missing agent returns -32001.
// Uses an in-memory store with zero agents inserted and a nil docker client
// (exec is never reached on the not-found path).
func TestChatHandlerAgentNotFound(t *testing.T) {
	repo := newTestRepo(t)

	var received []*protocol.Envelope
	h := daemon.NewChatHandler(silentLogger(), repo, nil)

	if err := h.Handle(context.Background(), chatParams(t, "alice", "hello"), 42, sendCollector(&received)); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(received) != 1 {
		t.Fatalf("expected 1 envelope sent, got %d", len(received))
	}
	env := received[0]
	if env.Error == nil {
		t.Fatalf("expected error envelope, got %+v", env)
	}
	if env.Error.Code != protocol.ErrAgentNotFound {
		t.Fatalf("expected code -32001, got %d", env.Error.Code)
	}
}

// TestChatHandlerMissingContent verifies that empty content returns -32602.
func TestChatHandlerMissingContent(t *testing.T) {
	var received []*protocol.Envelope
	// handler with nil repo — the missing-content check fires before any repo call.
	h := daemon.NewChatHandler(nil, nil, nil)
	params, _ := json.Marshal(protocol.AgentChatSendParams{Name: "alice", Content: ""})
	_ = h.Handle(context.Background(), params, 1, sendCollector(&received))

	if len(received) == 0 {
		t.Fatal("expected an error response")
	}
	if received[0].Error == nil {
		t.Fatal("expected error envelope")
	}
	if received[0].Error.Code != protocol.ErrInvalidParams {
		t.Fatalf("expected -32602, got %d", received[0].Error.Code)
	}
}

// TestChatHandlerContainerNotRunning verifies that an agent whose container
// reports status != "running" gets a chat error chunk (not a protocol error).
// After the synchronous ack, the client receives an agent.chat.chunk with
// role="error" and final=true.
func TestChatHandlerContainerNotRunning(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")

	mock := &mockDockerExec{
		inspectStatus: "exited",
		inspectErr:    nil,
	}

	var received []*protocol.Envelope
	h := daemon.NewChatHandler(silentLogger(), repo, mock)

	if err := h.Handle(context.Background(), chatParams(t, "alice", "hello"), 1, sendCollector(&received)); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// Expect exactly 2 envelopes: ack result + error chunk notification.
	if len(received) != 2 {
		t.Fatalf("expected 2 envelopes (ack + error chunk), got %d", len(received))
	}

	// First envelope: synchronous ack (result, not error).
	ack := received[0]
	if ack.Error != nil {
		t.Fatalf("first envelope should be ack result, got error: %v", ack.Error)
	}

	// Second envelope: error chunk notification.
	notif := received[1]
	if notif.Method != "agent.chat.chunk" {
		t.Fatalf("expected agent.chat.chunk notification, got method %q", notif.Method)
	}
	var chunk protocol.AgentChatChunkNotification
	if err := json.Unmarshal(notif.Params, &chunk); err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	if chunk.Role != "error" {
		t.Fatalf("expected role=error, got %q", chunk.Role)
	}
	if !chunk.Final {
		t.Fatal("expected final=true on error chunk")
	}
}

// TestChatHandlerInspectError verifies that an InspectStatus error (e.g.,
// container removed mid-flight) results in a clean error chunk.
func TestChatHandlerInspectError(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")

	mock := &mockDockerExec{
		inspectStatus: "",
		inspectErr:    errors.New("no such container"),
	}

	var received []*protocol.Envelope
	h := daemon.NewChatHandler(silentLogger(), repo, mock)

	if err := h.Handle(context.Background(), chatParams(t, "alice", "hello"), 1, sendCollector(&received)); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 envelopes (ack + error chunk), got %d", len(received))
	}
	var chunk protocol.AgentChatChunkNotification
	_ = json.Unmarshal(received[1].Params, &chunk)
	if chunk.Role != "error" {
		t.Fatalf("expected error chunk, got role=%q", chunk.Role)
	}
}

// TestChatHandlerSuccessfulExec verifies the happy path: InspectStatus returns
// "running", ExecIn returns stdout with exit code 0, a final agent chunk arrives.
func TestChatHandlerSuccessfulExec(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")

	mock := &mockDockerExec{
		inspectStatus: "running",
		execStdout:    "Hello from pi!\n",
		execCode:      0,
	}

	var received []*protocol.Envelope
	h := daemon.NewChatHandler(silentLogger(), repo, mock)

	if err := h.Handle(context.Background(), chatParams(t, "alice", "hello"), 1, sendCollector(&received)); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// Expect ack + final chunk.
	if len(received) != 2 {
		t.Fatalf("expected 2 envelopes, got %d", len(received))
	}

	var chunk protocol.AgentChatChunkNotification
	if err := json.Unmarshal(received[1].Params, &chunk); err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	if chunk.Role != "agent" {
		t.Fatalf("expected role=agent, got %q", chunk.Role)
	}
	if chunk.Text != "Hello from pi!\n" {
		t.Fatalf("expected stdout text, got %q", chunk.Text)
	}
	if !chunk.Final {
		t.Fatal("expected final=true")
	}
}

// TestChatHandlerNonZeroExitCode verifies that a non-zero exit code from ExecIn
// produces an error chunk (not an agent chunk) with the stderr content.
func TestChatHandlerNonZeroExitCode(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")

	mock := &mockDockerExec{
		inspectStatus: "running",
		execStdout:    "",
		execStderr:    "pi: command not found\n",
		execCode:      127,
	}

	var received []*protocol.Envelope
	h := daemon.NewChatHandler(silentLogger(), repo, mock)

	if err := h.Handle(context.Background(), chatParams(t, "alice", "hello"), 1, sendCollector(&received)); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 envelopes, got %d", len(received))
	}
	var chunk protocol.AgentChatChunkNotification
	_ = json.Unmarshal(received[1].Params, &chunk)
	if chunk.Role != "error" {
		t.Fatalf("expected error chunk on non-zero exit, got role=%q text=%q", chunk.Role, chunk.Text)
	}
	if !chunk.Final {
		t.Fatal("expected final=true on exit-code error chunk")
	}
	// Verify the exit code is mentioned in the text.
	if len(chunk.Text) == 0 {
		t.Fatal("expected non-empty error text")
	}
}

// TestChatHandlerExecError verifies that an ExecIn error (e.g., Docker daemon
// down mid-exec) produces an error chunk.
func TestChatHandlerExecError(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")

	mock := &mockDockerExec{
		inspectStatus: "running",
		execErr:       errors.New("docker: connection reset"),
	}

	var received []*protocol.Envelope
	h := daemon.NewChatHandler(silentLogger(), repo, mock)

	if err := h.Handle(context.Background(), chatParams(t, "alice", "hello"), 1, sendCollector(&received)); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 envelopes, got %d", len(received))
	}
	var chunk protocol.AgentChatChunkNotification
	_ = json.Unmarshal(received[1].Params, &chunk)
	if chunk.Role != "error" {
		t.Fatalf("expected error chunk on exec failure, got role=%q", chunk.Role)
	}
}
