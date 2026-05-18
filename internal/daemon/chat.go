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
	"errors"
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

const duplicateChatReplayWait = 5 * time.Minute

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
	if req.MessageID == "" {
		return send(protocol.ErrorResponse(reqID, protocol.ErrInvalidParams, "message_id required", nil))
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
	if err := h.persistChatMessage(ctx, rec.ID, "user", req.Content, req.ParentID, msgID, 0); err != nil {
		if errors.Is(err, store.ErrNameTaken) {
			return h.replayDuplicateChat(ctx, req, rec.ID, msgID, reqID, send)
		}
		return send(protocol.ErrorResponse(reqID, protocol.ErrChatHistoryUnavailable,
			fmt.Sprintf("chat history unavailable: %v", err), nil))
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
			Name:     req.Name,
			Role:     "error",
			Text:     "docker client not available",
			Sequence: 0,
			Final:    true,
		}
		notReady.MessageID = h.persistReplyID(ctx, rec.ID, "error", notReady.Text, msgID, 0, &notReady)
		return send(protocol.Notification("agent.chat.chunk", notReady))
	}

	status, statErr := h.docker.InspectStatus(ctx, rec.ContainerID)
	if statErr != nil || status != "running" {
		shown := status
		if statErr != nil {
			shown = "unknown"
		}
		notRunning := protocol.AgentChatChunkNotification{
			Name:     req.Name,
			Role:     "error",
			Text:     fmt.Sprintf("agent not running (container state: %s) — did you run 'dclaw agent start %s'?", shown, req.Name),
			Sequence: 0,
			Final:    true,
		}
		notRunning.MessageID = h.persistReplyID(ctx, rec.ID, "error", notRunning.Text, msgID, 0, &notRunning)
		return send(protocol.Notification("agent.chat.chunk", notRunning))
	}

	h.log.Debug("chat exec start", "agent", req.Name, "msg_id", msgID)

	// Alpha.3/alpha.4: synchronous exec — one final chunk. beta.3.1 routes
	// through the image wrapper so provider-specific handling (DeepSeek, pi
	// provider envs) stays owned by the agent image instead of the daemon.
	argv := []string{"node", "/app/run.mjs", req.Content}
	stdout, stderr, exitCode, execErr := h.docker.ExecIn(ctx, rec.ContainerID, argv)

	if execErr != nil {
		errChunk := protocol.AgentChatChunkNotification{
			Name:     req.Name,
			Role:     "error",
			Text:     execErr.Error(),
			Sequence: 0,
			Final:    true,
		}
		errChunk.MessageID = h.persistReplyID(ctx, rec.ID, "error", errChunk.Text, msgID, 0, &errChunk)
		return send(protocol.Notification("agent.chat.chunk", errChunk))
	}

	if exitCode != 0 {
		errText := stderr
		if errText == "" {
			errText = stdout
		}
		failChunk := protocol.AgentChatChunkNotification{
			Name:     req.Name,
			Role:     "error",
			Text:     fmt.Sprintf("pi exited with code %d: %s", exitCode, errText),
			Sequence: 0,
			Final:    true,
		}
		failChunk.MessageID = h.persistReplyID(ctx, rec.ID, "error", failChunk.Text, msgID, 0, &failChunk)
		return send(protocol.Notification("agent.chat.chunk", failChunk))
	}

	text := stdout
	if text == "" {
		text = stderr
	}
	finalChunk := protocol.AgentChatChunkNotification{
		Name:     req.Name,
		Role:     "agent",
		Text:     text,
		Sequence: 0,
		Final:    true,
	}
	finalChunk.MessageID = h.persistReplyID(ctx, rec.ID, "agent", text, msgID, 0, &finalChunk)
	return send(protocol.Notification("agent.chat.chunk", finalChunk))
}

func (h *ChatHandler) replayDuplicateChat(
	ctx context.Context,
	req protocol.AgentChatSendParams,
	agentID string,
	msgID string,
	reqID any,
	send func(*protocol.Envelope) error,
) error {
	replies, err := h.persistedReplies(ctx, agentID, msgID)
	if err != nil {
		return send(protocol.ErrorResponse(reqID, protocol.ErrChatHistoryUnavailable,
			fmt.Sprintf("chat history unavailable: %v", err), nil))
	}

	ack := protocol.SuccessResponse(reqID, protocol.AgentChatSendResult{
		MessageID:  msgID,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err := send(ack); err != nil {
		return err
	}
	if len(replies) == 0 {
		replies, err = h.waitForPersistedReplies(ctx, agentID, msgID)
		if err != nil {
			chunk := protocol.AgentChatChunkNotification{
				Name:     req.Name,
				Role:     "error",
				Text:     fmt.Sprintf("chat request already accepted; reply is not available: %v", err),
				Sequence: 0,
				Final:    true,
			}
			return send(protocol.Notification("agent.chat.chunk", chunk))
		}
	}
	for i, row := range replies {
		chunk := protocol.AgentChatChunkNotification{
			Name:      req.Name,
			Role:      row.Role,
			Text:      row.Content,
			Sequence:  row.Sequence,
			Final:     i == len(replies)-1,
			MessageID: row.MessageID,
		}
		if err := send(protocol.Notification("agent.chat.chunk", chunk)); err != nil {
			return err
		}
	}
	return nil
}

func (h *ChatHandler) waitForPersistedReplies(ctx context.Context, agentID, parentID string) ([]store.ChatMessageRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, duplicateChatReplayWait)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		replies, err := h.persistedReplies(ctx, agentID, parentID)
		if err != nil {
			return nil, err
		}
		if len(replies) > 0 {
			return replies, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (h *ChatHandler) persistedReplies(ctx context.Context, agentID, parentID string) ([]store.ChatMessageRecord, error) {
	rows, err := h.repo.ListChatHistory(ctx, agentID, 0)
	if err != nil {
		return nil, err
	}
	replies := make([]store.ChatMessageRecord, 0, 1)
	for _, row := range rows {
		if row.ParentID == parentID && row.Role != "user" {
			replies = append(replies, row)
		}
	}
	return replies, nil
}

func (h *ChatHandler) persistReplyID(ctx context.Context, agentID, role, content, parentID string, sequence int, chunk *protocol.AgentChatChunkNotification) string {
	msgID := store.NewID()
	if err := h.persistChatMessage(ctx, agentID, role, content, parentID, msgID, sequence); err != nil {
		if h.log != nil {
			h.log.Warn("chat history reply persist failed", "agent_id", agentID, "msg_id", msgID, "err", err)
		}
		if chunk != nil {
			chunk.Role = "error"
			chunk.Text = fmt.Sprintf("chat history unavailable: %v", err)
			chunk.MessageID = ""
		}
		return ""
	}
	return msgID
}

func (h *ChatHandler) persistChatMessage(ctx context.Context, agentID, role, content, parentID, messageID string, sequence int) error {
	if h == nil || h.repo == nil {
		return errors.New("chat history repo unavailable")
	}
	if messageID == "" {
		messageID = store.NewID()
	}
	err := h.repo.InsertChatMessage(ctx, store.ChatMessageRecord{
		ID:        store.NewID(),
		AgentID:   agentID,
		Role:      role,
		Content:   content,
		ParentID:  parentID,
		MessageID: messageID,
		Sequence:  sequence,
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		if errors.Is(err, store.ErrNameTaken) {
			if h.log != nil {
				h.log.Debug("chat history duplicate ignored", "agent_id", agentID, "msg_id", messageID)
			}
		} else if h.log != nil {
			h.log.Warn("chat history persist failed", "agent_id", agentID, "msg_id", messageID, "err", err)
		}
		return err
	}
	return nil
}
