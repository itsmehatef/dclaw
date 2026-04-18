# Phase 3 Alpha.4 Plan — v0.3.0-alpha.4 Reliability + Ergonomics

**Goal:** Four targeted reliability and ergonomics improvements on top of alpha.3's working chat. No new user-facing features. Scope locked to: (1) `--env` shell inheritance for well-known API keys, (2) DB status reconciliation via background goroutine, (3) Test 13 via `dclaw agent chat --one-shot`, (4) `chat_test.go` ExecIn coverage via interface extraction and mock injection.

**Prereq:** `v0.3.0-alpha.3` tagged at commit `a9da0c1`. All alpha.3 checklist items green. Docker daemon reachable. `go 1.25.0` installed.

---

## 0. Status

**SHIPPED (2026-04-18)** as `v0.3.0-alpha.4`.

| Field | Value |
|---|---|
| **Tag** | `v0.3.0-alpha.4` |
| **Branch** | `main` |
| **Base commit** | `a9da0c1` (`v0.3.0-alpha.3`) |
| **Implementation commits** | `2f97f9f` (Agent A: `--env` shell inheritance), `9962617` (Agent B: DB status reconciler), `6bf7512` (Agent C: Test 13 `--one-shot` + `chat_test` refactor + `DockerExecClient` interface) |
| **Duration** | 1 day (3 parallel agents) |
| **Prereqs** | v0.3.0-alpha.3 green; chat works end-to-end |
| **Next tag** | `v0.3.0-beta.1` (logs view + polish) |

---

## 1. Overview

Alpha.3 shipped the first dogfood moment: type a message to an agent and watch the response arrive. Alpha.4 is a reliability and ergonomics pass on that foundation. It contains no new user-facing features.

**What alpha.4 delivers:**

- **Item 1 — `--env` shell inheritance**: `dclaw agent create` and `dclaw agent update` automatically inherit well-known API keys (`ANTHROPIC_API_KEY`, `ANTHROPIC_OAUTH_TOKEN`) from the user's shell if not already passed via `--env`. Allowlist-controlled, explicit `--env` always wins. Zero protocol changes, zero daemon changes.
- **Item 2 — DB status reconciliation**: A background goroutine in `dclawd` polls all agent containers every 2 seconds and updates DB status on state change. Prevents the DB from permanently showing `running` for containers that have exited, OOM-killed, or died. Graceful shutdown on daemon stop.
- **Item 3 — Test 13, real chat round-trip smoke**: New `dclaw agent chat <name> --one-shot "<prompt>"` CLI subcommand. Sends one message, collects all chunks, exits after `final: true`. Smoke script Test 13 invokes it against a live agent and greps the output. CI gates this test behind `$ANTHROPIC_API_KEY` presence check.
- **Item 4 — `chat_test.go` ExecIn coverage**: Extract `DockerExecClient` interface from `sandbox.DockerClient`. `ChatHandler` takes the interface. Tests inject a struct-literal mock covering: successful exec path, non-zero exit code propagation, `InspectStatus` pre-check, container-not-running path.

**What alpha.4 does NOT deliver (explicitly out of scope):**

- Logs view → beta.1
- `:` vim command mode → beta.1
- Error toasts → beta.1
- Chat history persistence → beta.1
- True line-by-line Docker streaming → beta.1
- Docker events subscription (reconciler Option C) → beta.1
- Any other feature not listed above

**Sequence:**

```
alpha.1 → backend (daemon + docker + sqlite + CLI CRUD)
alpha.2 → TUI: look at your fleet (list + detail + describe + help)
alpha.3 → TUI: talk to an agent (chat streaming)
alpha.4 → reliability + ergonomics pass              ← this plan
beta.1  → TUI: watch an agent (log tail + event stream + polish)
v0.3.0  → GA
```

---

## 2. Dependencies

**No new direct dependencies.** All four items work with the existing `go.mod`.

Item 2 (background reconciler) does not need Docker events API — the chosen implementation (Option B, polling) uses `DockerClient.InspectStatus` which already exists. Docker events API (`client.Client.Events`) is available in the existing `github.com/docker/docker v26.1.3` SDK but is not used in alpha.4.

Item 3 (`--one-shot` subcommand) uses `client.RPCClient.ChatSend` which already exists in `internal/client/rpc.go`.

Item 4 (interface extraction) is a refactor within existing packages.

After any inadvertent `go.mod` touch, run `go mod tidy` from `/Users/hatef/workspace/agents/atlas/dclaw`.

---

## 3. File Changes

### New files

```
internal/daemon/reconciler.go   — StatusReconciler goroutine (Item 2)
```

### Modified files

```
internal/cli/agent.go           — wellKnownEnvKeys allowlist + mergeShellEnv helper
                                  agentCreateCmd.RunE: inherit before RPC call
                                  agentUpdateCmd.RunE: same treatment
                                  init(): --help text additions for both commands
                                  (Item 1, ~35 lines net)

internal/daemon/chat.go         — change docker field type from *sandbox.DockerClient
                                  to sandbox.DockerExecClient interface
                                  NewChatHandler signature update (Item 4, ~5 lines)

internal/daemon/chat_test.go    — add mockDockerExec struct + 5 new test cases
                                  (Item 4, ~70 lines net)

internal/daemon/router.go       — pass docker to NewChatHandler as DockerExecClient
                                  (Item 4, ~1 line: no change if DockerClient satisfies
                                  interface implicitly — verify at compile time)

internal/sandbox/docker.go      — add DockerExecClient interface declaration
                                  (Item 4, ~8 lines)

cmd/dclawd/main.go              — construct StatusReconciler, start it, plumb graceful
                                  shutdown (Item 2, ~15 lines)

scripts/smoke-daemon.sh         — add Test 13 (Item 3, ~15 lines)
```

### Files that do NOT change

```
internal/protocol/messages.go   — no wire protocol changes
internal/client/rpc.go          — no changes (ChatSend already exists)
internal/client/rpc_chat_test.go — no changes
internal/tui/                   — no TUI changes
internal/store/                 — no schema changes
agent/Dockerfile                — no container changes
go.mod / go.sum                 — no dependency changes
scripts/smoke-cli.sh            — no changes
Makefile                        — no changes (make test picks up new tests automatically)
docs/wire-protocol-spec.md      — no wire protocol changes
```

---

## 4. Exact File Contents

All paths are absolute. Each subsection is either a full file replacement or a precisely specified diff. Implementations agents MUST use these verbatim.

---

### 4.1 `internal/sandbox/docker.go` (MODIFIED — add DockerExecClient interface)

Append the following block immediately after the `DockerClient` struct declaration (after line 27, before `CreateSpec`). Do not change any existing code.

```go
// DockerExecClient is the subset of DockerClient methods needed by ChatHandler.
// Declaring it in this package (next to the concrete type) keeps sandbox as the
// single source of truth for Docker API shapes. ChatHandler accepts this
// interface so tests can inject a mock without a live Docker daemon.
type DockerExecClient interface {
	InspectStatus(ctx context.Context, id string) (string, error)
	ExecIn(ctx context.Context, id string, argv []string) (string, string, int, error)
}

// Verify DockerClient satisfies DockerExecClient at compile time.
var _ DockerExecClient = (*DockerClient)(nil)
```

The `DockerClient` struct already has both `InspectStatus` and `ExecIn` with those exact signatures. The compile-time assertion confirms correctness.

---

### 4.2 `internal/daemon/chat.go` (MODIFIED — accept DockerExecClient interface)

Full file replacement. Only the `docker` field type and `NewChatHandler` signature change; the Handle body is identical to alpha.3.

```go
// chat.go implements the agent.chat.send handler for dclawd. It resolves the
// named agent's container, runs docker exec, and streams output back as
// agent.chat.chunk notifications on the caller's connection.
//
// Alpha.3 uses the synchronous ExecIn path (single final chunk). Beta.1
// replaces this with true line-by-line streaming via docker attach.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

// ChatHandler processes agent.chat.send requests.
type ChatHandler struct {
	log    *slog.Logger
	repo   *store.Repo
	docker sandbox.DockerExecClient
}

// NewChatHandler returns a ChatHandler. docker accepts any DockerExecClient;
// pass a *sandbox.DockerClient in production, a mock in tests.
func NewChatHandler(log *slog.Logger, repo *store.Repo, docker sandbox.DockerExecClient) *ChatHandler {
	return &ChatHandler{log: log, repo: repo, docker: docker}
}

// Handle processes one agent.chat.send request. It sends the ack via send,
// then pushes agent.chat.chunk notifications until the exec completes or ctx
// is cancelled.
//
// send writes one JSON-RPC envelope on the active connection; it is provided
// by the server's serveConn loop so ChatHandler has no net.Conn import.
func (h *ChatHandler) Handle(
	ctx context.Context,
	params json.RawMessage,
	reqID any,
	send func(*protocol.Envelope) error,
) error {
	var req protocol.AgentChatSendParams
	if err := json.Unmarshal(params, &req); err != nil {
		return send(protocol.ErrorResponse(reqID, protocol.ErrInvalidParams, err.Error(), nil))
	}
	if req.Name == "" {
		return send(protocol.ErrorResponse(reqID, protocol.ErrInvalidParams, "name required", nil))
	}
	if req.Content == "" {
		return send(protocol.ErrorResponse(reqID, protocol.ErrInvalidParams, "content required", nil))
	}

	rec, err := h.repo.GetAgent(ctx, req.Name)
	if err != nil {
		return send(protocol.ErrorResponse(reqID, protocol.ErrAgentNotFound,
			fmt.Sprintf("agent %q not found", req.Name), nil))
	}
	if rec.ContainerID == "" {
		return send(protocol.ErrorResponse(reqID, protocol.ErrAgentNotRunning,
			fmt.Sprintf("agent %q has no container", req.Name), nil))
	}

	msgID := req.MessageID
	if msgID == "" {
		msgID = fmt.Sprintf("srv-%d", time.Now().UnixNano())
	}

	// Send synchronous ack before streaming begins.
	ack := protocol.SuccessResponse(reqID, protocol.AgentChatSendResult{
		MessageID:  msgID,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err := send(ack); err != nil {
		return err
	}

	// Readiness check: `docker exec` against a stopped container silently
	// fails, so surface a clean chat error instead of a confusing empty chunk.
	// NOTE: There is a TOCTOU window between this check and ContainerExecCreate.
	// The container can die in the microsecond gap. This is documented and
	// accepted for alpha.4; beta.1 adds retry logic.
	if h.docker == nil {
		// nil docker client — return a clear error rather than a nil dereference.
		notReady := protocol.AgentChatChunkNotification{
			Name:      req.Name,
			Role:      "error",
			Text:      "docker client not available",
			Sequence:  0,
			Final:     true,
			MessageID: msgID,
		}
		return send(protocol.Notification("agent.chat.chunk", notReady))
	}

	status, statErr := h.docker.InspectStatus(ctx, rec.ContainerID)
	if statErr != nil || status != "running" {
		shown := status
		if statErr != nil {
			shown = "unknown"
		}
		notRunning := protocol.AgentChatChunkNotification{
			Name:      req.Name,
			Role:      "error",
			Text:      fmt.Sprintf("agent not running (container state: %s) — did you run 'dclaw agent start %s'?", shown, req.Name),
			Sequence:  0,
			Final:     true,
			MessageID: msgID,
		}
		return send(protocol.Notification("agent.chat.chunk", notRunning))
	}

	h.log.Debug("chat exec start", "agent", req.Name, "msg_id", msgID)

	// Alpha.3/alpha.4: synchronous exec — one final chunk.
	// Beta.1: replace with ExecInStream (true line-by-line via docker attach).
	argv := []string{"pi", "-p", "--no-session", req.Content}
	stdout, stderr, exitCode, execErr := h.docker.ExecIn(ctx, rec.ContainerID, argv)

	if execErr != nil {
		errChunk := protocol.AgentChatChunkNotification{
			Name:      req.Name,
			Role:      "error",
			Text:      execErr.Error(),
			Sequence:  0,
			Final:     true,
			MessageID: msgID,
		}
		return send(protocol.Notification("agent.chat.chunk", errChunk))
	}

	if exitCode != 0 {
		errText := stderr
		if errText == "" {
			errText = stdout
		}
		failChunk := protocol.AgentChatChunkNotification{
			Name:      req.Name,
			Role:      "error",
			Text:      fmt.Sprintf("pi exited with code %d: %s", exitCode, errText),
			Sequence:  0,
			Final:     true,
			MessageID: msgID,
		}
		return send(protocol.Notification("agent.chat.chunk", failChunk))
	}

	text := stdout
	if text == "" {
		text = stderr
	}
	finalChunk := protocol.AgentChatChunkNotification{
		Name:      req.Name,
		Role:      "agent",
		Text:      text,
		Sequence:  0,
		Final:     true,
		MessageID: msgID,
	}
	return send(protocol.Notification("agent.chat.chunk", finalChunk))
}
```

---

### 4.3 `internal/daemon/chat_test.go` (MODIFIED — full replacement with mock + 5 new tests)

Full file replacement. Keeps the two alpha.3 tests (`TestChatHandlerAgentNotFound`, `TestChatHandlerMissingContent`) and adds five new cases.

```go
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
```

---

### 4.4 `internal/daemon/router.go` (MODIFIED — no code change needed, but verify)

The `router.go` currently constructs `ChatHandler` as:

```go
r.chatHandler = NewChatHandler(log, repo, docker)
```

where `docker` is `*sandbox.DockerClient`. After Item 4, `NewChatHandler` accepts `sandbox.DockerExecClient` (interface). Since `*sandbox.DockerClient` satisfies `DockerExecClient` (verified by the compile-time assertion in `sandbox/docker.go`), this line compiles without change. No diff needed for `router.go`.

Run `go build ./internal/daemon/...` to confirm. If it fails with a type mismatch, `sandbox.DockerClient` is missing a method signature — check `InspectStatus` and `ExecIn` have the exact signatures specified in `DockerExecClient`.

---

### 4.5 `internal/daemon/reconciler.go` (NEW — full file)

```go
// reconciler.go runs a background goroutine that polls all agent containers
// every 2 seconds and updates the DB status if Docker reports a different
// state. This prevents the DB from permanently showing "running" for
// containers that have exited, been OOM-killed, or died.
//
// Design: Option B (polling goroutine) chosen for alpha.4. Option C (Docker
// events stream subscription) is the right long-term answer and is explicitly
// deferred to beta.1 — the polling approach is simpler to test and reason
// about, with acceptable 2s eventual-consistency lag.
//
// Graceful shutdown: the goroutine exits when ctx is cancelled. The daemon's
// main loop cancels the context on SIGTERM/SIGINT. No drain needed: the
// goroutine does not own any resources it needs to release.
package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

const reconcilerInterval = 2 * time.Second

// StatusReconciler polls agent containers and keeps the DB status column
// accurate.
type StatusReconciler struct {
	log    *slog.Logger
	repo   *store.Repo
	docker *sandbox.DockerClient
}

// NewStatusReconciler constructs a StatusReconciler.
func NewStatusReconciler(log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *StatusReconciler {
	return &StatusReconciler{log: log, repo: repo, docker: docker}
}

// Run starts the reconciliation loop. It blocks until ctx is cancelled.
// Call it in a goroutine: go reconciler.Run(ctx).
func (r *StatusReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(reconcilerInterval)
	defer ticker.Stop()

	r.log.Debug("status reconciler started", "interval", reconcilerInterval)
	for {
		select {
		case <-ctx.Done():
			r.log.Debug("status reconciler stopped")
			return
		case <-ticker.C:
			r.reconcileOnce(ctx)
		}
	}
}

// reconcileOnce inspects all known agent containers and updates the DB for
// any that have a status mismatch. It is called on each tick and also at
// startup (see the daemon main) for initial synchronisation.
func (r *StatusReconciler) reconcileOnce(ctx context.Context) {
	agents, err := r.repo.ListAgents(ctx)
	if err != nil {
		r.log.Warn("reconciler: list agents failed", "err", err)
		return
	}

	for _, rec := range agents {
		if rec.ContainerID == "" {
			continue
		}
		// Containers in "created" state have never been started; no point polling.
		if rec.Status == "created" {
			continue
		}

		live, err := r.docker.InspectStatus(ctx, rec.ContainerID)
		if err != nil {
			// Container has been removed externally (e.g., docker rm by hand).
			// Mark as "dead" so the UI shows something meaningful.
			if rec.Status != "dead" {
				r.updateStatus(ctx, rec, "dead")
			}
			continue
		}

		if live != rec.Status {
			r.updateStatus(ctx, rec, live)
		}
	}
}

// updateStatus writes the new status to the DB and logs the transition.
func (r *StatusReconciler) updateStatus(ctx context.Context, rec store.AgentRecord, newStatus string) {
	old := rec.Status
	rec.Status = newStatus
	rec.UpdatedAt = time.Now().Unix()
	if err := r.repo.UpdateAgent(ctx, rec); err != nil {
		r.log.Warn("reconciler: DB update failed",
			"agent", rec.Name, "old", old, "new", newStatus, "err", err)
		return
	}
	r.log.Info("agent status reconciled",
		"agent", rec.Name, "container_id", rec.ContainerID[:min(12, len(rec.ContainerID))],
		"old_status", old, "new_status", newStatus)
}

// min returns the smaller of a and b.
// Inline helper because math.Min is float64 and min builtin requires Go 1.21.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

Note: the `min` builtin was added in Go 1.21. The module requires Go 1.25.0, so the builtin is available. The inline helper is redundant but safe — replace with the builtin `min(a, b)` directly if preferred. The above version is maximally explicit.

Actually, with `go 1.25.0`, use the builtin directly. Replace the helper and call sites:

```go
// updateStatus — corrected for Go 1.25 builtin min:
func (r *StatusReconciler) updateStatus(ctx context.Context, rec store.AgentRecord, newStatus string) {
	old := rec.Status
	rec.Status = newStatus
	rec.UpdatedAt = time.Now().Unix()
	if err := r.repo.UpdateAgent(ctx, rec); err != nil {
		r.log.Warn("reconciler: DB update failed",
			"agent", rec.Name, "old", old, "new", newStatus, "err", err)
		return
	}
	r.log.Info("agent status reconciled",
		"agent", rec.Name,
		"container_id", rec.ContainerID[:min(12, len(rec.ContainerID))],
		"old_status", old, "new_status", newStatus)
}
```

Remove the inline `min` function from the file entirely. Use Go 1.21+ builtin `min`.

---

### 4.6 `cmd/dclawd/main.go` (MODIFIED — start reconciler goroutine)

The current `main.go` has a `srv.Run(ctx)` call. Add the reconciler between the Docker client init and `srv.Run`. Full replacement of the core section:

Full file replacement:

```go
// dclawd is the dclaw daemon: the host-side control plane. It listens on a
// Unix domain socket, speaks JSON-RPC 2.0 to the dclaw CLI (and eventually
// to channel plugins and main-agent containers), and drives Docker via the
// official API client.
//
// Flags:
//
//	--socket <path>   Override the socket path (default: $XDG_RUNTIME_DIR/dclaw.sock).
//	--state-dir <d>   Override the state directory (default: ~/.dclaw).
//	--log-level lvl   debug|info|warn|error (default: info).
//	--foreground      Stay in the foreground; don't detach. Default when run from dclaw daemon start.
//	--migrate-only    Run pending SQLite migrations and exit 0. Used by `make migrate`.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
	"github.com/itsmehatef/dclaw/internal/version"
)

func main() {
	var (
		socketPath  = flag.String("socket", "", "Unix socket path (default: auto)")
		stateDir    = flag.String("state-dir", "", "state directory (default: ~/.dclaw)")
		logLevel    = flag.String("log-level", "info", "log level: debug|info|warn|error")
		foreground  = flag.Bool("foreground", true, "run in foreground (default: true)")
		showVer     = flag.Bool("version", false, "print version and exit")
		migrateOnly = flag.Bool("migrate-only", false, "run pending migrations and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("dclawd version %s (commit %s, built %s, %s)\n",
			version.Version, version.Commit, version.BuildDate, version.GoVersion())
		return
	}

	cfg, err := daemon.LoadConfig(*socketPath, *stateDir, *logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dclawd: config error: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel, cfg.LogPath)
	logger.Info("dclawd starting",
		"version", version.Version,
		"socket", cfg.SocketPath,
		"state_dir", cfg.StateDir,
	)

	// Initialize SQLite store + run embedded migrations.
	repo, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("store open failed", "err", err)
		os.Exit(65) // EX_DATAERR
	}
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		logger.Error("migration failed", "err", err)
		os.Exit(65)
	}

	// --migrate-only: run migrations and exit. No daemon, no Docker, no socket.
	if *migrateOnly {
		logger.Info("migrate-only: migrations complete; exiting")
		return
	}

	// Initialize Docker client.
	docker, err := sandbox.NewDockerClient()
	if err != nil {
		logger.Error("docker connect failed", "err", err)
		os.Exit(77) // EX_NOPERM
	}
	defer docker.Close()

	// Build context that cancels on SIGTERM/SIGINT.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Write pidfile for `dclaw daemon stop`.
	if err := cfg.WritePIDFile(os.Getpid()); err != nil {
		logger.Error("pidfile write failed", "err", err)
		os.Exit(1)
	}
	defer cfg.RemovePIDFile()

	// Start background status reconciler.
	// Run initial sync immediately (before the first 2s tick) so the DB is
	// accurate as soon as the daemon starts — e.g. after a daemon restart where
	// containers may have exited while dclawd was down.
	reconciler := daemon.NewStatusReconciler(logger, repo, docker)
	reconciler.ReconcileOnce(ctx) // synchronous initial pass
	go reconciler.Run(ctx)

	// Wire and run the server.
	srv := daemon.NewServer(cfg, logger, repo, docker)

	if _ = foreground; true {
		if err := srv.Run(ctx); err != nil {
			logger.Error("server stopped with error", "err", err)
			os.Exit(70) // EX_SOFTWARE
		}
	}

	logger.Info("dclawd stopped cleanly")
}

// newLogger constructs a slog.Logger writing to cfg.LogPath (falls back to
// stderr if the file can't be opened). Level is parsed from cfg.LogLevel.
func newLogger(levelStr, path string) *slog.Logger {
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var w *os.File = os.Stderr
	if path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			w = f
		}
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
```

Note: `reconciler.ReconcileOnce(ctx)` requires exporting `reconcileOnce` as `ReconcileOnce` in `reconciler.go`. Update `reconciler.go` accordingly: rename `reconcileOnce` to `ReconcileOnce` and update the internal call in `Run`.

Updated `reconciler.go` `Run` method body:

```go
func (r *StatusReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(reconcilerInterval)
	defer ticker.Stop()

	r.log.Debug("status reconciler started", "interval", reconcilerInterval)
	for {
		select {
		case <-ctx.Done():
			r.log.Debug("status reconciler stopped")
			return
		case <-ticker.C:
			r.ReconcileOnce(ctx)
		}
	}
}

// ReconcileOnce is exported so main.go can call it synchronously at startup
// for an immediate initial DB sync before the first tick fires.
func (r *StatusReconciler) ReconcileOnce(ctx context.Context) {
	// ... same body as reconcileOnce above
```

The final `reconciler.go` with `ReconcileOnce` exported:

```go
package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

const reconcilerInterval = 2 * time.Second

// StatusReconciler polls agent containers every reconcilerInterval and updates
// the DB status column on any state change. This ensures that containers that
// exit, are OOM-killed, or are removed externally do not remain "running"
// in the DB indefinitely.
//
// Design: polling goroutine (Option B). Docker events stream subscription
// (Option C) is the correct long-term approach and is deferred to beta.1.
// The polling approach provides sufficient reliability for alpha.4 with
// simpler code and no long-lived socket management.
//
// Graceful shutdown: the goroutine exits when its context is cancelled.
// The daemon cancels the context on SIGTERM/SIGINT. No drain needed.
type StatusReconciler struct {
	log    *slog.Logger
	repo   *store.Repo
	docker *sandbox.DockerClient
}

// NewStatusReconciler constructs a StatusReconciler.
func NewStatusReconciler(log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *StatusReconciler {
	return &StatusReconciler{log: log, repo: repo, docker: docker}
}

// Run starts the reconciliation loop. It blocks until ctx is cancelled.
// The caller should launch it as a goroutine: go reconciler.Run(ctx).
func (r *StatusReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(reconcilerInterval)
	defer ticker.Stop()

	r.log.Debug("status reconciler started", "interval", reconcilerInterval)
	for {
		select {
		case <-ctx.Done():
			r.log.Debug("status reconciler stopped")
			return
		case <-ticker.C:
			r.ReconcileOnce(ctx)
		}
	}
}

// ReconcileOnce inspects all known agent containers and updates the DB for
// any that have a status mismatch. It is exported so main.go can call it
// synchronously at startup for an immediate initial DB sync.
func (r *StatusReconciler) ReconcileOnce(ctx context.Context) {
	agents, err := r.repo.ListAgents(ctx)
	if err != nil {
		r.log.Warn("reconciler: list agents failed", "err", err)
		return
	}

	for _, rec := range agents {
		if rec.ContainerID == "" {
			continue
		}
		// "created" containers have never been started; skip to avoid noisy
		// "dead" transitions when Docker has removed a created-but-never-started
		// container (which can happen if the daemon restarts between create and
		// start).
		if rec.Status == "created" {
			continue
		}

		live, err := r.docker.InspectStatus(ctx, rec.ContainerID)
		if err != nil {
			// Container removed externally. Mark as "dead".
			if rec.Status != "dead" {
				r.writeStatus(ctx, rec, "dead")
			}
			continue
		}

		if live != rec.Status {
			r.writeStatus(ctx, rec, live)
		}
	}
}

// writeStatus updates the DB and logs the transition.
func (r *StatusReconciler) writeStatus(ctx context.Context, rec store.AgentRecord, newStatus string) {
	old := rec.Status
	rec.Status = newStatus
	rec.UpdatedAt = time.Now().Unix()
	if err := r.repo.UpdateAgent(ctx, rec); err != nil {
		r.log.Warn("reconciler: DB update failed",
			"agent", rec.Name, "old_status", old, "new_status", newStatus, "err", err)
		return
	}
	// Trim container ID for log readability.
	cid := rec.ContainerID
	if len(cid) > 12 {
		cid = cid[:12]
	}
	r.log.Info("agent status reconciled",
		"agent", rec.Name,
		"container_id", cid,
		"old_status", old,
		"new_status", newStatus)
}
```

---

### 4.7 `internal/cli/agent.go` (MODIFIED — `--env` shell inheritance)

**Change 1: add package-level allowlist var** (after the package declaration, before the first `var` block):

```go
// wellKnownEnvKeys is the allowlist of environment variable names that are
// automatically inherited from the shell when the user does not pass them
// explicitly via --env. This list is intentionally small and explicit —
// we do NOT inherit arbitrary shell environment. To extend the list, add
// entries here only for credentials that every dclaw user is expected to
// supply to their agents.
//
// Behaviour: for each key in this list, if the key is NOT already present in
// the --env slice AND os.Getenv(key) != "", the key=value pair is prepended
// to the slice as a lowest-priority default. Explicit --env always wins.
var wellKnownEnvKeys = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_OAUTH_TOKEN",
}
```

**Change 2: add `mergeShellEnv` helper** (append after `kvSliceToMap` at the bottom of the file):

```go
// mergeShellEnv takes an --env slice and returns a new slice that includes
// any well-known keys missing from the original. Keys already present in
// explicit are left unchanged (explicit always wins). Keys present in
// wellKnownEnvKeys but not in explicit are appended from os.Getenv if non-empty.
//
// The merge is done by name-presence check only (O(n*m), n=len(explicit),
// m=len(wellKnownEnvKeys)). Both lists are tiny (≤10 items each), so this
// is fine.
func mergeShellEnv(explicit []string) []string {
	// Build a set of key names already present in the explicit slice.
	present := make(map[string]bool, len(explicit))
	for _, kv := range explicit {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				present[kv[:i]] = true
				break
			}
		}
		// If there's no '=', treat the whole thing as a key with empty value.
		if !present[kv] {
			present[kv] = true
		}
	}

	out := make([]string, len(explicit))
	copy(out, explicit)

	for _, key := range wellKnownEnvKeys {
		if present[key] {
			continue // user-supplied value wins; do not override
		}
		if val := os.Getenv(key); val != "" {
			out = append(out, key+"="+val)
		}
	}
	return out
}
```

**Change 3: call `mergeShellEnv` in `agentCreateCmd.RunE`** — update the `RunE` body. Replace the existing `agentCreateEnv` usage:

Before (line 43):
```go
Env:       kvSliceToMap(agentCreateEnv),
```

After:
```go
Env:       kvSliceToMap(mergeShellEnv(agentCreateEnv)),
```

**Change 4: call `mergeShellEnv` in `agentUpdateCmd.RunE`** — update the `RunE` body:

Before (line 156):
```go
Env:    kvSliceToMap(agentUpdateEnv),
```

After:
```go
Env:    kvSliceToMap(mergeShellEnv(agentUpdateEnv)),
```

**Change 5: update `--help` text for `--env` flag in `init()`** — replace both `--env` flag registrations:

For `agentCreateCmd` (current line 308):
```go
agentCreateCmd.Flags().StringArrayVar(&agentCreateEnv, "env", nil,
	"set env var KEY=VAL (repeatable); ANTHROPIC_API_KEY and ANTHROPIC_OAUTH_TOKEN\n"+
		"\t\t\tare inherited from the shell if not specified")
```

For `agentUpdateCmd` (current line 313):
```go
agentUpdateCmd.Flags().StringArrayVar(&agentUpdateEnv, "env", nil,
	"set env var KEY=VAL (repeatable); ANTHROPIC_API_KEY and ANTHROPIC_OAUTH_TOKEN\n"+
		"\t\t\tare inherited from the shell if not specified")
```

The full modified `internal/cli/agent.go` file incorporates all five changes. The net diff is approximately 35 lines.

---

### 4.8 `internal/cli/agent_chat.go` (NEW — `dclaw agent chat --one-shot` subcommand)

This is a new file in the `cli` package. It wires into the existing `agentCmd` in `init()` by registering `agentChatCmd`. It must also be added to `agentCmd.AddCommand(...)` in `agent.go`'s `init()`.

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

// agentChatOneShotPrompt and agentChatTimeout hold the flag values for the
// agent chat subcommand.
var (
	agentChatOneShotPrompt string
	agentChatTimeout       time.Duration
)

// agentChatCmd implements `dclaw agent chat <name> --one-shot "<prompt>"`.
//
// It sends a single chat message to the named agent, collects all
// agent.chat.chunk notifications until final=true, prints each chunk's text
// to stdout, and exits 0 on success. If the timeout elapses before the stream
// completes, it exits 1. If the agent returns an error chunk (role="error"),
// it prints the error text to stderr and exits 2.
//
// The --one-shot flag is required. Interactive multi-turn chat is a TUI
// feature accessed via `dclaw` or `dclaw agent attach`. The --one-shot flag
// makes the intent explicit and keeps the command scriptable.
//
// Exit codes:
//   - 0: stream completed successfully (final=true, role="agent")
//   - 1: timeout, dial error, daemon RPC error
//   - 2: agent returned an error chunk (role="error")
var agentChatCmd = &cobra.Command{
	Use:   "chat <name>",
	Short: "Send a one-shot message to an agent and print the response",
	Long: `Send a single message to the named agent and print the response to stdout.

The agent.chat.send RPC is used; the daemon streams agent.chat.chunk
notifications which are printed as they arrive (each chunk on its own line).
The command exits after the final chunk.

Example:
  dclaw agent chat alice --one-shot "list the files in /workspace"

Exit codes:
  0 = success
  1 = RPC or network error
  2 = agent returned an error response (container not running, pi failed, etc.)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentChatOneShotPrompt == "" {
			return fmt.Errorf("--one-shot is required")
		}

		agentName := args[0]
		timeout := agentChatTimeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()

		return withClient(ctx, func(c *client.RPCClient) error {
			chunks, err := c.ChatSend(ctx, agentName, agentChatOneShotPrompt, "")
			if err != nil {
				return HandleRPCError(cmd, err)
			}

			exitCode := 0
			for event := range chunks {
				if event.Err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "error: stream error: %v\n", event.Err)
					os.Exit(1)
				}
				if event.Role == "error" {
					fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", event.Text)
					exitCode = 2
				} else {
					fmt.Fprint(cmd.OutOrStdout(), event.Text)
				}
				if event.Final {
					break
				}
			}

			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return nil
		})
	},
}

func init() {
	agentChatCmd.Flags().StringVar(&agentChatOneShotPrompt, "one-shot", "",
		"send this prompt to the agent and exit after the response (required)")
	agentChatCmd.Flags().DurationVar(&agentChatTimeout, "timeout", 60*time.Second,
		"maximum time to wait for the agent response")
}
```

Also add `agentChatCmd` to the `agentCmd.AddCommand(...)` call in `agent.go`'s `init()`:

```go
agentCmd.AddCommand(
    agentCreateCmd,
    agentListCmd,
    agentGetCmd,
    agentDescribeCmd,
    agentUpdateCmd,
    agentDeleteCmd,
    agentStartCmd,
    agentStopCmd,
    agentRestartCmd,
    agentLogsCmd,
    agentExecCmd,
    agentAttachCmd, // alpha.2
    agentChatCmd,   // alpha.4 --one-shot
)
```

---

### 4.9 `scripts/smoke-daemon.sh` (MODIFIED — add Test 13)

Append before the final `echo "All daemon smoke tests passed."` line:

```bash
echo "--- Test 13: agent chat real round-trip (requires ANTHROPIC_API_KEY) ---"
if [ -z "${ANTHROPIC_API_KEY:-}" ] && [ -z "${ANTHROPIC_OAUTH_TOKEN:-}" ]; then
  echo "SKIP: Test 13 requires ANTHROPIC_API_KEY or ANTHROPIC_OAUTH_TOKEN — skipping (set the var to enable)"
else
  STATE_DIR_T13=$(mktemp -d -t dclaw-smoke-t13-XXXX)
  SOCKET_T13="$STATE_DIR_T13/dclaw.sock"
  "$DCLAW_BIN" --daemon-socket "$SOCKET_T13" daemon start || fail "t13-start"
  # Create agent with the API key; --env inheritance handles the key if not set.
  "$DCLAW_BIN" --daemon-socket "$SOCKET_T13" agent create chatbot13 \
    --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T13" || fail "t13-create"
  "$DCLAW_BIN" --daemon-socket "$SOCKET_T13" agent start chatbot13 || fail "t13-agent-start"
  OUT=$("$DCLAW_BIN" --daemon-socket "$SOCKET_T13" agent chat chatbot13 \
    --one-shot "reply with only the word: SMOKE_CONFIRMED" \
    --timeout 90s 2>&1) || fail "t13-chat-cmd failed (exit $?)"
  echo "$OUT" | grep -qi "SMOKE_CONFIRMED\|smoke_confirmed\|smoke confirmed" \
    || fail "Test 13 expected SMOKE_CONFIRMED in chat output, got: $OUT"
  "$DCLAW_BIN" --daemon-socket "$SOCKET_T13" daemon stop >/dev/null 2>&1 || true
  rm -rf "$STATE_DIR_T13"
  pass "agent chat real round-trip"
fi
```

---

## 5. Modified Files Diff Summary

| File | Change type | Net lines | Notes |
|---|---|---|---|
| `internal/sandbox/docker.go` | add interface + assertion | +10 | DockerExecClient interface; no behavior change |
| `internal/daemon/chat.go` | field type change | ~0 net | `*sandbox.DockerClient` → `sandbox.DockerExecClient`; nil-guard added |
| `internal/daemon/chat_test.go` | full replacement | +100 | mockDockerExec + 5 new test cases |
| `internal/daemon/reconciler.go` | new file | +80 | StatusReconciler goroutine |
| `cmd/dclawd/main.go` | add reconciler startup | +10 | 3 lines in main(); reconciler import |
| `internal/cli/agent.go` | add allowlist + helper + two call sites + help text | +35 | wellKnownEnvKeys + mergeShellEnv + 2 RunE changes + 2 flag help changes |
| `internal/cli/agent_chat.go` | new file | +80 | agentChatCmd with --one-shot |
| `scripts/smoke-daemon.sh` | add Test 13 | +18 | credential-gated chat round-trip |

**Total estimated diff: ~330 lines added, ~5 lines modified/removed.**

---

## 6. `--env` Shell Inheritance Details

### Allowlist rationale

The allowlist approach (Option C from prior investigation) is chosen over two alternatives:

- **Option A (explicit always)**: current behavior. Every `dclaw agent create` requires `--env ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY`. The user hit this during alpha.3 smoke testing and it's genuinely ergonomically rough when iterating.
- **Option B (full shell env inheritance)**: inherit everything from `os.Environ()`. Dangerous — would silently pass secrets the user doesn't intend to send (e.g., `AWS_SECRET_ACCESS_KEY`, `GITHUB_TOKEN`) and could cause hard-to-debug behavior when the agent's environment differs unexpectedly from the host's.
- **Option C (allowlist)**: merge only a small explicit set of well-known keys. Safe, predictable, documented.

The allowlist is a `var` (not `const`) so implementations can extend it in one place without touching the merge logic. The initial list is minimal by design: `ANTHROPIC_API_KEY` (classic API key) and `ANTHROPIC_OAUTH_TOKEN` (OAuth token for pi-mono). Both are in the handoff doc §9 note about the user's key format.

### Merge priority

```
priority (highest → lowest):
  1. Keys from --env flag (explicit user input)
  2. Keys from wellKnownEnvKeys via os.Getenv (shell inheritance)
```

The merge function copies the explicit slice first, then appends missing well-known keys. Because the CLI passes the merged slice to `kvSliceToMap`, and `kvSliceToMap` processes items in order with last-writer-wins on duplicates... wait, actually `kvSliceToMap` stops at the first `=` and uses `out[key[:i]] = kv[i+1:]`, so if the same key appears twice, the LAST value wins. That means appended inherited keys could overwrite explicit keys if a user passed `--env ANTHROPIC_API_KEY=X` and the shell also has `ANTHROPIC_API_KEY=Y` — the inherited value would win.

**Fix:** the `mergeShellEnv` function checks `present` before appending. Keys already in the explicit slice are skipped entirely. So inherited keys are only appended when the key is absent. `kvSliceToMap` then only sees each key once. This is correct.

The `present` check uses a string-split loop identical to `kvSliceToMap`'s approach: find the first `=`. If a user passes `--env ANTHROPIC_API_KEY=` (explicit empty value), that key is in `present` and the inherited non-empty value is NOT added. Explicit empty beats inherited non-empty — that is the correct behavior (user explicitly cleared the var).

### `--help` text additions

Both `create` and `update` flags mention the inheritance behavior inline. The text is kept short (`--help` text has limited real estate). The full flag description is visible in `dclaw agent create --help`.

### Test coverage

Item 1 does not introduce a new test file. Coverage via:

- **Existing `internal/cli/cli_test.go`**: the table-driven test already exercises `agentCreateCmd`. A new row should be added verifying that `mergeShellEnv(nil)` with `ANTHROPIC_API_KEY` set in the environment produces the expected key in the result. This is a pure unit test on the helper function — no daemon needed.
- **`go test ./internal/cli/...`** confirms the helper passes.

Add to `cli_test.go` or a new `agent_env_test.go` file (either works; use existing file for simplicity):

```go
func TestMergeShellEnvInheritsWellKnown(t *testing.T) {
    // Set up a well-known env var.
    t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
    result := mergeShellEnv(nil)
    found := false
    for _, kv := range result {
        if kv == "ANTHROPIC_API_KEY=sk-ant-test" {
            found = true
            break
        }
    }
    if !found {
        t.Fatalf("expected ANTHROPIC_API_KEY to be inherited, got %v", result)
    }
}

func TestMergeShellEnvExplicitWins(t *testing.T) {
    t.Setenv("ANTHROPIC_API_KEY", "should-not-appear")
    explicit := []string{"ANTHROPIC_API_KEY=explicit-value"}
    result := mergeShellEnv(explicit)
    count := 0
    for _, kv := range result {
        if len(kv) >= 17 && kv[:17] == "ANTHROPIC_API_KEY" {
            count++
        }
    }
    if count != 1 {
        t.Fatalf("expected exactly 1 ANTHROPIC_API_KEY entry, got %d in %v", count, result)
    }
    if result[0] != "ANTHROPIC_API_KEY=explicit-value" {
        t.Fatalf("expected explicit value to win, got %v", result)
    }
}

func TestMergeShellEnvNoInheritWhenUnset(t *testing.T) {
    t.Setenv("ANTHROPIC_API_KEY", "")
    result := mergeShellEnv(nil)
    for _, kv := range result {
        if len(kv) >= 17 && kv[:17] == "ANTHROPIC_API_KEY" {
            t.Fatalf("expected no ANTHROPIC_API_KEY when unset, got %v in %v", kv, result)
        }
    }
}
```

Note: `t.Setenv` (Go 1.17+) automatically restores the original value after the test. These tests are safe to run in any environment.

---

## 7. DB Status Reconciliation Design

### Chosen approach: Option B — Background polling goroutine

**Rationale for choosing B over C (Docker events) for alpha.4:**

Option C (Docker events stream) is architecturally superior for production: it reacts to container state changes in real time (~1ms latency vs ~2s), has zero idle polling load, and is idiomatic for a long-running service. The Docker SDK in `go.mod` (`github.com/docker/docker v26.1.3`) includes `client.Events` which returns `(<-chan events.Message, <-chan error)`. However, Option C introduces:

1. **Restart-gap problem**: events emitted while `dclawd` was down are missed. Mitigation requires a poll-on-startup pass. That's the `ReconcileOnce` call in main.go — which Option B already has. So both options share that complexity.
2. **Stream reconnection**: the Docker events stream can drop if the Docker daemon restarts or the socket hiccups. Proper handling requires a reconnect loop with backoff.
3. **Filter management**: must filter for only `dclaw.managed=true` containers to avoid event noise from unrelated containers.
4. **~40 more lines** than Option B for non-trivial production reliability.

For a reliability pass that ships in 1-2 days, Option B's ~80 lines with its constant and predictable behavior are the right call. Option C is explicitly deferred to beta.1 with a `// TODO(beta.1): replace with Docker events subscription` comment in `reconciler.go`.

### Goroutine lifecycle

```
main.go:
  ctx, cancel := signal.NotifyContext(...)
  defer cancel()
  reconciler.ReconcileOnce(ctx)    // synchronous initial pass
  go reconciler.Run(ctx)           // background loop
  srv.Run(ctx)                     // blocks until SIGTERM
  // ctx cancelled by signal → reconciler goroutine exits on next ticker.C
```

The `StatusReconciler` goroutine is fire-and-forget from main's perspective. It does not own resources that need draining (no pending writes, no open connections beyond the Docker API call in flight). When `ctx.Done()` fires, the goroutine exits after completing any in-progress `ReconcileOnce` call. Maximum exit latency: `reconcilerInterval` (2 seconds) + time for one Docker `InspectStatus` call per agent.

### Failure modes

| Failure | Behavior |
|---|---|
| Docker daemon unreachable mid-run | `InspectStatus` returns error; reconciler logs a warning and continues to next agent. Status NOT updated. On next tick, tries again. |
| DB write fails | `writeStatus` logs a warning and returns. Status mismatch persists until next successful reconcile. |
| `ListAgents` fails | Reconciler logs warning and skips the entire tick. |
| Daemon restart (container exited while dclawd was down) | `ReconcileOnce` in main.go runs before `srv.Run`. DB updated before first client connects. |
| Agent container removed by `docker rm` | `InspectStatus` returns error (no such container). Status set to "dead". |
| Reconciler panics | `go reconciler.Run(ctx)` has no recover. A panic crashes the daemon. If this proves problematic, wrap with a recover-and-restart loop in beta.1. For alpha.4, a panic here represents a code bug that should surface loudly. |

### Effect on `AgentList` and `AgentGet`

`AgentList` in `lifecycle.go` already calls `InspectStatus` per-agent (lines 100-103). This means `agent list` and the TUI list view always show live Docker status regardless of the reconciler. The reconciler's value is keeping the DB column accurate, which matters for:

1. The stored audit trail (events table)
2. `AgentUpdate`'s precondition check (line 148: `rec.Status != "created" && ...`)
3. Future beta.1 features that query DB status directly
4. The `ChatHandler`'s readiness check, which uses `InspectStatus` live (not DB) — already correct; reconciler doesn't change this path

There is intentional redundancy: the reconciler updates the DB; `AgentList` and `AgentGet` independently re-read Docker live status. This is fine — two independent sources of truth agreeing is better than relying on one.

### Test coverage for reconciler

Unit test in a new file `internal/daemon/reconciler_test.go` (Agent B responsibility):

```go
// TestReconcilerUpdatesExitedAgent verifies that a container whose Docker
// status changes to "exited" gets its DB record updated by ReconcileOnce.
func TestReconcilerUpdatesExitedAgent(t *testing.T)
```

The test uses `newTestRepo` from `chat_test.go` (exported helper), inserts an agent with status "running", creates a mock `DockerClient`... but `StatusReconciler` takes `*sandbox.DockerClient`, not an interface. The reconciler does NOT need to be mockable for alpha.4 — it is an internal goroutine tested via integration (smoke script confirms it in the end-to-end flow).

If a unit test for the reconciler is needed, Agent B may optionally refactor it to accept a `StatusInspector` interface (just `InspectStatus`), but this is not required for alpha.4. Integration coverage via the smoke script (agents running, daemon restart, status check) is sufficient. Mark as optional in the build sequence.

---

## 8. Test 13 Mechanism

### Chosen approach: Option B — `dclaw agent chat <name> --one-shot "<prompt>"`

**Rationale:**

Option A (raw bash + nc + jq on the Unix socket) would be fragile, wire-format-tied, and painful to maintain. One JSON-RPC frame has a `json.Encoder` LF-terminated format — easily broken by a newline in the content.

Option B (`--one-shot` subcommand) is the clean choice:

1. It exercises the full client-side code path: `withClient` → `RPCClient.ChatSend` → chunk drain loop — exactly what the TUI does, via the same `RPCClient` code.
2. It's scriptable: `dclaw agent chat alice --one-shot "list files" | grep README` is useful in real-world automation.
3. The smoke test invocation is readable: `dclaw agent chat chatbot13 --one-shot "reply with SMOKE_CONFIRMED"`.
4. Exit codes are meaningful (0/1/2), enabling the smoke script to fail properly.

Option C (Go integration test) would be cleaner from a testing theory perspective but is heavier (requires Docker image running in CI) and doesn't help Test 12's limitation (bypasses pi). The `--one-shot` subcommand actually calls `pi` via `agent.chat.send`.

### Subcommand specification

**Usage:** `dclaw agent chat <name> --one-shot "<prompt>" [--timeout <duration>]`

**Args:** exactly one positional arg (agent name).

**Flags:**
- `--one-shot <string>` — required. The prompt to send.
- `--timeout <duration>` — optional, default `60s`. How long to wait before giving up.

**Output format:**

Chunks are printed to stdout in order as they arrive. Each chunk's `text` field is printed via `fmt.Fprint(cmd.OutOrStdout(), event.Text)` — no newline added (the text already includes its own newlines from pi's output). The final result is the concatenation of all chunks, which is the complete pi response.

On error role:
- Error text printed to stderr
- Exit code 2

**Exit codes:**

| Code | Meaning |
|---|---|
| 0 | Stream completed, all chunks had `role="agent"` |
| 1 | Network/RPC error (dial failed, ack error, stream decode error, timeout) |
| 2 | Agent returned `role="error"` chunk (container not running, pi exited non-zero, etc.) |

**Timeout handling:**

`context.WithTimeout(cmd.Context(), agentChatTimeout)` wraps the entire operation. If the context expires before `final=true` arrives, `ChatSend`'s drain goroutine returns `ChatChunkEvent{Err: context.DeadlineExceeded}` on the channel. The drain loop in `agentChatCmd.RunE` detects `event.Err != nil`, prints to stderr, and calls `os.Exit(1)`.

**Credential gating in Test 13:**

Test 13 checks `[ -z "${ANTHROPIC_API_KEY:-}" ] && [ -z "${ANTHROPIC_OAUTH_TOKEN:-}" ]` before proceeding. If both are empty, the test is SKIPPED (not FAILED). This matches standard CI conventions: the test requires external credentials and cannot run without them. In CI environments that have the key configured (e.g., in GitHub Actions secrets), Test 13 runs and provides real LLM round-trip coverage.

The `--env` inheritance (Item 1) means the chatbot agent automatically gets `ANTHROPIC_API_KEY` from the smoke script's environment without explicit `--env` flags on `agent create`.

**Test 13 prompt design:**

The prompt `"reply with only the word: SMOKE_CONFIRMED"` is chosen because:
1. It's deterministic enough that `grep -qi SMOKE_CONFIRMED` is very likely to match
2. It doesn't depend on any workspace files (workspace is `$STATE_DIR_T13`, empty)
3. It's short enough that pi responds in <90s even on slow API

The grep uses `-qi` (case-insensitive) to tolerate minor variation in pi's response format.

### One-shot vs interactive chat CLI

The `--one-shot` flag makes the one-shot behavior explicit and prevents ambiguity about what `dclaw agent chat alice` alone does. If we later add interactive chat mode (a readline loop), it would be the default behavior without `--one-shot`. For alpha.4, `--one-shot` is the only mode and is required.

The `Short` description says "Send a one-shot message" to distinguish it from the TUI's interactive `dclaw agent attach` chat. Users who want multi-turn interactive chat should use `dclaw agent attach <name>`.

---

## 9. `chat_test.go` Refactor Plan

### Interface extraction

The interface is declared in `internal/sandbox/docker.go` (not in `daemon/`) to keep the sandbox package as the canonical Docker API surface. The `daemon` package imports `sandbox`, so the interface is available where needed.

```
sandbox.DockerExecClient {
    InspectStatus(ctx context.Context, id string) (string, error)
    ExecIn(ctx context.Context, id string, argv []string) (string, string, int, error)
}
```

The compile-time assertion `var _ DockerExecClient = (*DockerClient)(nil)` in `docker.go` confirms the concrete type satisfies the interface.

`ChatHandler.docker` changes from `*sandbox.DockerClient` to `sandbox.DockerExecClient`. The `NewChatHandler` signature changes accordingly. In `router.go`, `NewChatHandler(log, repo, docker)` where `docker` is `*sandbox.DockerClient` still compiles because `*DockerClient` satisfies `DockerExecClient`.

### Mock design

`mockDockerExec` is a private test struct in `daemon_test` package (same package as `chat_test.go`). It is not exported — it's only used within `chat_test.go`. The struct has explicit fields for controlling return values; it does not use a function-based callback approach (simpler for the test cases needed here).

```go
type mockDockerExec struct {
    inspectStatus string
    inspectErr    error
    execStdout    string
    execStderr    string
    execCode      int
    execErr       error
}
```

For test cases that need different behavior on successive calls (e.g., "first call returns 'stopping', second returns 'exited'"), the struct can be extended with a slice-based call counter pattern — but this is NOT needed for alpha.4's test cases. All test cases use a single fixed return value.

### Test cases covered by the 5 new tests

| Test | Path exercised |
|---|---|
| `TestChatHandlerContainerNotRunning` | InspectStatus returns "exited" → error chunk sent |
| `TestChatHandlerInspectError` | InspectStatus returns error → error chunk sent |
| `TestChatHandlerSuccessfulExec` | Happy path: "running" + stdout → agent chunk |
| `TestChatHandlerNonZeroExitCode` | ExecIn exit code 127 → error chunk with stderr |
| `TestChatHandlerExecError` | ExecIn returns error → error chunk |

Plus the two existing alpha.3 tests (unchanged):

| Test | Path exercised |
|---|---|
| `TestChatHandlerAgentNotFound` | Missing agent → -32001 error response |
| `TestChatHandlerMissingContent` | Empty content → -32602 error response |

**Total chat tests after alpha.4:** 7 tests.

### Why NOT use `chat_test.go` for the nil docker client path (previously tested in alpha.3)

The alpha.3 `chat_test.go` tested `TestChatHandlerAgentNotFound` with a nil docker client because the code never reached the docker call on the not-found path. With the new nil-guard added in `chat.go` (§4.2), passing `nil` as the docker client still works for the not-found path (repo lookup fails before InspectStatus is called). The guard protects the InspectStatus call explicitly.

---

## 10. Backward Compatibility

### CLI flags

`--env` behavior change is additive: previously the flag only accepted explicit values. Now it also inherits from the shell for the two allowlisted keys. This is a behavior change but not a breaking change:
- Users who do NOT have `ANTHROPIC_API_KEY` set in their shell: behavior unchanged (no inheritance).
- Users who DO have it set: the key is now automatically included. This is the desired behavior.
- Users who already pass `--env ANTHROPIC_API_KEY=X`: their explicit value still wins. No regression.

The new `dclaw agent chat <name> --one-shot "<prompt>"` subcommand is additive. No existing subcommand is modified.

### Wire protocol

**Zero wire protocol changes.** `agent.chat.send` and `agent.chat.chunk` are unchanged. The reconciler writes to the DB but does not add any new RPC methods or change existing ones. `docs/wire-protocol-spec.md` does not need updating.

### Daemon API

The `NewChatHandler` signature changes (`*sandbox.DockerClient` → `sandbox.DockerExecClient`). This is an internal package API — no external callers. The change is backward-compatible within the module.

### go.mod / go.sum

No changes. All four items use existing dependencies.

---

## 11. Step-by-Step Implementation Order

Four parallel agents. Dependency structure:

```
Agent A (Item 1: --env shell inheritance) — no dependencies, starts immediately
Agent B (Item 2: reconciler) — no dependencies, starts immediately
Agent C (Item 3: --one-shot + Item 4: chat_test.go refactor) — no dependencies, starts immediately
Agent D (housekeeping: docs update, README note) — waits for A, B, C
```

Agents A, B, C are fully disjoint (different file sets). Run all three in parallel.

---

### Agent A — `--env` Shell Inheritance

Owns: `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent.go`, `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent_env_test.go` (new)

Prereq: starts from `v0.3.0-alpha.3` HEAD (`a9da0c1`).

Steps:

1. Add `wellKnownEnvKeys` package-level var at the top of `agent.go` per §4.7 Change 1.
2. Add `mergeShellEnv` helper at the bottom of `agent.go` per §4.7 Change 2.
3. Update `agentCreateCmd.RunE`: `kvSliceToMap(mergeShellEnv(agentCreateEnv))` per §4.7 Change 3.
4. Update `agentUpdateCmd.RunE`: `kvSliceToMap(mergeShellEnv(agentUpdateEnv))` per §4.7 Change 4.
5. Update both `--env` flag help text in `init()` per §4.7 Change 5.
6. Create `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent_env_test.go` with the three `TestMergeShellEnv*` tests from §6.
7. `go build ./internal/cli/... ./cmd/dclaw/...` — must compile.
8. `go test ./internal/cli/...` — all tests including the 3 new ones must pass.
9. `go vet ./internal/cli/...` — clean.
10. Commit: `"alpha.4(A): --env shell inheritance for well-known API keys"`.

Agent A delivers: `wellKnownEnvKeys` allowlist, `mergeShellEnv` helper, and `--help` text updates. Zero protocol or daemon changes.

---

### Agent B — DB Status Reconciler

Owns: `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/reconciler.go` (new), `/Users/hatef/workspace/agents/atlas/dclaw/cmd/dclawd/main.go`

Prereq: starts from `v0.3.0-alpha.3` HEAD (`a9da0c1`).

Steps:

1. Create `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/reconciler.go` with the full `StatusReconciler` implementation per §4.5. Export `ReconcileOnce`.
2. Modify `/Users/hatef/workspace/agents/atlas/dclaw/cmd/dclawd/main.go` per §4.6: add reconciler import, construct `StatusReconciler`, call `ReconcileOnce`, launch `go reconciler.Run(ctx)`.
3. `go build ./internal/daemon/... ./cmd/dclawd/...` — must compile.
4. `go vet ./internal/daemon/... ./cmd/dclawd/...` — clean.
5. Optional: create `internal/daemon/reconciler_test.go` with `TestReconcilerUpdatesExitedAgent` (see §7). Mark as optional in §12 test plan.
6. `go test ./internal/daemon/...` — must pass (existing tests unaffected).
7. Manual smoke: start daemon, create + start an agent, manually `docker stop <containerID>`, wait 3s, `dclaw agent get <name>` — DB status should now show "exited" (previously showed "running" indefinitely).
8. Commit: `"alpha.4(B): background DB status reconciler (2s poll)"`.

Agent B delivers: `StatusReconciler` goroutine, wired into daemon main with graceful shutdown.

---

### Agent C — Test 13 (`--one-shot`) + `chat_test.go` ExecIn Coverage

Owns: `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent_chat.go` (new), `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent.go` (add `agentChatCmd` to AddCommand), `/Users/hatef/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` (add interface), `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/chat.go` (change field type), `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/chat_test.go` (full replacement), `/Users/hatef/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` (add Test 13)

Prereq: starts from `v0.3.0-alpha.3` HEAD (`a9da0c1`).

Steps:

**Item 4 first (interface + refactor + tests):**

1. Append `DockerExecClient` interface and compile-time assertion to `/Users/hatef/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` per §4.1.
2. Replace `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/chat.go` with the full content from §4.2 (field type change + nil-guard).
3. Replace `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/chat_test.go` with the full content from §4.3 (mock + 7 tests).
4. `go build ./internal/sandbox/... ./internal/daemon/...` — must compile.
5. `go test ./internal/daemon/...` — all 7 chat tests must pass.

**Item 3 next (--one-shot subcommand):**

6. Create `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent_chat.go` per §4.8.
7. In `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent.go` `init()`, add `agentChatCmd` to the `agentCmd.AddCommand(...)` call per §4.8 instruction.
8. `go build ./internal/cli/... ./cmd/dclaw/...` — must compile.
9. `go test ./internal/cli/...` — must pass.
10. Add Test 13 to `/Users/hatef/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` per §4.9.
11. `go vet ./...` — clean.
12. Run `./scripts/smoke-daemon.sh` — Tests 1–12 must pass; Test 13 SKIPs if `ANTHROPIC_API_KEY` is unset (expected), passes if set.
13. Commit: `"alpha.4(C): Test 13 --one-shot + chat_test.go ExecIn coverage + DockerExecClient interface"`.

Agent C delivers: interface extraction, 5 new chat tests, `--one-shot` subcommand, Test 13 smoke.

---

### Agent D — Housekeeping

Owns: `/Users/hatef/workspace/agents/atlas/dclaw/docs/phase-3-alpha4-plan.md` (this document, promote from draft), handoff doc update.

Prereq: Agents A, B, C all merged.

Steps:

1. `go test ./...` — all tests pass.
2. `go vet ./...` — clean.
3. `go build ./...` — both binaries compile.
4. `./scripts/smoke-daemon.sh` — Tests 1–13 pass (13 skips if no key).
5. Update handoff doc `/Users/hatef/.claude/projects/-Users-hatef-workspace-agents-atlas/handoff/dclaw.md`:
   - Update §3 "Current state" to reflect alpha.4 shipped.
   - Add alpha.4 entry to §6 phase plan.
   - Add alpha.4 notes to §9 Claude-to-Claude notes.
6. Update `docs/phase-3-alpha4-plan.md` §0 Status from DRAFT to SHIPPED with commit SHAs.
7. Commit: `"alpha.4(D): handoff doc update + promote plan to shipped"`.
8. `git tag -a v0.3.0-alpha.4 -m "Phase 3 alpha.4: reliability + ergonomics pass"`.
9. `git push origin main v0.3.0-alpha.4`.

---

### Final integration

After all four agents' commits are on `main`:

1. `make build` — both binaries compile.
2. `go test ./...` — all tests pass.
3. `go vet ./...` — clean.
4. `./scripts/smoke-daemon.sh` — 13 tests (12 pass, Test 13 skips or passes per env).
5. Manual item verification (§12 manual steps).
6. Tag `v0.3.0-alpha.4`.

---

## 12. Test Plan

### Automated tests

| Test | Location | Exercises | New in alpha.4 |
|---|---|---|---|
| `TestMergeShellEnvInheritsWellKnown` | `internal/cli/agent_env_test.go` | Shell inheritance when key is set | Yes |
| `TestMergeShellEnvExplicitWins` | `internal/cli/agent_env_test.go` | Explicit `--env` overrides shell | Yes |
| `TestMergeShellEnvNoInheritWhenUnset` | `internal/cli/agent_env_test.go` | No inheritance when key is unset | Yes |
| `TestChatHandlerAgentNotFound` | `internal/daemon/chat_test.go` | Missing agent → -32001 | Kept from alpha.3 |
| `TestChatHandlerMissingContent` | `internal/daemon/chat_test.go` | Empty content → -32602 | Kept from alpha.3 |
| `TestChatHandlerContainerNotRunning` | `internal/daemon/chat_test.go` | Exited container → error chunk | Yes |
| `TestChatHandlerInspectError` | `internal/daemon/chat_test.go` | Docker inspect error → error chunk | Yes |
| `TestChatHandlerSuccessfulExec` | `internal/daemon/chat_test.go` | Happy path: stdout → agent chunk | Yes |
| `TestChatHandlerNonZeroExitCode` | `internal/daemon/chat_test.go` | exit 127 → error chunk | Yes |
| `TestChatHandlerExecError` | `internal/daemon/chat_test.go` | ExecIn error → error chunk | Yes |
| All alpha.3 tests | various | Regression | Regression |
| `./scripts/smoke-daemon.sh` (Tests 1–12) | bash | Integration (no LLM needed) | Regression |
| `./scripts/smoke-daemon.sh` Test 13 | bash | Real chat round-trip (LLM required) | Yes |

**Total new tests: 8 unit tests + 1 conditional integration test.**

### Manual verification steps

```
Prerequisite: make build
              ./bin/dclaw daemon start
              export ANTHROPIC_API_KEY=<your-key>

Item 1 — --env shell inheritance:
1. Verify key is in shell: echo $ANTHROPIC_API_KEY (non-empty)
2. dclaw agent create test-inherit --image=dclaw-agent:v0.1 --workspace=/tmp
   (do NOT pass --env ANTHROPIC_API_KEY)
3. dclaw agent describe test-inherit
   EXPECT: Env section includes ANTHROPIC_API_KEY=<your-key>
4. dclaw agent create test-explicit --image=dclaw-agent:v0.1 --workspace=/tmp \
     --env ANTHROPIC_API_KEY=override-value
5. dclaw agent describe test-explicit
   EXPECT: ANTHROPIC_API_KEY=override-value (explicit wins)
6. (cleanup) dclaw agent delete test-inherit test-explicit

Item 2 — DB status reconciliation:
7. dclaw agent create rec-test --image=dclaw-agent:v0.1 --workspace=/tmp
   dclaw agent start rec-test
8. CONTAINER_ID=$(dclaw agent get rec-test -o json | jq -r '.container_id')
   docker stop $CONTAINER_ID
9. sleep 3
10. dclaw agent get rec-test -o json | jq -r '.status'
    EXPECT: "exited" (was previously "running" indefinitely)
11. (cleanup) dclaw agent delete rec-test

Item 3 — --one-shot subcommand:
12. dclaw agent create chat-test --image=dclaw-agent:v0.1 --workspace=/tmp
    dclaw agent start chat-test
13. dclaw agent chat chat-test --one-shot "reply with only: HELLO_FROM_PI"
    EXPECT: output contains "HELLO_FROM_PI" (or close variant), exit 0
14. dclaw agent stop chat-test
15. dclaw agent chat chat-test --one-shot "hello" --timeout 5s
    EXPECT: exit 2, stderr contains "not running"
16. dclaw agent chat nosuchagent --one-shot "hello"
    EXPECT: exit 1, error message mentioning "not found"
17. (cleanup) dclaw agent delete chat-test

Item 4 — chat_test.go ExecIn coverage:
18. go test -v ./internal/daemon/... -run TestChatHandler
    EXPECT: all 7 TestChatHandler* tests PASS, none SKIP
19. Verify mock_docker_exec is not exported (it's in daemon_test package, unexported)

General regression:
20. ./scripts/smoke-daemon.sh — 12/13 tests pass (13 skips if no key)
21. go vet ./... — clean
22. go test ./... — all tests pass

Cleanup:
23. dclaw daemon stop
```

---

## 13. Release Checklist for v0.3.0-alpha.4

1. [ ] All alpha.3 checklist items still green
2. [ ] `go vet ./...` clean
3. [ ] `go build ./...` — both `./bin/dclaw` and `./bin/dclawd` compile
4. [ ] `go test ./...` — all tests including 8 new ones pass
5. [ ] `./scripts/smoke-daemon.sh` — Tests 1–12 pass; Test 13 skips (no key) or passes (key set)
6. [ ] Manual verification §12 steps 1–22 completed
7. [ ] Item 1: `dclaw agent create` without `--env ANTHROPIC_API_KEY` but with key in shell → key inherited
8. [ ] Item 1: `dclaw agent create` with `--env ANTHROPIC_API_KEY=X` → explicit value wins
9. [ ] Item 1: `dclaw agent create --help` shows inheritance note in `--env` flag description
10. [ ] Item 2: container stop after agent start → `dclaw agent get` shows "exited" within 3s
11. [ ] Item 2: daemon restart after container exit → `dclaw agent get` shows "exited" immediately
12. [ ] Item 3: `dclaw agent chat <name> --one-shot "hello" --timeout 90s` exits 0 and produces output
13. [ ] Item 3: `dclaw agent chat <name> --one-shot "hello"` against stopped agent exits 2
14. [ ] Item 3: `dclaw agent chat --help` shows `--one-shot` and `--timeout` flags
15. [ ] Item 4: `go test -v ./internal/daemon/... -run TestChatHandler` — 7 tests pass
16. [ ] Item 4: `go build` with the interface change — `router.go` compiles without change
17. [ ] `go.mod` unchanged (no new direct deps)
18. [ ] Handoff doc `/Users/hatef/.claude/projects/-Users-hatef-workspace-agents-atlas/handoff/dclaw.md` updated
19. [ ] `docs/phase-3-alpha4-plan.md` §0 Status promoted from DRAFT to SHIPPED with commit SHAs
20. [ ] Commit: `"Phase 3 alpha.4: reliability + ergonomics pass (v0.3.0-alpha.4)"`
21. [ ] `git tag -a v0.3.0-alpha.4 -m "Phase 3 alpha.4: reliability + ergonomics pass"`
22. [ ] `git push origin main v0.3.0-alpha.4`

---

## 14. Open Questions

This section documents the three design decisions flagged in the task brief. Each is **decided** here — no question stays open without a concrete blocker requiring user input.

---

### Q1: Which reconciler approach?

**Decision: Option B — background polling goroutine.**

Options:

- **Option A** (on-read InspectStatus in AgentList): NOT chosen. `AgentList` in `lifecycle.go` lines 92-108 already does this — it calls `InspectStatus` per-agent for every `agent.list` RPC. The issue is the DB column remaining stale. Option A would have already fixed the `agent list` display but doesn't — because it's already implemented. The remaining problem is DB staleness. Option A doesn't help further.
- **Option B** (background goroutine, 2s poll): chosen. Constant Docker load (N * InspectStatus calls every 2s regardless of query activity), simple goroutine lifecycle, 2s eventual consistency acceptable for alpha.4.
- **Option C** (Docker events subscription): deferred to beta.1. Architecturally superior but adds ~40 lines of reconnect logic. A TODO comment in `reconciler.go` points to beta.1.

**Rationale in one line:** Option A is already implemented; Option C is architecturally better but too much complexity for a reliability pass; Option B is the right intermediate.

**Correction to problem statement:** The problem statement says "AgentList shows stale DB data for 49 agents at a time." Looking at the actual code, `lifecycle.go:AgentList` (lines 92-108) already calls `InspectStatus` per-agent and updates the local `live` variable before building the wire response. The list response always shows live Docker status. The DB column is still stale, but `agent list` output is already correct. The reconciler's value is keeping the DB column accurate for audit trail, update precondition checks, and future features.

---

### Q2: Which Test 13 mechanism?

**Decision: Option B — `dclaw agent chat <name> --one-shot "<prompt>"` subcommand.**

Options:

- **Option A** (bash + nc + jq on Unix socket): NOT chosen. Fragile, tests wire format details, not maintainable.
- **Option B** (`--one-shot` subcommand): chosen. Exercises the full `RPCClient.ChatSend` path. Independently useful. Smoke test invocation is clean. The new CLI command is scoped with clear stability semantics: it's in `internal/cli/agent.go`, alpha-quality, `--one-shot` flag required, `--timeout` flag optional.
- **Option C** (Go integration test in `cmd/dclawd/integration_test.go`): NOT chosen for alpha.4. Would require Docker + running agent image in pure test context. Heavy for a reliability pass.

**Credential gating:** option (a) — CI skips Test 13 if `$ANTHROPIC_API_KEY` and `$ANTHROPIC_OAUTH_TOKEN` are both empty. No second Docker image needed. This is the correct tradeoff: credentials-required tests should be optional in CI unless a credentials provider is configured.

**Stability note:** The `dclaw agent chat` subcommand is introduced as alpha-quality. It is NOT documented as stable. The handoff doc should note this explicitly. The `--one-shot` flag is the only supported mode in alpha.4 — interactive multi-turn via CLI (a readline loop) is explicitly deferred.

**Rationale in one line:** Option B turns a test gap into a useful CLI primitive while exercising the exact code path the TUI uses.

---

### Q3: Which `chat_test.go` mock strategy?

**Decision: Option A — `DockerExecClient` interface injected into `ChatHandler`.**

Options:

- **Option A** (interface injection): chosen. Standard Go test pattern. Interface declared in `sandbox` package next to the concrete type. `ChatHandler` accepts the interface. Tests use a struct literal mock. No Docker daemon dependency.
- **Option B** (real Docker containers): NOT chosen. Slow, requires Docker daemon, can't run offline.

**Interface placement rationale:** The interface goes in `internal/sandbox/docker.go` (not in `internal/daemon/`) because the sandbox package owns the Docker API surface. If a future feature needs the same interface in a different package, it imports `sandbox.DockerExecClient` rather than a parallel declaration in `daemon`.

**Mock design rationale:** Simple struct with fixed return-value fields rather than function callbacks. All alpha.4 test cases use single fixed behavior. If future tests need per-call variation (e.g., first call returns "stopping", second returns "exited"), add a `callCount` field and a `[]string` of sequential return values — but defer that complexity until actually needed.

**Rationale in one line:** Interface injection is the idiomatic Go approach, enables offline unit tests, and adds minimal complexity.

---

## Appendix: Corrections to Alpha.3 Plan/State Found During Drafting

1. **`AgentList` already calls `InspectStatus` live**: The alpha.3 plan and the alpha.4 task brief both describe "AgentList shows stale DB data." Looking at `lifecycle.go` lines 92-108, `AgentList` already enriches each record with live `InspectStatus` before building the wire response. The DB column IS stale, but the list response is NOT — it already shows live status. The reconciler's value is for DB accuracy (audit trail, update preconditions), not for fixing `agent list` display. The alpha.3 plan §0 status line "status lie at line 198" refers to `AgentStart` writing `status="running"` synchronously after container start (line 214 in the actual shipped code, not 198 — the line number shifted slightly in the final commit). The description is accurate; the line number is slightly off.

2. **`chat_test.go` already uses `newTestRepo`**: The shipped alpha.3 `chat_test.go` already has `newTestRepo` and `silentLogger` helpers and fully implements `TestChatHandlerAgentNotFound` (the plan showed it as a `t.Skip` placeholder, but the actual shipped file has the real implementation). The alpha.4 plan must not regress this test.

3. **`agentCreateCmd.RunE` is at line 34**: The task brief says "agentCreateCmd.RunE (line 34)" — confirmed correct. The `RunE` anonymous function starts at line 34.

4. **`--env` flag is at line 308 (not 309)**: The task brief says "line 309" but the actual `StringArrayVar` call for `agentCreateEnv` is at line 308. Minor; irrelevant to implementation.

5. **No `agentUpdateCmd` line 139 confusion**: The task brief says "agentUpdateCmd at line 139 / flag at line 314." Actual line for `agentUpdateCmd.RunE` starts at line 147 (not 139). The `agentUpdateEnv` flag is at line 313. Again minor; the code is clear.

---
