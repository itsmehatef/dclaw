package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/paths"
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
	mu sync.Mutex

	// InspectStatus returns these.
	inspectStatus string
	inspectErr    error

	// ExecIn returns these.
	execStdout string
	execStderr string
	execCode   int
	execErr    error
	execArgv   []string

	inspectCalls int
	execCalls    int
	execStarted  chan struct{}
	execRelease  chan struct{}
}

func (m *mockDockerExec) InspectStatus(_ context.Context, _ string) (string, error) {
	m.mu.Lock()
	m.inspectCalls++
	m.mu.Unlock()
	return m.inspectStatus, m.inspectErr
}

func (m *mockDockerExec) ExecIn(_ context.Context, _ string, argv []string) (string, string, int, error) {
	m.mu.Lock()
	m.execCalls++
	m.execArgv = append([]string(nil), argv...)
	if m.execStarted != nil && m.execCalls == 1 {
		close(m.execStarted)
	}
	release := m.execRelease
	m.mu.Unlock()
	if release != nil {
		<-release
	}
	return m.execStdout, m.execStderr, m.execCode, m.execErr
}

func (m *mockDockerExec) calls() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inspectCalls, m.execCalls
}

func (m *mockDockerExec) argv() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.execArgv...)
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
	b, err := json.Marshal(protocol.AgentChatSendParams{Name: name, Content: content, MessageID: "user-msg-" + name})
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

func TestChatHandlerMissingMessageID(t *testing.T) {
	var received []*protocol.Envelope
	h := daemon.NewChatHandler(nil, nil, nil)
	params, _ := json.Marshal(protocol.AgentChatSendParams{Name: "alice", Content: "hello"})
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
	gotArgv := mock.argv()
	wantArgv := []string{"node", "/app/run.mjs", "hello"}
	if len(gotArgv) != len(wantArgv) {
		t.Fatalf("exec argv len=%d (%v), want %d (%v)", len(gotArgv), gotArgv, len(wantArgv), wantArgv)
	}
	for i := range wantArgv {
		if gotArgv[i] != wantArgv[i] {
			t.Fatalf("exec argv[%d]=%q, want %q (full argv %v)", i, gotArgv[i], wantArgv[i], gotArgv)
		}
	}

	rec, err := repo.GetAgent(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	history, err := repo.ListChatHistory(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatalf("ListChatHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len=%d want 2", len(history))
	}
	if history[0].Role != "user" || history[0].Content != "hello" || history[0].MessageID != "user-msg-alice" {
		t.Fatalf("unexpected user history row: %#v", history[0])
	}
	if history[1].Role != "agent" || history[1].Content != "Hello from pi!\n" || history[1].ParentID != "user-msg-alice" {
		t.Fatalf("unexpected agent history row: %#v", history[1])
	}
	if history[1].MessageID != chunk.MessageID {
		t.Fatalf("reply message id mismatch: history=%q chunk=%q", history[1].MessageID, chunk.MessageID)
	}
}

func TestChatHandlerDuplicateMessageIDReplaysReplyWithoutExec(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")

	mock := &mockDockerExec{
		inspectStatus: "running",
		execStdout:    "Hello once!\n",
		execCode:      0,
	}

	h := daemon.NewChatHandler(silentLogger(), repo, mock)
	params := chatParams(t, "alice", "hello")

	var first []*protocol.Envelope
	if err := h.Handle(context.Background(), params, 1, sendCollector(&first)); err != nil {
		t.Fatalf("first Handle returned error: %v", err)
	}
	_, execCalls := mock.calls()
	if execCalls != 1 {
		t.Fatalf("exec calls after first send=%d want 1", execCalls)
	}

	var replay []*protocol.Envelope
	if err := h.Handle(context.Background(), params, 2, sendCollector(&replay)); err != nil {
		t.Fatalf("replay Handle returned error: %v", err)
	}
	inspectCalls, execCalls := mock.calls()
	if inspectCalls != 1 || execCalls != 1 {
		t.Fatalf("duplicate should not inspect/exec again, inspect=%d exec=%d", inspectCalls, execCalls)
	}
	if len(replay) != 2 {
		t.Fatalf("expected replay ack + chunk, got %d envelopes", len(replay))
	}
	if replay[0].Error != nil {
		t.Fatalf("replay ack should not be error: %v", replay[0].Error)
	}
	var chunk protocol.AgentChatChunkNotification
	if err := json.Unmarshal(replay[1].Params, &chunk); err != nil {
		t.Fatalf("unmarshal replay chunk: %v", err)
	}
	if chunk.Role != "agent" || chunk.Text != "Hello once!\n" || !chunk.Final {
		t.Fatalf("unexpected replay chunk: %#v", chunk)
	}

	rec, err := repo.GetAgent(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	history, err := repo.ListChatHistory(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatalf("ListChatHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("duplicate send should not add history rows, got %d", len(history))
	}
}

func TestChatHandlerDuplicateMessageIDWaitsForInFlightReply(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")
	rec, err := repo.GetAgent(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if err := repo.InsertChatMessage(context.Background(), store.ChatMessageRecord{
		ID:        store.NewID(),
		AgentID:   rec.ID,
		Role:      "user",
		Content:   "hello",
		MessageID: "user-msg-alice",
		Timestamp: time.Now().Unix(),
	}); err != nil {
		t.Fatalf("InsertChatMessage user: %v", err)
	}

	mock := &mockDockerExec{
		inspectStatus: "running",
		execStdout:    "should not run\n",
		execCode:      0,
	}
	h := daemon.NewChatHandler(silentLogger(), repo, mock)

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = repo.InsertChatMessage(context.Background(), store.ChatMessageRecord{
			ID:        store.NewID(),
			AgentID:   rec.ID,
			Role:      "agent",
			Content:   "eventual reply\n",
			ParentID:  "user-msg-alice",
			MessageID: "reply-msg-alice",
			Timestamp: time.Now().Unix(),
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var received []*protocol.Envelope
	if err := h.Handle(ctx, chatParams(t, "alice", "hello"), 2, sendCollector(&received)); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	inspectCalls, execCalls := mock.calls()
	if inspectCalls != 0 || execCalls != 0 {
		t.Fatalf("duplicate should not inspect/exec, inspect=%d exec=%d", inspectCalls, execCalls)
	}
	if len(received) != 2 {
		t.Fatalf("expected ack + replay chunk, got %d envelopes", len(received))
	}
	var chunk protocol.AgentChatChunkNotification
	if err := json.Unmarshal(received[1].Params, &chunk); err != nil {
		t.Fatalf("unmarshal replay chunk: %v", err)
	}
	if chunk.Role != "agent" || chunk.Text != "eventual reply\n" || chunk.MessageID != "reply-msg-alice" || !chunk.Final {
		t.Fatalf("unexpected replay chunk: %#v", chunk)
	}
}

func TestChatHandlerConcurrentDuplicateMessageIDWaitsForOriginalReply(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")

	execStarted := make(chan struct{})
	execRelease := make(chan struct{})
	mock := &mockDockerExec{
		inspectStatus: "running",
		execStdout:    "original reply\n",
		execCode:      0,
		execStarted:   execStarted,
		execRelease:   execRelease,
	}
	h := daemon.NewChatHandler(silentLogger(), repo, mock)
	params := chatParams(t, "alice", "hello")

	firstEnvs := make(chan *protocol.Envelope, 4)
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- h.Handle(context.Background(), params, 1, func(env *protocol.Envelope) error {
			firstEnvs <- env
			return nil
		})
	}()
	select {
	case <-execStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first send did not reach ExecIn")
	}

	secondEnvs := make(chan *protocol.Envelope, 4)
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- h.Handle(context.Background(), params, 2, func(env *protocol.Envelope) error {
			secondEnvs <- env
			return nil
		})
	}()

	select {
	case env := <-secondEnvs:
		if env.Error != nil {
			t.Fatalf("duplicate ack should not be error: %v", env.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("duplicate did not ack")
	}
	select {
	case env := <-secondEnvs:
		t.Fatalf("duplicate should wait for original reply before final chunk, got %#v", env)
	case <-time.After(100 * time.Millisecond):
	}

	close(execRelease)

	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first Handle returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first Handle did not finish")
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second Handle returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second Handle did not finish")
	}

	var replay protocol.AgentChatChunkNotification
	for len(secondEnvs) > 0 {
		env := <-secondEnvs
		if env.Method != "agent.chat.chunk" {
			continue
		}
		if err := json.Unmarshal(env.Params, &replay); err != nil {
			t.Fatalf("unmarshal replay: %v", err)
		}
	}
	if replay.Role != "agent" || replay.Text != "original reply\n" || !replay.Final || replay.MessageID == "" {
		t.Fatalf("unexpected replay chunk: %#v", replay)
	}
	_, execCalls := mock.calls()
	if execCalls != 1 {
		t.Fatalf("duplicate should not execute again, exec calls=%d", execCalls)
	}
}

func TestChatHandlerUserPersistenceFailureAbortsBeforeAckOrExec(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")
	if err := repo.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback 0003: %v", err)
	}

	mock := &mockDockerExec{
		inspectStatus: "running",
		execStdout:    "should not run\n",
		execCode:      0,
	}
	var received []*protocol.Envelope
	h := daemon.NewChatHandler(silentLogger(), repo, mock)
	if err := h.Handle(context.Background(), chatParams(t, "alice", "hello"), 1, sendCollector(&received)); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("expected one error response, got %d", len(received))
	}
	if received[0].Error == nil || received[0].Error.Code != protocol.ErrChatHistoryUnavailable {
		t.Fatalf("expected ErrChatHistoryUnavailable, got %#v", received[0].Error)
	}
	inspectCalls, execCalls := mock.calls()
	if inspectCalls != 0 || execCalls != 0 {
		t.Fatalf("history failure should not inspect/exec, inspect=%d exec=%d", inspectCalls, execCalls)
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
	rec, err := repo.GetAgent(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	history, err := repo.ListChatHistory(context.Background(), rec.ID, 0)
	if err != nil {
		t.Fatalf("ListChatHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len=%d want 2", len(history))
	}
	if history[1].Role != "error" || history[1].ParentID != "user-msg-alice" {
		t.Fatalf("unexpected error history row: %#v", history[1])
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

func TestChatHistoryAppendValidationAndDuplicate(t *testing.T) {
	repo := newTestRepo(t)
	insertRunningAgent(t, repo, "alice", "ctr-abc")
	r := daemon.NewRouter(silentLogger(), repo, nil, paths.Policy{}, nil)

	invalidRole := dispatchTestRPC(t, r, "agent.chat.history.append", protocol.ChatHistoryAppendParams{
		Name:      "alice",
		Role:      "bogus",
		Content:   "hello",
		MessageID: "manual-1",
	})
	if invalidRole.Error == nil || invalidRole.Error.Code != protocol.ErrInvalidParams {
		t.Fatalf("invalid role error=%#v want ErrInvalidParams", invalidRole.Error)
	}

	emptyContent := dispatchTestRPC(t, r, "agent.chat.history.append", protocol.ChatHistoryAppendParams{
		Name:      "alice",
		Role:      "user",
		MessageID: "manual-1",
	})
	if emptyContent.Error == nil || emptyContent.Error.Code != protocol.ErrInvalidParams {
		t.Fatalf("empty content error=%#v want ErrInvalidParams", emptyContent.Error)
	}

	missingMessageID := dispatchTestRPC(t, r, "agent.chat.history.append", protocol.ChatHistoryAppendParams{
		Name:    "alice",
		Role:    "user",
		Content: "hello",
	})
	if missingMessageID.Error == nil || missingMessageID.Error.Code != protocol.ErrInvalidParams {
		t.Fatalf("missing message_id error=%#v want ErrInvalidParams", missingMessageID.Error)
	}

	params := protocol.ChatHistoryAppendParams{
		Name:      "alice",
		Role:      "user",
		Content:   "hello",
		MessageID: "manual-1",
	}
	ok := dispatchTestRPC(t, r, "agent.chat.history.append", params)
	if ok.Error != nil {
		t.Fatalf("append error: %v", ok.Error)
	}
	dupe := dispatchTestRPC(t, r, "agent.chat.history.append", params)
	if dupe.Error == nil || dupe.Error.Code != protocol.ErrInvalidParams {
		t.Fatalf("duplicate error=%#v want ErrInvalidParams", dupe.Error)
	}
}

func dispatchTestRPC(t *testing.T, r *daemon.Router, method string, params any) *protocol.Envelope {
	t.Helper()
	env := protocol.Request(1, method, params)
	resp := r.Dispatch(context.Background(), env, func(*protocol.Envelope) error {
		t.Fatal("unexpected streaming send")
		return nil
	})
	if resp == nil {
		t.Fatal("expected response envelope")
	}
	return resp
}
