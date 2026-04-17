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
	docker *sandbox.DockerClient
}

// NewChatHandler returns a ChatHandler.
func NewChatHandler(log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *ChatHandler {
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

	// Alpha.3: synchronous exec — one final chunk.
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
