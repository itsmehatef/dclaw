package daemon_test

import (
	"context"
	"encoding/json"
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

// silentLogger returns an slog.Logger that discards all output. ChatHandler
// only calls log.Debug in the non-error path we don't exercise here, but the
// agent-not-found path is pure error handling and never dereferences the
// logger — still, we supply a real logger to be safe against future changes.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestChatHandlerAgentNotFound verifies that a missing agent returns -32001.
// Uses an in-memory store with zero agents inserted and a nil docker client
// (exec is never reached on the not-found path).
func TestChatHandlerAgentNotFound(t *testing.T) {
	repo := newTestRepo(t)

	var received []*protocol.Envelope
	send := func(env *protocol.Envelope) error {
		received = append(received, env)
		return nil
	}

	h := daemon.NewChatHandler(silentLogger(), repo, nil)
	params, err := json.Marshal(protocol.AgentChatSendParams{
		Name:    "alice",
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	if err := h.Handle(context.Background(), params, 42, send); err != nil {
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
	send := func(env *protocol.Envelope) error {
		received = append(received, env)
		return nil
	}

	// handler with nil repo — the missing-content check fires before any repo call.
	h := daemon.NewChatHandler(nil, nil, nil)
	params, _ := json.Marshal(protocol.AgentChatSendParams{Name: "alice", Content: ""})
	_ = h.Handle(context.Background(), params, 1, send)

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
