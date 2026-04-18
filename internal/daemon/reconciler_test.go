package daemon_test

import (
	"context"
	"errors"
	"testing"

	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/store"
)

// mockReconcilerDocker is a test double for sandbox.DockerExecClient that
// implements InspectStatus from a per-containerID map. ExecIn is unused by
// the reconciler — implemented as a no-op to satisfy the interface.
type mockReconcilerDocker struct {
	// statuses maps containerID → status string returned by InspectStatus.
	statuses map[string]string
	// errs maps containerID → error returned by InspectStatus (mutually
	// exclusive with statuses for a given id).
	errs map[string]error
}

func (m *mockReconcilerDocker) InspectStatus(_ context.Context, id string) (string, error) {
	if err, ok := m.errs[id]; ok {
		return "", err
	}
	if s, ok := m.statuses[id]; ok {
		return s, nil
	}
	return "", errors.New("mockReconcilerDocker: no canned response for id " + id)
}

func (m *mockReconcilerDocker) ExecIn(_ context.Context, _ string, _ []string) (string, string, int, error) {
	return "", "", 0, nil
}

// insertAgentWithStatus inserts an agent at the given status. Mirrors
// insertRunningAgent but allows the test to seed any starting status.
func insertAgentWithStatus(t *testing.T, repo *store.Repo, name, containerID, status string) {
	t.Helper()
	err := repo.InsertAgent(context.Background(), store.AgentRecord{
		ID:          "test-id-" + name,
		Name:        name,
		Image:       "dclaw-agent:v0.1",
		Status:      status,
		ContainerID: containerID,
		Workspace:   "/tmp",
		Env:         "{}",
		Labels:      "{}",
		CreatedAt:   1000000,
		UpdatedAt:   1000000,
	})
	if err != nil {
		t.Fatalf("insertAgentWithStatus: %v", err)
	}
}

// TestReconcilerUpdatesExitedAgent verifies the core reconciler behaviour:
// an agent stored as "running" whose container Docker reports as "exited"
// gets its DB status column updated on ReconcileOnce.
//
// Also covers two negative cases in the same pass:
//   - An agent with an empty ContainerID is left untouched (reconciler
//     skips it — there's nothing to inspect).
//   - An agent already in "created" state is left untouched (reconciler
//     skips per its never-started guard).
func TestReconcilerUpdatesExitedAgent(t *testing.T) {
	repo := newTestRepo(t)

	// alice: running in DB, Docker reports exited → should update to "exited".
	insertAgentWithStatus(t, repo, "alice", "ctr-alice", "running")
	// bob: running in DB, Docker reports running → no change expected.
	insertAgentWithStatus(t, repo, "bob", "ctr-bob", "running")
	// carol: no container ID → reconciler must skip entirely.
	insertAgentWithStatus(t, repo, "carol", "", "running")
	// dave: status "created" → reconciler's never-started guard skips it.
	insertAgentWithStatus(t, repo, "dave", "ctr-dave", "created")

	mock := &mockReconcilerDocker{
		statuses: map[string]string{
			"ctr-alice": "exited",
			"ctr-bob":   "running",
			"ctr-dave":  "running", // shouldn't be consulted (agent skipped)
		},
	}

	r := daemon.NewStatusReconciler(silentLogger(), repo, mock)
	r.ReconcileOnce(context.Background())

	// Verify alice: should now be "exited".
	aliceRec, err := repo.GetAgent(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetAgent(alice): %v", err)
	}
	if aliceRec.Status != "exited" {
		t.Errorf("alice status: got %q, want %q", aliceRec.Status, "exited")
	}

	// Verify bob: still "running" (no change needed).
	bobRec, err := repo.GetAgent(context.Background(), "bob")
	if err != nil {
		t.Fatalf("GetAgent(bob): %v", err)
	}
	if bobRec.Status != "running" {
		t.Errorf("bob status: got %q, want %q", bobRec.Status, "running")
	}

	// Verify carol: untouched (empty ContainerID).
	carolRec, err := repo.GetAgent(context.Background(), "carol")
	if err != nil {
		t.Fatalf("GetAgent(carol): %v", err)
	}
	if carolRec.Status != "running" {
		t.Errorf("carol should be untouched: got %q, want %q", carolRec.Status, "running")
	}

	// Verify dave: untouched ("created" guard).
	daveRec, err := repo.GetAgent(context.Background(), "dave")
	if err != nil {
		t.Fatalf("GetAgent(dave): %v", err)
	}
	if daveRec.Status != "created" {
		t.Errorf("dave should be untouched: got %q, want %q", daveRec.Status, "created")
	}
}

// TestReconcilerMarksDeadWhenInspectFails verifies that if InspectStatus
// returns an error (e.g., the container has been removed externally), the
// reconciler marks the agent as "dead" in the DB.
func TestReconcilerMarksDeadWhenInspectFails(t *testing.T) {
	repo := newTestRepo(t)
	insertAgentWithStatus(t, repo, "ghost", "ctr-ghost", "running")

	mock := &mockReconcilerDocker{
		errs: map[string]error{
			"ctr-ghost": errors.New("No such container: ctr-ghost"),
		},
	}

	r := daemon.NewStatusReconciler(silentLogger(), repo, mock)
	r.ReconcileOnce(context.Background())

	rec, err := repo.GetAgent(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("GetAgent(ghost): %v", err)
	}
	if rec.Status != "dead" {
		t.Errorf("ghost status: got %q, want %q", rec.Status, "dead")
	}
}
