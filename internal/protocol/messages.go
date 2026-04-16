// Package protocol defines the wire-protocol message shapes for dclaw. The
// authoritative spec is docs/wire-protocol-spec.md. This file is the Go
// representation of the 23 spec message types plus the CLI<->daemon
// sub-boundary methods added in v0.3 (agent.*, channel.*, daemon.*).
package protocol

import (
	"encoding/json"
)

// ---------- JSON-RPC envelope ----------

// Envelope is the wire shape for any JSON-RPC 2.0 message (request, response,
// or notification). Exactly one of Method / (Result or Error) is populated;
// notifications lack an ID.
type Envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      any             `json:"id,omitempty"`
}

// RPCError is the JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes plus dclaw custom codes.
// See wire-protocol-spec.md Section 8.
const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603

	// dclaw custom
	ErrAgentNotFound     = -32001 // reused semantic: "worker not found" in spec; dclaw v0.3 means agent
	ErrAgentNotRunning   = -32002
	ErrQuotaExceeded     = -32003
	ErrSpawnFailed       = -32004
	ErrTimeout           = -32005
	ErrChannelNotReady   = -32006
)

// ---------- Handshake ----------

// (Handshake + HandshakeResult live in protocol.go from Phase 2; we don't
// duplicate them here.)

// ---------- CLI<->daemon methods (NEW in v0.3) ----------

// DaemonStatusResult is the result of `daemon.status`.
type DaemonStatusResult struct {
	Agents   int `json:"agents"`
	Running  int `json:"running"`
	Channels int `json:"channels"`
}

// DaemonVersionResult is the result of `daemon.version`.
type DaemonVersionResult struct {
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocol_version"`
}

// AckResult is a trivial {"ack": true} result shared by idempotent mutations.
type AckResult struct {
	Ack bool `json:"ack"`
}

// Agent is the wire projection of an agent record.
type Agent struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Status      string            `json:"status"`
	ContainerID string            `json:"container_id,omitempty"`
	Workspace   string            `json:"workspace,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   int64             `json:"created_at"`
	UpdatedAt   int64             `json:"updated_at"`
}

// Channel is the wire projection of a channel record.
type Channel struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Config string `json:"config,omitempty"`
}

// Event is a log-style event record attached to an agent.
type Event struct {
	Type      string `json:"type"`
	Data      string `json:"data"`
	Timestamp int64  `json:"timestamp"`
}

// AgentCreateParams is the request body for `agent.create`.
type AgentCreateParams struct {
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	Workspace string   `json:"workspace,omitempty"`
	Env       []string `json:"env,omitempty"`    // KEY=VAL
	Labels    []string `json:"labels,omitempty"` // KEY=VAL
	Channel   string   `json:"channel,omitempty"`
}

// AgentCreateResult is the response for `agent.create`.
type AgentCreateResult struct {
	Agent Agent `json:"agent"`
}

// AgentByNameParams is used by any RPC that takes just a name.
type AgentByNameParams struct {
	Name string `json:"name"`
}

// AgentListResult is the response for `agent.list`.
type AgentListResult struct {
	Agents []Agent `json:"agents"`
}

// AgentGetResult is the response for `agent.get` / `agent.update`.
type AgentGetResult struct {
	Agent Agent `json:"agent"`
}

// AgentDescribeResult is the response for `agent.describe`.
type AgentDescribeResult struct {
	Agent  Agent   `json:"agent"`
	Events []Event `json:"events"`
}

// AgentUpdateParams is the request body for `agent.update`.
type AgentUpdateParams struct {
	Name   string   `json:"name"`
	Image  string   `json:"image,omitempty"`
	Env    []string `json:"env,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

// AgentLogsParams is the request body for `agent.logs`.
type AgentLogsParams struct {
	Name   string `json:"name"`
	Tail   int    `json:"tail,omitempty"`
	Follow bool   `json:"follow,omitempty"`
}

// AgentLogsResult is the response for `agent.logs` (bulk fetch).
type AgentLogsResult struct {
	Lines []string `json:"lines"`
}

// AgentExecParams is the request body for `agent.exec`.
type AgentExecParams struct {
	Name string   `json:"name"`
	Argv []string `json:"argv"`
}

// AgentExecResult is the response for `agent.exec`.
type AgentExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// ChannelCreateParams is the request body for `channel.create`.
type ChannelCreateParams struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Config string `json:"config,omitempty"`
}

// ChannelByNameParams is used by any channel RPC that takes just a name.
type ChannelByNameParams struct {
	Name string `json:"name"`
}

// ChannelListResult is the response for `channel.list`.
type ChannelListResult struct {
	Channels []Channel `json:"channels"`
}

// ChannelGetResult is the response for `channel.get` / `channel.create`.
type ChannelGetResult struct {
	Channel Channel `json:"channel"`
}

// ChannelAttachParams is the request body for `channel.attach` / `channel.detach`.
type ChannelAttachParams struct {
	AgentName   string `json:"agent_name"`
	ChannelName string `json:"channel_name"`
}

// ---------- Wire spec's 23 message types (boundary 1, 2, 3) ----------
//
// These are declared for completeness and for use by later phases. The v0.3
// daemon does not route boundary 1 or boundary 3 traffic, but the types must
// be present so protocol tests can unmarshal example payloads from the spec.

// ChannelMessageReceived is the payload for `channel.message_received` (boundary 1, plugin -> main).
type ChannelMessageReceived struct {
	ChannelID   string       `json:"channel_id"`
	MessageID   string       `json:"message_id"`
	UserID      string       `json:"user_id"`
	UserName    string       `json:"user_name"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
	Timestamp   string       `json:"timestamp"`
	ChannelType string       `json:"channel_type"`
	ReplyTo     string       `json:"reply_to,omitempty"`
}

// Attachment is a file attachment reference inside ChannelMessageReceived.
type Attachment struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

// ChannelReactionReceived is the payload for `channel.reaction_received`.
type ChannelReactionReceived struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	UserID    string `json:"user_id"`
	Emoji     string `json:"emoji"`
}

// ChannelStatusChanged is the payload for `channel.status_changed`.
type ChannelStatusChanged struct {
	PluginName   string `json:"plugin_name"`
	Version      string `json:"version"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// ChannelSendMessage is the payload for `channel.send_message`.
type ChannelSendMessage struct {
	ChannelID string   `json:"channel_id"`
	Text      string   `json:"text"`
	ReplyTo   string   `json:"reply_to,omitempty"`
	Files     []string `json:"files,omitempty"`
}

// ChannelSendReaction is the payload for `channel.send_reaction`.
type ChannelSendReaction struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
}

// ChannelEditMessage is the payload for `channel.edit_message`.
type ChannelEditMessage struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	NewText   string `json:"new_text"`
}

// ChannelFetchHistory is the payload for `channel.fetch_history`.
type ChannelFetchHistory struct {
	ChannelID string `json:"channel_id"`
	Limit     int    `json:"limit"`
	Before    string `json:"before,omitempty"`
}

// WorkerSpawn is the payload for `worker.spawn` (boundary 2, main -> dispatcher).
type WorkerSpawn struct {
	Task            string            `json:"task"`
	Workspace       string            `json:"workspace"`
	Model           string            `json:"model,omitempty"`
	Tools           []string          `json:"tools,omitempty"`
	EgressAllowlist []string          `json:"egress_allowlist,omitempty"`
	TimeoutSeconds  int               `json:"timeout_seconds,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// WorkerSpawnResult is the response for `worker.spawn`.
type WorkerSpawnResult struct {
	WorkerID    string `json:"worker_id"`
	ContainerID string `json:"container_id"`
}

// WorkerSendMessage is the payload for `worker.send_message`.
type WorkerSendMessage struct {
	WorkerID string `json:"worker_id"`
	Message  string `json:"message"`
}

// WorkerGetStatus is the payload for `worker.get_status`.
type WorkerGetStatus struct {
	WorkerID string `json:"worker_id"`
}

// WorkerStatusResult is the response for `worker.get_status`.
type WorkerStatusResult struct {
	Status          string  `json:"status"`
	ExitCode        *int    `json:"exit_code"`
	StartedAt       string  `json:"started_at"`
	ElapsedSeconds  float64 `json:"elapsed_seconds"`
	CostUSD         float64 `json:"cost_usd"`
}

// WorkerListParams is the payload for `worker.list`.
type WorkerListParams struct {
	StatusFilter string `json:"status_filter,omitempty"`
}

// WorkerSummary is a short projection of a worker row.
type WorkerSummary struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	Task      string  `json:"task"`
	StartedAt string  `json:"started_at"`
	CostUSD   float64 `json:"cost_usd"`
}

// WorkerListResult is the response for `worker.list`.
type WorkerListResult struct {
	Workers []WorkerSummary `json:"workers"`
}

// WorkerKillParams is the payload for `worker.kill`.
type WorkerKillParams struct {
	WorkerID string `json:"worker_id"`
	Reason   string `json:"reason,omitempty"`
}

// WorkerKillResult is the response for `worker.kill`.
type WorkerKillResult struct {
	Killed bool `json:"killed"`
}

// WorkerGetOutput is the payload for `worker.get_output`.
type WorkerGetOutput struct {
	WorkerID string `json:"worker_id"`
}

// WorkerGetOutputResult is the response for `worker.get_output`.
type WorkerGetOutputResult struct {
	Output          string  `json:"output"`
	ExitCode        int     `json:"exit_code"`
	DurationSeconds float64 `json:"duration_seconds"`
	CostUSD         float64 `json:"cost_usd"`
}

// WorkerStatusChanged is the notification payload for `worker.status_changed`.
type WorkerStatusChanged struct {
	WorkerID  string  `json:"worker_id"`
	OldStatus string  `json:"old_status"`
	NewStatus string  `json:"new_status"`
	Output    string  `json:"output,omitempty"`
	Error     string  `json:"error,omitempty"`
	CostUSD   float64 `json:"cost_usd"`
}

// WorkerMessage is the notification payload for `worker.message`.
type WorkerMessage struct {
	WorkerID  string `json:"worker_id"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// QuotaWarning is the notification payload for `quota.warning`.
type QuotaWarning struct {
	Metric  string  `json:"metric"`
	Current float64 `json:"current"`
	Limit   float64 `json:"limit"`
	Percent float64 `json:"percent"`
}

// MainReport is the payload for `main.report` (boundary 3, worker -> dispatcher).
type MainReport struct {
	Message string `json:"message"`
	Type    string `json:"type"` // progress|result|error|question
}

// MainAsk is the payload for `main.ask`.
type MainAsk struct {
	Question       string `json:"question"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// MainAskResult is the response for `main.ask`.
type MainAskResult struct {
	Answer string `json:"answer"`
}

// WorkerHeartbeat is the payload for `worker.heartbeat`.
type WorkerHeartbeat struct {
	WorkerID       string  `json:"worker_id"`
	MemoryMB       int     `json:"memory_mb"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
}

// WorkerHeartbeatResult is the response for `worker.heartbeat`.
type WorkerHeartbeatResult struct {
	Continue bool `json:"continue"`
}

// WorkerDone is the payload for `worker.done`.
type WorkerDone struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

// WorkerMessageFromMain is the notification payload for `worker.message_from_main`.
type WorkerMessageFromMain struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// WorkerKillSignal is the notification payload for `worker.kill_signal`.
type WorkerKillSignal struct {
	Reason string `json:"reason"` // timeout|user_killed|quota_exceeded
}
