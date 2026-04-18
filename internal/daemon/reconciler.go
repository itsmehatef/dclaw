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
// TODO(beta.1): replace with Docker events subscription.
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
// docker is the sandbox.DockerExecClient interface (not the concrete
// *sandbox.DockerClient) so tests can inject a mock without a live Docker
// daemon. The concrete type satisfies the interface automatically — no
// constructor call-site changes needed.
//
// Graceful shutdown: the goroutine exits when its context is cancelled.
// The daemon cancels the context on SIGTERM/SIGINT. No drain needed.
type StatusReconciler struct {
	log    *slog.Logger
	repo   *store.Repo
	docker sandbox.DockerExecClient
}

// NewStatusReconciler constructs a StatusReconciler. docker accepts any
// DockerExecClient; pass a *sandbox.DockerClient in production, a mock in
// tests.
func NewStatusReconciler(log *slog.Logger, repo *store.Repo, docker sandbox.DockerExecClient) *StatusReconciler {
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
		// Honor cancellation between agents so a slow pass does not delay
		// shutdown by the full per-agent Docker round-trip budget.
		if ctx.Err() != nil {
			return
		}
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
