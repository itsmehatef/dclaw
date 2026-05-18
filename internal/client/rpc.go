// rpc.go is the real implementation of client.Client for v0.3+. It opens a
// Unix-domain-socket connection to dclawd, performs the JSON-RPC handshake,
// and exposes method wrappers that match the Client interface.
package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/itsmehatef/dclaw/internal/config"
	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/version"
)

// RPCClient is the production Client implementation.
type RPCClient struct {
	socket string

	mu     sync.Mutex
	conn   net.Conn
	dec    *json.Decoder
	enc    *json.Encoder
	nextID int64
}

// NewRPCClient constructs an RPCClient bound to the given socket path. It
// does NOT open the connection; Dial does that, and all methods dial lazily
// on first use.
func NewRPCClient(socket string) *RPCClient {
	return &RPCClient{socket: socket}
}

// Dial opens the socket and performs the handshake. Safe to call multiple
// times; subsequent calls are no-ops if already connected.
func (c *RPCClient) Dial(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return nil
	}

	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", c.socket)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.socket, err)
	}
	c.conn = conn
	c.dec = json.NewDecoder(conn)
	c.enc = json.NewEncoder(conn)

	// Handshake.
	if err := c.handshakeLocked(ctx); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// Close shuts the connection.
func (c *RPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *RPCClient) handshakeLocked(ctx context.Context) error {
	req := protocol.Envelope{
		JSONRPC: "2.0",
		Method:  "dclaw.handshake",
		ID:      c.newIDLocked(),
	}
	params, _ := json.Marshal(protocol.Handshake{
		ProtocolVersion:  protocol.Version,
		ComponentType:    protocol.ComponentType("cli"),
		ComponentVersion: version.Version,
		ComponentID:      uuid.NewString(),
	})
	req.Params = params
	if err := c.enc.Encode(&req); err != nil {
		return fmt.Errorf("handshake send: %w", err)
	}
	var resp protocol.Envelope
	if err := c.dec.Decode(&resp); err != nil {
		return fmt.Errorf("handshake recv: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("handshake rejected: %s", resp.Error.Message)
	}
	var hr protocol.HandshakeResult
	if err := json.Unmarshal(resp.Result, &hr); err != nil {
		return err
	}
	if !hr.Accepted {
		return errors.New("handshake rejected")
	}
	return nil
}

func (c *RPCClient) newIDLocked() int64 {
	return atomic.AddInt64(&c.nextID, 1)
}

// call sends a JSON-RPC request and unmarshals the response's Result into
// out (pass nil if no result is needed).
func (c *RPCClient) call(ctx context.Context, method string, params any, out any) error {
	if err := c.Dial(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	req := protocol.Request(int(c.newIDLocked()), method, params)
	if err := c.enc.Encode(req); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}
	var resp protocol.Envelope
	if err := c.dec.Decode(&resp); err != nil {
		return fmt.Errorf("recv %s: %w", method, err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// ---------- Client interface ----------

func (c *RPCClient) DaemonVersion(ctx context.Context) (string, error) {
	var out protocol.DaemonVersionResult
	if err := c.call(ctx, "daemon.version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

func (c *RPCClient) AgentCreate(ctx context.Context, a Agent) error {
	return c.call(ctx, "agent.create", protocol.AgentCreateParams{
		Name:                 a.Name,
		Image:                a.Image,
		Workspace:            a.Workspace,
		WorkspaceTrustReason: a.WorkspaceTrustReason,
		Env:                  mapToKVList(a.Env),
		Labels:               mapToKVList(a.Labels),
		Channel:              a.Channel,
	}, nil)
}

func (c *RPCClient) AgentList(ctx context.Context) ([]Agent, error) {
	var out protocol.AgentListResult
	if err := c.call(ctx, "agent.list", struct{}{}, &out); err != nil {
		return nil, err
	}
	agents := make([]Agent, 0, len(out.Agents))
	for _, a := range out.Agents {
		agents = append(agents, wireToAgent(a))
	}
	return agents, nil
}

func (c *RPCClient) AgentGet(ctx context.Context, name string) (Agent, error) {
	var out protocol.AgentGetResult
	if err := c.call(ctx, "agent.get", protocol.AgentByNameParams{Name: name}, &out); err != nil {
		return Agent{}, err
	}
	return wireToAgent(out.Agent), nil
}

func (c *RPCClient) AgentUpdate(ctx context.Context, a Agent) error {
	return c.call(ctx, "agent.update", protocol.AgentUpdateParams{
		Name:   a.Name,
		Image:  a.Image,
		Env:    mapToKVList(a.Env),
		Labels: mapToKVList(a.Labels),
	}, nil)
}

func (c *RPCClient) AgentDelete(ctx context.Context, name string) error {
	return c.call(ctx, "agent.delete", protocol.AgentByNameParams{Name: name}, nil)
}

func (c *RPCClient) AgentStart(ctx context.Context, name string) error {
	return c.call(ctx, "agent.start", protocol.AgentByNameParams{Name: name}, nil)
}

func (c *RPCClient) AgentStop(ctx context.Context, name string) error {
	return c.call(ctx, "agent.stop", protocol.AgentByNameParams{Name: name}, nil)
}

func (c *RPCClient) AgentRestart(ctx context.Context, name string) error {
	return c.call(ctx, "agent.restart", protocol.AgentByNameParams{Name: name}, nil)
}

func (c *RPCClient) AgentLogs(ctx context.Context, name string, tail int, follow bool) (<-chan string, error) {
	// v0.3 alpha.1 implements bulk fetch only. follow=true is a tight-loop
	// poll over bulk fetches every 2s until ctx is cancelled. beta.1 replaces
	// with the notification-stream variant from internal/daemon/logs.go.
	if !follow {
		var out protocol.AgentLogsResult
		if err := c.call(ctx, "agent.logs", protocol.AgentLogsParams{Name: name, Tail: tail}, &out); err != nil {
			return nil, err
		}
		ch := make(chan string, len(out.Lines))
		for _, l := range out.Lines {
			ch <- l
		}
		close(ch)
		return ch, nil
	}
	return c.agentLogsFollowPoll(ctx, name, tail)
}

func (c *RPCClient) AgentExec(ctx context.Context, name string, argv []string) (int, error) {
	var out protocol.AgentExecResult
	if err := c.call(ctx, "agent.exec", protocol.AgentExecParams{Name: name, Argv: argv}, &out); err != nil {
		return 1, err
	}
	// Stream stdout/stderr to the caller's stdio via a package-level hook.
	if ExecStdoutWriter != nil {
		_, _ = ExecStdoutWriter.Write([]byte(out.Stdout))
	}
	if ExecStderrWriter != nil {
		_, _ = ExecStderrWriter.Write([]byte(out.Stderr))
	}
	return out.ExitCode, nil
}

func (c *RPCClient) ChannelCreate(ctx context.Context, ch Channel) error {
	return c.call(ctx, "channel.create", protocol.ChannelCreateParams{
		Name:   ch.Name,
		Type:   ch.Type,
		Config: ch.Config,
	}, nil)
}

func (c *RPCClient) ChannelList(ctx context.Context) ([]Channel, error) {
	var out protocol.ChannelListResult
	if err := c.call(ctx, "channel.list", struct{}{}, &out); err != nil {
		return nil, err
	}
	chs := make([]Channel, 0, len(out.Channels))
	for _, c := range out.Channels {
		chs = append(chs, Channel{Name: c.Name, Type: c.Type, Config: c.Config})
	}
	return chs, nil
}

func (c *RPCClient) ChannelGet(ctx context.Context, name string) (Channel, error) {
	var out protocol.ChannelGetResult
	if err := c.call(ctx, "channel.get", protocol.ChannelByNameParams{Name: name}, &out); err != nil {
		return Channel{}, err
	}
	return Channel{Name: out.Channel.Name, Type: out.Channel.Type, Config: out.Channel.Config}, nil
}

func (c *RPCClient) ChannelDelete(ctx context.Context, name string) error {
	return c.call(ctx, "channel.delete", protocol.ChannelByNameParams{Name: name}, nil)
}

func (c *RPCClient) ChannelAttach(ctx context.Context, agentName, channelName string) error {
	return c.call(ctx, "channel.attach", protocol.ChannelAttachParams{AgentName: agentName, ChannelName: channelName}, nil)
}

func (c *RPCClient) ChannelDetach(ctx context.Context, agentName, channelName string) error {
	return c.call(ctx, "channel.detach", protocol.ChannelAttachParams{AgentName: agentName, ChannelName: channelName}, nil)
}

func (c *RPCClient) DaemonStart(ctx context.Context) error {
	// The CLI handles `daemon start` by forking dclawd directly rather than by
	// calling into the socket (the socket doesn't exist yet!). This method
	// exists only to satisfy the Client interface; the CLI does not call it.
	return errors.New("DaemonStart is handled by the CLI, not the RPC client")
}

func (c *RPCClient) DaemonStop(ctx context.Context) error {
	// Request a graceful shutdown via an RPC notification, then the CLI
	// fallback (SIGTERM to pid in pidfile) takes over if the RPC fails.
	return c.call(ctx, "daemon.shutdown", struct{}{}, nil)
}

func (c *RPCClient) DaemonStatus(ctx context.Context) (string, error) {
	var out protocol.DaemonStatusResult
	if err := c.call(ctx, "daemon.status", struct{}{}, &out); err != nil {
		return "", err
	}
	return fmt.Sprintf("agents=%d running=%d channels=%d", out.Agents, out.Running, out.Channels), nil
}

// ChatHistoryList returns persisted chat history for one agent.
func (c *RPCClient) ChatHistoryList(ctx context.Context, agentName string, limit int) ([]protocol.ChatMessage, error) {
	var out protocol.ChatHistoryListResult
	if err := c.call(ctx, "agent.chat.history.list", protocol.ChatHistoryListParams{Name: agentName, Limit: limit}, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

// ChatHistoryAppend appends one history row through the daemon. The normal
// chat path persists directly server-side; this wrapper is useful for tests
// and future operator tools.
func (c *RPCClient) ChatHistoryAppend(ctx context.Context, params protocol.ChatHistoryAppendParams) error {
	return c.call(ctx, "agent.chat.history.append", params, nil)
}

// Ensure RPCClient implements Client at compile time.
var _ Client = (*RPCClient)(nil)

// ---------- helpers ----------

// ExecStdoutWriter / ExecStderrWriter are package-level sinks set by the CLI
// so that AgentExec can stream stdio to the user's terminal. Set from
// internal/cli before calling AgentExec.
var (
	ExecStdoutWriter io.Writer
	ExecStderrWriter io.Writer
)

// DefaultSocketPath returns the resolved socket path for this host. It
// delegates to internal/config so every entrypoint (CLI, TUI, daemon) shares
// one resolution ladder. Falls back to /tmp/dclaw.sock when even the home
// directory cannot be determined — matches the legacy behavior callers relied on.
func DefaultSocketPath() string {
	paths, err := config.Resolve("", "")
	if err != nil {
		return "/tmp/dclaw.sock"
	}
	return config.DefaultSocketPath(paths.StateDir)
}

func (c *RPCClient) agentLogsFollowPoll(ctx context.Context, name string, tail int) (<-chan string, error) {
	out := make(chan string, 256)
	go func() {
		defer close(out)
		var last string
		for {
			if ctx.Err() != nil {
				return
			}
			var res protocol.AgentLogsResult
			err := c.call(ctx, "agent.logs", protocol.AgentLogsParams{Name: name, Tail: tail}, &res)
			if err != nil {
				return
			}
			for _, l := range res.Lines {
				if l == last {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- l:
					last = l
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return out, nil
}

func mapToKVList(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func wireToAgent(a protocol.Agent) Agent {
	return Agent{
		Name:                 a.Name,
		Image:                a.Image,
		Channel:              "",
		Workspace:            a.Workspace,
		WorkspaceTrustReason: a.WorkspaceTrustReason,
		Env:                  a.Env,
		Labels:               a.Labels,
		Status:               a.Status,
	}
}

// ---------- Chat streaming (alpha.3) ----------

// ChatChunkEvent is one event delivered on the channel returned by ChatSend.
// When Final is true the channel is closed immediately after this event.
// When Err is non-nil the stream broke before Final arrived.
type ChatChunkEvent struct {
	Role      string // "agent" | "system" | "error"
	Text      string // incremental delta text
	Sequence  int
	Final     bool
	MessageID string
	Err       error
}

// LogLineEvent is one event delivered on the channel returned by LogsStream.
type LogLineEvent struct {
	Name      string
	Line      string
	Stream    string
	Timestamp string
	Err       error
}

// LogsStream opens a dedicated connection and subscribes to agent.log.line
// notifications produced by agent.logs.stream.
func (c *RPCClient) LogsStream(ctx context.Context, agentName string, tail int) (<-chan LogLineEvent, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.socket)
	if err != nil {
		return nil, fmt.Errorf("logs dial: %w", err)
	}

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	hsParams, _ := json.Marshal(protocol.Handshake{
		ProtocolVersion:  protocol.Version,
		ComponentType:    protocol.ComponentType("cli"),
		ComponentVersion: version.Version,
		ComponentID:      uuid.NewString(),
	})
	hsEnv := protocol.Envelope{
		JSONRPC: "2.0",
		Method:  "dclaw.handshake",
		ID:      int64(1),
	}
	hsEnv.Params = hsParams
	if err := enc.Encode(&hsEnv); err != nil {
		conn.Close()
		return nil, fmt.Errorf("logs handshake send: %w", err)
	}
	var hsResp protocol.Envelope
	if err := dec.Decode(&hsResp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("logs handshake recv: %w", err)
	}
	if hsResp.Error != nil {
		conn.Close()
		return nil, fmt.Errorf("logs handshake rejected: %s", hsResp.Error.Message)
	}

	reqEnv := protocol.Request(2, "agent.logs.stream", protocol.LogsStreamParams{
		Name:   agentName,
		Tail:   tail,
		Follow: true,
	})
	if err := enc.Encode(reqEnv); err != nil {
		conn.Close()
		return nil, fmt.Errorf("logs stream send: %w", err)
	}

	var ackEnv protocol.Envelope
	if err := dec.Decode(&ackEnv); err != nil {
		conn.Close()
		return nil, fmt.Errorf("logs stream ack recv: %w", err)
	}
	if ackEnv.Error != nil {
		conn.Close()
		if isMethodNotFoundRPC(ackEnv.Error) {
			return c.logsStreamPollFallback(ctx, agentName, tail)
		}
		return nil, ackEnv.Error
	}

	ch := make(chan LogLineEvent, 128)
	go func() {
		defer conn.Close()
		defer close(ch)
		for {
			if ctx.Err() != nil {
				return
			}
			var env protocol.Envelope
			if err := dec.Decode(&env); err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
					return
				}
				select {
				case ch <- LogLineEvent{Err: fmt.Errorf("logs stream read: %w", err)}:
				case <-ctx.Done():
				}
				return
			}
			if env.Method != "agent.log.line" {
				if env.Method == "agent.log.done" {
					return
				}
				if env.Method == "agent.log.error" {
					var streamErr protocol.LogsStreamErrorNotification
					if err := json.Unmarshal(env.Params, &streamErr); err != nil {
						select {
						case ch <- LogLineEvent{Err: fmt.Errorf("log error decode: %w", err)}:
						case <-ctx.Done():
						}
						return
					}
					select {
					case ch <- LogLineEvent{Name: streamErr.Name, Err: errors.New(streamErr.Error)}:
					case <-ctx.Done():
					}
					return
				}
				continue
			}
			var line protocol.LogsStreamLineNotification
			if err := json.Unmarshal(env.Params, &line); err != nil {
				select {
				case ch <- LogLineEvent{Err: fmt.Errorf("log line decode: %w", err)}:
				case <-ctx.Done():
				}
				return
			}
			event := LogLineEvent{
				Name:      line.Name,
				Line:      line.Line,
				Stream:    line.Stream,
				Timestamp: line.Timestamp,
			}
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (c *RPCClient) logsStreamPollFallback(ctx context.Context, agentName string, tail int) (<-chan LogLineEvent, error) {
	lines, err := c.AgentLogs(ctx, agentName, tail, true)
	if err != nil {
		return nil, err
	}
	out := make(chan LogLineEvent, 256)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-lines:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				case out <- LogLineEvent{Name: agentName, Line: line, Stream: "stdout"}:
				}
			}
		}
	}()
	return out, nil
}

func isMethodNotFoundRPC(err *protocol.RPCError) bool {
	return err != nil && err.Code == protocol.ErrMethodNotFound
}

// ChatSend sends agent.chat.send to the daemon and returns a channel that
// yields agent.chat.chunk notifications until Final=true or ctx is cancelled.
//
// ChatSend opens a SECOND dedicated connection for the stream so it does not
// contend with the shared encoder/decoder on the primary connection. The
// dedicated connection is closed when the channel drains or ctx is cancelled.
func (c *RPCClient) ChatSend(ctx context.Context, agentName, content, parentID string) (<-chan ChatChunkEvent, error) {
	msgID := chatMessageID(agentName, parentID, content)

	// Dial dedicated stream connection.
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.socket)
	if err != nil {
		return nil, fmt.Errorf("chat dial: %w", err)
	}

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	// Handshake on dedicated connection.
	hsParams, _ := json.Marshal(protocol.Handshake{
		ProtocolVersion:  protocol.Version,
		ComponentType:    protocol.ComponentType("cli"),
		ComponentVersion: version.Version,
		ComponentID:      uuid.NewString(),
	})
	hsEnv := protocol.Envelope{
		JSONRPC: "2.0",
		Method:  "dclaw.handshake",
		ID:      int64(1),
	}
	hsEnv.Params = hsParams
	if err := enc.Encode(&hsEnv); err != nil {
		conn.Close()
		return nil, fmt.Errorf("chat handshake send: %w", err)
	}
	var hsResp protocol.Envelope
	if err := dec.Decode(&hsResp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("chat handshake recv: %w", err)
	}
	if hsResp.Error != nil {
		conn.Close()
		return nil, fmt.Errorf("chat handshake rejected: %s", hsResp.Error.Message)
	}

	// Send agent.chat.send.
	reqEnv := protocol.Request(2, "agent.chat.send", protocol.AgentChatSendParams{
		Name:      agentName,
		Content:   content,
		ParentID:  parentID,
		MessageID: msgID,
	})
	if err := enc.Encode(reqEnv); err != nil {
		conn.Close()
		return nil, fmt.Errorf("chat send: %w", err)
	}

	// Read the synchronous ack response (JSON-RPC result for id=2).
	var ackEnv protocol.Envelope
	if err := dec.Decode(&ackEnv); err != nil {
		conn.Close()
		return nil, fmt.Errorf("chat ack recv: %w", err)
	}
	if ackEnv.Error != nil {
		conn.Close()
		return nil, ackEnv.Error
	}

	// Drain agent.chat.chunk notifications asynchronously.
	ch := make(chan ChatChunkEvent, 64)
	go func() {
		defer conn.Close()
		defer close(ch)
		for {
			if ctx.Err() != nil {
				return
			}
			var env protocol.Envelope
			if err := dec.Decode(&env); err != nil {
				select {
				case ch <- ChatChunkEvent{Err: fmt.Errorf("stream read: %w", err)}:
				case <-ctx.Done():
				}
				return
			}
			if env.Method != "agent.chat.chunk" {
				continue
			}
			var chunk protocol.AgentChatChunkNotification
			if err := json.Unmarshal(env.Params, &chunk); err != nil {
				select {
				case ch <- ChatChunkEvent{Err: fmt.Errorf("chunk decode: %w", err)}:
				case <-ctx.Done():
				}
				return
			}
			event := ChatChunkEvent{
				Role:      chunk.Role,
				Text:      chunk.Text,
				Sequence:  chunk.Sequence,
				Final:     chunk.Final,
				MessageID: chunk.MessageID,
			}
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
			if chunk.Final {
				return
			}
		}
	}()

	return ch, nil
}

// chatMessageID computes the content-addressed ID for a chat message.
// ID = lower-hex( sha256( agentName + "|" + parentID + "|" + content ) )
func chatMessageID(agentName, parentID, content string) string {
	h := sha256.New()
	h.Write([]byte(agentName))
	h.Write([]byte("|"))
	h.Write([]byte(parentID))
	h.Write([]byte("|"))
	h.Write([]byte(content))
	return fmt.Sprintf("%x", h.Sum(nil))
}
