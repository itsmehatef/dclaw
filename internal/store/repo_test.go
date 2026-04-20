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

// TestMigrate0002UpDownRoundTrip exercises the migration's reversibility.
// Up then Down should leave the schema without the workspace_trust_reason
// column (SQLite 3.35+ supports DROP COLUMN). A second Up brings it back.
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
	// Roll back the most-recent migration (0002).
	if err := repo.Rollback(ctx); err != nil {
		t.Fatalf("down: %v", err)
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
