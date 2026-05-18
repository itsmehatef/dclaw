package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/itsmehatef/dclaw/internal/store"
)

// TestMigrate0002 verifies the 0002_workspace_trust.sql migration applies
// against the 0001 schema. We open a fresh DB, migrate to the latest
// head (which includes 0002), and verify the workspace_trust_reason
// column exists by INSERTing a row with a non-empty value and reading
// it back.
func TestMigrate0002(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate.db")
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Smoke: INSERT a row with WorkspaceTrustReason set, SELECT it back.
	ctx := context.Background()
	in := store.AgentRecord{
		ID:                   "ulid-test",
		Name:                 "migrate-test-agent",
		Image:                "image:latest",
		Status:               "created",
		ContainerID:          "",
		Workspace:            "/tmp/ws",
		WorkspaceTrustReason: "legacy migration test",
		Labels:               "{}",
		Env:                  "{}",
		CreatedAt:            1,
		UpdatedAt:            1,
	}
	if err := repo.InsertAgent(ctx, in); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := repo.GetAgent(ctx, in.Name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorkspaceTrustReason != "legacy migration test" {
		t.Fatalf("workspace_trust_reason round-trip: got %q want %q", got.WorkspaceTrustReason, "legacy migration test")
	}
}

func TestMigrate0003ChatMessagesTableExists(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate-0003.db")
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`PRAGMA table_info(chat_messages)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found[name] = true
	}
	for _, want := range []string{"id", "agent_id", "role", "content", "parent_id", "message_id", "sequence", "timestamp"} {
		if !found[want] {
			t.Fatalf("chat_messages column %q not found", want)
		}
	}
}

// TestGetAgentNotFoundSentinel asserts that GetAgent on a missing name
// returns an error that wraps store.ErrNotFound. The router's mapError
// depends on this.
func TestGetAgentNotFoundSentinel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sentinel.db")
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, err = repo.GetAgent(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatalf("expected error on missing agent")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound wrap, got %v", err)
	}
}

// TestInsertAgentNameTakenSentinel asserts UNIQUE violation surfaces as
// ErrNameTaken.
func TestInsertAgentNameTakenSentinel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "unique.db")
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rec := store.AgentRecord{
		ID: "id-1", Name: "dup", Image: "i", Status: "created",
		Labels: "{}", Env: "{}", CreatedAt: 1, UpdatedAt: 1,
	}
	if err := repo.InsertAgent(context.Background(), rec); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	rec.ID = "id-2"
	err = repo.InsertAgent(context.Background(), rec)
	if err == nil {
		t.Fatalf("expected error on duplicate name")
	}
	if !errors.Is(err, store.ErrNameTaken) {
		t.Fatalf("expected ErrNameTaken wrap, got %v", err)
	}
}

// TestMigrate0002UpDownRoundTrip exercises 0002's reversibility. Now that
// 0003 exists, it rolls back twice: first 0003, then 0002.
func TestMigrate0002UpDownRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "roundtrip.db")
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer repo.Close()
	ctx := context.Background()
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("up #1: %v", err)
	}
	// Roll back 0003, then 0002.
	if err := repo.Rollback(ctx); err != nil {
		t.Fatalf("down #1: %v", err)
	}
	if err := repo.Rollback(ctx); err != nil {
		t.Fatalf("down #2: %v", err)
	}

	// Verify column is gone.
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`PRAGMA table_info(agents)`)
	if err != nil {
		t.Fatalf("pragma post-down: %v", err)
	}
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "workspace_trust_reason" {
			found = true
		}
	}
	rows.Close()
	if found {
		t.Fatalf("workspace_trust_reason still present after down migration")
	}

	// Re-apply up.
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("up #2: %v", err)
	}

	// Verify column is back.
	rows2, err := db.Query(`PRAGMA table_info(agents)`)
	if err != nil {
		t.Fatalf("pragma post-up: %v", err)
	}
	defer rows2.Close()
	found = false
	for rows2.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows2.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "workspace_trust_reason" {
			found = true
		}
	}
	if !found {
		t.Fatalf("workspace_trust_reason missing after re-apply")
	}
}

func TestMigrate0003UpDownRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "roundtrip-0003.db")
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer repo.Close()
	ctx := context.Background()
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("up #1: %v", err)
	}
	if err := repo.Rollback(ctx); err != nil {
		t.Fatalf("down: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`PRAGMA table_info(chat_messages)`)
	if err != nil {
		t.Fatalf("pragma post-down: %v", err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatalf("chat_messages table still present after down migration")
	}
	rows.Close()

	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("up #2: %v", err)
	}
	rows2, err := db.Query(`PRAGMA table_info(chat_messages)`)
	if err != nil {
		t.Fatalf("pragma post-up: %v", err)
	}
	defer rows2.Close()
	if !rows2.Next() {
		t.Fatalf("chat_messages table missing after re-apply")
	}
}

// TestWorkspaceTrustReasonColumnExists opens the DB via the raw sql.DB
// after Migrate and performs a PRAGMA table_info inspection to confirm
// the column landed. This double-checks the schema shape independent
// of InsertAgent.
func TestWorkspaceTrustReasonColumnExists(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pragma.db")
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Reach into the sql.DB via a tiny helper query. We don't expose
	// the *sql.DB on the repo, but we can re-open a second *sql.DB
	// on the same file to inspect.
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`PRAGMA table_info(agents)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "workspace_trust_reason" {
			found = true
			if typ != "TEXT" {
				t.Fatalf("workspace_trust_reason column type %q, want TEXT", typ)
			}
		}
	}
	if !found {
		t.Fatalf("workspace_trust_reason column not found in agents table")
	}
}

func TestInsertAndListChatHistory(t *testing.T) {
	repo := openMigratedRepo(t, "chat-history.db")
	defer repo.Close()
	ctx := context.Background()

	a1 := insertTestAgent(t, repo, "agent-one")
	a2 := insertTestAgent(t, repo, "agent-two")
	for _, rec := range []store.ChatMessageRecord{
		{ID: "m1", AgentID: a1.ID, Role: "user", Content: "hello", MessageID: "msg-1", Sequence: 0, Timestamp: 10},
		{ID: "m2", AgentID: a1.ID, Role: "agent", Content: "hi", ParentID: "msg-1", MessageID: "msg-2", Sequence: 1, Timestamp: 11},
		{ID: "m3", AgentID: a1.ID, Role: "user", Content: "again", ParentID: "msg-2", MessageID: "msg-3", Sequence: 0, Timestamp: 12},
		{ID: "m4", AgentID: a2.ID, Role: "user", Content: "other", MessageID: "msg-4", Sequence: 0, Timestamp: 13},
	} {
		if err := repo.InsertChatMessage(ctx, rec); err != nil {
			t.Fatalf("insert chat %s: %v", rec.ID, err)
		}
	}

	got, err := repo.ListChatHistory(ctx, a1.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	for i, want := range []string{"msg-1", "msg-2", "msg-3"} {
		if got[i].MessageID != want {
			t.Fatalf("message[%d]=%q want %q", i, got[i].MessageID, want)
		}
	}

	limited, err := repo.ListChatHistory(ctx, a1.ID, 2)
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(limited) != 2 || limited[0].MessageID != "msg-1" || limited[1].MessageID != "msg-2" {
		t.Fatalf("limited result = %#v", limited)
	}
}

func TestInsertChatMessageDuplicateMessageID(t *testing.T) {
	repo := openMigratedRepo(t, "chat-history-dup.db")
	defer repo.Close()
	ctx := context.Background()
	a := insertTestAgent(t, repo, "agent-dup")

	rec := store.ChatMessageRecord{
		ID: "m1", AgentID: a.ID, Role: "user", Content: "hello", MessageID: "dup-message-id", Timestamp: 10,
	}
	if err := repo.InsertChatMessage(ctx, rec); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	rec.ID = "m2"
	err := repo.InsertChatMessage(ctx, rec)
	if err == nil {
		t.Fatalf("expected duplicate message_id error")
	}
	if !errors.Is(err, store.ErrNameTaken) {
		t.Fatalf("expected ErrNameTaken wrap, got %v", err)
	}
}

func TestDeleteChatHistoryForAgent(t *testing.T) {
	repo := openMigratedRepo(t, "chat-history-delete.db")
	defer repo.Close()
	ctx := context.Background()
	a := insertTestAgent(t, repo, "agent-delete-history")

	for _, rec := range []store.ChatMessageRecord{
		{ID: "m1", AgentID: a.ID, Role: "user", Content: "hello", MessageID: "delete-1", Timestamp: 10},
		{ID: "m2", AgentID: a.ID, Role: "agent", Content: "hi", MessageID: "delete-2", Timestamp: 11},
	} {
		if err := repo.InsertChatMessage(ctx, rec); err != nil {
			t.Fatalf("insert chat %s: %v", rec.ID, err)
		}
	}
	if err := repo.DeleteChatHistoryForAgent(ctx, a.ID); err != nil {
		t.Fatalf("delete history: %v", err)
	}
	got, err := repo.ListChatHistory(ctx, a.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("history len=%d want 0", len(got))
	}
}

func TestDeleteAgentCascadesChatHistory(t *testing.T) {
	repo := openMigratedRepo(t, "chat-history-cascade.db")
	defer repo.Close()
	ctx := context.Background()
	a := insertTestAgent(t, repo, "agent-cascade")
	if err := repo.InsertChatMessage(ctx, store.ChatMessageRecord{
		ID: "m1", AgentID: a.ID, Role: "user", Content: "hello", MessageID: "cascade-1", Timestamp: 10,
	}); err != nil {
		t.Fatalf("insert chat: %v", err)
	}
	if err := repo.DeleteAgent(ctx, a.Name); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	got, err := repo.ListChatHistory(ctx, a.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("history len=%d want 0 after agent delete cascade", len(got))
	}
}

func openMigratedRepo(t *testing.T, name string) *store.Repo {
	t.Helper()
	repo, err := store.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := repo.Migrate(context.Background()); err != nil {
		_ = repo.Close()
		t.Fatalf("migrate: %v", err)
	}
	return repo
}

func insertTestAgent(t *testing.T, repo *store.Repo, name string) store.AgentRecord {
	t.Helper()
	rec := store.AgentRecord{
		ID: store.NewID(), Name: name, Image: "image:latest", Status: "created",
		Labels: "{}", Env: "{}", CreatedAt: 1, UpdatedAt: 1,
	}
	if err := repo.InsertAgent(context.Background(), rec); err != nil {
		t.Fatalf("insert agent %s: %v", name, err)
	}
	return rec
}
