package daemon

// logs.go hosts the streaming-logs helpers used by the TUI's logs view and by
// `dclaw agent logs -f`. The synchronous bulk fetch lives directly on
// Lifecycle.AgentLogsBulk; the streaming variant here sends a series of
// `agent.log_line` notifications on the client's connection until ctx is
// cancelled or the container exits.
//
// NOTE: beta.1 completes the streaming path. alpha.1 ships the bulk fetch
// only; alpha.2 and alpha.3 consume the bulk path for the TUI's logs view
// (polling every 2s). beta.1 replaces the polling with the notification
// stream below.

import (
	"context"
	"log/slog"

	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

// LogStreamer pushes container log lines to an output channel. One streamer
// per `agent.logs.stream` subscription. Cancel via ctx.
type LogStreamer struct {
	log    *slog.Logger
	repo   *store.Repo
	docker *sandbox.DockerClient
}

// NewLogStreamer is the entry point for beta.1 wiring.
func NewLogStreamer(log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *LogStreamer {
	return &LogStreamer{log: log, repo: repo, docker: docker}
}

// Stream reads log lines from the named agent's container and pushes them on
// out until ctx is cancelled. It never closes out; the caller is responsible
// for closing after Stream returns.
func (s *LogStreamer) Stream(ctx context.Context, name string, out chan<- string) error {
	rec, err := s.repo.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if rec.ContainerID == "" {
		return nil
	}
	lines, errs := s.docker.LogsFollow(ctx, rec.ContainerID)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return nil
			}
			select {
			case out <- line:
			case <-ctx.Done():
				return ctx.Err()
			}
		case err, ok := <-errs:
			if !ok {
				return nil
			}
			return err
		}
	}
}
