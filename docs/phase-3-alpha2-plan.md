# Phase 3 Alpha.2 Plan — v0.3.0-alpha.2 TUI Dashboard

**Goal:** Ship the k9s-style TUI dashboard for `dclaw`. Bare `dclaw` invocation on an interactive TTY opens a live agent list, allows drilling into a detail view and a describe view, provides a help overlay, and shows a friendly error screen when the daemon is not running. Chat mode, logs streaming, and command mode are explicitly deferred to alpha.3 / beta.1. This plan is self-contained and executable by implementation agents working from it alone.

**Scope:** `internal/tui/` package (new), `cmd/dclaw/main.go` modification, `internal/cli/root.go` modification, `internal/cli/agent_attach.go` (new), `scripts/smoke-daemon.sh` negative-path additions, `go.mod` additions, teatest pseudoversion fix.

**Prereq:** `v0.3.0-alpha.1` tagged at commit `4210a91`. All alpha.1 checklist items green. Docker daemon reachable. `go 1.25.0` installed.

---

## 0. Status

**SHIPPED (2026-04-17) as `v0.3.0-alpha.2`.** Commits on top of `v0.3.0-alpha.1` (`4210a91`):

- `961b993` — deps: bubbletea v1.3.10, lipgloss v1.1.0, bubbles v1.0.0
- `0dd6f56` — `internal/tui/` scaffolding (Agent A)
- `54e520e` — wire TUI into CLI root + `agent attach` (Agent B)
- `b697f54` — housekeeping: pid fix, +4 smoke tests, smoke-tui wrapper (Agent C)

| Field | Value |
|---|---|
| **Tag** | `v0.3.0-alpha.2` (shipped) |
| **Branch** | `main` |
| **Base commit** | `4210a91` (`v0.3.0-alpha.1`) |
| **Est. duration** | 2–3 days |
| **Prereqs** | v0.3.0-alpha.1 green; bubbletea v1.3.10 added |
| **Next tag** | `v0.3.0-alpha.3` (TUI chat mode + streaming) |

---

## 1. Overview

Alpha.1 delivered the daemon backbone: SQLite, Docker, the JSON-RPC server, and the full `dclaw agent/channel/daemon/status` CLI surface. Alpha.2 adds the interactive face.

**What alpha.2 delivers:**

- Bare `dclaw` (no subcommand) on a TTY launches the bubbletea TUI instead of cobra help.
- **noDaemon view** — if the daemon dial fails, show a user-friendly screen ("daemon not running — run `dclaw daemon start`") with a `r` retry key instead of dying with an error message.
- **List view** — all agents, columns: NAME / ID-PREFIX / STATUS / IMAGE / CREATED. Polls `agent.list` every 2s. `j`/`k`/`↑`/`↓` navigate.
- **Detail view** — `enter` from list drills into the selected agent. Shows all fields from `agent.get`. Polls every 2s.
- **Describe view** — `d` from detail view shows container-inspect-style data (labels, env, mounts, channel). One-shot; not polled.
- **Help overlay** — `?` anywhere shows a modal keybinding reference. `?` again or `esc` closes it.
- `--no-mouse` flag on `dclaw` — mouse on by default; opt-out for Terminal.app users.
- k9s-style status bar: `[view-name] daemon:ok agents:N  ↑↓/jk:nav  enter:open  d:describe  esc:back  r:refresh  ?:help  q:quit`
- `dclaw agent attach <name>` cobra subcommand (new in alpha.2) — bypasses list view and opens TUI with the selected agent pre-focused in detail view. Chat view is alpha.3 scope; attach in alpha.2 lands on detail, not chat.

**What alpha.2 does NOT deliver (deferred):**

- Chat mode / message streaming → alpha.3
- Logs view → beta.1
- `:` vim-style command mode → beta.1
- Error toasts with auto-dismiss → beta.1
- Filtering/search → beta.1
- Multi-select → beta.1

**How alpha.2 fits in the sequence:**

```
alpha.1 → backend done (daemon + docker + sqlite + CLI CRUD)
alpha.2 → TUI: look at your fleet (list + detail + describe + help)   ← this plan
alpha.3 → TUI: talk to an agent (chat streaming)
beta.1  → TUI: watch an agent (log tail + event stream + polish)
v0.3.0  → GA release cut
```

---

## 2. Dependencies

**New direct dependencies** to add to `go.mod`. The current `go.mod` (`v0.3.0-alpha.1`) has `go 1.25.0` and does not include any charmbracelet libraries as direct deps. `mattn/go-isatty v0.0.20` is already present as an indirect dep (pulled by docker); it must be promoted to direct.

Add to the `require (...)` direct block:

```
github.com/charmbracelet/bubbletea v1.3.10
github.com/charmbracelet/lipgloss  v1.1.0
github.com/charmbracelet/bubbles   v1.0.0
github.com/charmbracelet/x/exp/teatest v0.0.0-20260413165052-6921c759c913
github.com/mattn/go-isatty v0.0.20
```

**Notes on version choices:**

- `bubbletea v1.3.10` — the latest stable v1.x release (September 2025). v2 is in beta/RC at `charm.land/bubbletea/v2` with a different module path; skip it for v0.3. The v1 API is stable and matches what alpha.1 pre-planned.
- `lipgloss v1.1.0` — latest stable v1.x (March 2025). v2 exists but v1 is stable; avoid chasing majors during the v0.3 release cycle.
- `bubbles v1.0.0` — stable release (February 2026). The list, viewport, help, and key sub-packages are all present.
- `teatest v0.0.0-20260413165052-6921c759c913` — the correct current pseudoversion from `pkg.go.dev/github.com/charmbracelet/x/exp/teatest`. The alpha.1 plan used `v0.0.0-20240229115032-4b47b6fdaf28`, which is stale (upstream rebased). This is the fix referenced in the alpha.2 scope.
- `go-isatty v0.0.20` — already in go.sum as an indirect dep. Promoted to direct because `cmd/dclaw/main.go` imports it directly for the bare-TUI-dispatch guard.

After editing `go.mod`, run:

```bash
cd /Users/hatef/workspace/agents/atlas/dclaw
go mod tidy
```

`go mod tidy` will populate the new indirect deps in `go.sum`. Do not hand-write the indirect block.

---

## 3. File Changes

### New files

```
internal/tui/
  app.go            NEW — bubbletea.Program wiring + Run() / RunAttached()
  model.go          NEW — root Model struct, Init/Update/View, handleKey
  messages.go       NEW — tea.Msg types (agentsLoadedMsg, agentFetchedMsg, pollTickMsg, etc.)
  keys.go           NEW — key.Binding definitions
  styles.go         NEW — lipgloss style vars
  poll.go           NEW — tea.Cmd helpers (fetchAgents, fetchAgent, tickPoll)
  views/
    view.go         NEW — View enum (ViewList, ViewDetail, ViewDescribe)
    list.go         NEW — list view model + delegate
    detail.go       NEW — detail view model + viewport
    describe.go     NEW — describe view model
    help.go         NEW — help overlay model
    noDaemon.go     NEW — "daemon not running" screen model

internal/cli/
  agent_attach.go   NEW — agentAttachCmd cobra command
```

### Modified files

```
cmd/dclaw/main.go             MODIFIED — bare-TUI-dispatch + --no-mouse flag
internal/cli/root.go          MODIFIED — add --no-mouse flag registration + tui.NoMouse var bridge
internal/cli/agent.go         MODIFIED — add agentAttachCmd to AddCommand list
go.mod                        MODIFIED — add 5 direct deps (see §2)
go.sum                        REGENERATED by go mod tidy
scripts/smoke-daemon.sh       MODIFIED — add 4 negative-path tests (§11)
```

### Files that do NOT change

```
cmd/dclawd/main.go
internal/daemon/
internal/client/
internal/protocol/
internal/store/
internal/sandbox/
internal/cli/{channel,daemon,exit,output,status,version,cli_test}.go
scripts/smoke-cli.sh
Makefile   (tui and smoke-tui targets already stubbed from alpha.1 — activate in step 17)
```

---

## 4. Exact File Contents

Each subsection is copy-paste ready. File paths are absolute.

### 4.1 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/views/view.go` (NEW)

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
)
```

### 4.2 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/messages.go` (NEW)

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
// ViewNoDaemon on the first error and stays there until a manual retry
// (key 'r') succeeds.
type daemonErrMsg struct {
	err error
}

// retryMsg is injected by the 'r' key handler to kick off a reconnection
// attempt from the noDaemon view.
type retryMsg struct{}
```

### 4.3 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/keys.go` (NEW)

```go
package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all key bindings for the dclaw TUI. Bindings are defined once
// here and referenced by both the Update() dispatcher and the help overlay.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Describe key.Binding
	Refresh  key.Binding
	Help     key.Binding
	Quit     key.Binding
}

// DefaultKeys returns the shared global keymap for alpha.2.
// Keys deferred to later alphas (c, l, :) are intentionally absent.
func DefaultKeys() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open detail")),
		Back:     key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
		Describe: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "describe")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp returns the abbreviated help row for the status bar.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Back, k.Describe, k.Refresh, k.Help, k.Quit}
}

// FullHelp returns all bindings, grouped as a single row (bubbles/help expects
// [][]key.Binding; we put everything in one row).
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
```

### 4.4 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/styles.go` (NEW)

```go
package tui

import "github.com/charmbracelet/lipgloss"

// Shared lipgloss styles. All views import this package to get consistent
// colours. If a terminal has NO_COLOR set, lipgloss degrades gracefully.

var (
	// TopBarStyle is the full-width header: view name + daemon state.
	TopBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1).
			Bold(true)

	// BottomBarStyle is the full-width keybinding hint row.
	BottomBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("240")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1)

	// ListHeaderStyle renders the column-header row in the list view.
	ListHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	// SelectedRowStyle highlights the cursor row in the list.
	SelectedRowStyle = lipgloss.NewStyle().
				Reverse(true).
				Bold(true)

	// DimStyle is used for secondary text (id-prefix, timestamps).
	DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// SectionHeaderStyle renders labelled sections inside detail/describe.
	SectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("213"))

	// ErrorStyle renders the noDaemon error box.
	ErrorStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1, 2)

	// HelpOverlayStyle is the full-screen help modal background.
	HelpOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62")).
				Padding(1, 2)
)

// StatusColor maps agent status strings to ANSI 256 colour codes for the
// list view's STATUS column.
func StatusColor(status string) lipgloss.Style {
	switch status {
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82")) // green
	case "stopped", "exited":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // orange
	case "created":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // blue
	case "dead", "oomkilled":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // grey
	}
}
```

### 4.5 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/poll.go` (NEW)

```go
package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsmehatef/dclaw/internal/client"
)

const pollInterval = 2 * time.Second
const rpcTimeout = 5 * time.Second

// tickPoll schedules the next poll tick. Called at the end of every successful
// agentsLoadedMsg handling to keep the 2s cadence running.
func tickPoll() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return pollTickMsg(t)
	})
}

// fetchAgents is a tea.Cmd that calls agent.list on the daemon and returns
// either agentsLoadedMsg (success) or daemonErrMsg (failure). The command
// creates its own timeout-scoped context so it does not leak on view changes.
func fetchAgents(ctx context.Context, rpc *client.RPCClient) tea.Cmd {
	return func() tea.Msg {
		cctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		agents, err := rpc.AgentList(cctx)
		if err != nil {
			return daemonErrMsg{err: err}
		}
		return agentsLoadedMsg{agents: agents}
	}
}

// fetchAgent is a tea.Cmd that calls agent.get for the named agent and returns
// either agentFetchedMsg or daemonErrMsg.
func fetchAgent(ctx context.Context, rpc *client.RPCClient, name string) tea.Cmd {
	return func() tea.Msg {
		cctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		a, err := rpc.AgentGet(cctx, name)
		if err != nil {
			return daemonErrMsg{err: err}
		}
		return agentFetchedMsg{agent: a}
	}
}

// retryDial is a tea.Cmd issued by the noDaemon view's 'r' key. It attempts a
// fresh Dial on the existing RPCClient (Dial is idempotent; it re-opens the
// connection if closed). On success it returns agentsLoadedMsg to restore the
// list view.
func retryDial(ctx context.Context, rpc *client.RPCClient) tea.Cmd {
	return func() tea.Msg {
		cctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		if err := rpc.Dial(cctx); err != nil {
			return daemonErrMsg{err: err}
		}
		agents, err := rpc.AgentList(cctx)
		if err != nil {
			return daemonErrMsg{err: err}
		}
		return agentsLoadedMsg{agents: agents}
	}
}
```

### 4.6 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/views/noDaemon.go` (NEW)

```go
package views

import "fmt"

// NoDaemonModel renders the "daemon not running" screen. This view is shown
// whenever the TUI cannot reach the daemon socket. The user can press 'r' to
// retry (see retryDial in poll.go) or 'q' to quit.
type NoDaemonModel struct {
	err string
}

// NewNoDaemonModel returns a model pre-populated with the dial error text.
func NewNoDaemonModel(err error) NoDaemonModel {
	msg := "daemon not running"
	if err != nil {
		msg = err.Error()
	}
	return NoDaemonModel{err: msg}
}

// SetErr replaces the error message (used when a retry fails with a new error).
func (m *NoDaemonModel) SetErr(err error) {
	if err != nil {
		m.err = err.Error()
	}
}

// View renders the no-daemon screen into the given terminal dimensions.
func (m *NoDaemonModel) View(width, height int) string {
	box := fmt.Sprintf(
		"  dclaw daemon is not running\n\n"+
			"  Error: %s\n\n"+
			"  Start the daemon:  dclaw daemon start\n\n"+
			"  Press 'r' to retry, 'q' to quit.",
		m.err,
	)
	// Centre the box vertically (rough approximation).
	padding := (height - 7) / 2
	if padding < 0 {
		padding = 0
	}
	out := ""
	for i := 0; i < padding; i++ {
		out += "\n"
	}
	out += box
	return out
}
```

### 4.7 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/views/list.go` (NEW)

```go
package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/itsmehatef/dclaw/internal/client"
)

// ListModel is the agent-list view. It maintains a sorted-by-name slice and a
// cursor position. It does not own a bubbletea list.Model because our row
// format (five columns including a coloured STATUS badge) benefits from a
// hand-rolled renderer over the list bubble's opinionated delegate system.
// Beta.1 can migrate to list.Model if the row count grows large enough to
// warrant virtualised scrolling.
type ListModel struct {
	items  []client.Agent
	cursor int
}

// NewListModel returns an empty list model.
func NewListModel() ListModel { return ListModel{} }

// SetAgents replaces the backing slice and clamps the cursor.
func (m *ListModel) SetAgents(items []client.Agent) {
	m.items = items
	if m.cursor >= len(items) {
		m.cursor = max(0, len(items)-1)
	}
}

// Up moves the cursor one row up (wraps at 0).
func (m *ListModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}

// Down moves the cursor one row down (clamps at len-1).
func (m *ListModel) Down() {
	if m.cursor < len(m.items)-1 {
		m.cursor++
	}
}

// SelectedName returns the name of the highlighted agent, or "" if the list is
// empty.
func (m *ListModel) SelectedName() string {
	if len(m.items) == 0 {
		return ""
	}
	return m.items[m.cursor].Name
}

// View renders the list into width × height. The header occupies one row;
// rows beyond height-1 are clipped (no scrolling — beta.1 adds it).
func (m *ListModel) View(width, height int) string {
	var b strings.Builder

	// Header row
	header := fmt.Sprintf("%-24s %-10s %-28s %-10s",
		"NAME", "STATUS", "IMAGE", "CREATED")
	b.WriteString(header + "\n")
	b.WriteString(strings.Repeat("─", min(width, len(header)+4)) + "\n")

	available := height - 2 // two rows for header + divider
	for i, a := range m.items {
		if i >= available {
			break
		}
		marker := "  "
		line := fmt.Sprintf("%-24s %-10s %-28s %-10s",
			truncate(a.Name, 22),
			a.Status,
			truncate(a.Image, 26),
			humanAge(a),
		)
		if i == m.cursor {
			marker = "> "
			line = marker + line
		} else {
			line = marker + line
		}
		b.WriteString(line + "\n")
	}
	if len(m.items) == 0 {
		b.WriteString("  (no agents — run: dclaw agent create <name> --image=<img>)\n")
	}
	return b.String()
}

// helpers

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func humanAge(a client.Agent) string {
	// client.Agent does not expose CreatedAt directly; we surface Status as a
	// proxy. Beta.1 adds CreatedAt to the client.Agent type and renders age.
	// For alpha.2 this column shows the status, which is already in the STATUS
	// column — the CREATED column is left as "-" until the field is wired.
	_ = a
	return "-"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// humanDuration formats a time.Duration for display in the age column.
// Exported for use in tests.
func HumanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
```

### 4.8 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/views/detail.go` (NEW)

```go
package views

import (
	"fmt"
	"strings"

	"github.com/itsmehatef/dclaw/internal/client"
)

// DetailModel renders the full agent record for a single selected agent. It
// uses a simple string builder rather than a viewport bubble because the
// content is short and static (re-rendered on every poll tick). Beta.1 can
// upgrade to viewport.Model if the field list grows.
type DetailModel struct {
	agent client.Agent
	name  string // name of the agent being viewed (held separately for the title)
}

// NewDetailModel returns an empty detail model.
func NewDetailModel() DetailModel { return DetailModel{} }

// SetAgent replaces the backing record. The name is stored separately so the
// title remains stable while a background refresh is in flight.
func (m *DetailModel) SetAgent(a client.Agent) {
	m.agent = a
	m.name = a.Name
}

// Name returns the agent name this model is currently tracking.
func (m *DetailModel) Name() string { return m.name }

// View renders the detail pane into width × height.
func (m *DetailModel) View(width, height int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Agent: %s\n", m.name))
	b.WriteString(strings.Repeat("─", min(width, 60)) + "\n")
	b.WriteString(fmt.Sprintf("  Status:    %s\n", m.agent.Status))
	b.WriteString(fmt.Sprintf("  Image:     %s\n", m.agent.Image))
	b.WriteString(fmt.Sprintf("  Workspace: %s\n", m.agent.Workspace))
	if len(m.agent.Labels) > 0 {
		b.WriteString("  Labels:\n")
		for k, v := range m.agent.Labels {
			b.WriteString(fmt.Sprintf("    %s = %s\n", k, v))
		}
	}
	if len(m.agent.Env) > 0 {
		b.WriteString("  Env:\n")
		for k, v := range m.agent.Env {
			b.WriteString(fmt.Sprintf("    %s = %s\n", k, v))
		}
	}
	b.WriteString("\n")
	b.WriteString("  Press 'd' to describe container, 'esc' to return to list.\n")
	return b.String()
}
```

### 4.9 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/views/describe.go` (NEW)

```go
package views

import (
	"fmt"
	"strings"

	"github.com/itsmehatef/dclaw/internal/client"
)

// DescribeModel renders a kubectl-describe-style verbose view for a single
// agent. Alpha.2 populates it from client.Agent (which is the result of
// agent.get). Beta.1 upgrades the data source to agent.describe (which
// includes the SQLite events table) once the daemon exposes that method via
// the RPC client.
type DescribeModel struct {
	agent client.Agent
	name  string
}

// NewDescribeModel returns an empty describe model.
func NewDescribeModel() DescribeModel { return DescribeModel{} }

// SetAgent replaces the backing record.
func (m *DescribeModel) SetAgent(a client.Agent) {
	m.agent = a
	m.name = a.Name
}

// View renders the describe pane into width × height.
func (m *DescribeModel) View(width, height int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Describe: %s\n", m.name))
	b.WriteString(strings.Repeat("─", min(width, 60)) + "\n\n")

	b.WriteString("Container\n")
	b.WriteString(fmt.Sprintf("  Image:     %s\n", m.agent.Image))
	b.WriteString(fmt.Sprintf("  Status:    %s\n", m.agent.Status))
	b.WriteString(fmt.Sprintf("  Workspace: %s\n", m.agent.Workspace))

	if len(m.agent.Labels) > 0 {
		b.WriteString("\nLabels\n")
		for k, v := range m.agent.Labels {
			b.WriteString(fmt.Sprintf("  %s = %s\n", k, v))
		}
	}

	if len(m.agent.Env) > 0 {
		b.WriteString("\nEnvironment\n")
		for k, v := range m.agent.Env {
			b.WriteString(fmt.Sprintf("  %s = %s\n", k, v))
		}
	}

	b.WriteString("\nMounts\n")
	if m.agent.Workspace != "" {
		b.WriteString(fmt.Sprintf("  /workspace ← %s (bind)\n", m.agent.Workspace))
	} else {
		b.WriteString("  (none)\n")
	}

	b.WriteString("\nNetwork\n")
	b.WriteString("  bridge (default)\n")

	b.WriteString("\nEvents\n")
	b.WriteString("  (events from daemon.describe — wired in beta.1)\n")

	b.WriteString("\n  Press 'esc' to return to detail view.\n")
	return b.String()
}
```

### 4.10 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/views/help.go` (NEW)

```go
package views

import "strings"

// HelpModel is a full-screen modal help overlay. It is toggled by the '?'
// key from any view. When active, it replaces the entire terminal output until
// the user presses '?' or 'esc' again.
type HelpModel struct {
	active bool
}

// NewHelpModel returns a help model in the inactive state.
func NewHelpModel() HelpModel { return HelpModel{} }

// Toggle flips the overlay on/off.
func (m *HelpModel) Toggle() { m.active = !m.active }

// Active reports whether the overlay is currently visible.
func (m *HelpModel) Active() bool { return m.active }

// View renders the full-screen help text.
func (m *HelpModel) View(width, height int) string {
	var b strings.Builder
	b.WriteString("dclaw TUI — Keybinding Reference\n")
	b.WriteString("Press '?' or 'esc' to close this overlay\n")
	b.WriteString("\n")
	b.WriteString("Navigation\n")
	b.WriteString("  j / ↓        move cursor down\n")
	b.WriteString("  k / ↑        move cursor up\n")
	b.WriteString("  enter        open detail view for selected agent\n")
	b.WriteString("  esc / ←      return to previous view (list ← detail ← describe)\n")
	b.WriteString("\n")
	b.WriteString("Views\n")
	b.WriteString("  (list view)   default — press 'enter' to drill in\n")
	b.WriteString("  (detail view) per-agent info — press 'd' to describe\n")
	b.WriteString("  (describe)    container inspect data — press 'esc' to go back\n")
	b.WriteString("\n")
	b.WriteString("Actions\n")
	b.WriteString("  r            force-refresh data from daemon\n")
	b.WriteString("  d            open describe view (from detail view)\n")
	b.WriteString("  ?            toggle this help overlay\n")
	b.WriteString("  q / ctrl+c   quit\n")
	b.WriteString("\n")
	b.WriteString("Coming in alpha.3\n")
	b.WriteString("  c            open chat view for the selected agent\n")
	b.WriteString("\n")
	b.WriteString("Coming in beta.1\n")
	b.WriteString("  l            open live log tail for the selected agent\n")
	b.WriteString("  :            enter command mode (:q :refresh :help)\n")
	b.WriteString("\n")
	b.WriteString("Flags\n")
	b.WriteString("  dclaw --no-mouse    disable mouse support (macOS Terminal.app fix)\n")
	return b.String()
}
```

### 4.11 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/model.go` (NEW)

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

// Model is the root bubbletea.Model for the dclaw TUI. It owns the entire
// application state and dispatches all messages to the appropriate sub-model.
// The single-model design avoids nested Update loops and makes state
// transitions explicit.
type Model struct {
	ctx context.Context
	rpc *client.RPCClient

	// current view
	current views.View

	// sub-models
	list     views.ListModel
	detail   views.DetailModel
	desc     views.DescribeModel
	help     views.HelpModel
	noDaemon views.NoDaemonModel

	// chrome
	width  int
	height int

	// selection: the name of the currently selected agent
	selected string

	// keys
	keys KeyMap
}

// attachTarget carries an optional pre-selected agent for RunAttached().
// If non-nil, the TUI starts on ViewDetail for that agent instead of ViewList.
type attachTarget struct {
	agentName string
}

// NewModel constructs the root Model. target is nil for a bare TUI launch.
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
		keys:     DefaultKeys(),
	}
	if target != nil {
		m.selected = target.agentName
		m.current = views.ViewDetail
	}
	return m
}

// Init sends the first fetch and schedules the poll timer.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		fetchAgents(m.ctx, m.rpc),
		tickPoll(),
	)
}

// Update dispatches incoming messages to the appropriate handler.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agentsLoadedMsg:
		m.list.SetAgents(msg.agents)
		// If we're on ViewNoDaemon and a load succeeded, restore ViewList.
		if m.current == views.ViewNoDaemon {
			m.current = views.ViewList
		}
		return m, tickPoll()

	case agentFetchedMsg:
		m.detail.SetAgent(msg.agent)
		m.desc.SetAgent(msg.agent)
		return m, tickPoll()

	case pollTickMsg:
		// Route poll to the appropriate fetch based on current view.
		switch m.current {
		case views.ViewDetail:
			if m.selected != "" {
				return m, fetchAgent(m.ctx, m.rpc, m.selected)
			}
		case views.ViewDescribe:
			// Describe is one-shot: no background refresh.
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
	}

	return m, nil
}

// View renders the full terminal output: top bar + main pane + bottom bar.
// When the help overlay is active it replaces everything.
func (m *Model) View() string {
	if m.help.Active() {
		return m.renderChrome("help", m.help.View(m.width, m.height-2))
	}

	var main string
	var viewName string

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
	default:
		viewName = "agents"
		main = m.list.View(m.width, m.height-2)
	}

	return m.renderChrome(viewName, main)
}

// renderChrome wraps main content with the top bar and bottom bar.
func (m *Model) renderChrome(viewName, main string) string {
	agentCount := len(m.list.Items())
	topContent := fmt.Sprintf("[%s]  daemon:ok  agents:%d", viewName, agentCount)
	if m.current == views.ViewNoDaemon {
		topContent = fmt.Sprintf("[%s]  daemon:DOWN", viewName)
	}
	top := TopBarStyle.Width(m.width).Render(topContent)

	var hintParts []string
	switch m.current {
	case views.ViewList:
		hintParts = []string{"↑↓/jk:nav", "enter:open", "r:refresh", "?:help", "q:quit"}
	case views.ViewDetail:
		hintParts = []string{"d:describe", "r:refresh", "esc:back", "?:help", "q:quit"}
	case views.ViewDescribe:
		hintParts = []string{"esc:back", "r:refresh", "?:help", "q:quit"}
	case views.ViewNoDaemon:
		hintParts = []string{"r:retry", "?:help", "q:quit"}
	default:
		hintParts = []string{"?:help", "q:quit"}
	}
	bottom := BottomBarStyle.Width(m.width).Render(strings.Join(hintParts, "  "))

	return lipgloss.JoinVertical(lipgloss.Left, top, main, bottom)
}

// handleKey dispatches keyboard events to the right view handler.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Help overlay: any key except another '?' closes it on esc; '?' toggles it.
	if m.help.Active() {
		switch msg.String() {
		case "?", "esc":
			m.help.Toggle()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	// Global keys active in all non-help views.
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help.Toggle()
		return m, nil
	case "r":
		// Manual refresh: schedule an immediate fetch regardless of view.
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

	// Per-view keys.
	switch m.current {
	case views.ViewList:
		return m.handleListKey(msg)
	case views.ViewDetail:
		return m.handleDetailKey(msg)
	case views.ViewDescribe:
		return m.handleDescribeKey(msg)
	case views.ViewNoDaemon:
		// No additional keys beyond global r/q/?
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
		name := m.list.SelectedName()
		if name != "" {
			m.selected = name
			m.current = views.ViewDetail
			// Kick an immediate fetch so the detail view is populated.
			return m, fetchAgent(m.ctx, m.rpc, name)
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
```

**Note:** `m.list.Items()` is referenced in `renderChrome`. Add a corresponding `Items()` method to `ListModel` in `views/list.go`:

```go
// Items returns the current agent slice (read-only view for rendering).
func (m *ListModel) Items() []client.Agent { return m.items }
```

### 4.12 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/app.go` (NEW)

```go
// Package tui is the dclaw interactive dashboard. Entry points:
//
//   - Run()             — bare `dclaw` launch on a TTY, starts on ViewList.
//   - RunAttached(name) — `dclaw agent attach <name>`, starts on ViewDetail.
//
// Both entry points share the same bubbletea.Program construction; Run() passes
// nil as the attach target, RunAttached() passes the agent name.
package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsmehatef/dclaw/internal/client"
)

// NoMouse is set to true by cmd/dclaw/main.go when the --no-mouse flag is
// passed. When true, mouse cell motion is NOT registered with bubbletea.
// Defaults to false (mouse on). Exposed as a package-level var so both Run()
// and RunAttached() pick it up without extra plumbing.
var NoMouse bool

// Run launches the TUI starting on the agent list. Called from
// cmd/dclaw/main.go when the bare invocation check passes.
func Run(socketPath string) error {
	return runTUI(socketPath, nil)
}

// RunAttached launches the TUI pre-focused on the detail view for the given
// agent. Called from internal/cli/agent_attach.go.
func RunAttached(socketPath, agentName string) error {
	return runTUI(socketPath, &attachTarget{agentName: agentName})
}

// runTUI is the shared implementation. It dials the daemon, constructs the
// root Model, and starts the bubbletea event loop.
func runTUI(socketPath string, target *attachTarget) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rpc := client.NewRPCClient(socketPath)

	// Attempt initial dial. On failure, we still launch the TUI but start on
	// the noDaemon view so the user gets a friendly error screen.
	var startErr error
	if err := rpc.Dial(ctx); err != nil {
		startErr = err
	}

	m := NewModel(ctx, rpc, target)
	if startErr != nil {
		// Pre-populate the noDaemon view and skip the initial list fetch.
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

// resolveSocketPath returns the default socket path. It mirrors
// internal/client.DefaultSocketPath() to avoid importing internal/client in
// this package (which would be fine but creates a tighter coupling).
// The value here is passed in from cmd/dclaw/main.go via Run(socket).
//
// NOTE: this function is intentionally unused in this file — socket resolution
// is the caller's responsibility (cmd/dclaw/main.go reads --daemon-socket).
func resolveSocketPath() string {
	return client.DefaultSocketPath()
}

// views import is needed for the ViewNoDaemon reference in runTUI.
// Add to imports: "github.com/itsmehatef/dclaw/internal/tui/views"
```

**Important:** `app.go` references `views.ViewNoDaemon` to pre-set the starting view. Add the import:

```go
import (
    "context"
    "fmt"

    tea "github.com/charmbracelet/bubbletea"

    "github.com/itsmehatef/dclaw/internal/client"
    "github.com/itsmehatef/dclaw/internal/tui/views"
)
```

And remove the unused `resolveSocketPath()` function body's `client.DefaultSocketPath()` call or mark the function `//nolint:unused` — the cleaner fix is to delete the function entirely since socket resolution lives in `cmd/dclaw/main.go`.

**Revised `app.go` without the dead function:**

```go
// Package tui is the dclaw interactive dashboard. Entry points:
//
//   - Run(socketPath)             — bare `dclaw` launch on a TTY, starts on ViewList.
//   - RunAttached(socketPath, name) — `dclaw agent attach <name>`, starts on ViewDetail.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/tui/views"
)

// NoMouse is set to true by cmd/dclaw/main.go when --no-mouse is passed.
// When true, tea.WithMouseCellMotion() is not registered.
var NoMouse bool

// Run launches the TUI on the default list view.
func Run(socketPath string) error {
	return runTUI(socketPath, nil)
}

// RunAttached launches the TUI pre-focused on the detail view for agentName.
func RunAttached(socketPath, agentName string) error {
	return runTUI(socketPath, &attachTarget{agentName: agentName})
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

### 4.13 `/Users/hatef/workspace/agents/atlas/dclaw/internal/tui/app_test.go` (NEW)

```go
package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// TestTUISmoke exercises the basic key dispatch without a live daemon.
// The Model is constructed directly (no RPC dial) so this test has no
// external dependencies.
func TestTUISmoke(t *testing.T) {
	// Build a model with no RPC client (nil). The noDaemon view handles the
	// nil gracefully because Init() will receive a daemonErrMsg when the
	// first fetchAgents call returns a connection error, but since we inject
	// keys immediately the test does not wait for that cycle.
	m := NewModel(t.Context(), nil, nil)
	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(120, 30),
	)

	// Navigate: down, up — should not crash on an empty list.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})

	// Open and close help overlay.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// Quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// TestTUIListNav verifies cursor movement clamping on an empty list.
func TestTUIListNav(t *testing.T) {
	m := NewModel(t.Context(), nil, nil)
	// Inject agents directly via the message path.
	_, _ = m.Update(agentsLoadedMsg{agents: nil})

	m.list.Down()
	m.list.Up()
	if m.list.SelectedName() != "" {
		t.Fatalf("expected empty selection, got %q", m.list.SelectedName())
	}
}
```

**Note:** `t.Context()` requires Go 1.21+. The `go.mod` has `go 1.25.0`, so this is fine.

**Note on nil RPC client in tests:** `fetchAgents(ctx, nil)` will panic when calling `nil.AgentList(...)`. To make `TestTUISmoke` safe, add a nil guard to `fetchAgents` in `poll.go`:

```go
func fetchAgents(ctx context.Context, rpc *client.RPCClient) tea.Cmd {
	return func() tea.Msg {
		if rpc == nil {
			return daemonErrMsg{err: fmt.Errorf("no rpc client")}
		}
		cctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		agents, err := rpc.AgentList(cctx)
		if err != nil {
			return daemonErrMsg{err: err}
		}
		return agentsLoadedMsg{agents: agents}
	}
}
```

Similarly add a nil guard to `fetchAgent` and `retryDial`.

### 4.14 `/Users/hatef/workspace/agents/atlas/dclaw/internal/cli/agent_attach.go` (NEW)

```go
package cli

import (
	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/tui"
)

// agentAttachCmd opens the TUI pre-focused on the detail view for the named
// agent. Chat mode is alpha.3 scope; for alpha.2 attach lands on ViewDetail.
var agentAttachCmd = &cobra.Command{
	Use:   "attach <name>",
	Short: "Open the TUI focused on a specific agent (detail view)",
	Long: `Attach opens the dclaw TUI pre-focused on the named agent's detail view.

In alpha.3, 'c' from detail will open the chat pane.
For alpha.2, this is equivalent to: dclaw; then navigate to the agent; then press enter.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.RunAttached(daemonSocket, args[0])
	},
}
```

**Wire it in `internal/cli/agent.go`:** in the `init()` func's `agentCmd.AddCommand(...)` call, add `agentAttachCmd` to the list. The current alpha.1 `agent.go` already has the comment `// ---------- attach (NEW) ----------` and a note that `agent_attach.go is deferred to alpha.2`. Simply add the import-free reference:

Change the `agentCmd.AddCommand(...)` call from:

```go
agentCmd.AddCommand(
    agentCreateCmd,
    agentListCmd,
    agentGetCmd,
    agentDescribeCmd,
    agentUpdateCmd,
    agentDeleteCmd,
    agentStartCmd,
    agentStopCmd,
    agentRestartCmd,
    agentLogsCmd,
    agentExecCmd,
)
```

To:

```go
agentCmd.AddCommand(
    agentCreateCmd,
    agentListCmd,
    agentGetCmd,
    agentDescribeCmd,
    agentUpdateCmd,
    agentDeleteCmd,
    agentStartCmd,
    agentStopCmd,
    agentRestartCmd,
    agentLogsCmd,
    agentExecCmd,
    agentAttachCmd, // alpha.2
)
```

### 4.15 Modified `cmd/dclaw/main.go`

Full file replacement:

```go
package main

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/itsmehatef/dclaw/internal/cli"
	"github.com/itsmehatef/dclaw/internal/tui"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "dclaw: panic: %v\n", r)
			os.Exit(1)
		}
	}()

	// Bare invocation on an interactive terminal = launch TUI.
	// Any args, any flags, non-TTY stdin/stdout = cobra.
	if shouldLaunchTUI(os.Args) {
		// Resolve --no-mouse before handing off. We do a manual scan of
		// os.Args here rather than parsing through cobra so the TUI launch
		// path is independent of cobra flag registration.
		for _, a := range os.Args[1:] {
			if a == "--no-mouse" {
				tui.NoMouse = true
			}
		}
		// Resolve the daemon socket path from the env/default (same logic as
		// internal/client.DefaultSocketPath, duplicated to avoid a circular
		// import between cmd/dclaw and internal/tui).
		socket := resolveSocket()
		if err := tui.Run(socket); err != nil {
			fmt.Fprintf(os.Stderr, "dclaw tui: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

// shouldLaunchTUI returns true only for the literal bare invocation
// (len(argv)==1) on an interactive terminal pair (stdin and stdout both TTY).
// Any argument, flag, or non-TTY context falls through to cobra.
func shouldLaunchTUI(argv []string) bool {
	if len(argv) != 1 {
		return false
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
		return false
	}
	return true
}

// resolveSocket mirrors daemon.DefaultSocketPath without importing internal/daemon,
// which would create an oversized dependency in main. The canonical copy lives in
// internal/client.DefaultSocketPath(); keep both in sync.
func resolveSocket() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/dclaw.sock"
	}
	return home + "/.dclaw/dclaw.sock"
}
```

**Note on `--no-mouse` and cobra flags:** The scan loop in `shouldLaunchTUI`'s caller handles `--no-mouse` for the bare TUI launch path. When the user runs `dclaw --no-mouse` (no subcommand but with the flag), `len(argv) == 2` so `shouldLaunchTUI` returns false and cobra handles it — cobra doesn't know `--no-mouse`, which will print a usage error. To avoid this, register `--no-mouse` as a PersistentFlag on `rootCmd` too (see §5 below).

### 4.16 Modified `internal/cli/root.go`

Add `--no-mouse` as a PersistentFlag so cobra does not error on `dclaw --no-mouse` and the flag is available to subcommands. Add a `PersistentPreRunE` that sets `tui.NoMouse`:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/tui"
)

var (
	outputFormat string
	daemonSocket string
	verbose      bool
	noMouse      bool
)

var rootCmd = &cobra.Command{
	Use:   "dclaw",
	Short: "dclaw — container-native multi-agent platform",
	Long: `dclaw is a container-native multi-agent platform.

It runs AI agents inside mandatory Docker sandboxes with per-agent isolation,
fleet management, and independently versioned channel plugins.

Run 'dclaw' with no arguments on an interactive terminal to open the TUI
dashboard.`,
	SilenceUsage:  true,
	SilenceErrors: false,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if noMouse {
			tui.NoMouse = true
		}
		return nil
	},
}

// Execute is the main entry point called from cmd/dclaw/main.go.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&outputFormat, "output", "o", "table",
		"output format for list/get/status commands: table, json, yaml",
	)
	rootCmd.PersistentFlags().StringVar(
		&daemonSocket, "daemon-socket", defaultSocketPath(),
		"path to the dclaw daemon Unix socket",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false,
		"verbose logging to stderr",
	)
	rootCmd.PersistentFlags().BoolVar(
		&noMouse, "no-mouse", false,
		"disable mouse support in the TUI (use on stock macOS Terminal.app)",
	)

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(channelCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(statusCmd)
}

func validateOutputFormat() error {
	switch outputFormat {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("invalid --output %q: must be one of table, json, yaml", outputFormat)
	}
}

func defaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/dclaw.sock"
	}
	return daemon.DefaultSocketPath(filepath.Join(home, ".dclaw"))
}

func newClient(ctx context.Context) (*client.RPCClient, error) {
	c := client.NewRPCClient(daemonSocket)
	if err := c.Dial(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func withClient(ctx context.Context, fn func(c *client.RPCClient) error) error {
	c, err := newClient(ctx)
	if err != nil {
		return DaemonUnreachable(err)
	}
	defer c.Close()
	return fn(c)
}
```

---

## 5. Modified Files — Diff Summary

### `go.mod` changes

Add five lines to the `require (...)` direct block. The existing direct block currently ends with `gopkg.in/yaml.v3 v3.0.1`. Insert before that line:

```diff
 require (
+	github.com/charmbracelet/bubbles   v1.0.0
+	github.com/charmbracelet/bubbletea v1.3.10
+	github.com/charmbracelet/lipgloss  v1.1.0
+	github.com/charmbracelet/x/exp/teatest v0.0.0-20260413165052-6921c759c913
 	github.com/docker/docker v26.1.3+incompatible
 	...
+	github.com/mattn/go-isatty v0.0.20
 	...
 )
```

`go-isatty` is already in the `require` indirect block as `v0.0.20`; promote it by removing the `// indirect` comment. `go mod tidy` will merge and reconcile.

### `internal/cli/agent.go` changes

One-line addition in `init()`. Add `agentAttachCmd` to the `agentCmd.AddCommand(...)` variadic call (see §4.14 above).

### `scripts/smoke-daemon.sh` changes

See §11 for the exact bash additions.

---

## 6. Keybinding Reference

```
Key          From view(s)    Action
----------   -------------   ------------------------------------------------------
j / ↓        list            cursor down
k / ↑        list            cursor up
enter        list            open detail view for selected agent
d            detail          open describe view
esc          detail          back to list
esc          describe        back to detail
backspace    detail          back to list (alias for esc)
backspace    describe        back to detail (alias for esc)
r            any             force-refresh data from daemon (retryDial if noDaemon)
?            any             toggle help overlay
?            help            close help overlay
esc          help            close help overlay
q            any             quit
ctrl+c       any             quit

--no-mouse   (flag)          disable bubbletea mouse cell motion (CLI flag, not TUI key)
```

Mouse left-click on a list row is equivalent to moving the cursor to that row and pressing `enter` (bubbletea's `tea.MouseMsg` handling — wired via `tea.WithMouseCellMotion()`). Mouse scrolling moves the list cursor. This is the default; `--no-mouse` disables it.

---

## 7. View Specifications

### 7.1 List View (ViewList)

**ASCII mockup (120×30 terminal):**

```
[agents]  daemon:ok  agents:3                                                                     120 cols
────────────────────────────────────────────────────────────────────────────────────────────────────────
NAME                      STATUS     IMAGE                          CREATED
────────────────────────────────────────────────────────────────────────────────────────────────────────
> alice                   running    dclaw-agent:v0.1               -
  bob                     stopped    dclaw-agent:v0.1               -
  charlie                 created    custom-agent:latest            -



  ↑↓/jk:nav  enter:open  r:refresh  ?:help  q:quit
```

**Data source:** `client.RPCClient.AgentList()` — calls `agent.list` on the daemon.

**Refresh cadence:** Automatic poll every 2s via `tickPoll()` + `agentsLoadedMsg`. Manual force-refresh on `r` key.

**Keybindings in this view:** `j`/`↓` down, `k`/`↑` up, `enter` → detail, `r` refresh, `?` help, `q` quit.

**Empty state:** Single row: `(no agents — run: dclaw agent create <name> --image=<img>)`

### 7.2 Detail View (ViewDetail)

**ASCII mockup:**

```
[detail: alice]  daemon:ok  agents:3
────────────────────────────────────────────────────────
Agent: alice
────────────────────────────────────────────────────────
  Status:    running
  Image:     dclaw-agent:v0.1
  Workspace: /Users/me/workspace/proj

  Labels:
    dclaw.managed = true
    dclaw.name    = alice

  Env:
    AGENT_NAME = alice

  Press 'd' to describe container, 'esc' to return to list.

  d:describe  r:refresh  esc:back  ?:help  q:quit
```

**Data source:** `client.RPCClient.AgentGet(name)` — calls `agent.get` on the daemon.

**Refresh cadence:** Polls `agent.get` every 2s while in this view. On `esc` the background tick fires `fetchAgents` instead.

**Keybindings:** `d` → describe, `esc`/`backspace` → list, `r` refresh, `?` help, `q` quit.

### 7.3 Describe View (ViewDescribe)

**ASCII mockup:**

```
[describe: alice]  daemon:ok  agents:3
────────────────────────────────────────────────────────
Describe: alice
────────────────────────────────────────────────────────

Container
  Image:     dclaw-agent:v0.1
  Status:    running
  Workspace: /Users/me/workspace/proj

Labels
  dclaw.managed = true
  dclaw.name    = alice

Environment
  AGENT_NAME = alice

Mounts
  /workspace ← /Users/me/workspace/proj (bind)

Network
  bridge (default)

Events
  (events from daemon.describe — wired in beta.1)

  Press 'esc' to return to detail view.

  esc:back  r:refresh  ?:help  q:quit
```

**Data source:** Uses the `client.Agent` record already fetched for the detail view. No additional RPC call in alpha.2. Beta.1 upgrades the data source to `agent.describe` (which returns the SQLite events table).

**Refresh cadence:** One-shot — not polled. Data is the last `agentFetchedMsg` payload stored in `m.desc`.

**Keybindings:** `esc`/`backspace` → detail, `r` refresh (re-fetches `agent.get`), `?` help, `q` quit.

### 7.4 Help Overlay (modal)

**Triggered by:** `?` from any view.

**Dismissed by:** `?` again, `esc`, or `q` (quit also terminates the program).

**Content:** Full keybinding reference including deferred-alpha keys (labelled "coming in alpha.3" / "coming in beta.1").

**Rendering:** Replaces entire terminal content. The chrome (top/bottom bars) is preserved — the overlay text is placed as the "main" content slot.

### 7.5 NoDaemon View (ViewNoDaemon)

**Triggered by:** Failed `rpc.Dial()` at startup, or any `daemonErrMsg` during polling.

**ASCII mockup:**

```
[no-daemon]  daemon:DOWN
────────────────────────────────────────────────────────


  dclaw daemon is not running

  Error: dial /Users/me/.dclaw/dclaw.sock: connect: no such file or directory

  Start the daemon:  dclaw daemon start

  Press 'r' to retry, 'q' to quit.



  r:retry  ?:help  q:quit
```

**Recovery:** Press `r` → calls `retryDial` (dials + re-fetches agent list). On success, transitions to `ViewList`.

---

## 8. State Machine

```
          ┌──────────────────────────────────────────────┐
          │   Any view  ──── ? ────> HelpOverlay          │
          │   Any view  ──── q ────> [quit]               │
          │   HelpOverlay ─ ?/esc ─> previous view        │
          └──────────────────────────────────────────────┘

                  startup / daemon-err
                  ↓
         ┌─────────────────┐
         │   NoDaemon      │ ←── any daemonErrMsg
         └─────────────────┘
               │ r (retry succeeds)
               ↓
         ┌─────────────────┐
         │   List          │ ←── esc from Detail
         └─────────────────┘
               │ enter
               ↓
         ┌─────────────────┐
         │   Detail        │ ←── esc from Describe
         └─────────────────┘
               │ d
               ↓
         ┌─────────────────┐
         │   Describe      │
         └─────────────────┘
```

**Transitions:**

| From | Key | To | Side-effect |
|---|---|---|---|
| List | enter | Detail | `fetchAgent(selected)` fired |
| List | ? | HelpOverlay | |
| Detail | esc / backspace | List | `fetchAgents()` fired |
| Detail | d | Describe | |
| Detail | ? | HelpOverlay | |
| Describe | esc / backspace | Detail | |
| Describe | ? | HelpOverlay | |
| HelpOverlay | ? / esc | (previous) | |
| NoDaemon | r | (NoDaemon or List) | `retryDial()` fired; on success → List |
| Any | q / ctrl+c | [quit] | `tea.Quit` |
| Any | daemonErrMsg | NoDaemon | |

**Invariant:** `m.selected` is never cleared on `esc`. If the user goes List→Detail→Describe→Detail→List→Detail, the same agent is pre-selected in Detail on the second entry.

---

## 9. Polling Strategy

### Cadence

- List view: every 2s (`tickPoll()` → `fetchAgents`).
- Detail view: every 2s (`tickPoll()` → `fetchAgent(m.selected)`).
- Describe view: NOT polled (one-shot).
- NoDaemon view: NOT polled (user-driven retry via `r`).
- Help overlay: poll is suspended — `pollTickMsg` is ignored while help is active (falls through the `Update` default branch).

### Cancellation on view change

The poll timer is implemented as a recursive `tea.Tick`. Each `pollTickMsg` handler either fires the appropriate fetch or returns `nil` (no next tick) for views that don't poll. The next `tickPoll()` is only scheduled as the return value of a successful `agentsLoadedMsg` or `agentFetchedMsg` handler. This means:

- Switching to Describe cancels the running poll (next `agentFetchedMsg` will be the last; its handler schedules no new tick because `m.current == ViewDescribe` triggers `return m, nil`).
- Switching back to Detail resumes: the `r`-key handler or the natural `esc`-key handler fires a fresh `fetchAgent` which on success schedules the next tick.

### Backoff on daemon errors

Alpha.2 does not implement exponential backoff. On `daemonErrMsg`, the TUI transitions to `ViewNoDaemon` and stops polling. The user presses `r` to retry. Beta.1 can add automatic reconnect with the backoff schedule from wire-protocol-spec.md §11.4 (100ms / 200ms / 400ms / 800ms / 1600ms / 5000ms).

### Context lifetime

The root `context.Context` (created in `runTUI`) is cancelled in the `defer cancel()` on TUI exit. All in-flight `fetchAgents`/`fetchAgent` commands that hold a derived `cctx` will be cancelled cleanly.

---

## 10. `--no-mouse` Flag

### Plumbing chain

```
os.Args scan in cmd/dclaw/main.go (bare TUI launch path)
  → tui.NoMouse = true
  → runTUI() checks tui.NoMouse
  → tea.WithMouseCellMotion() is NOT added to opts

cobra PersistentPreRunE in internal/cli/root.go (subcommand path)
  → reads noMouse bool var (bound to --no-mouse PersistentFlag)
  → tui.NoMouse = true
  → RunAttached() checks tui.NoMouse
  → tea.WithMouseCellMotion() is NOT added to opts
```

### Why two paths

Bare `dclaw` (no subcommand) bypasses cobra entirely — the `shouldLaunchTUI` check fires before `cli.Execute()`. So the flag must be checked both in the raw `os.Args` scan (for the bare path) and in cobra's `PersistentPreRunE` (for `dclaw agent attach`).

### Effect when disabled

`tea.WithMouseCellMotion()` is not passed to `tea.NewProgram`. Bubbletea receives no `tea.MouseMsg` events. The TUI is fully functional via keyboard. Users on stock macOS Terminal.app who experience ghost characters or scroll artefacts should use `dclaw --no-mouse`.

### Note: no click-to-select in alpha.2

The `--no-mouse` flag toggles bubbletea's cell-motion capture; it does **not** toggle click-to-select on list rows — because click-to-select is not wired in alpha.2. With mouse on (default), cell-motion capture forwards scroll-wheel events and instructs the terminal emulator to suppress its native text-selection highlighting during TUI use (which prevents highlight artefacts from corrupting the rendered view). With `--no-mouse`, cell-motion is not registered, so terminal-native text selection works normally for copy/paste — at the cost of the scroll wheel no longer reaching future scrollable views (logs in beta.1, chat transcript in alpha.3). There are **zero `tea.MouseMsg` handlers in alpha.2's `Update` funcs**; clicking anywhere in the TUI does nothing. Click-to-select on list rows is **deferred to beta.1**.

---

## 11. Housekeeping

### 11.1 teatest pseudoversion fix

**Problem:** The alpha.1 plan pinned `github.com/charmbracelet/x/exp/teatest v0.0.0-20240229115032-4b47b6fdaf28`. The upstream repository rebased and that commit no longer exists, causing `go mod download` to fail with a "410 Gone" or checksum mismatch.

**Fix:** Replace with the current pseudoversion `v0.0.0-20260413165052-6921c759c913` (verified against `pkg.go.dev/github.com/charmbracelet/x/exp/teatest` on 2026-04-16).

In `go.mod`, change:

```
github.com/charmbracelet/x/exp/teatest v0.0.0-20240229115032-4b47b6fdaf28
```

to:

```
github.com/charmbracelet/x/exp/teatest v0.0.0-20260413165052-6921c759c913
```

Then run `go mod tidy` and commit the updated `go.sum`.

### 11.2 `pid -1` log cosmetic fix

**Problem:** `scripts/smoke-daemon.sh` logs `pid -1` in some scenarios because `daemonProc.Process.Pid` can be `-1` if the OS has not yet assigned a PID at the time `fmt.Fprintf` runs after `daemonProc.Start()` + `Process.Release()`.

**Fix:** In `internal/cli/daemon.go`'s `daemonStartCmd.RunE`, read the PID from the pidfile after the socket becomes reachable, rather than using `daemonProc.Process.Pid`. The pidfile is written by `dclawd`'s `cfg.WritePIDFile(os.Getpid())` call; after the socket poll confirms the daemon is up, the pidfile exists and contains the real PID.

Change in `internal/cli/daemon.go`:

```go
// After the socket-ready loop succeeds:
pid, _ := cfg.ReadPIDFile()
if pid <= 0 {
    pid = daemonProc.Process.Pid // fallback to the forked PID if pidfile not yet written
}
fmt.Fprintf(cmd.OutOrStdout(), "dclaw daemon started (pid %d, socket %s)\n",
    pid, cfg.SocketPath)
```

This replaces the existing line inside the `if _, err := os.Stat(cfg.SocketPath); err == nil {` block.

### 11.3 Smoke script negative-path additions

Add four tests to `/Users/hatef/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` after the current Test 7 "daemon stop" and before `echo "All daemon smoke tests passed."`:

```bash
# ---- NEGATIVE PATH TESTS ----
# These tests verify that the daemon and CLI return proper errors for invalid
# operations. Each test restarts the daemon in a fresh STATE_DIR because some
# negative paths leave the daemon in a broken state.

echo "--- Test 8: duplicate agent name is rejected ---"
# Restart daemon for this test.
STATE_DIR_NEG=$(mktemp -d -t dclaw-smoke-neg-XXXX)
SOCKET_NEG="$STATE_DIR_NEG/dclaw.sock"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG" daemon start || fail "neg-start"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG" agent create dup \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_NEG" || fail "dup-create-1"
if "$DCLAW_BIN" --daemon-socket "$SOCKET_NEG" agent create dup \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_NEG" 2>/dev/null; then
  fail "duplicate agent name should have been rejected"
fi
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG" daemon stop >/dev/null 2>&1 || true
rm -rf "$STATE_DIR_NEG"
pass "duplicate agent name rejected"

echo "--- Test 9: get non-existent agent returns error ---"
STATE_DIR_NEG2=$(mktemp -d -t dclaw-smoke-neg2-XXXX)
SOCKET_NEG2="$STATE_DIR_NEG2/dclaw.sock"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG2" daemon start || fail "neg2-start"
if "$DCLAW_BIN" --daemon-socket "$SOCKET_NEG2" agent get nosuchagent 2>/dev/null; then
  fail "get non-existent agent should have failed"
fi
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG2" daemon stop >/dev/null 2>&1 || true
rm -rf "$STATE_DIR_NEG2"
pass "get non-existent agent returned error"

echo "--- Test 10: daemon already-running is idempotent ---"
STATE_DIR_NEG3=$(mktemp -d -t dclaw-smoke-neg3-XXXX)
SOCKET_NEG3="$STATE_DIR_NEG3/dclaw.sock"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG3" daemon start || fail "neg3-start-1"
# Starting again should print "already running" and exit 0, not error.
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG3" daemon start || fail "neg3-start-2 (idempotent start failed)"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG3" daemon stop >/dev/null 2>&1 || true
rm -rf "$STATE_DIR_NEG3"
pass "daemon start is idempotent"

echo "--- Test 11: daemon CLI fails gracefully when daemon is not running ---"
BAD_SOCKET="/tmp/dclaw-smoke-notexist-$$.sock"
OUT=$("$DCLAW_BIN" --daemon-socket "$BAD_SOCKET" agent list 2>&1 || true)
echo "$OUT" | grep -qi "not running\|no such file\|connection refused\|dial" \
  || fail "expected daemon-not-running error, got: $OUT"
pass "CLI fails gracefully when daemon not running"
```

These four tests cover: duplicate-name rejection (Test 8), get-nonexistent (Test 9), daemon-already-running idempotence (Test 10), and docker-unavailable / no-daemon CLI graceful failure (Test 11). Tests 8–10 create and clean up their own state dirs to avoid interfering with each other.

---

## 12. Step-by-Step Implementation Order

Split across three parallel agents. Each agent owns a disjoint set of files so there are no merge conflicts. Agent A finishes first (scaffolding), then Agent B and C can run in parallel.

### Agent A — Scaffolding (internal/tui foundation)

Agent A creates all `internal/tui/` files that have no inter-file dependencies within the TUI package. It does NOT touch cobra or main.go.

**Prerequisites:** Agent A starts from the `v0.3.0-alpha.1` HEAD.

1. Update `go.mod`: add the five new direct deps from §2. Run `go mod tidy`. Commit the `go.mod` + `go.sum` changes as a standalone "add charmbracelet deps" commit.

2. Create `internal/tui/views/view.go` — copy §4.1 verbatim.

3. Create `internal/tui/messages.go` — copy §4.2 verbatim.

4. Create `internal/tui/keys.go` — copy §4.3 verbatim.

5. Create `internal/tui/styles.go` — copy §4.4 verbatim.

6. Create `internal/tui/poll.go` — copy §4.5 verbatim. Add the nil guard described in §4.13 to both `fetchAgents` and `fetchAgent` (add a `fmt.Errorf` import for the nil-client error).

7. Create `internal/tui/views/noDaemon.go` — copy §4.6 verbatim.

8. Create `internal/tui/views/list.go` — copy §4.7 verbatim. Add the `Items()` method noted at the end of §4.11.

9. Create `internal/tui/views/detail.go` — copy §4.8 verbatim.

10. Create `internal/tui/views/describe.go` — copy §4.9 verbatim.

11. Create `internal/tui/views/help.go` — copy §4.10 verbatim.

12. Create `internal/tui/model.go` — copy §4.11 verbatim. Fix the import block and ensure the `views` sub-package is referenced correctly.

13. Create `internal/tui/app.go` — copy the final clean version in §4.12 verbatim.

14. Compile check: `go build ./internal/tui/...`. Fix any import-cycle, undefined-symbol, or vet errors.

15. Create `internal/tui/app_test.go` — copy §4.13 verbatim.

16. Run unit tests: `go test ./internal/tui/...`. Expect TestTUISmoke and TestTUIListNav to pass.

17. Commit all `internal/tui/` files.

**Agent A delivers:** A compilable, unit-tested `internal/tui/` package. No changes to CLI, main.go, or scripts.

---

### Agent B — CLI wiring (cobra + main.go)

Agent B starts after Agent A's commit is merged (because `agent_attach.go` and `root.go` import `internal/tui`).

1. Create `internal/cli/agent_attach.go` — copy §4.14 verbatim.

2. Modify `internal/cli/agent.go` — add `agentAttachCmd` to `agentCmd.AddCommand(...)` per §4.14.

3. Modify `internal/cli/root.go` — full file replacement per §4.16.

4. Modify `cmd/dclaw/main.go` — full file replacement per §4.15.

5. Compile check: `go build ./cmd/dclaw`. Fix any errors.

6. Manual check on a TTY:
   ```bash
   ./bin/dclaw                    # must open TUI (even with daemon down → noDaemon view)
   ./bin/dclaw --no-mouse         # must open TUI without mouse (noDaemon view since daemon likely not running)
   ./bin/dclaw --help             # must print cobra help
   echo "" | ./bin/dclaw          # must print cobra help (non-TTY stdin)
   ./bin/dclaw agent attach foo   # must open TUI on ViewDetail (noDaemon view if daemon down)
   ```

7. Start the daemon and repeat:
   ```bash
   ./bin/dclaw daemon start
   ./bin/dclaw agent create alice --image=dclaw-agent:v0.1 --workspace=/tmp
   ./bin/dclaw                    # list view with alice
   ./bin/dclaw agent attach alice # detail view for alice
   ./bin/dclaw daemon stop
   ```

8. Run existing CLI tests: `go test ./internal/cli/...`. Update `TestHelpDoesNotError` to include `"agent attach --help"` in the cases slice:

   ```go
   "agent attach --help",
   ```

9. Run full test suite: `go test ./...`.

10. Commit all modified CLI and main files.

**Agent B delivers:** Bare `dclaw` launches TUI. `dclaw agent attach <name>` opens TUI on ViewDetail. `--no-mouse` flag wired end-to-end.

---

### Agent C — Housekeeping

Agent C is fully independent and can run in parallel with Agent B from Agent A's commit.

1. Fix teatest pseudoversion in `go.mod` — change the old pseudoversion to `v0.0.0-20260413165052-6921c759c913` per §11.1.

2. Run `go mod tidy` — verify `go.sum` updates cleanly.

3. Fix `pid -1` log in `internal/cli/daemon.go` — apply the `ReadPIDFile()` fallback change from §11.2. Locate the `fmt.Fprintf(cmd.OutOrStdout(), "dclaw daemon started (pid %d ...")` line and replace with the updated snippet.

4. Modify `scripts/smoke-daemon.sh` — insert the four negative-path tests from §11.3 after Test 7 (before the final `echo "All daemon smoke tests passed."` line).

5. Add `scripts/smoke-tui.sh` (if not already present from alpha.1):
   ```bash
   #!/usr/bin/env bash
   # Wrapper that runs the teatest TUI smoke via go test.
   set -euo pipefail
   go test -run TestTUISmoke -v ./internal/tui/... -timeout 60s
   ```
   `chmod +x scripts/smoke-tui.sh`

6. Enable the Makefile `tui` and `smoke-tui` targets. The alpha.1 Makefile stubs them with `@exit 1` / `@exit 0`. Replace the stubs with the real targets from the alpha.1 plan §7.34:
   ```makefile
   tui: cli
   	DCLAWD_BIN=$(BIN_DIR)/$(BINARY_D) $(BIN_DIR)/$(BINARY_CLI)

   smoke-tui: build
   	DCLAW_BIN=$(BIN_DIR)/$(BINARY_CLI) DCLAWD_BIN=$(BIN_DIR)/$(BINARY_D) \
   		./scripts/smoke-tui.sh
   ```

7. Run `./scripts/smoke-daemon.sh` (requires Docker and `dclaw-agent:v0.1` image). All 11 tests should pass.

8. Commit housekeeping changes.

**Agent C delivers:** Fixed teatest pin, fixed pid-1 log, expanded smoke script with 4 negative-path tests, active `smoke-tui` Makefile target.

---

### Final integration step (after A, B, C merged)

Once all three agents' commits are on `main`:

1. `make build` — both binaries compile.
2. `go test ./...` — all tests pass including `TestTUISmoke` and `TestTUIListNav`.
3. `go vet ./...` — clean.
4. `./scripts/smoke-daemon.sh` — 11 tests pass.
5. `make smoke-tui` — `TestTUISmoke` passes.
6. Manual TUI smoke (see §13).
7. Commit tag `v0.3.0-alpha.2`.

---

## 13. Test Plan

### Automated tests

| Test | Location | What it exercises |
|---|---|---|
| `TestTUISmoke` | `internal/tui/app_test.go` | Key dispatch (down, up, ?, esc, q) on empty model; program terminates cleanly |
| `TestTUIListNav` | `internal/tui/app_test.go` | Cursor clamping on empty list |
| `TestHelpDoesNotError` | `internal/cli/cli_test.go` | Cobra help for all subcommands including `agent attach --help` |
| `TestInvalidOutputFormat` | `internal/cli/cli_test.go` | validateOutputFormat rejects bad values |
| All existing alpha.1 unit tests | `internal/{store,protocol,daemon,client,sandbox}` | Regression (must still pass) |
| `./scripts/smoke-daemon.sh` | bash | 11 tests: 7 positive CRUD + 4 negative paths |
| `make smoke-tui` | `scripts/smoke-tui.sh` | `TestTUISmoke` via go test |

### Manual TUI smoke procedure

Execute these steps in order against a live Docker daemon with `dclaw-agent:v0.1` available.

```
1. Build both binaries
   make build

2. Start daemon
   ./bin/dclaw daemon start
   EXPECT: "dclaw daemon started (pid <N>, socket ~/.dclaw/dclaw.sock)"
   EXPECT: pid is a positive integer (not -1)

3. Create two agents
   ./bin/dclaw agent create alice --image=dclaw-agent:v0.1 --workspace=/tmp
   ./bin/dclaw agent create bob   --image=dclaw-agent:v0.1 --workspace=/tmp

4. Launch TUI (default, mouse enabled)
   ./bin/dclaw
   EXPECT: TUI opens in alt-screen
   EXPECT: top bar shows "[agents]  daemon:ok  agents:2"
   EXPECT: alice and bob appear in the list
   EXPECT: cursor is on alice (first row, marked with ">")

5. Navigate down then up
   Press j — bob highlighted
   Press k — alice highlighted again
   EXPECT: no crash, cursor moves correctly

6. Open detail view
   Press enter on alice
   EXPECT: transitions to "[detail: alice]" top bar
   EXPECT: detail pane shows Status, Image, Workspace fields
   EXPECT: poll continues (detail view re-renders every ~2s)

7. Open describe view
   Press d
   EXPECT: transitions to "[describe: alice]" top bar
   EXPECT: Container section shows Image and Workspace
   EXPECT: Mounts section shows /workspace ← /tmp (bind)
   EXPECT: Events section shows "(events from daemon.describe — wired in beta.1)"

8. Navigate back
   Press esc
   EXPECT: back to "[detail: alice]"
   Press esc again
   EXPECT: back to "[agents]" list view

9. Help overlay
   Press ?
   EXPECT: help overlay appears with keybinding reference
   EXPECT: top/bottom bars still visible
   Press esc
   EXPECT: returns to list view

10. Force refresh
    Press r
    EXPECT: list reloads (agents count may flicker briefly)

11. Quit
    Press q
    EXPECT: TUI closes, terminal restores to normal (no residual alt-screen)
    EXPECT: exit code 0

12. Test --no-mouse flag
    ./bin/dclaw --no-mouse
    EXPECT: TUI opens identically to step 4
    EXPECT: no mouse events registered (verify by checking that mouse clicks
            do NOT move cursor or drill into detail — behaviour is terminal-
            dependent but no crash)

13. Test noDaemon screen
    ./bin/dclaw daemon stop
    ./bin/dclaw
    EXPECT: "[no-daemon]  daemon:DOWN" top bar
    EXPECT: error message shows "daemon not running" and suggests dclaw daemon start
    EXPECT: pressing r shows "retrying" then re-shows noDaemon (daemon is stopped)
    EXPECT: pressing q exits cleanly

14. Test agent attach
    ./bin/dclaw daemon start
    ./bin/dclaw agent attach alice
    EXPECT: TUI opens directly in "[detail: alice]" view
    EXPECT: no list view visible
    Press esc
    EXPECT: transitions to list view (with alice and bob)
    Press q to quit

15. Cleanup
    ./bin/dclaw agent delete alice
    ./bin/dclaw agent delete bob
    ./bin/dclaw daemon stop
```

---

## 14. Release Checklist for v0.3.0-alpha.2

1. [ ] All alpha.1 release checklist items still green
2. [ ] `go vet ./...` clean
3. [ ] `go build ./...` produces both `./bin/dclaw` and `./bin/dclawd` without errors
4. [ ] `go test ./...` passes (includes `TestTUISmoke`, `TestTUIListNav`, all alpha.1 tests)
5. [ ] `./scripts/smoke-daemon.sh` passes all 11 tests (7 original + 4 negative paths)
6. [ ] `make smoke-tui` passes (`TestTUISmoke` via teatest)
7. [ ] Manual TUI smoke procedure (§13 steps 1–15) completed without crash or regression
8. [ ] `./bin/dclaw` bare invocation opens TUI on a TTY (verified in step 4 above)
9. [ ] `./bin/dclaw --no-mouse` opens TUI without mouse (verified in step 12)
10. [ ] noDaemon view appears when daemon is not running (verified in step 13)
11. [ ] `dclaw agent attach <name>` opens TUI on ViewDetail (verified in step 14)
12. [ ] `./bin/dclaw --help` prints cobra help and does NOT launch TUI
13. [ ] `echo "" | ./bin/dclaw` prints cobra help and does NOT launch TUI (non-TTY guard)
14. [ ] teatest pseudoversion fix is in `go.mod` (`v0.0.0-20260413165052-6921c759c913`)
15. [ ] `pid -1` cosmetic fix applied in `internal/cli/daemon.go`
16. [ ] `go.mod` has bubbletea v1.3.10, lipgloss v1.1.0, bubbles v1.0.0 as direct deps
17. [ ] Commit message: `"Phase 3 alpha.2: TUI dashboard (list + detail + describe) (v0.3.0-alpha.2)"`
18. [ ] Commit tagged: `git tag -a v0.3.0-alpha.2 -m "Phase 3 alpha.2: TUI dashboard"`
19. [ ] Tag pushed: `git push origin main v0.3.0-alpha.2`
20. [ ] Handoff doc updated: `~/.claude/projects/-Users-hatef-workspace-agents-atlas/handoff/dclaw.md`
    - "Last updated" = 2026-04-16 (or actual date)
    - New sub-milestone: `v0.3.0-alpha.2 — TUI dashboard (list + detail + describe + help + noDaemon)`
    - Status: Phase 3 alpha.2 complete; alpha.3 (chat streaming) next

---

## 15. Open Questions

No blocking open questions were identified during the architecture review. All design decisions below are decided and locked.

**Q1: bubbletea v1 vs v2**

The alpha.1 plan pre-selected bubbletea v0.25.0. As of 2026-04-16, bubbletea v1.3.10 is the latest stable v1.x and v2 is in release-candidate phase at a different module path (`charm.land/bubbletea/v2`). This plan uses v1.3.10.

Decision: v1.3.10. Rationale: v2's module path change (`charm.land/`) is a large migration that touches every `import "github.com/charmbracelet/bubbletea"` line. The v1 API is stable and fully capable for alpha.2 scope. Revisit for v0.4.0 when the v2 path stabilises.

**Q2: List model — hand-rolled vs bubbles/list.Model**

The alpha.1 plan used a hand-rolled list renderer. This plan keeps that approach.

Decision: hand-rolled. Rationale: the five-column row format with coloured STATUS badge is easier to own than customising the list.Model delegate. Beta.1 can migrate to list.Model if the agent count grows large enough to warrant virtualised scrolling.

**Q3: `dclaw agent attach` pre-selects ViewDetail, not ViewChat**

The alpha.1 plan pre-selected ViewChat for `agent attach`. Chat mode is deferred to alpha.3.

Decision: `attach` opens ViewDetail in alpha.2. ViewChat is not implemented. The `agentAttachCmd.Long` field documents this explicitly so users are not surprised. Alpha.3 changes the target from ViewDetail to ViewChat (a one-line change in `agent_attach.go`).

**Q4: `pid -1` fix scope**

The fix in §11.2 applies to `internal/cli/daemon.go`. This is a cosmetic fix in the CLI, not in the daemon itself.

Decision: fix in alpha.2. Rationale: it is a cosmetic bug visible in every smoke run. The fix is three lines and has no risk of breaking anything.

**Q5: Smoke script negative-path tests require Docker**

Tests 8–10 in the expanded smoke script create real daemon processes, which requires Docker to be reachable (for the handshake — even though these tests never start a container). Test 11 does not require Docker.

Decision: all four tests run in `smoke-daemon.sh` (tag-push-only in CI per the alpha.1 Makefile/CI config). They do not run in `smoke-tui.sh` (which runs in all CI jobs). This preserves the alpha.1 CI invariant that the Docker-dependent smoke is gated on tag pushes.

**Q6: `bubbles v1.0.0` dependency is added but not yet used**

The bubbles package (viewport, textinput, help, key sub-packages) is added to `go.mod` in alpha.2 so that `go mod tidy` resolves it and `go.sum` is stable. Alpha.2 uses only `bubbles/key` (for `key.Binding`). The full viewport/list/textinput widgets are used in beta.1.

Decision: add as direct dep in alpha.2 even though only `key` is consumed. Rationale: having the full package resolved avoids a `go mod tidy` churn when beta.1 adds the other sub-packages.

---

*End of plan. Section counts: 15 sections. Implementation agents should work from §4 (exact file contents) and §12 (implementation order) without referring back to this prompt or to `docs/phase-3-daemon-plan.md`.*

Sources:
- [tea package - github.com/charmbracelet/bubbletea - Go Packages](https://pkg.go.dev/github.com/charmbracelet/bubbletea)
- [GitHub - charmbracelet/bubbletea: A powerful little TUI framework](https://github.com/charmbracelet/bubbletea)
- [teatest package - github.com/charmbracelet/x/exp/teatest - Go Packages](https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest)
- [lipgloss package - github.com/charmbracelet/lipgloss - Go Packages](https://pkg.go.dev/github.com/charmbracelet/lipgloss@v1.1.0)
- [bubbles package - github.com/charmbracelet/bubbles - Go Packages](https://pkg.go.dev/github.com/charmbracelet/bubbles@v1.0.0)
