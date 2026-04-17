# Phase 3 Alpha.3 Plan — v0.3.0-alpha.3 TUI Chat Mode + Wire Protocol Streaming

**Goal:** Add interactive chat to the dclaw TUI and implement the `agent.chat.send` / `agent.chat.chunk` streaming protocol round-trip. `dclaw agent attach <name>` now opens ViewChat. In-memory message history only; persistence deferred to beta.1.

**Prereq:** `v0.3.0-alpha.2` tagged at commit `1888ce5`. All alpha.2 checklist items green. Docker daemon reachable. `go 1.25.0` installed.

---

## 0. Status

**SHIPPED (2026-04-17) as `v0.3.0-alpha.3`.** Commits on top of `v0.3.0-alpha.2` (`1888ce5`):

- `88eea62` — alpha.3(A): wire protocol chat types + ChatSend streaming client
- `35bfe31` — alpha.3(B): daemon ChatHandler + router streaming dispatch
- `5864dc0` — alpha.3(C): TUI ViewChat (viewport + textarea + state machine)
- `9acdd27` — alpha.3(D): agent_attach → ViewChat, smoke script Test 12
- `865d43a` — alpha.3: fix container keep-alive (tini+tail, readiness check, liveness poll)

| Field | Value |
|---|---|
| **Tag** | `v0.3.0-alpha.3` (shipped) |
| **Branch** | `main` |
| **Base commit** | `1888ce5` (`v0.3.0-alpha.2`) |
| **Est. duration** | 3–4 days |
| **Prereqs** | v0.3.0-alpha.2 green; bubbles v1.0.0 already in go.mod |
| **Next tag** | `v0.3.0-beta.1` (log tail + event stream + polish) |

Alpha.3 sits between the TUI scaffolding (alpha.2) and the observability/polish phase (beta.1). It adds the first interactive feature: typing a message to an agent and watching a streamed response arrive token-by-token.

---

## 1. Overview

Alpha.2 delivered the fleet dashboard: list, detail, describe, help, and noDaemon views. Alpha.3 adds the fifth view — **ViewChat** — and the full round-trip streaming path that feeds it.

**What alpha.3 delivers:**

- **ViewChat** — 5th view in the state machine. Layout: scrollable message history (`bubbles/viewport`, ~80% of terminal height) + fixed-bottom textarea input (`bubbles/textarea`, multi-line, up to 5 visible lines, then internal scroll) + status bar (streaming indicator, connection state, keybind hints).
- **Navigation:** `c` from list view (on selected agent) or `c` from detail view opens ViewChat. `esc`/`backspace` from ViewChat returns to the previous view (blocked while a stream is active). ViewChat is not reachable from ViewDescribe.
- **`dclaw agent attach <name>`** now opens ViewChat instead of ViewDetail — one-line change in `agent_attach.go` (pre-declared in alpha.2 §15 Q3).
- **Wire protocol — `agent.chat.send`:** CLI sends `{name, content, parent_id?, message_id?}` to daemon. Daemon returns a synchronous ack, then executes `docker exec <container> pi -p --no-session "<message>"` and streams stdout as `agent.chat.chunk` notifications on the same connection until `final: true`.
- **Wire protocol — `agent.chat.chunk`:** daemon→CLI notification with `{name, role, text, sequence, final, message_id}`. One notification per output line in beta.1; one notification for the complete output in alpha.3 (wraps synchronous `ExecIn`).
- **Content-addressed message IDs:** `msgID = hex(sha256(agentName + "|" + parentID + "|" + content))` using stdlib `crypto/sha256`. No new dependency.
- **RPC client addition:** `RPCClient.ChatSend(ctx, agentName, content, parentID) (<-chan ChatChunkEvent, error)` opens a dedicated second connection for the stream lifetime.
- **Daemon addition:** `internal/daemon/chat.go` — `ChatHandler` that resolves agent → container → `docker exec`, fans chunks back on the caller's connection.
- **TUI key bindings:** `c` opens chat, `enter` sends, `shift+enter` newline, `ctrl+c` cancels active stream, `esc` exits (guarded while streaming).
- **Message rendering:** role-coloured bubbles — user (cyan "you"), agent (green `<name>`), error (red "⚠ error"). Relative timestamps ("2s ago"). Streaming glyph `…` on in-progress messages.

**What alpha.3 does NOT deliver (deferred):**

- Chat history persistence (SQLite) → beta.1
- Logs view → beta.1
- `:` vim command mode → beta.1
- Error toasts → beta.1
- Filter/search in chat → beta.1
- Multiple simultaneous ViewChat instances — one at a time in alpha.3
- True line-by-line docker streaming (beta.1 replaces `ExecIn` with `ExecInStream`)

**Sequence:**

```
alpha.1 → backend (daemon + docker + sqlite + CLI CRUD)
alpha.2 → TUI: look at your fleet (list + detail + describe + help)
alpha.3 → TUI: talk to an agent (chat streaming)             ← this plan
beta.1  → TUI: watch an agent (log tail + event stream + polish)
v0.3.0  → GA
```

---

## 2. Dependencies

**No new direct dependencies.** All required packages are already in `go.mod` from alpha.2:

```
github.com/charmbracelet/bubbles   v1.0.0   // textarea + viewport now consumed (were unused in alpha.2)
github.com/charmbracelet/bubbletea v1.3.10
github.com/charmbracelet/lipgloss  v1.1.0
```

Alpha.2 §15 Q6 noted that `bubbles v1.0.0` was added but only `bubbles/key` consumed. Alpha.3 is the planned consumer of `bubbles/textarea` and `bubbles/viewport`.

**Hash function:** `crypto/sha256` from Go stdlib. No new `go.mod` entry.

After any inadvertent `go.mod` touch, run `go mod tidy` from `/Users/hatef/workspace/agents/atlas/dclaw`.

---

## 3. File Changes

### New files

```
internal/tui/views/chat.go      — ChatModel: viewport + textarea + message history
internal/daemon/chat.go         — ChatHandler: docker exec + per-connection streaming
```

### Modified files

```
internal/tui/views/view.go      — add ViewChat constant (iota 4)
internal/protocol/messages.go   — append chat wire types
internal/tui/messages.go        — append 5 chat tea.Msg types
internal/tui/keys.go            — add Chat key.Binding
internal/tui/model.go           — add chat sub-model, ViewChat transitions, stream lifecycle
internal/tui/app.go             — add RunChatAttached() entry point
internal/client/rpc.go          — add ChatChunkEvent, ChatSend, chatMessageID
internal/daemon/router.go       — change Dispatch signature; register agent.chat.send
internal/daemon/server.go       — pass send closure to Dispatch
internal/sandbox/docker.go      — add ExecInStream (wraps ExecIn for alpha.3)
internal/cli/agent_attach.go    — ONE LINE: RunAttached → RunChatAttached
internal/tui/app_test.go        — append 2 chat navigation tests
internal/client/rpc_chat_test.go — NEW: TestChatMessageIDDeterministic
internal/daemon/chat_test.go    — NEW: TestChatHandlerAgentNotFound
```

### Files that do NOT change

```
cmd/dclaw/main.go
cmd/dclawd/main.go
internal/tui/styles.go
internal/tui/poll.go
internal/tui/views/{list,detail,describe,help,noDaemon}.go
internal/daemon/{lifecycle,config,logs}.go
internal/protocol/{protocol,encoding}.go
internal/client/client.go
internal/store/
internal/cli/{channel,daemon,agent,root,exit,output,status,version,cli_test}.go
scripts/smoke-cli.sh
Makefile
go.mod / go.sum
```

---

## 4. Exact File Contents

All paths are absolute. Subsections are copy-paste ready.

---

### 4.1 `internal/tui/views/view.go` (MODIFIED — add ViewChat)

Full file replacement:

```go
// Package views contains per-view models for the dclaw TUI. Each view is
// a self-contained struct with a View(...) method that renders into
// width×height cells and a small set of mutating helpers called by the root
// model's Update() loop.
package views

// View identifies the current main-pane content.
type View int

const (
	// ViewList is the default agent-list view.
	ViewList View = iota
	// ViewDetail is the single-agent detail pane.
	ViewDetail
	// ViewDescribe is the one-shot container-inspect pane.
	ViewDescribe
	// ViewNoDaemon is shown when the daemon is not reachable.
	ViewNoDaemon
	// ViewChat is the interactive chat pane for a selected agent.
	// Introduced in alpha.3.
	ViewChat
)
```

---

### 4.2 `internal/protocol/messages.go` (MODIFIED — append chat types)

Append the following block at the very end of the existing file (after `WorkerKillSignal`). Do not modify any existing type.

```go
// ---------- agent.chat.send / agent.chat.chunk (Boundary 4, alpha.3) ----------

// AgentChatSendParams is the request body for `agent.chat.send`.
// name matches the agent-by-name convention used by all Boundary 4 methods.
// parent_id is the content-addressed ID of the previous message in the thread;
// empty string means "new conversation". message_id is pre-computed by the
// caller as hex(sha256(name|parent_id|content)); the daemon echoes it back.
type AgentChatSendParams struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	ParentID  string `json:"parent_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
}

// AgentChatSendResult is the synchronous ack returned before any chunks are
// sent. The daemon sends this as the JSON-RPC response to agent.chat.send,
// then follows it with agent.chat.chunk notifications on the same connection.
type AgentChatSendResult struct {
	MessageID  string `json:"message_id"`
	AcceptedAt string `json:"accepted_at"` // RFC 3339
}

// AgentChatChunkNotification is the payload of an `agent.chat.chunk`
// notification. The daemon sends a stream of these on the connection that
// received agent.chat.send. The last chunk in a stream has Final set to true.
// Sequence is monotonically increasing from 0 within a single send.
type AgentChatChunkNotification struct {
	Name      string `json:"name"`
	Role      string `json:"role"`      // "agent" | "system" | "error"
	Text      string `json:"text"`      // delta text for this chunk
	Sequence  int    `json:"sequence"`
	Final     bool   `json:"final"`
	MessageID string `json:"message_id,omitempty"`
}
```

---

### 4.3 `internal/tui/messages.go` (MODIFIED — append chat msg types)

Full file replacement (adds five chat types after the existing five):

```go
package tui

import (
	"time"

	"github.com/itsmehatef/dclaw/internal/client"
)

// agentsLoadedMsg carries a fresh agent list from the daemon.
type agentsLoadedMsg struct {
	agents []client.Agent
}

// agentFetchedMsg carries a single agent record for the detail/describe view.
type agentFetchedMsg struct {
	agent client.Agent
}

// pollTickMsg fires on the 2-second polling cadence to trigger a fresh fetch.
type pollTickMsg time.Time

// daemonErrMsg is emitted when an RPC call fails. The TUI transitions to
// ViewNoDaemon on the first error and stays there until a manual retry succeeds.
type daemonErrMsg struct {
	err error
}

// retryMsg is injected by the 'r' key handler to kick off a reconnection attempt.
type retryMsg struct{}

// ---------- chat messages (alpha.3) ----------

// chatStreamOpenedMsg is emitted when ChatSend has been accepted by the daemon
// and the chunk channel is ready to drain.
type chatStreamOpenedMsg struct {
	agentName string
	messageID string
	chunks    <-chan client.ChatChunkEvent
}

// chatDeltaMsg is emitted for each chunk received from the daemon stream.
type chatDeltaMsg struct {
	chunk client.ChatChunkEvent
}

// chatAckedMsg is emitted when a message stream reaches final=true.
// The complete assembled text is included for future persistence hooks.
type chatAckedMsg struct {
	agentName string
	messageID string
	fullText  string
}

// chatErrorMsg is emitted when the stream breaks before final=true.
type chatErrorMsg struct {
	agentName string
	err       error
}

// chatStreamClosedMsg signals the drain goroutine has exited (channel closed).
type chatStreamClosedMsg struct{}
```

---

### 4.4 `internal/client/rpc.go` (MODIFIED — append ChatSend + helpers)

**Step 1:** Add `"crypto/sha256"` to the import block. The full updated import block:

```go
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/version"
)
```

**Step 2:** Append this block at the end of `rpc.go` (after the existing `mapToKVList` and `wireToAgent` helpers):

```go
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
```

---

### 4.5 `internal/client/rpc_chat_test.go` (NEW)

```go
package client

import "testing"

func TestChatMessageIDDeterministic(t *testing.T) {
	id1 := chatMessageID("alice", "", "hello world")
	id2 := chatMessageID("alice", "", "hello world")
	if id1 != id2 {
		t.Fatalf("expected stable ID, got %q vs %q", id1, id2)
	}
	id3 := chatMessageID("alice", "", "different content")
	if id1 == id3 {
		t.Fatal("expected different IDs for different content")
	}
	id4 := chatMessageID("bob", "", "hello world")
	if id1 == id4 {
		t.Fatal("expected different IDs for different agent names")
	}
	id5 := chatMessageID("alice", "parentXYZ", "hello world")
	if id1 == id5 {
		t.Fatal("expected different IDs for different parent IDs")
	}
	// Length must be 64 hex chars (sha256 = 32 bytes = 64 hex).
	if len(id1) != 64 {
		t.Fatalf("expected 64-char hex ID, got length %d: %q", len(id1), id1)
	}
}
```

---

### 4.6 `internal/tui/keys.go` (MODIFIED — add Chat binding)

Full file replacement:

```go
package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all key bindings for the dclaw TUI.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Describe key.Binding
	Chat     key.Binding // alpha.3: open chat view
	Refresh  key.Binding
	Help     key.Binding
	Quit     key.Binding
}

// DefaultKeys returns the shared global keymap.
func DefaultKeys() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open detail")),
		Back:     key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
		Describe: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "describe")),
		Chat:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "chat")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp returns the abbreviated help row.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Back, k.Describe, k.Chat, k.Refresh, k.Help, k.Quit}
}

// FullHelp returns all bindings in one row.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
```

---

### 4.7 `internal/tui/views/chat.go` (NEW)

```go
package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/itsmehatef/dclaw/internal/client"
)

// ChatMessage is one in-memory message in the conversation.
// Alpha.3: stored in RAM only. Beta.1 persists to SQLite.
type ChatMessage struct {
	Role      string    // "user" | "agent" | "system" | "error"
	AgentName string    // agent name for display (agent messages only)
	Content   string    // complete text (accumulated from deltas)
	Streaming bool      // true while this message is still receiving chunks
	MessageID string    // content-addressed ID; set when Final=true
	Timestamp time.Time
}

// ChatModel is the TUI model for the interactive chat view.
//
// Layout inside the available content area (terminal height − 2 chrome rows):
//
//	┌──────────────────────────────────────┐
//	│         message history (viewport)   │  ~80% of height
//	├──────────────────────────────────────┤
//	│   ─────────────── separator          │  1 row
//	│   > input (textarea, 3–5 lines)      │  3–5 rows
//	└──────────────────────────────────────┘
type ChatModel struct {
	agentName string
	messages  []ChatMessage
	lastMsgID string // ID of last complete message (used as parent_id)

	vp        viewport.Model
	ta        textarea.Model
	width     int
	height    int
	streaming bool

	userStyle   lipgloss.Style
	agentStyle  lipgloss.Style
	systemStyle lipgloss.Style
	errorStyle  lipgloss.Style
	dimStyle    lipgloss.Style
}

// NewChatModel returns a ChatModel with sensible defaults.
func NewChatModel() ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message… (enter to send, shift+enter for newline)"
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	ta.KeyMap.InsertNewline.SetKeys("shift+enter")
	ta.Focus()

	vp := viewport.New(80, 20)
	vp.SetContent("")

	return ChatModel{
		ta: ta,
		vp: vp,

		userStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
		agentStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true),
		systemStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		errorStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		dimStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
}

// SetAgent configures the model for a specific agent.
func (m *ChatModel) SetAgent(name string) {
	m.agentName = name
}

// SetSize recalculates viewport and textarea dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	taLines := 3
	if height > 20 {
		taLines = 5
	}
	vpHeight := height - taLines - 2 // 1 separator + 1 padding
	if vpHeight < 3 {
		vpHeight = 3
	}

	m.vp.Width = width
	m.vp.Height = vpHeight
	m.ta.SetWidth(width)
	m.ta.SetHeight(taLines)
	m.rebuildViewport()
}

// SetStreaming marks whether a stream is active.
func (m *ChatModel) SetStreaming(s bool) {
	m.streaming = s
	m.rebuildViewport()
}

// AppendUserMessage adds the user's outgoing message to history immediately.
func (m *ChatModel) AppendUserMessage(content string) {
	m.messages = append(m.messages, ChatMessage{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	m.rebuildViewport()
	m.vp.GotoBottom()
}

// AppendChunk handles an incoming ChatChunkEvent from the daemon stream.
func (m *ChatModel) AppendChunk(chunk client.ChatChunkEvent) {
	if chunk.Err != nil {
		m.AppendError(chunk.Err)
		return
	}
	// Append to in-progress agent message if one exists.
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		if last.Role == "agent" && last.Streaming {
			last.Content += chunk.Text
			last.Streaming = !chunk.Final
			if chunk.Final {
				last.MessageID = chunk.MessageID
				m.lastMsgID = chunk.MessageID
			}
			m.rebuildViewport()
			m.vp.GotoBottom()
			return
		}
	}
	// Start a new agent message.
	m.messages = append(m.messages, ChatMessage{
		Role:      "agent",
		AgentName: m.agentName,
		Content:   chunk.Text,
		Streaming: !chunk.Final,
		MessageID: chunk.MessageID,
		Timestamp: time.Now(),
	})
	if chunk.Final {
		m.lastMsgID = chunk.MessageID
	}
	m.rebuildViewport()
	m.vp.GotoBottom()
}

// AppendError adds a system-level error message to history.
func (m *ChatModel) AppendError(err error) {
	m.messages = append(m.messages, ChatMessage{
		Role:      "error",
		Content:   err.Error(),
		Timestamp: time.Now(),
	})
	m.rebuildViewport()
	m.vp.GotoBottom()
}

// SubmitInput returns and clears the textarea content. Returns "" if empty.
func (m *ChatModel) SubmitInput() string {
	val := strings.TrimSpace(m.ta.Value())
	m.ta.Reset()
	return val
}

// LastMessageID returns the content-addressed ID of the last complete message.
func (m *ChatModel) LastMessageID() string {
	return m.lastMsgID
}

// Reset clears history and textarea. Called when leaving the chat view.
func (m *ChatModel) Reset() {
	m.messages = nil
	m.lastMsgID = ""
	m.ta.Reset()
	m.vp.SetContent("")
}

// Update forwards tea.Msgs to the textarea sub-model.
func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

// View renders the chat pane (history + separator + textarea).
func (m *ChatModel) View(width, height int) string {
	if m.width != width || m.height != height {
		m.SetSize(width, height)
	}
	sep := m.dimStyle.Render(strings.Repeat("─", m.width))
	return lipgloss.JoinVertical(lipgloss.Left, m.vp.View(), sep, m.ta.View())
}

// rebuildViewport re-renders all messages into the viewport content.
func (m *ChatModel) rebuildViewport() {
	var b strings.Builder
	for _, msg := range m.messages {
		b.WriteString(m.renderMessage(msg))
		b.WriteString("\n")
	}
	m.vp.SetContent(b.String())
}

// renderMessage renders one ChatMessage as a styled string.
func (m *ChatModel) renderMessage(msg ChatMessage) string {
	ts := m.dimStyle.Render(chatRelTime(msg.Timestamp))
	var label string
	switch msg.Role {
	case "user":
		label = m.userStyle.Render("you")
	case "agent":
		name := msg.AgentName
		if name == "" {
			name = m.agentName
		}
		label = m.agentStyle.Render(name)
	case "system":
		label = m.systemStyle.Render("system")
	case "error":
		label = m.errorStyle.Render("⚠ error")
	default:
		label = msg.Role
	}
	glyph := ""
	if msg.Streaming {
		glyph = m.dimStyle.Render(" …")
	}
	header := fmt.Sprintf("%s  %s%s", label, ts, glyph)
	body := msg.Content
	if body == "" && msg.Streaming {
		body = m.dimStyle.Render("(streaming)")
	}
	indented := strings.ReplaceAll(body, "\n", "\n  ")
	return header + "\n  " + indented
}

// chatRelTime formats a time.Time as a relative duration string.
func chatRelTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
```

---

### 4.8 `internal/tui/model.go` (MODIFIED — full replacement)

```go
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/tui/views"
)

// Model is the root bubbletea.Model for the dclaw TUI.
type Model struct {
	ctx context.Context
	rpc *client.RPCClient

	current  views.View
	prevView views.View // view to return to when leaving ViewChat

	list     views.ListModel
	detail   views.DetailModel
	desc     views.DescribeModel
	help     views.HelpModel
	noDaemon views.NoDaemonModel
	chat     views.ChatModel

	width  int
	height int

	selected string // name of the currently selected agent

	streaming    bool
	streamCancel context.CancelFunc // non-nil when a stream is active

	keys KeyMap
}

// attachTarget carries an optional pre-selected agent for Run* entry points.
type attachTarget struct {
	agentName string
	startChat bool // if true, open ViewChat; otherwise ViewDetail
}

// NewModel constructs the root Model.
func NewModel(ctx context.Context, rpc *client.RPCClient, target *attachTarget) *Model {
	m := &Model{
		ctx:      ctx,
		rpc:      rpc,
		current:  views.ViewList,
		list:     views.NewListModel(),
		detail:   views.NewDetailModel(),
		desc:     views.NewDescribeModel(),
		help:     views.NewHelpModel(),
		noDaemon: views.NewNoDaemonModel(nil),
		chat:     views.NewChatModel(),
		keys:     DefaultKeys(),
	}
	if target != nil {
		m.selected = target.agentName
		if target.startChat {
			m.current = views.ViewChat
			m.prevView = views.ViewList
			m.chat.SetAgent(target.agentName)
		} else {
			m.current = views.ViewDetail
		}
	}
	return m
}

// Init sends the first fetch and schedules the poll timer.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(fetchAgents(m.ctx, m.rpc), tickPoll())
}

// Update dispatches incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chat.SetSize(msg.Width, m.chatHeight())
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agentsLoadedMsg:
		m.list.SetAgents(msg.agents)
		if m.current == views.ViewNoDaemon {
			m.current = views.ViewList
		}
		return m, tickPoll()

	case agentFetchedMsg:
		m.detail.SetAgent(msg.agent)
		m.desc.SetAgent(msg.agent)
		return m, tickPoll()

	case pollTickMsg:
		// While in ViewChat, skip fetching but keep the tick alive so
		// polling resumes normally when the user leaves.
		if m.current == views.ViewChat {
			return m, tickPoll()
		}
		switch m.current {
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		case views.ViewDescribe:
			return m, nil
		default:
			return m, fetchAgents(m.ctx, m.rpc)
		}
		return m, fetchAgents(m.ctx, m.rpc)

	case daemonErrMsg:
		m.noDaemon.SetErr(msg.err)
		m.current = views.ViewNoDaemon
		return m, nil

	case retryMsg:
		return m, retryDial(m.ctx, m.rpc)

	// chat events

	case chatStreamOpenedMsg:
		m.streaming = true
		m.chat.SetStreaming(true)
		return m, drainChatStream(msg.chunks)

	case chatDeltaMsg:
		m.chat.AppendChunk(msg.chunk)
		if msg.chunk.Final {
			m.streaming = false
			m.streamCancel = nil
			m.chat.SetStreaming(false)
			return m, nil
		}
		return m, drainChatStream(msg.chunks)

	case chatErrorMsg:
		if m.streamCancel != nil {
			m.streamCancel()
			m.streamCancel = nil
		}
		m.streaming = false
		m.chat.SetStreaming(false)
		m.chat.AppendError(msg.err)
		return m, nil

	case chatStreamClosedMsg:
		m.streaming = false
		m.chat.SetStreaming(false)
		return m, nil

	case chatAckedMsg:
		return m, nil // reserved for beta.1 persistence hook
	}

	// Forward non-key msgs to chat textarea when in ViewChat.
	if m.current == views.ViewChat {
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the full terminal output.
func (m *Model) View() string {
	if m.help.Active() {
		return m.renderChrome("help", m.help.View(m.width, m.height-2))
	}

	var main, viewName string
	switch m.current {
	case views.ViewList:
		viewName = "agents"
		main = m.list.View(m.width, m.height-2)
	case views.ViewDetail:
		viewName = fmt.Sprintf("detail: %s", m.selected)
		main = m.detail.View(m.width, m.height-2)
	case views.ViewDescribe:
		viewName = fmt.Sprintf("describe: %s", m.selected)
		main = m.desc.View(m.width, m.height-2)
	case views.ViewNoDaemon:
		viewName = "no-daemon"
		main = m.noDaemon.View(m.width, m.height-2)
	case views.ViewChat:
		viewName = fmt.Sprintf("chat: %s", m.selected)
		main = m.chat.View(m.width, m.height-2)
	default:
		viewName = "agents"
		main = m.list.View(m.width, m.height-2)
	}
	return m.renderChrome(viewName, main)
}

// renderChrome wraps main content with top/bottom bars.
func (m *Model) renderChrome(viewName, main string) string {
	agentCount := len(m.list.Items())
	topContent := fmt.Sprintf("[%s]  daemon:ok  agents:%d", viewName, agentCount)
	if m.current == views.ViewNoDaemon {
		topContent = fmt.Sprintf("[%s]  daemon:DOWN", viewName)
	}
	if m.current == views.ViewChat && m.streaming {
		topContent += "  streaming…"
	}
	top := TopBarStyle.Width(m.width).Render(topContent)

	var hints []string
	switch m.current {
	case views.ViewList:
		hints = []string{"↑↓/jk:nav", "enter:open", "c:chat", "r:refresh", "?:help", "q:quit"}
	case views.ViewDetail:
		hints = []string{"c:chat", "d:describe", "r:refresh", "esc:back", "?:help", "q:quit"}
	case views.ViewDescribe:
		hints = []string{"esc:back", "r:refresh", "?:help", "q:quit"}
	case views.ViewNoDaemon:
		hints = []string{"r:retry", "?:help", "q:quit"}
	case views.ViewChat:
		if m.streaming {
			hints = []string{"ctrl+c:cancel stream", "esc:blocked while streaming"}
		} else {
			hints = []string{"enter:send", "shift+enter:newline", "esc:back", "q:quit"}
		}
	default:
		hints = []string{"?:help", "q:quit"}
	}
	bottom := BottomBarStyle.Width(m.width).Render(strings.Join(hints, "  "))
	return lipgloss.JoinVertical(lipgloss.Left, top, main, bottom)
}

// chatHeight returns height available for the chat sub-model.
func (m *Model) chatHeight() int {
	h := m.height - 2
	if h < 4 {
		return 4
	}
	return h
}

// handleKey dispatches keyboard events.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.help.Active() {
		switch msg.String() {
		case "?", "esc":
			m.help.Toggle()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	if m.current == views.ViewChat {
		return m.handleChatKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help.Toggle()
		return m, nil
	case "r":
		switch m.current {
		case views.ViewNoDaemon:
			return m, func() tea.Msg { return retryMsg{} }
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		default:
			return m, fetchAgents(m.ctx, m.rpc)
		}
	}

	switch m.current {
	case views.ViewList:
		return m.handleListKey(msg)
	case views.ViewDetail:
		return m.handleDetailKey(msg)
	case views.ViewDescribe:
		return m.handleDescribeKey(msg)
	}
	return m, nil
}

func (m *Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.list.Down()
	case "k", "up":
		m.list.Up()
	case "enter":
		if name := m.list.SelectedName(); name != "" {
			m.selected = name
			m.current = views.ViewDetail
			return m, fetchAgent(m.ctx, m.rpc, name)
		}
	case "c":
		if name := m.list.SelectedName(); name != "" {
			return m.openChat(name, views.ViewList)
		}
	}
	return m, nil
}

func (m *Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.current = views.ViewList
		return m, fetchAgents(m.ctx, m.rpc)
	case "d":
		if m.selected != "" {
			m.current = views.ViewDescribe
		}
	case "c":
		if m.selected != "" {
			return m.openChat(m.selected, views.ViewDetail)
		}
	}
	return m, nil
}

func (m *Model) handleDescribeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.current = views.ViewDetail
	}
	return m, nil
}

func (m *Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.streaming {
			if m.streamCancel != nil {
				m.streamCancel()
				m.streamCancel = nil
			}
			m.streaming = false
			m.chat.SetStreaming(false)
			return m, nil
		}
		return m, tea.Quit

	case "esc", "backspace":
		if m.streaming {
			return m, nil // blocked while streaming
		}
		m.current = m.prevView
		m.chat.Reset()
		switch m.prevView {
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		default:
			return m, fetchAgents(m.ctx, m.rpc)
		}
		return m, nil

	case "q":
		if m.streaming {
			return m, nil // swallow q while streaming
		}
		return m, tea.Quit

	case "enter":
		return m.handleChatEnter()

	default:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	}
}

// handleChatEnter fires when enter is pressed in the chat textarea.
func (m *Model) handleChatEnter() (tea.Model, tea.Cmd) {
	if m.streaming {
		return m, nil
	}
	content := m.chat.SubmitInput()
	if content == "" {
		return m, nil
	}
	if m.rpc == nil {
		m.chat.AppendError(fmt.Errorf("no daemon connection"))
		return m, nil
	}
	parentID := m.chat.LastMessageID()
	agentName := m.selected

	streamCtx, cancel := context.WithCancel(m.ctx)
	m.streamCancel = cancel
	m.chat.AppendUserMessage(content)

	return m, func() tea.Msg {
		ch, err := m.rpc.ChatSend(streamCtx, agentName, content, parentID)
		if err != nil {
			cancel()
			return chatErrorMsg{agentName: agentName, err: err}
		}
		return chatStreamOpenedMsg{agentName: agentName, chunks: ch}
	}
}

// openChat transitions to ViewChat for the given agent.
func (m *Model) openChat(agentName string, from views.View) (tea.Model, tea.Cmd) {
	m.selected = agentName
	m.prevView = from
	m.current = views.ViewChat
	m.chat.SetAgent(agentName)
	m.chat.SetSize(m.width, m.chatHeight())
	return m, nil
}

// drainChatStream returns a tea.Cmd that reads exactly one chunk from ch.
// chatDeltaMsg carries both the chunk and the channel so the next drain
// can be scheduled from Update.
func drainChatStream(ch <-chan client.ChatChunkEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return chatStreamClosedMsg{}
		}
		if event.Err != nil {
			return chatErrorMsg{err: event.Err}
		}
		return chatDeltaMsg{chunk: event}
	}
}
```

Note: `chatDeltaMsg` in `Update` references `msg.chunks` which is not a field on `chatDeltaMsg`. Fix: `drainChatStream` is re-called with the same channel stored in `chatStreamOpenedMsg`. The channel is kept in `chatStreamOpenedMsg` and passed through to the drain loop. The implementation uses a closure over `ch` rather than storing it in the message. The drain loop in `Update` for `chatDeltaMsg` must call `drainChatStream` with the original channel. To accomplish this cleanly, store the active channel on `Model`:

Add field to `Model` struct:
```go
streamCh <-chan client.ChatChunkEvent // active stream channel; nil when not streaming
```

Update `chatStreamOpenedMsg` case in `Update`:
```go
case chatStreamOpenedMsg:
    m.streaming = true
    m.streamCh = msg.chunks
    m.chat.SetStreaming(true)
    return m, drainChatStream(m.streamCh)
```

Update `chatDeltaMsg` case:
```go
case chatDeltaMsg:
    m.chat.AppendChunk(msg.chunk)
    if msg.chunk.Final {
        m.streaming = false
        m.streamCh = nil
        m.streamCancel = nil
        m.chat.SetStreaming(false)
        return m, nil
    }
    return m, drainChatStream(m.streamCh)
```

Update `chatErrorMsg` and `chatStreamClosedMsg`:
```go
case chatErrorMsg:
    if m.streamCancel != nil {
        m.streamCancel()
        m.streamCancel = nil
    }
    m.streaming = false
    m.streamCh = nil
    m.chat.SetStreaming(false)
    m.chat.AppendError(msg.err)
    return m, nil

case chatStreamClosedMsg:
    m.streaming = false
    m.streamCh = nil
    m.chat.SetStreaming(false)
    return m, nil
```

The `drainChatStream` function signature is simplified to not need the channel in the message:
```go
// drainChatStream reads the next chunk from ch and returns it as chatDeltaMsg.
func drainChatStream(ch <-chan client.ChatChunkEvent) tea.Cmd {
    return func() tea.Msg {
        event, ok := <-ch
        if !ok {
            return chatStreamClosedMsg{}
        }
        if event.Err != nil {
            return chatErrorMsg{err: event.Err}
        }
        return chatDeltaMsg{chunk: event}
    }
}
```

This is the correct final form. The `Model` struct receives `streamCh` field, and `drainChatStream` is called with `m.streamCh` in both `chatStreamOpenedMsg` and `chatDeltaMsg` cases.

---

### 4.9 `internal/tui/app.go` (MODIFIED — add RunChatAttached)

```go
// Package tui is the dclaw interactive dashboard. Entry points:
//
//   - Run(socketPath)                    — bare `dclaw` launch, ViewList.
//   - RunAttached(socketPath, name)      — `dclaw agent attach` alpha.2 path; ViewDetail.
//   - RunChatAttached(socketPath, name)  — `dclaw agent attach` alpha.3+ path; ViewChat.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/tui/views"
)

// NoMouse is set to true by cmd/dclaw/main.go when --no-mouse is passed.
var NoMouse bool

// Run launches the TUI on the default list view.
func Run(socketPath string) error {
	return runTUI(socketPath, nil)
}

// RunAttached launches the TUI on ViewDetail for agentName (alpha.2 compat).
func RunAttached(socketPath, agentName string) error {
	return runTUI(socketPath, &attachTarget{agentName: agentName, startChat: false})
}

// RunChatAttached launches the TUI on ViewChat for agentName (alpha.3+).
func RunChatAttached(socketPath, agentName string) error {
	return runTUI(socketPath, &attachTarget{agentName: agentName, startChat: true})
}

func runTUI(socketPath string, target *attachTarget) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rpc := client.NewRPCClient(socketPath)

	var startErr error
	if err := rpc.Dial(ctx); err != nil {
		startErr = err
	}

	m := NewModel(ctx, rpc, target)
	if startErr != nil {
		m.noDaemon.SetErr(startErr)
		m.current = views.ViewNoDaemon
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if !NoMouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(m, opts...)
	_, err := p.Run()
	return err
}
```

---

### 4.10 `internal/daemon/chat.go` (NEW)

```go
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

	h.log.Debug("chat exec start", "agent", req.Name, "msg_id", msgID)

	// Alpha.3: synchronous exec — one final chunk.
	// Beta.1: replace with ExecInStream (true line-by-line via docker attach).
	argv := []string{"pi", "-p", "--no-session", req.Content}
	stdout, stderr, _, execErr := h.docker.ExecIn(ctx, rec.ContainerID, argv)

	text := stdout
	if text == "" {
		text = stderr
	}

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
```

---

### 4.11 `internal/daemon/chat_test.go` (NEW)

```go
package daemon_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/protocol"
)

// TestChatHandlerAgentNotFound verifies that a missing agent returns -32001.
// Uses a nil docker client (exec is never reached) and a store that returns
// "not found" for any agent name.
func TestChatHandlerAgentNotFound(t *testing.T) {
	// The test store stub: GetAgent always returns not-found error.
	// Because store.Repo is a concrete struct we cannot easily stub it here
	// without an interface. Instead we test via the router integration path
	// using a real in-memory store with no agents inserted.
	//
	// This test is intentionally left as a placeholder to be filled in by
	// Agent B using the test infrastructure pattern from existing daemon tests.
	// The assertion: send must receive an envelope with error.code == -32001.
	t.Skip("implement with test-store stub in Agent B step 6")
}

// TestChatHandlerMissingContent verifies that empty content returns -32602.
func TestChatHandlerMissingContent(t *testing.T) {
	var received []*protocol.Envelope
	send := func(env *protocol.Envelope) error {
		received = append(received, env)
		return nil
	}

	// handler with nil repo — the missing-content check fires before any repo call.
	h := daemon.NewChatHandler(nil, nil, nil)
	params, _ := json.Marshal(protocol.AgentChatSendParams{Name: "alice", Content: ""})
	_ = h.Handle(context.Background(), params, 1, send)

	if len(received) == 0 {
		t.Fatal("expected an error response")
	}
	if received[0].Error == nil {
		t.Fatal("expected error envelope")
	}
	if received[0].Error.Code != protocol.ErrInvalidParams {
		t.Fatalf("expected -32602, got %d", received[0].Error.Code)
	}
}
```

---

### 4.12 `internal/daemon/router.go` (MODIFIED)

Two changes. Show only the changed regions:

**Change 1** — Add `chatHandler` field to `Router` struct (after `handlers` field):

```go
type Router struct {
	log       *slog.Logger
	repo      *store.Repo
	docker    *sandbox.DockerClient
	lifecycle *Lifecycle
	handlers  map[string]handlerFunc
	chatHandler *ChatHandler  // streaming handler for agent.chat.send
}
```

**Change 2** — In `NewRouter`, after the existing handler registrations, instantiate chatHandler:

```go
r.chatHandler = NewChatHandler(log, repo, docker)
```

**Change 3** — Change `Dispatch` signature and add the streaming branch. Replace the existing `Dispatch` function entirely:

```go
// Dispatch routes an incoming envelope to its handler. For streaming methods
// (agent.chat.send), the handler sends its own response via send and returns
// nil to indicate no further response is needed. For all other methods, the
// normal result/error response is returned.
//
// send is a function that writes one JSON-RPC envelope on the active
// connection; it is provided by server.serveConn.
func (r *Router) Dispatch(ctx context.Context, env *protocol.Envelope, send func(*protocol.Envelope) error) *protocol.Envelope {
	if env.JSONRPC != "2.0" {
		return protocol.ErrorResponse(env.ID, protocol.ErrInvalidRequest, "jsonrpc must be \"2.0\"", nil)
	}

	// Streaming methods handle their own response via send.
	if env.Method == "agent.chat.send" {
		if err := r.chatHandler.Handle(ctx, env.Params, env.ID, send); err != nil {
			r.log.Warn("chat handler error", "err", err)
		}
		return nil
	}

	h, ok := r.handlers[env.Method]
	if !ok {
		if env.ID == nil {
			return nil
		}
		return protocol.ErrorResponse(env.ID, protocol.ErrMethodNotFound,
			fmt.Sprintf("method not found: %s", env.Method),
			map[string]any{"method": env.Method})
	}

	result, rpcErr := h(ctx, env.Params)

	if env.ID == nil {
		return nil
	}
	if rpcErr != nil {
		return protocol.ErrorResponse(env.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	return protocol.SuccessResponse(env.ID, result)
}
```

---

### 4.13 `internal/daemon/server.go` (MODIFIED)

One change only: build the `send` closure and pass it to `Dispatch`. Replace the message loop body in `serveConn`:

```go
// 2. Main message loop.
send := func(env *protocol.Envelope) error {
    return enc.Encode(env)
}
for {
    if ctx.Err() != nil {
        return
    }
    var env protocol.Envelope
    if err := dec.Decode(&env); err != nil {
        if !errors.Is(err, io.EOF) {
            s.log.Debug("conn decode done", "err", err)
        }
        return
    }
    resp := s.router.Dispatch(ctx, &env, send)
    if resp != nil {
        if err := enc.Encode(resp); err != nil {
            return
        }
    }
}
```

---

### 4.14 `internal/cli/agent_attach.go` (MODIFIED — one-line change)

Change `tui.RunAttached` to `tui.RunChatAttached`. Full file:

```go
package cli

import (
	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/tui"
)

// agentAttachCmd opens the TUI in chat mode for the named agent.
// As of alpha.3, attach opens ViewChat directly.
var agentAttachCmd = &cobra.Command{
	Use:   "attach <name>",
	Short: "Open the TUI in chat mode for a specific agent",
	Long: `Attach opens the dclaw TUI pre-focused on the named agent's chat view.

Press 'esc' to return to the agent list. Use 'ctrl+c' to cancel a streaming
response. Press 'enter' to send; 'shift+enter' for a newline.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.RunChatAttached(daemonSocket, args[0])
	},
}
```

---

### 4.15 `internal/tui/app_test.go` (MODIFIED — append 2 tests)

Add these imports to the existing import block (add `"github.com/itsmehatef/dclaw/internal/client"` and `"github.com/itsmehatef/dclaw/internal/tui/views"` if not already present):

```go
import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/tui/views"
)
```

Append these two test functions:

```go
// TestTUIChatOpenFromList verifies that pressing 'c' from the list view
// with a selected agent transitions to ViewChat.
func TestTUIChatOpenFromList(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "alice"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.current != views.ViewChat {
		t.Fatalf("expected ViewChat after 'c', got %v", m.current)
	}
	if m.selected != "alice" {
		t.Fatalf("expected selected=alice, got %q", m.selected)
	}
}

// TestTUIChatEscReturns verifies that pressing 'esc' from a non-streaming
// chat view returns to the previous view.
func TestTUIChatEscReturns(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	_, _ = m.Update(agentsLoadedMsg{agents: []client.Agent{{Name: "bob"}}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.current != views.ViewChat {
		t.Fatalf("expected ViewChat, got %v", m.current)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.current != views.ViewList {
		t.Fatalf("expected ViewList after esc, got %v", m.current)
	}
}

// Suppress unused import warning for teatest in files that only use the
// inline test model approach above. Remove this line if teatest is already
// used elsewhere in the file.
var _ = teatest.WithFinalTimeout
```

---

## 5. Modified Files Diff Summary

| File | Type | Summary |
|---|---|---|
| `internal/tui/views/view.go` | Additive | `ViewChat = 4` |
| `internal/protocol/messages.go` | Additive | 3 new types appended |
| `internal/tui/messages.go` | Additive | 5 new tea.Msg types appended |
| `internal/client/rpc.go` | Additive | `ChatChunkEvent`, `ChatSend`, `chatMessageID` appended; `crypto/sha256` import added |
| `internal/tui/keys.go` | Replacement | `Chat` binding added to struct, `DefaultKeys`, `ShortHelp`, `FullHelp` |
| `internal/tui/model.go` | Replacement | `chat`, `prevView`, `streaming`, `streamCancel`, `streamCh` fields; all ViewChat handlers |
| `internal/tui/app.go` | Additive | `RunChatAttached` function added |
| `internal/daemon/router.go` | Modified | `Dispatch` signature change; `chatHandler` field; streaming branch for `agent.chat.send` |
| `internal/daemon/server.go` | Modified | `send` closure built in `serveConn`; passed to `Dispatch` |
| `internal/cli/agent_attach.go` | One-line | `RunAttached` → `RunChatAttached` |
| `internal/tui/app_test.go` | Additive | 2 new test functions; import additions |

---

## 6. Keybinding Reference

### Global (all non-chat views)

| Key | Action |
|---|---|
| `q` / `ctrl+c` | Quit TUI |
| `?` | Toggle help overlay |
| `r` | Manual refresh |

### ViewList

| Key | Action |
|---|---|
| `j` / `↓` | Cursor down |
| `k` / `↑` | Cursor up |
| `enter` | Open ViewDetail for selected agent |
| `c` | Open ViewChat for selected agent |

### ViewDetail

| Key | Action |
|---|---|
| `c` | Open ViewChat |
| `d` | Open ViewDescribe |
| `esc` / `backspace` | Return to ViewList |

### ViewChat — idle (not streaming)

| Key | Action |
|---|---|
| `enter` | Submit message |
| `shift+enter` | Newline in textarea |
| `esc` / `backspace` | Return to previous view; clears history |
| `q` | Quit TUI |
| `ctrl+c` | Quit TUI (stream not active) |

### ViewChat — streaming active

| Key | Action |
|---|---|
| `ctrl+c` | Cancel active stream; stay in ViewChat |
| `esc` | Swallowed (guarded while streaming) |
| `q` | Swallowed |
| all others | Forwarded to textarea (input is buffered during stream) |

---

## 7. View Specification for ViewChat

### ASCII mockup (120×40 terminal)

```
[chat: alice]  daemon:ok  agents:2  streaming…                   ← TopBarStyle (1 row)
────────────────────────────────────────────────────────────────
you  3s ago
  Can you list the files in /workspace?

alice  1s ago  …
  Here are the files in /workspace:                              ← viewport (scrollable)
  - README.md
  - src/
  - Makefile


────────────────────────────────────────────────────────────────  ← separator (DimStyle ─)
  > Type a message… (enter to send, shift+enter for newline)    ← textarea (3–5 lines)


────────────────────────────────────────────────────────────────
ctrl+c:cancel stream  esc:blocked while streaming               ← BottomBarStyle (1 row)
```

### Layout calculations

- Terminal height `H`
- Chrome rows: 2 (top bar + bottom bar)
- Content height passed to `ChatModel.View()`: `H - 2`
- `taLines`: 3 if content height ≤ 20; 5 otherwise
- `vpHeight`: `content_height - taLines - 2`
- If `vpHeight < 3`, clamp to 3

### Data sources and refresh cadence

ViewChat does not participate in the 2s poll. The viewport updates on every `chatDeltaMsg` via `AppendChunk` → `rebuildViewport` → `vp.SetContent`. The `pollTickMsg` handler returns `tickPoll()` without fetching anything while `ViewChat` is current, keeping the poll alive for when the user returns to other views.

---

## 8. State Machine Update

### States

```
ViewList     = 0  (unchanged from alpha.2)
ViewDetail   = 1  (unchanged)
ViewDescribe = 2  (unchanged)
ViewNoDaemon = 3  (unchanged)
ViewChat     = 4  (NEW in alpha.3)
```

### Transitions

```
ViewList     --enter-->         ViewDetail
ViewList     --c-->             ViewChat       (prevView = ViewList)
ViewDetail   --esc/backspace--> ViewList
ViewDetail   --d-->             ViewDescribe
ViewDetail   --c-->             ViewChat       (prevView = ViewDetail)
ViewDescribe --esc/backspace--> ViewDetail
ViewChat     --esc (idle)-->    prevView       (history cleared)
ViewChat     --ctrl+c (active)-->[cancel stream, stay ViewChat]
any view     --daemon error-->  ViewNoDaemon
ViewNoDaemon --r (retry ok)-->  ViewList
```

### State diagram

```
           start
             │
             ▼
        ┌─────────┐ ◄──────────────────────────────── retry-ok ──┐
        │ ViewList │                                               │
        └──┬──┬───┘                                          ViewNoDaemon
           │  │ c                                                  ▲
         enter└────────────────────────┐              daemon-err (any view)
           │                           │
    ┌──────▼──────┐             ┌──────▼──────┐
    │ ViewDetail  │             │  ViewChat   │
    └──┬──┬──┬───┘             └──────┬──────┘
       │  │  │c                       │ esc (idle)
      esc  d  └──(already above)      └──► prevView
       │   │
    ViewList ViewDescribe
               │ esc
           ViewDetail
```

---

## 9. Wire Protocol Additions

### Method: `agent.chat.send` (CLI → Daemon)

**Type:** Request (streaming — ack returned immediately, chunks follow as notifications)

Request:

```json
{
  "jsonrpc": "2.0",
  "method": "agent.chat.send",
  "params": {
    "name": "alice",
    "content": "List the files in /workspace",
    "parent_id": "",
    "message_id": "a3f82b1c9d0e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9"
  },
  "id": 3
}
```

Synchronous ack response (sent before any chunks):

```json
{
  "jsonrpc": "2.0",
  "result": {
    "message_id": "a3f82b1c9d0e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9",
    "accepted_at": "2026-04-17T14:32:07Z"
  },
  "id": 3
}
```

### Method: `agent.chat.chunk` (Daemon → CLI)

**Type:** Notification (no id)

Mid-stream chunk:

```json
{
  "jsonrpc": "2.0",
  "method": "agent.chat.chunk",
  "params": {
    "name": "alice",
    "role": "agent",
    "text": "Here are the files:\n",
    "sequence": 0,
    "final": false,
    "message_id": "a3f82b1c..."
  }
}
```

Final chunk:

```json
{
  "jsonrpc": "2.0",
  "method": "agent.chat.chunk",
  "params": {
    "name": "alice",
    "role": "agent",
    "text": "- Makefile\n",
    "sequence": 5,
    "final": true,
    "message_id": "a3f82b1c..."
  }
}
```

Error chunk (docker exec failed):

```json
{
  "jsonrpc": "2.0",
  "method": "agent.chat.chunk",
  "params": {
    "name": "alice",
    "role": "error",
    "text": "OOMKilled",
    "sequence": 0,
    "final": true,
    "message_id": "a3f82b1c..."
  }
}
```

### Error responses (before ack)

Agent not found (`-32001`):
```json
{"jsonrpc":"2.0","error":{"code":-32001,"message":"agent \"alice\" not found"},"id":3}
```

Agent has no container (`-32002`):
```json
{"jsonrpc":"2.0","error":{"code":-32002,"message":"agent \"alice\" has no container"},"id":3}
```

### Sequencing guarantees

1. The ack response (matching `id: 3`) arrives **before** any `agent.chat.chunk` notifications.
2. Notifications are sent in `sequence` order on the same connection.
3. Wire protocol v1: one-request-at-a-time. The dedicated stream connection cannot send another request until `final: true` or the connection is closed.
4. After `final: true`, the connection may be reused for another `agent.chat.send` or closed.

### Wire protocol spec gap — naming discrepancy

The task brief calls the notification `agent.chat.stream`. The `docs/wire-protocol-spec.md` §7a.2 calls it `agent.chat.chunk`. This plan follows the **spec** (`agent.chat.chunk`). See §15 Q9 for the decision record and required action.

---

## 10. Content-Addressed Message Semantics

### Formula

```
messageID = lower-hex( sha256( agentName + "|" + parentID + "|" + content ) )
```

Implemented in `internal/client/rpc.go` as `chatMessageID(agentName, parentID, content string) string`.

The separator `|` is chosen because agent names and content cannot contain `|` (agent names are alphanumeric+hyphen by convention; content is arbitrary UTF-8 but the sha256 input is a concatenation with fixed-position separators, so collisions require finding a sha256 preimage).

### Idempotent retry

The same `(agentName, parentID, content)` triple always produces the same `message_id`. If a send is retried (network blip before ack arrives), the daemon receives the same `message_id` twice. **Alpha.3 does not deduplicate** — it re-executes `docker exec` on retry. Beta.1 adds a `chat_messages` SQLite table keyed by `message_id` to deduplicate and return cached results.

### Parent ID threading

`ChatModel.LastMessageID()` returns the `message_id` of the last complete (non-streaming) message. This is passed as `parentID` in the next `ChatSend`. The chain: `""` (first message) → `id1` → `id2` → … Conversation threads are implicitly captured in the ID chain. No explicit thread/conversation ID is needed for alpha.3.

---

## 11. Chat Stream Lifecycle

### Open

1. User presses `c` → `openChat` sets `ViewChat`, calls `chat.SetAgent(name)`.
2. User types message, presses `enter` → `handleChatEnter` fires.
3. `context.WithCancel(m.ctx)` creates `streamCtx`; cancel stored in `m.streamCancel`.
4. `chat.AppendUserMessage(content)` — user message appears immediately.
5. Tea cmd goroutine calls `rpc.ChatSend(streamCtx, agentName, content, parentID)`.
6. `ChatSend` dials a dedicated connection, handshakes, sends `agent.chat.send`, reads ack.
7. `chatStreamOpenedMsg` arrives in `Update` → `m.streaming = true`, `m.streamCh = ch`, `drainChatStream(ch)` scheduled.
8. Drain loop reads chunks one-by-one, each triggers a `chatDeltaMsg` → `chat.AppendChunk`.

### Close (normal)

1. Daemon sends `final: true` chunk.
2. `chatDeltaMsg{chunk: {Final: true}}` arrives.
3. `Update` sets `streaming = false`, `streamCh = nil`, `streamCancel = nil`, `chat.SetStreaming(false)`.
4. No further `drainChatStream` is scheduled.
5. Channel closes naturally when the daemon's goroutine returns; `chatStreamClosedMsg` is never sent in the normal path (channel is already drained).

### Cancel (ctrl+c)

1. User presses `ctrl+c` while `streaming = true`.
2. `handleChatKey("ctrl+c")` calls `m.streamCancel()`.
3. The stream goroutine in `ChatSend` detects `ctx.Err() != nil`, returns.
4. Channel closes → `drainChatStream` returns `chatStreamClosedMsg`.
5. `Update` handles `chatStreamClosedMsg`: `streaming = false`.

### Daemon disconnect mid-stream

1. Unix socket closes unexpectedly.
2. `json.Decoder.Decode` in the drain goroutine returns `io.EOF` or `net.OpError`.
3. Goroutine sends `ChatChunkEvent{Err: ...}` on channel.
4. `drainChatStream` returns `chatErrorMsg`.
5. `Update` handles `chatErrorMsg`: cancel cancel func, `streaming = false`, `chat.AppendError`.
6. No auto-reconnect in alpha.3. User sees `⚠ error: stream read: EOF`.

---

## 12. Step-by-Step Implementation Order

Four parallel agents. Agent A must finish before B and C can start. D waits for all three.

### Agent A — Wire protocol + client streaming

Owns: `internal/protocol/messages.go`, `internal/client/rpc.go`, `internal/client/rpc_chat_test.go`

Prereq: starts from `v0.3.0-alpha.2` HEAD.

1. Append chat types to `/Users/hatef/workspace/agents/atlas/dclaw/internal/protocol/messages.go` per §4.2.
2. Add `"crypto/sha256"` to import block in `/Users/hatef/workspace/agents/atlas/dclaw/internal/client/rpc.go`.
3. Append `ChatChunkEvent`, `ChatSend`, `chatMessageID` to `rpc.go` per §4.4.
4. Create `/Users/hatef/workspace/agents/atlas/dclaw/internal/client/rpc_chat_test.go` per §4.5.
5. `go build ./internal/protocol/... ./internal/client/...` — fix any errors.
6. `go test ./internal/client/...` — `TestChatMessageIDDeterministic` must pass.
7. Commit: `"alpha.3(A): wire protocol chat types + ChatSend streaming client"`.

Agent A delivers: protocol types and streaming client. No daemon or TUI changes.

---

### Agent B — Daemon chat handler

Owns: `internal/daemon/chat.go`, `internal/daemon/chat_test.go`, `internal/daemon/router.go`, `internal/daemon/server.go`

Prereq: Agent A merged (needs `protocol.AgentChatSendParams` and `protocol.AgentChatChunkNotification`).

1. Create `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/chat.go` per §4.10.
2. Create `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/chat_test.go` per §4.11. Flesh out `TestChatHandlerAgentNotFound` using an in-memory store (look at existing daemon tests for the pattern).
3. Modify `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/router.go` per §4.12:
   - Add `chatHandler *ChatHandler` field to struct.
   - Add `r.chatHandler = NewChatHandler(log, repo, docker)` in `NewRouter`.
   - Replace `Dispatch` with the new signature+body from §4.12.
4. Modify `/Users/hatef/workspace/agents/atlas/dclaw/internal/daemon/server.go` per §4.13: build `send` closure, pass to `Dispatch`.
5. `go build ./internal/daemon/... ./cmd/dclawd/...` — fix any errors.
6. `go test ./internal/daemon/...` — all tests pass.
7. Commit: `"alpha.3(B): daemon ChatHandler + router streaming dispatch"`.

Agent B delivers: daemon accepts `agent.chat.send` and pushes `agent.chat.chunk` notifications.

---

### Agent C — TUI chat view

Owns: `internal/tui/views/view.go`, `internal/tui/views/chat.go`, `internal/tui/messages.go`, `internal/tui/keys.go`, `internal/tui/model.go`, `internal/tui/app.go`, `internal/tui/app_test.go`

Prereq: Agent A merged (needs `client.ChatChunkEvent` for `chatDeltaMsg`).

1. Modify `internal/tui/views/view.go` — add `ViewChat` per §4.1.
2. Replace `internal/tui/messages.go` per §4.3.
3. Create `internal/tui/views/chat.go` per §4.7.
4. Replace `internal/tui/keys.go` per §4.6.
5. Replace `internal/tui/model.go` per §4.8 (incorporating the `streamCh` field correction noted at the end of §4.8).
6. Replace `internal/tui/app.go` per §4.9.
7. `go build ./internal/tui/...` — fix any errors.
8. Modify `internal/tui/app_test.go` — append per §4.15.
9. `go test ./internal/tui/...` — all 4 tests pass (`TestTUISmoke`, `TestTUIListNav`, `TestTUIChatOpenFromList`, `TestTUIChatEscReturns`).
10. Commit: `"alpha.3(C): TUI ViewChat (viewport + textarea + state machine)"`.

Agent C delivers: full ViewChat view with navigation. With nil rpc, chat errors are shown gracefully.

---

### Agent D — Housekeeping + integration

Owns: `internal/cli/agent_attach.go`, smoke script expansion.

Prereq: Agents A, B, C all merged.

1. Modify `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent_attach.go` per §4.14.
2. `go build ./cmd/dclaw/...` — fix any errors.
3. `go test ./...` — all tests pass.
4. `go vet ./...` — clean.
5. Expand `scripts/smoke-tui.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
go test -run TestTUISmoke              -v ./internal/tui/...    -timeout 60s
go test -run TestTUIChatOpenFromList   -v ./internal/tui/...    -timeout 60s
go test -run TestTUIChatEscReturns     -v ./internal/tui/...    -timeout 60s
go test -run TestChatMessageIDDeterministic -v ./internal/client/... -timeout 30s
```

6. Add Test 12 to `scripts/smoke-daemon.sh` after Test 11, before `echo "All daemon smoke tests passed."`:

```bash
echo "--- Test 12: agent chat RPC smoke (exec proxy) ---"
STATE_DIR_CHAT=$(mktemp -d -t dclaw-smoke-chat-XXXX)
SOCKET_CHAT="$STATE_DIR_CHAT/dclaw.sock"
"$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" daemon start || fail "chat-start"
"$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" agent create chatbot \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_CHAT" || fail "chat-create"
"$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" agent start chatbot || fail "chat-agent-start"
OUT=$("$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" agent exec chatbot -- echo "smoke-ok" 2>&1)
echo "$OUT" | grep -q "smoke-ok" || fail "expected 'smoke-ok' in exec output, got: $OUT"
"$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" daemon stop >/dev/null 2>&1 || true
rm -rf "$STATE_DIR_CHAT"
pass "agent chat RPC smoke"
```

7. Commit: `"alpha.3(D): agent_attach → ViewChat, smoke script Test 12"`.

---

### Final integration

After all four agents' commits are on `main`:

1. `make build` — both binaries compile.
2. `go test ./...` — all tests pass.
3. `go vet ./...` — clean.
4. `./scripts/smoke-daemon.sh` — 12 tests pass.
5. `make smoke-tui` — 4 TUI tests + 1 client test pass.
6. Manual chat smoke (§13 steps 1–12).
7. Tag `v0.3.0-alpha.3`.

---

## 13. Test Plan

### Automated tests

| Test | Location | Exercises |
|---|---|---|
| `TestTUISmoke` | `internal/tui/app_test.go` | Key dispatch on empty model; clean quit |
| `TestTUIListNav` | `internal/tui/app_test.go` | Cursor clamping on empty list |
| `TestTUIChatOpenFromList` | `internal/tui/app_test.go` | `c` key → ViewChat transition |
| `TestTUIChatEscReturns` | `internal/tui/app_test.go` | `esc` from idle chat → prevView |
| `TestChatMessageIDDeterministic` | `internal/client/rpc_chat_test.go` | sha256 ID stability + uniqueness |
| `TestChatHandlerMissingContent` | `internal/daemon/chat_test.go` | Empty content → -32602 |
| `TestChatHandlerAgentNotFound` | `internal/daemon/chat_test.go` | Missing agent → -32001 |
| All alpha.1/alpha.2 tests | various | Regression |
| `./scripts/smoke-daemon.sh` | bash | 12 integration tests |
| `make smoke-tui` | `scripts/smoke-tui.sh` | 5 tests (4 TUI + 1 client) |

### Manual chat smoke

```
1. make build

2. ./bin/dclaw daemon start
   ./bin/dclaw agent create alice --image=dclaw-agent:v0.1 --workspace=/tmp
   ./bin/dclaw agent start alice

3. ./bin/dclaw agent attach alice
   EXPECT: TUI opens in "[chat: alice]" view
   EXPECT: empty history, focused textarea

4. Type "hello" → enter
   EXPECT: "you" message appears immediately
   EXPECT: "alice" streaming indicator appears (… glyph, "streaming…" in top bar)
   EXPECT: agent response appears (single chunk in alpha.3)
   EXPECT: streaming indicator disappears after final chunk

5. Press ctrl+c during a slow exec (if possible)
   EXPECT: stream cancels, TUI stays in ViewChat, no crash

6. Press esc (after stream completes)
   EXPECT: returns to ViewList, chat history cleared

7. Navigate to alice in list, press 'c'
   EXPECT: ViewChat opens for alice
   Press esc → ViewList

8. Press enter on alice → ViewDetail → press 'c'
   EXPECT: ViewChat opens
   Press esc → ViewDetail (prevView=ViewDetail)

9. Press q
   EXPECT: clean exit, exit code 0

10. ./bin/dclaw agent delete alice
    ./bin/dclaw daemon stop
```

---

## 14. Release Checklist for v0.3.0-alpha.3

1. [ ] All alpha.2 checklist items still green
2. [ ] `go vet ./...` clean
3. [ ] `go build ./...` — both `./bin/dclaw` and `./bin/dclawd`
4. [ ] `go test ./...` — all tests including the 7 new ones
5. [ ] `./scripts/smoke-daemon.sh` — 12 tests pass
6. [ ] `make smoke-tui` — 5 tests pass
7. [ ] Manual smoke §13 steps 1–10 completed
8. [ ] `dclaw agent attach <name>` opens ViewChat (not ViewDetail)
9. [ ] `c` from ViewList opens ViewChat for selected agent
10. [ ] `c` from ViewDetail opens ViewChat; `esc` returns to ViewDetail
11. [ ] `ctrl+c` cancels active stream and stays in ViewChat
12. [ ] `esc` is blocked while streaming; works after cancel/completion
13. [ ] User messages rendered in cyan; agent in green; errors in red
14. [ ] Relative timestamps render ("2s ago", "1m ago")
15. [ ] Top bar shows "streaming…" while stream active
16. [ ] `go.mod` unchanged (no new direct deps)
17. [ ] §15 Q9 decision recorded and spec updated (or acknowledged as pending)
18. [ ] Commit: `"Phase 3 alpha.3: TUI chat mode + agent.chat.send streaming (v0.3.0-alpha.3)"`
19. [ ] `git tag -a v0.3.0-alpha.3 -m "Phase 3 alpha.3: TUI chat mode"`
20. [ ] `git push origin main v0.3.0-alpha.3`
21. [ ] Handoff doc updated: `~/.claude/projects/-Users-hatef-workspace-agents-atlas/handoff/dclaw.md`

---

## 15. Open Questions

---

**Q1: `c` vs `enter` for opening chat from list**

Options: (A) `c` — mnemonic, keeps `enter` = detail. (B) `enter` — most natural but breaks alpha.2 detail navigation. (C) modifier key.

Recommendation: **`c`** (Option A). Keeps `enter` = ViewDetail (established muscle memory from alpha.2). `c` follows the `d`=describe convention. **Locked in this plan.**

---

**Q2: Chat history persistence — alpha.3 vs beta.1**

Options: (A) alpha.3 in-memory only. (B) beta.1: add `chat_messages` SQLite table. (C) v0.3.1 patch.

Recommendation: **Defer to beta.1** (Option B). The `message_id` and `parent_id` infrastructure is in place; hooking to SQLite in beta.1 is a clean addition. Alpha.3 in-memory is sufficient for UX evaluation.

---

**Q3: Subscription model — single-connection vs fan-out**

Options: (A) Single-connection: one CLI client per stream, no fan-out manager. (B) Build `SubscriptionManager` now for multi-client fan-out. (C) Defer even single-connection and use poll.

Recommendation: **Option A for alpha.3**. One CLI at a time. `SubscriptionManager` (parallel to `LogStreamer` in `logs.go`) is beta.1 scope. This plan's `ChatHandler` is documented as "single-connection for alpha.3."

---

**Q4: Streaming cancellation UX — `ctrl+c` vs `esc` vs both**

Options: (A) `ctrl+c` to cancel, `esc` blocked while streaming. (B) `esc` cancels and exits. (C) Both cancel but `ctrl+c` stays, `esc` exits.

Recommendation: **Option A** (ctrl+c = cancel, esc = blocked). Avoids accidental exit during long streams. **Locked in this plan.**

---

**Q5: Backpressure when worker streams faster than TUI renders**

Options: (A) Buffer cap=64, block goroutine on full. (B) Drop tokens. (C) Unbounded buffer.

Recommendation: **Option A**. 64-item buffer absorbs bursts; blocking the docker socket read is correct backpressure. **Implemented** in `ChatSend` (`make(chan ChatChunkEvent, 64)`).

---

**Q6: Hash function — `crypto/sha256` vs blake3**

Options: (A) stdlib `crypto/sha256`. (B) external blake3. (C) any 256-bit hash.

Recommendation: **Option A** (stdlib). Chat message IDs are computed at human typing speed; performance is irrelevant. Zero new `go.mod` entries. **Locked in this plan.**

---

**Q7: Worker crash mid-stream — error display vs auto-reconnect**

Options: (A) Show `⚠ error` in history, user retries manually. (B) Auto-reconnect by resending last message. (C) Inline `[retry]` prompt.

Recommendation: **Option A for alpha.3**. Auto-reconnect risks duplicate execution. Inline prompt is beta.1 polish. **Implemented** via `chatErrorMsg`.

---

**Q8: Multiple ViewChat instances — one at a time vs tabs**

Options: (A) One ViewChat at a time; new `c` press resets history. (B) Tab multiplexing.

Recommendation: **Option A for alpha.3**. Tab multiplexing requires a session manager; beta.1 scope. **Locked in this plan.**

---

**Q9: Wire protocol notification name — `agent.chat.chunk` (spec) vs `agent.chat.stream` (brief)**

Statement: The task brief uses `agent.chat.stream`. `docs/wire-protocol-spec.md` §7a.2 uses `agent.chat.chunk`. These are different strings with different semantics implications.

Options: (A) Follow spec: `agent.chat.chunk`. Update handoff doc. (B) Follow brief: `agent.chat.stream`. Update spec. (C) Rename to `agent.chat.delta` and update both.

Recommendation: **Option A** (`agent.chat.chunk`). The spec is authoritative. `agent.chat.chunk` is consistent with the existing `agent.exec.chunk` naming pattern. This plan implements `agent.chat.chunk` throughout.

**Action required before implementation starts:** Confirm `agent.chat.chunk` with the project owner and update `docs/wire-protocol-spec.md` if needed. If the decision is `agent.chat.stream` instead, change 5 string literals across `protocol/messages.go`, `daemon/chat.go`, and `client/rpc.go`.

---

**Wire protocol spec gaps identified**

1. **`agent.chat.send` params underspecified (MINOR GAP):** Spec §7a.1 shows `{name, message}`. This plan uses `{name, content, parent_id, message_id}`. The spec should be updated to match this plan's shape. Not a blocker for implementation (the plan is self-consistent), but the spec should be kept in sync.

2. **Boundaries 1-3 have no chat types (NOT A BLOCKER FOR ALPHA.3):** Boundaries 2 and 3 (main↔dispatcher, worker↔dispatcher) have no streaming-delta message types. The brief describes "worker streams delta tokens back to daemon" which implies future Boundary 3 additions. Alpha.3 sidesteps this entirely: the daemon runs `docker exec` directly and streams the complete exec output as one final chunk. True LLM token streaming via Boundary 3 is v0.4+ scope when pi-mono workers are actually deployed.

3. **No subscription semantics in spec §7a:** The spec lists `agent.chat.chunk` as triggered by `agent.chat.send` but provides no subscription lifecycle, multi-client fan-out, or backpressure rules. This plan defines these for alpha.3 (single-connection, no fan-out) and flags beta.1 for the full subscription manager.

---

**Corrections to the alpha.2 plan spotted during drafting**

1. **alpha.2 §4.5 `poll.go` nil-guard:** The alpha.2 plan's §4.5 verbatim listing omits the `if rpc == nil` guard that the §12 implementation note says to add. The shipped `poll.go` has the guard. The discrepancy is harmless (note covered it) but means the §4 verbatim listing was not truly copy-paste complete. This alpha.3 plan avoids the same issue by including the nil-rpc path in `handleChatEnter` verbatim.

2. **`internal/tui/views/list.go` local `max`/`min` functions:** Go 1.21+ has `builtin.max`/`builtin.min`. The local definitions shadow them and will produce vet warnings on newer toolchains. Harmless for now. Beta.1 housekeeping should remove them.

3. **`go.mod` `go 1.25.0` directive:** Go 1.25 does not exist as of the knowledge cutoff. This is either a forward-dated directive or a typo. The module still compiles correctly because the `go` line is advisory for the minimum Go version. Alpha.3 does not change `go.mod` so this is a pre-existing issue to clean up in beta.1.

---

*End of plan. Implementation agents begin with Agent A (wire protocol types + ChatSend). Agent B and C can start in parallel after Agent A's commit is on main. Agent D runs last.*
