package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

// Router dispatches JSON-RPC methods to handler functions. Methods are
// organized by subject (agent.*, channel.*, daemon.*, worker.*). Unknown
// methods yield JSON-RPC -32601 (method not found).
type Router struct {
	log      *slog.Logger
	repo     *store.Repo
	docker   *sandbox.DockerClient
	lifecycle *Lifecycle
	handlers map[string]handlerFunc
}

type handlerFunc func(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError)

// NewRouter constructs and registers all v0.3 handlers.
func NewRouter(log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *Router {
	r := &Router{
		log:    log,
		repo:   repo,
		docker: docker,
	}
	r.lifecycle = NewLifecycle(log, repo, docker)

	r.handlers = map[string]handlerFunc{
		// Daemon health
		"daemon.ping":    r.handleDaemonPing,
		"daemon.status":  r.handleDaemonStatus,
		"daemon.version": r.handleDaemonVersion,

		// Agent CRUD
		"agent.create":   r.handleAgentCreate,
		"agent.list":     r.handleAgentList,
		"agent.get":      r.handleAgentGet,
		"agent.describe": r.handleAgentDescribe,
		"agent.update":   r.handleAgentUpdate,
		"agent.delete":   r.handleAgentDelete,
		"agent.start":    r.handleAgentStart,
		"agent.stop":     r.handleAgentStop,
		"agent.restart":  r.handleAgentRestart,
		"agent.logs":     r.handleAgentLogs,
		"agent.exec":     r.handleAgentExec,

		// Channel CRUD (record-only in v0.3)
		"channel.create": r.handleChannelCreate,
		"channel.list":   r.handleChannelList,
		"channel.get":    r.handleChannelGet,
		"channel.delete": r.handleChannelDelete,
		"channel.attach": r.handleChannelAttach,
		"channel.detach": r.handleChannelDetach,
	}

	return r
}

// Dispatch routes an incoming envelope to its handler and returns the response
// envelope (or nil if the incoming message was a notification).
func (r *Router) Dispatch(ctx context.Context, env *protocol.Envelope) *protocol.Envelope {
	if env.JSONRPC != "2.0" {
		return protocol.ErrorResponse(env.ID, protocol.ErrInvalidRequest, "jsonrpc must be \"2.0\"", nil)
	}
	h, ok := r.handlers[env.Method]
	if !ok {
		if env.ID == nil {
			return nil // notification to unknown method: silently drop
		}
		return protocol.ErrorResponse(env.ID, protocol.ErrMethodNotFound,
			fmt.Sprintf("method not found: %s", env.Method),
			map[string]any{"method": env.Method})
	}

	result, rpcErr := h(ctx, env.Params)

	// Notifications get no response.
	if env.ID == nil {
		return nil
	}
	if rpcErr != nil {
		return protocol.ErrorResponse(env.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	return protocol.SuccessResponse(env.ID, result)
}

// ---------- daemon.* ----------

func (r *Router) handleDaemonPing(ctx context.Context, _ json.RawMessage) (any, *protocol.RPCError) {
	return map[string]any{"pong": true}, nil
}

func (r *Router) handleDaemonStatus(ctx context.Context, _ json.RawMessage) (any, *protocol.RPCError) {
	agents, err := r.repo.ListAgents(ctx)
	if err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInternal, Message: err.Error()}
	}
	channels, err := r.repo.ListChannels(ctx)
	if err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInternal, Message: err.Error()}
	}
	running := 0
	for _, a := range agents {
		if a.Status == "running" {
			running++
		}
	}
	return protocol.DaemonStatusResult{
		Agents:   len(agents),
		Running:  running,
		Channels: len(channels),
	}, nil
}

func (r *Router) handleDaemonVersion(ctx context.Context, _ json.RawMessage) (any, *protocol.RPCError) {
	return protocol.DaemonVersionResult{
		Version:         versionString(),
		ProtocolVersion: protocol.Version,
	}, nil
}

// ---------- agent.* ----------

func (r *Router) handleAgentCreate(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentCreateParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	a, err := r.lifecycle.AgentCreate(ctx, req)
	if err != nil {
		return nil, mapError(err)
	}
	return protocol.AgentCreateResult{Agent: a}, nil
}

func (r *Router) handleAgentList(ctx context.Context, _ json.RawMessage) (any, *protocol.RPCError) {
	items, err := r.lifecycle.AgentList(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return protocol.AgentListResult{Agents: items}, nil
}

func (r *Router) handleAgentGet(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentByNameParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	a, err := r.lifecycle.AgentGet(ctx, req.Name)
	if err != nil {
		return nil, mapError(err)
	}
	return protocol.AgentGetResult{Agent: a}, nil
}

func (r *Router) handleAgentDescribe(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentByNameParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	d, err := r.lifecycle.AgentDescribe(ctx, req.Name)
	if err != nil {
		return nil, mapError(err)
	}
	return d, nil
}

func (r *Router) handleAgentUpdate(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentUpdateParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	a, err := r.lifecycle.AgentUpdate(ctx, req)
	if err != nil {
		return nil, mapError(err)
	}
	return protocol.AgentGetResult{Agent: a}, nil
}

func (r *Router) handleAgentDelete(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentByNameParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	if err := r.lifecycle.AgentDelete(ctx, req.Name); err != nil {
		return nil, mapError(err)
	}
	return protocol.AckResult{Ack: true}, nil
}

func (r *Router) handleAgentStart(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentByNameParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	if err := r.lifecycle.AgentStart(ctx, req.Name); err != nil {
		return nil, mapError(err)
	}
	return protocol.AckResult{Ack: true}, nil
}

func (r *Router) handleAgentStop(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentByNameParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	if err := r.lifecycle.AgentStop(ctx, req.Name); err != nil {
		return nil, mapError(err)
	}
	return protocol.AckResult{Ack: true}, nil
}

func (r *Router) handleAgentRestart(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentByNameParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	if err := r.lifecycle.AgentRestart(ctx, req.Name); err != nil {
		return nil, mapError(err)
	}
	return protocol.AckResult{Ack: true}, nil
}

func (r *Router) handleAgentLogs(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	// Synchronous bulk fetch for v0.3. Streaming follow mode uses a separate
	// long-lived RPC `agent.logs.stream` which returns chunk notifications;
	// see internal/daemon/logs.go for the stream variant.
	var req protocol.AgentLogsParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	chunks, err := r.lifecycle.AgentLogsBulk(ctx, req.Name, req.Tail)
	if err != nil {
		return nil, mapError(err)
	}
	return protocol.AgentLogsResult{Lines: chunks}, nil
}

func (r *Router) handleAgentExec(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.AgentExecParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	res, err := r.lifecycle.AgentExec(ctx, req)
	if err != nil {
		return nil, mapError(err)
	}
	return res, nil
}

// ---------- channel.* ----------

func (r *Router) handleChannelCreate(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.ChannelCreateParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	c, err := r.lifecycle.ChannelCreate(ctx, req)
	if err != nil {
		return nil, mapError(err)
	}
	return protocol.ChannelGetResult{Channel: c}, nil
}

func (r *Router) handleChannelList(ctx context.Context, _ json.RawMessage) (any, *protocol.RPCError) {
	items, err := r.repo.ListChannels(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return protocol.ChannelListResult{Channels: items}, nil
}

func (r *Router) handleChannelGet(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.ChannelByNameParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	c, err := r.repo.GetChannel(ctx, req.Name)
	if err != nil {
		return nil, mapError(err)
	}
	return protocol.ChannelGetResult{Channel: c}, nil
}

func (r *Router) handleChannelDelete(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.ChannelByNameParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	if err := r.repo.DeleteChannel(ctx, req.Name); err != nil {
		return nil, mapError(err)
	}
	return protocol.AckResult{Ack: true}, nil
}

func (r *Router) handleChannelAttach(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.ChannelAttachParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	if err := r.repo.AttachChannel(ctx, req.AgentName, req.ChannelName); err != nil {
		return nil, mapError(err)
	}
	return protocol.AckResult{Ack: true}, nil
}

func (r *Router) handleChannelDetach(ctx context.Context, params json.RawMessage) (any, *protocol.RPCError) {
	var req protocol.ChannelAttachParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: err.Error()}
	}
	if err := r.repo.DetachChannel(ctx, req.AgentName, req.ChannelName); err != nil {
		return nil, mapError(err)
	}
	return protocol.AckResult{Ack: true}, nil
}

// ---------- helpers ----------

// mapError translates a lifecycle-layer error into a wire-protocol RPCError.
func mapError(err error) *protocol.RPCError {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		return &protocol.RPCError{Code: protocol.ErrAgentNotFound, Message: msg}
	case strings.Contains(msg, "already exists"):
		return &protocol.RPCError{Code: protocol.ErrInvalidParams, Message: msg}
	case strings.Contains(msg, "docker"):
		return &protocol.RPCError{Code: protocol.ErrSpawnFailed, Message: msg}
	default:
		return &protocol.RPCError{Code: protocol.ErrInternal, Message: msg}
	}
}

// versionString is a thin indirection so tests can override daemon version
// reporting. In production it returns the injected ldflags build string.
var versionString = func() string { return "v0.3.0-daemon" }
