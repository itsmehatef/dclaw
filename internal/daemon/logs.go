package daemon

// logs.go hosts the streaming-logs helpers used by the TUI's logs view. The
// synchronous bulk fetch lives directly on Lifecycle.AgentLogsBulk; the
// streaming variant here sends a series of `agent.log.line` notifications on
// the client's connection until ctx is cancelled or the container exits.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

// LogStreamer pushes container log lines to an output channel. One streamer
// per `agent.logs.stream` subscription. Cancel via ctx.
type LogStreamer struct {
	log    *slog.Logger
	repo   *store.Repo
	docker sandbox.DockerLogsClient
}

// NewLogStreamer constructs the low-level log stream reader.
func NewLogStreamer(log *slog.Logger, repo *store.Repo, docker sandbox.DockerLogsClient) *LogStreamer {
	return &LogStreamer{log: log, repo: repo, docker: docker}
}

// Stream reads log lines from the named agent's container and pushes them on
// out until ctx is cancelled. It never closes out; the caller is responsible
// for closing after Stream returns.
func (s *LogStreamer) Stream(ctx context.Context, name string, tail int, out chan<- string) error {
	rec, err := s.repo.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if rec.ContainerID == "" {
		return nil
	}
	lines, errs := s.docker.LogsFollow(ctx, rec.ContainerID, tail)
	var linesDone, errsDone bool
	for {
		if linesDone && errsDone {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				linesDone = true
				lines = nil
				continue
			}
			select {
			case out <- line:
			case <-ctx.Done():
				return ctx.Err()
			}
		case err, ok := <-errs:
			if !ok {
				errsDone = true
				errs = nil
				continue
			}
			return err
		}
	}
}

// LogStreamHandler processes agent.logs.stream requests.
type LogStreamHandler struct {
	log      *slog.Logger
	streamer *LogStreamer
}

// NewLogStreamHandler returns a handler for the agent.logs.stream RPC.
func NewLogStreamHandler(log *slog.Logger, repo *store.Repo, docker sandbox.DockerLogsClient) *LogStreamHandler {
	return &LogStreamHandler{
		log:      log,
		streamer: NewLogStreamer(log, repo, docker),
	}
}

// Handle processes one agent.logs.stream request. It sends a synchronous ack,
// then emits agent.log.line notifications until the stream ends.
func (h *LogStreamHandler) Handle(
	ctx context.Context,
	params json.RawMessage,
	reqID any,
	send func(*protocol.Envelope) error,
) error {
	var req protocol.LogsStreamParams
	if err := json.Unmarshal(params, &req); err != nil {
		return send(protocol.ErrorResponse(reqID, protocol.ErrInvalidParams, err.Error(), nil))
	}
	if req.Name == "" {
		return send(protocol.ErrorResponse(reqID, protocol.ErrInvalidParams, "name required", nil))
	}
	if h.streamer == nil || h.streamer.docker == nil {
		return send(protocol.ErrorResponse(reqID, protocol.ErrInternal, "docker client not available", nil))
	}
	rec, err := h.streamer.repo.GetAgent(ctx, req.Name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return send(protocol.ErrorResponse(reqID, protocol.ErrAgentNotFound,
				fmt.Sprintf("agent %q not found", req.Name), nil))
		}
		return send(protocol.ErrorResponse(reqID, protocol.ErrInternal, err.Error(), nil))
	}
	if rec.ContainerID == "" {
		return send(protocol.ErrorResponse(reqID, protocol.ErrAgentNotRunning,
			fmt.Sprintf("agent %q has no container", req.Name), nil))
	}

	if err := send(protocol.SuccessResponse(reqID, protocol.AckResult{Ack: true})); err != nil {
		return err
	}

	lines := make(chan string, 128)
	errCh := make(chan error, 1)
	go func() {
		defer close(lines)
		if err := h.streamer.Stream(ctx, req.Name, req.Tail, lines); err != nil && ctx.Err() == nil {
			errCh <- err
		}
		close(errCh)
	}()

	var linesDone, errDone bool
	for {
		if linesDone && errDone {
			payload := protocol.LogsStreamDoneNotification{
				Name:      req.Name,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			return send(protocol.Notification("agent.log.done", payload))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-errCh:
			if !ok {
				errDone = true
				errCh = nil
				continue
			}
			if err != nil {
				h.log.Warn("logs stream error", "agent", req.Name, "err", err)
				payload := protocol.LogsStreamErrorNotification{
					Name:      req.Name,
					Error:     err.Error(),
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				return send(protocol.Notification("agent.log.error", payload))
			}
		case line, ok := <-lines:
			if !ok {
				linesDone = true
				lines = nil
				continue
			}
			payload := protocol.LogsStreamLineNotification{
				Name:      req.Name,
				Line:      line,
				Stream:    "stdout",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			if err := send(protocol.Notification("agent.log.line", payload)); err != nil {
				return err
			}
		}
	}
}
