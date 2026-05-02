# Phase 3 Beta.3 Wipe-Recovery Plan — v0.3.0-beta.3 Re-Derivation of Lost beta.1 Product Content

**Goal:** One batched, five-PR series that re-derives the three pieces of product content lost when Hatef's machine was wiped on 2026-04-18 mid-beta.1: the **logs view** (TUI live-tail of agent stdout/stderr via a streaming RPC), **toasts** (bottom-right floating notifications), and **chat history persistence** (SQLite-backed per-agent message log loaded on every chat open). Phase-0 prep — schema migration `0003_chat_history.sql`, new protocol types, and repo methods — re-derives the lost `d84554d` commit. Zero infrastructure work; every line is product-surface that the original beta.1 plan had already designed and shipped privately before the wipe took the disk. beta.3 lands these on top of the hardened sandbox + paths floor that beta.1-paths-hardening + beta.2-sandbox-hardening + the beta.2.X polish series have laid down.

**Prereq:** `v0.3.0-beta.2.6-platform-port` tagged at commit `fe69729`. Main tip `5b8d46d` (WORKLOG-only commit on top). `docs/phase-3-beta2-sandbox-hardening-plan.md` is the most recently shipped phase doc and the structural template for this plan. Migrations on disk: `0001_initial.sql`, `0002_workspace_trust.sql`. `go 1.25.0` installed; Docker reachable for end-to-end smoke (Tests 24/25 require it). `dclaw-agent:v0.1` image built. `internal/daemon/logs.go` already contains a `LogStreamer` skeleton (alpha.1-vintage stub with `LogsFollow` plumbing into `internal/sandbox/docker.go:LogsFollow`); beta.3 wires it to a real RPC.

---

## 0. Status

**DRAFT (2026-04-25).** Plan written; build not yet started. Gated on Hatef sign-off.

| Field | Value |
|---|---|
| **Target tag** | `v0.3.0-beta.3-wipe-recovery` (with `.1`, `.2`, ... patch revs as needed, matching the beta.1 / beta.2 cadence) |
| **Branch** | `main` (single batched review cycle; sub-branches per PR, squash-merged) |
| **Base commit** | `5b8d46d` (main tip — WORKLOG-only commit on top of `v0.3.0-beta.2.6-platform-port` @ `fe69729`) |
| **Est. duration** | 3–4 days (5 PRs: Phase-0 + A + (B parallel C) + D) |
| **Prereqs** | beta.2.6 green; smoke-daemon.sh Tests 1-23 green on tip; main-push docker-smoke trigger active (per beta.2.1) |
| **Trigger** | Pre-wipe content recovery — original beta.1 plan & code shipped privately on Hatef's machine before the 2026-04-18 wipe; never pushed. WORKLOG.md 2026-04-19 lists the lost artifacts: `29633a8` (logs view + Test 14), Agent B (toasts, Q1=bottom-right-float), Agent C (chat history, Q2=Option A "load on every openChat"), Phase 0 commit `d84554d` (migration `0003_` + protocol types + repo methods). |

---

## 1. Overview

beta.1-paths-hardening and beta.2-sandbox-hardening were both infrastructure pivots forced by the 2026-04-18 wipe; the original beta.1 plan was a **product** plan and three of its four PRs delivered user-facing TUI capability. beta.3 is the re-derivation. None of beta.3's surface area touches sandbox posture, paths policy, or the wire-protocol envelope — those are stable. What changes is the content INSIDE the wire: a new streaming RPC (`agent.logs.stream`), two new chat-history RPCs (`agent.chat.history.list`, `agent.chat.history.append`), one new SQLite table (`chat_messages`), one new TUI view (`ViewLogs`), one new TUI component (`components/toasts`), and a slice of integration glue.

The hole has three orthogonal product dimensions, each with its own implementation site:

1. **Logs view.** TUI today has no live-tail mechanism; users must shell out to `dclaw agent logs -f <name>` (a tight-loop poll added in alpha.1, see `internal/client/rpc.go:332-365 agentLogsFollowPoll`). A native TUI view is overdue. The streaming groundwork is half-built: `internal/daemon/logs.go` already has a `LogStreamer` struct that wraps `internal/sandbox/docker.go:LogsFollow`. beta.3 finishes the wiring with a JSON-RPC streaming method (`agent.logs.stream` mirroring `agent.chat.send` / `agent.chat.chunk` precedent) and a new `internal/tui/views/logs.go` that consumes the stream.
2. **Toasts.** TUI has no transient-notification surface today. Errors render in-place (e.g., `m.chat.AppendError`) or transition the whole view to `ViewNoDaemon`. Bottom-right floating notifications are needed for non-blocking events: agent created/deleted/started/stopped, daemon disconnected, error during render. Per the lost original Q1 decision, the Hatef-locked answer is **bottom-right float**, not a status-line ribbon.
3. **Chat history persistence.** TUI's `ChatModel` (alpha.3) holds messages in RAM and zeros them on `Reset()` whenever the user leaves `ViewChat`. Re-opening chat starts from scratch. Per the lost Q2 decision, the Hatef-locked answer is **Option A — load every openChat**, simple non-cached round-trip; no fancy caching, no incremental sync.

**What beta.3-wipe-recovery delivers (IN SCOPE):**

- **PR-Phase0 — Schema migration + protocol types + repo methods.** Re-derives the lost `d84554d` commit from scratch. New `internal/store/migrations/0003_chat_history.sql` adding a `chat_messages` table (FK to `agents.id`, with role / content / parent_id / message_id / sequence / timestamp). New protocol types in `internal/protocol/messages.go`: `LogsStreamParams`, `LogsStreamLineNotification`, `ChatHistoryListParams`, `ChatHistoryListResult`, `ChatHistoryAppendParams`, `ChatHistoryAppendResult`, `ChatMessage` wire shape. New repo methods on `internal/store/repo.go`: `InsertChatMessage`, `ListChatHistory(agentID string, limit int)`, `DeleteChatHistoryForAgent(agentID string)` (called from `DeleteAgent` so chat-history rows get cascade-deleted via the `ON DELETE CASCADE` FK declared in the migration; `DeleteChatHistoryForAgent` exists for tests + future operator subcommands). One Phase-0 commit, no behavior change visible to the CLI/TUI yet — gates everything below.
- **PR-A — Logs view + `agent.logs.stream` RPC.** `internal/protocol/messages.go` types from Phase-0 are now consumed. New daemon-side handler in `internal/daemon/router.go`: `agent.logs.stream` follows the `agent.chat.send` precedent — a streaming method whose handler does its own send (returns nil from `Dispatch` so the router doesn't double-send). Existing `LogStreamer.Stream` (`internal/daemon/logs.go:38`) is wired into the new handler. New `internal/client/rpc.go:LogsStream` mirroring `ChatSend`'s dedicated-connection pattern. New `internal/tui/views/logs.go` (`LogsModel`) — a viewport-bubble-backed scrollback of agent stdout, polled-as-stream like the chat chunks. New `ViewLogs` constant in `internal/tui/views/view.go`. Root model gains a `m.logs` field plus key handler `l` to open from `ViewList` / `ViewDetail`. Smoke Test 24 (start agent, attach logs view, assert stdout content appears).
- **PR-B — Toasts component.** New `internal/tui/components/` package (lipgloss-styled, no bubbletea sub-model — purely a render helper plus a tiny FIFO-with-timer state machine on the root). Bottom-right float per Q1. Auto-dismiss timer (3s steady-state; see §11 Q3). Max stack depth 3 (toasts beyond push the oldest off-screen). Dismissal via `t` key swallows the topmost. Integrated into the root `View()` so any view can `m.toast(level, message)` to push. Wire into `agent.create` / `agent.delete` / daemon-disconnect / error-render paths. Smoke Test (visual; not in `smoke-daemon.sh` — toast rendering is asserted via Go unit tests on the `toasts.Render` function and a `tea.Program`-snapshot test in `internal/tui/app_test.go`).
- **PR-C — Chat history persistence.** Daemon side: `ChatHandler.Handle` (`internal/daemon/chat.go:40`) hooks into `repo.InsertChatMessage` for both the user-supplied content (immediately, before exec) and the assembled agent reply (on Final=true). New `agent.chat.history.list` / `agent.chat.history.append` RPCs registered in `internal/daemon/router.go`. CLI side: `internal/client/rpc.go:ChatHistoryList` method. TUI side: `internal/tui/model.go:openChat` calls `m.rpc.ChatHistoryList(ctx, agentName, 0)` (0 = all per Q2 cap discussion in §11 Q4) and pre-populates `m.chat.messages` before the textarea takes focus. Smoke Test 25 (chat round-trip, leave chat, re-open chat, assert prior messages visible).
- **PR-D — Cleanup + docs.** `README.md` mentions the new view + toast surface in the "Try it" example. `docs/architecture.md` updates the wire-protocol sub-section to add the three new RPCs. `agent/README.md` is unchanged (agent-side untouched). New `docs/phase-3-beta3-wipe-recovery-plan.md` flipped from DRAFT to SHIPPED. WORKLOG entry. Possibly: tighten any TUI corners exposed by the new components (e.g., the `views/help.go` "Coming in beta.1" stale text).

**What this phase does NOT deliver (NOT IN SCOPE):**

- **Logs view scrollback persistence.** Logs view is in-memory only; closing the view drops the buffer. Re-opening re-streams from `Tail: "all"` per the existing `sandbox.LogsFollow` shape. A SQLite-backed log mirror is a separate phase and is gated on a clear retention story (audit-log rotation in beta.2.3 only covers audit decisions, not container stdout). See §11 Q5.
- **Multi-agent log multiplexing.** ViewLogs streams ONE agent at a time. A "system view" that interleaves all agents is a future concept; needs UX design for color-coding and source attribution. Out of scope.
- **Toast persistence across daemon restarts.** Toasts are ephemeral — they live on the root TUI model and die when the program exits. No persistence layer.
- **Toast colors / icons beyond level (info, warning, error).** A more elaborate iconography (e.g., per-event-type glyphs) is polish; out of scope.
- **Chat history edit / delete surfaces.** beta.3 only adds the persistence and load-on-open. CLI-side editing or a "clear chat history" subcommand is a follow-up.
- **Chat history cross-agent search.** No search index, no fuzzy match. The TUI's chat view shows linear history per agent.
- **Channel.send wiring of chat history.** Channels are record-only in v0.3 (per `docs/phase-3-daemon-plan.md`). beta.3 does not change that. Channel-bridged chat is a beta.4+ phase.
- **Wire-protocol version bump.** The new RPC methods and types are additive; `protocol.Version` stays at 1. Older clients don't call the new methods. New clients against an older daemon get JSON-RPC `-32601 method not found` and degrade gracefully (chat works without history; logs view falls back to the alpha.1 polling path).
- **Logs view filtering / regex search.** Show-as-tail only. Filtering is a polish follow-up.

**Sequence relative to the product roadmap:**

```
alpha.4.1                       → reliability + ergonomics (shipped 2026-04-17/18)
                                            ← machine wipe 2026-04-18 ←
beta.1-paths-hardening          → --workspace validation (shipped 2026-04-22)
beta.2-sandbox-hardening        → container escape surface (shipped 2026-04-23/24)
beta.2.{1..6}                   → polish series (shipped 2026-04-25)
beta.3-wipe-recovery            → re-derive lost beta.1 product content ← THIS PLAN
beta.4+ / v0.3.0 GA             → channel bridging, plugin polish, GA
```

---

## 2. Dependencies

**No new Go dependencies.** Every type beta.3 needs is already in the toolbox:

- `github.com/charmbracelet/bubbletea` and `github.com/charmbracelet/bubbles/viewport` — already direct deps for the existing chat view; logs view uses the same viewport bubble.
- `github.com/charmbracelet/lipgloss` — already direct; toasts use the same `Border` + `Padding` style vocabulary that `internal/tui/styles.go` already defines.
- `modernc.org/sqlite` + `github.com/pressly/goose/v3` — already wired through `internal/store/schema.go`. Migration `0003_` slots in alongside existing `0001_`/`0002_`.
- `github.com/oklog/ulid/v2` — already in `internal/store/repo.go:NewID`. New `chat_messages.id` column uses ULIDs to match `agents.id` and `channels.id` precedent.

**No migration of existing data.** `chat_messages` is a new empty table on first migration apply. No backfill from the existing in-memory chat sessions (those vanish when the dclaw process exits anyway).

**No wire protocol envelope change.** All new methods slot under existing JSON-RPC 2.0; new error code uses the existing dclaw custom range (`-32008` reserved for `ErrChatHistoryUnavailable`, see §6.2).

**Promoted indirect → direct (optional).** None.

After any inadvertent `go.mod` touch, run `go mod tidy` from the repo root.

---

## 3. Sequencing

**PR dependency graph (different shape from beta.1/beta.2 — Phase-0 is a true prerequisite, B and C are parallel after A):**

```
PR-Phase0  (migration 0003_chat_history.sql + protocol types + repo methods, ~250 lines)
    ↓
PR-A       (logs view + agent.logs.stream RPC + smoke Test 24, ~500 lines)
    ↓
    ├── PR-B  (toasts component + integration, ~300 lines)
    └── PR-C  (chat history persistence end-to-end + smoke Test 25, ~400 lines)
                                ↓ (both merged)
                              PR-D  (docs + cleanup + WORKLOG, ~150 lines)
```

- **PR-Phase0 is the absolute gate.** It contains zero behavior change visible to CLI or TUI but is the precondition for every PR below: A consumes `LogsStreamParams`/`LogsStreamLineNotification` types, C consumes `ChatHistoryListParams`/`ChatMessage` types AND the new repo methods AND migration 0003. B does not depend on Phase-0 directly (toasts are pure TUI) but ships in the same series so PR-D can sweep all three surfaces as one cleanup.
- **PR-A is the next gate after Phase-0.** It establishes the streaming-RPC pattern reuse (`agent.logs.stream` mirrors `agent.chat.send`/`agent.chat.chunk` from alpha.3) which PR-C also leans on for the `agent.chat.history.append` notification path. Wiring A first means C's review can focus on the SQLite-vs-RAM diff rather than re-litigating the streaming mechanism.
- **PR-B and PR-C are independent** after PR-A merges. Different file sets:
  - PR-B touches `internal/tui/components/toasts.go` (new file), `internal/tui/model.go` (root state machine), `internal/tui/messages.go` (toast tea.Msgs).
  - PR-C touches `internal/daemon/chat.go`, `internal/daemon/router.go`, `internal/client/rpc.go`, `internal/tui/views/chat.go`, `internal/tui/model.go` (one-liner in `openChat`).
  - The model.go overlap is small (B adds a `m.toasts` field + ticker handler; C adds one line in `openChat`). Coordinate via §11 Q1 — recommend serializing by branching B first, then rebasing C on top, to avoid the parallel-agent shared-CWD race that bit beta.1 (per `WORKLOG.md` 2026-04-20).
- **PR-D is the tail.** Doc sweep, WORKLOG entry, plan §0 SHIPPED flip.

All five PRs reviewed together as one batched cycle matching the beta.1 / beta.2 cadence; merged in the sequence above.

**Lesson applied from beta.1 PR-B+C race:** Although B and C are technically parallel, deploy them **serially** to avoid the shared-CWD `git checkout` race that triggered the stash-and-recover saga during beta.1 PR-B+C concurrent agent runs. Either branch-per-agent + explicit checkout-before-edit, or single-agent-at-a-time. See `WORKLOG.md` 2026-04-20.

---

## 4. Per-PR Spec

### 4.1 PR-Phase0 — Schema + Protocol Types + Repo Methods

**Goal:** Re-derive the lost `d84554d` commit. Pure additive prep: one new migration, new protocol types, new repo methods, new error code. Zero behavior change observable through CLI or TUI; all four downstream PRs depend on these symbols.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/store/migrations/0003_chat_history.sql` | NEW | New table `chat_messages`. Columns: `id TEXT PRIMARY KEY` (ULID), `agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE`, `role TEXT NOT NULL CHECK(role IN ('user','agent','system','error'))`, `content TEXT NOT NULL`, `parent_id TEXT NOT NULL DEFAULT ''` (content-addressed parent ID echo of `protocol.AgentChatSendParams.ParentID`; empty string for root-of-thread), `message_id TEXT NOT NULL UNIQUE` (matches `chatMessageID()` hash from `internal/client/rpc.go:524`), `sequence INTEGER NOT NULL DEFAULT 0` (monotonic within a thread; mirrors `protocol.AgentChatChunkNotification.Sequence`), `timestamp INTEGER NOT NULL` (unix seconds). Two indexes: `chat_messages_agent_ts_idx ON chat_messages(agent_id, timestamp DESC)` for the list-newest query, and SQLite implicitly creates one for the UNIQUE on `message_id`. Both up + down statements bracketed in `+goose StatementBegin/End`. Down: `DROP INDEX chat_messages_agent_ts_idx; DROP TABLE chat_messages;`. Net lines: ~30. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/protocol/messages.go` | MODIFIED | Append at the end (after the existing `AgentChatChunkNotification`): five new types. (1) `LogsStreamParams { Name string; Tail int; Follow bool }` — `Follow` defaults to true server-side; `Tail` defaults to "all" if zero. (2) `LogsStreamLineNotification { Name string; Line string; Stream string ("stdout"|"stderr"); Timestamp string }` — the params payload of an `agent.log.line` notification. Stream attribution comes from `sandbox.DockerClient.LogsFollow`'s `stdcopy.StdCopy` demux (currently merged; see §11 Q5). (3) `ChatHistoryListParams { Name string; Limit int }` — `Limit=0` means all (Q2/Q4 decision). (4) `ChatHistoryListResult { Messages []ChatMessage }`. (5) `ChatHistoryAppendParams { Name string; Role string; Content string; ParentID string; MessageID string; Sequence int }` — used internally by the daemon's `ChatHandler` to record both halves of a round-trip; not generally invoked by the CLI. (6) `ChatHistoryAppendResult` (typed alias of `AckResult` for clarity). (7) `ChatMessage` wire shape (NOT to be confused with `views.ChatMessage` in `internal/tui/views/chat.go`; the wire shape lives in `protocol`, the in-memory shape lives in `views`). Fields: `MessageID string`, `Role string`, `Content string`, `ParentID string`, `Sequence int`, `Timestamp int64`. (8) New error code constant: `ErrChatHistoryUnavailable = -32008` (next free in the dclaw custom range; previous extension was `ErrWorkspaceForbidden = -32007` in beta.1). Net lines: ~+70. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/store/repo.go` | MODIFIED | New `ChatMessageRecord` struct mirroring the table columns. New methods: (1) `InsertChatMessage(ctx, rec ChatMessageRecord) error` — single-row INSERT, uniqueness conflict on `message_id` returns a wrapped `ErrNameTaken`-equivalent (re-using the existing `ErrNameTaken` is fine since `mapError` translates it to `ErrInvalidParams = -32602` which is the right shape for "duplicate message_id, this round-trip already persisted"). (2) `ListChatHistory(ctx, agentID string, limit int) ([]ChatMessageRecord, error)` — `SELECT * FROM chat_messages WHERE agent_id=? ORDER BY timestamp ASC LIMIT ?`. Limit==0 → no LIMIT clause; query as `if limit > 0 { sql += " LIMIT ?"; args = append(args, limit) }` to keep it parameterized. ASCending by timestamp because the TUI renders top-to-bottom oldest-first (`internal/tui/views/chat.go:rebuildViewport` iterates `m.messages` linearly). (3) `DeleteChatHistoryForAgent(ctx, agentID string) error` — explicit method even though `ON DELETE CASCADE` covers the `agent_id` FK; the explicit method exists for future operator subcommands (e.g., `dclaw agent chat clear <name>`) and for unit-test setup convenience. Net lines: ~+90. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/store/repo_test.go` | MODIFIED | Three new test functions: `TestInsertAndListChatHistory` (insert 3 rows for one agent + 2 for another, list returns 3 in timestamp order), `TestInsertChatMessageDuplicateMessageID` (UNIQUE constraint trips → ErrNameTaken wrap), `TestDeleteChatHistoryForAgent` (insert 5, delete, list returns empty). Net lines: ~+120. |

**Test plan:**

- `go test ./internal/store/...` — three new functions plus the existing `TestMigrate` that loops up→down→up.
- `go test ./internal/protocol/...` — protocol package has no tests today; if a `messages_test.go` exists, run regression. (Spot-check: only encoding-level tests live here.)
- `go test ./...` — no regression.
- `go vet ./...` clean.
- `go build ./cmd/dclaw ./cmd/dclawd` both compile.
- Manual: `./bin/dclawd --migrate-only` against a fresh state-dir produces a state.db where `sqlite3 state.db ".schema chat_messages"` shows the table.

**Acceptance criteria:**

1. `go test ./internal/store/...` passes with three new chat-history tests.
2. `internal/store/migrations/` has exactly three files: `0001_initial.sql`, `0002_workspace_trust.sql`, `0003_chat_history.sql`.
3. `grep "ErrChatHistoryUnavailable" internal/protocol/messages.go` returns one match (the constant declaration).
4. `grep -E "InsertChatMessage|ListChatHistory|DeleteChatHistoryForAgent" internal/store/repo.go` returns ≥3 lines (one per method definition).
5. No CLI or TUI code touched in this PR (modifying only `internal/store`, `internal/protocol`).
6. Migration 0003 up→down→up roundtrip green via the existing `repo.Rollback` test pattern.

**Rollout risk:** Low. New empty table + new symbols. No existing query touches the new table. If the migration fails to apply (e.g., a stale daemon trying to roll back to 0002), the daemon refuses to start with a `goose up` error; operator runs `dclawd --migrate-only` first per the existing pattern.

**Rollback:** Revert merge commit. `goose down` removes the `chat_messages` table. No data loss (table is empty until PR-C lands).

---

### 4.2 PR-A — Logs View + `agent.logs.stream` RPC

**Goal:** Wire the half-built `LogStreamer` (`internal/daemon/logs.go`) into a real JSON-RPC streaming method, add a TUI view that consumes it, ship Test 24 to prove end-to-end. The chat-streaming precedent (alpha.3) gives us the entire shape; logs is just "same shape, log lines instead of chat chunks."

**Naming decision (locked):** RPC method name is `agent.logs.stream` and the per-line notification method is `agent.log.line`. This mirrors the chat precedent exactly: `agent.chat.send` (request) + `agent.chat.chunk` (notification). The ".stream" suffix on the request is the marker that the response is followed by notifications until terminal. The synchronous bulk fetch keeps its existing `agent.logs` name from alpha.1.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/router.go` | MODIFIED | (1) New struct field `logHandler *LogStreamHandler` alongside the existing `chatHandler` (line 26). (2) Constructor initializes via `r.logHandler = NewLogStreamHandler(log, repo, docker)` after the existing chat handler init (line 70). (3) `Dispatch` (line 82) gains a streaming-method clause: `if env.Method == "agent.logs.stream" { if err := r.logHandler.Handle(ctx, env.Params, env.ID, send); err != nil { r.log.Warn("logs handler error", "err", err) } return nil }` — same shape as the existing `agent.chat.send` clause (lines 88-93). Net lines: ~+12. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/logs.go` | MODIFIED | (1) Existing `LogStreamer` retained, plus new `LogStreamHandler` wrapper with the same constructor signature shape as `ChatHandler`. (2) New method `func (h *LogStreamHandler) Handle(ctx context.Context, params json.RawMessage, reqID any, send func(*protocol.Envelope) error) error`. Body: parse `LogsStreamParams`, look up agent via `repo.GetAgent`, emit synchronous ack `protocol.SuccessResponse(reqID, protocol.AckResult{Ack: true})`, then call the existing `LogStreamer.Stream` and forward each line as `agent.log.line` notification with `protocol.LogsStreamLineNotification` payload. Stream is "stdout" by default; per §11 Q5 we leave the source-attribution coarse for now (every line is `Stream: "stdout"`) because `sandbox.LogsFollow`'s `stdcopy.StdCopy(pw, pw, rc)` merges both into one writer. Splitting requires per-stream pipes — follow-up. (3) Existing `LogStreamer` type unchanged. Net lines: ~+95. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/logs_test.go` | NEW | New file. Two tests: `TestLogStreamHandlerForwardsLines` — uses a stub docker client (mirroring `mockDockerExec` in `internal/daemon/chat_test.go`) that returns a fixed slice of log lines on `LogsFollow`; assert the notifications hit `send` in order with `Method == "agent.log.line"`. `TestLogStreamHandlerErrorPath` — agent name not found → error notification (or RPC error before the ack). Net lines: ~+150. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/sandbox/docker.go` | UNCHANGED | `LogsFollow` (line 345) already returns `<-chan string, <-chan error` — exactly what `LogStreamHandler` consumes. No change. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/protocol/messages.go` | UNCHANGED | Types added in PR-Phase0; consumed here. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/client/rpc.go` | MODIFIED | New method `LogsStream(ctx context.Context, agentName string, tail int) (<-chan LogLineEvent, error)` mirroring `ChatSend`'s shape exactly: dedicated connection, handshake, `agent.logs.stream` request, drain `agent.log.line` notifications until ctx cancellation or stream EOF. Returns a typed channel `chan LogLineEvent { Line, Stream, Timestamp string; Err error }`. Existing `AgentLogs` (line 212) and `agentLogsFollowPoll` (line 332) are NOT removed — they remain the bulk + poll fallback for `dclaw agent logs -f` CLI usage. The new `LogsStream` is the path the TUI takes. Net lines: ~+120. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/views/view.go` | MODIFIED | New constant `ViewLogs View` = next ordinal after `ViewChat`. Doc-comment one-liner. Net lines: ~+3. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/views/logs.go` | NEW | New file. `LogsModel` struct: `agentName string`, `lines []string` (capped at `MaxLogLines = 5000` — see §11 Q5), `vp viewport.Model`, `width, height int`, `streaming bool`. Methods mirror `ChatModel`: `NewLogsModel`, `SetAgent(name)`, `SetSize(w, h)`, `SetStreaming(bool)`, `AppendLine(LogLineEvent)`, `AppendError(error)`, `Reset()`, `Update(tea.Msg)` (forwards to viewport), `View(w, h string)`. `AppendLine` truncates `lines` from the front when length exceeds `MaxLogLines` (FIFO scrollback bound). `View` joins `lines` with newlines into the viewport, autoscrolls to bottom. Net lines: ~+180. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/messages.go` | MODIFIED | New tea.Msgs: `logsStreamOpenedMsg { agentName string; lines <-chan client.LogLineEvent }`, `logsLineMsg { line client.LogLineEvent }`, `logsStreamClosedMsg {}`, `logsErrorMsg { err error }`. Mirror the existing chat-stream messages at lines 31-62. Net lines: ~+25. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/model.go` | MODIFIED | (1) New field `logs views.LogsModel` (line ~28). (2) Constructor inits `m.logs = views.NewLogsModel()`. (3) New cases in `Update` for the four new tea.Msgs (mirror existing chat cases at lines 132-168). (4) New `View` case for `views.ViewLogs` (mirror `views.ViewChat` at line 200). (5) New `'l'` key handler in `handleListKey` and `handleDetailKey` that calls a new `m.openLogs(name, prevView)`. (6) `openLogs` method: same shape as `openChat` at line 423, but kicks off `LogsStream` via a tea.Cmd that returns `logsStreamOpenedMsg`. (7) `chatHeight()` analog `logsHeight()`. (8) On `'esc'` in ViewLogs: cancel the stream context, reset `m.logs`, return to `prevView`. Net lines: ~+100. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/poll.go` | UNCHANGED | Polling for the agent list continues at the 2s cadence. Logs do NOT use the poll cycle — they use the dedicated stream connection. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/views/help.go` | MODIFIED | "Coming in beta.1" line for `l` (line 48) → moved into the active key list in the "Actions" section. Net lines: ~+1 / ~-1. |
| `/Users/macmini/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` | MODIFIED | Add Test 24 (logs stream end-to-end). Body in §4.2a. |

**§4.2a — Test 24 body:**

```bash
echo "--- Test 24: agent logs stream — TUI-shape RPC delivers lines ---"
# Smoke validates the agent.logs.stream RPC end-to-end. Doesn't touch the
# TUI (TUI is exercised separately via smoke-tui.sh); this asserts the
# wire-level path: start agent, kick it into emitting stdout, open a
# stream subscription, receive at least one line. Uses a small Go helper
# (or in-process: just `dclaw agent logs <name>` with a new --stream flag,
# see §11 Q2 — for beta.3 we wire a hidden `dclaw agent logs --stream`
# alias for smoke testing only, no operator-facing surface).
STATE_DIR_T24=$(mktemp -d -t dclaw-smoke-t24-XXXXXXXX)
case "$STATE_DIR_T24" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T24=$STATE_DIR_T24 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T24="$STATE_DIR_T24/dclaw.sock"
cleanup_t24() {
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T24" --daemon-socket "$SOCKET_T24" daemon stop >/dev/null 2>&1 || true
  docker rm -f dclaw-smoke-logs-stream >/dev/null 2>&1 || true
  rm -rf "${STATE_DIR_T24:?refuse empty}"
}
trap cleanup_t24 EXIT
"$DCLAW_BIN" --state-dir "$STATE_DIR_T24" --daemon-socket "$SOCKET_T24" daemon start || fail "t24-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T24" --daemon-socket "$SOCKET_T24" agent create smoke-logs-stream \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T24" || fail "t24-create"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T24" --daemon-socket "$SOCKET_T24" agent start smoke-logs-stream || fail "t24-start-agent"
# Force the container to emit stdout deterministically.
"$DCLAW_BIN" --state-dir "$STATE_DIR_T24" --daemon-socket "$SOCKET_T24" agent exec smoke-logs-stream \
  -- echo "T24_PROBE_OK" >/dev/null || fail "t24-exec"
# Stream for up to 5 seconds and grep.
set +e
OUT=$(timeout 5 "$DCLAW_BIN" --state-dir "$STATE_DIR_T24" --daemon-socket "$SOCKET_T24" agent logs --stream smoke-logs-stream 2>&1)
EX=$?
set -e
# timeout exits 124; we accept that since stream is long-lived by design.
if [ "$EX" -ne 0 ] && [ "$EX" -ne 124 ]; then
  fail "Test 24: agent logs --stream errored unexpectedly: exit=$EX, out=$OUT"
fi
echo "$OUT" | grep -q "T24_PROBE_OK" \
  || fail "Test 24: expected T24_PROBE_OK in streamed logs, got: $OUT"
cleanup_t24
trap cleanup EXIT
pass "agent.logs.stream RPC delivers stdout lines"
```

**Test plan:**

- `go test ./internal/daemon/...` — new `TestLogStreamHandlerForwardsLines` + regression on existing chat tests.
- `go test ./internal/client/...` — needs a new `rpc_logs_test.go` mirroring `rpc_chat_test.go` shape (mock daemon → assert the dedicated-connection drain works).
- `go test ./internal/tui/...` — extend `app_test.go` with a key-press test that asserts `'l'` from `ViewList` opens `ViewLogs`.
- `go test ./...` regression.
- `./scripts/smoke-daemon.sh` Tests 1-23 still green plus new Test 24.
- Manual: `./bin/dclaw` → cursor on a running agent → `l` → live logs scrollback. ESC returns to list.

**Acceptance criteria:**

1. `go test ./internal/daemon/...` passes including the new logs-handler tests.
2. `internal/tui/views/logs.go` exists; `internal/tui/views/view.go` declares `ViewLogs`.
3. Smoke Test 24 green on a docker-reachable host.
4. `grep "agent.logs.stream\|agent.log.line" internal/daemon/router.go internal/protocol/messages.go internal/client/rpc.go internal/tui/` returns ≥4 hits each.
5. The existing alpha.1 polling fallback (`agentLogsFollowPoll`) is retained, NOT deleted — it still serves `dclaw agent logs -f` CLI usage. Grep `grep agentLogsFollowPoll internal/client/rpc.go` returns ≥1 match.
6. Wire protocol version unchanged: `protocol.Version == 1` (visible in `internal/protocol/protocol.go:4`).

**Rollout risk:** Medium. The new streaming RPC parallels the existing chat streaming, but logs are higher-volume — a chatty container could blast 100s of lines/sec and the TUI must keep up. Mitigation: `MaxLogLines = 5000` FIFO bound prevents unbounded RAM growth; viewport rendering is incremental via the `vp.GotoBottom()` call. If a container produces output faster than TUI rendering can consume, the buffered channel (`internal/sandbox/docker.go:LogsFollow` opens with `make(chan string, 128)`) backpressures; lines older than the buffer drop on the floor — acceptable for a TUI logs view (operators who need every byte use `dclaw agent logs -f` redirected to a file).

**Rollback:** Revert. The TUI loses the new view; chat / detail / describe unaffected. `dclaw agent logs -f` CLI continues to work via the alpha.1 polling path.

---

### 4.3 PR-B — Toasts Component

**Goal:** Bottom-right floating notifications. Non-blocking. Auto-dismiss. Stack of up to 3 visible. Per the lost original Q1 decision: bottom-right float, NOT a status-line ribbon.

**Decision (locked):** Toast lifecycle parameters baked in:

- `ToastDuration = 3 * time.Second` — see §11 Q3.
- `ToastMaxStack = 3` — see §11 Q3.
- Dismissal: pressing `t` (mnemonic: "toast") removes the topmost. Auto-dismiss timer also fires. View change does NOT clear stack (so a transient toast about "agent created" still visible after navigating from list to detail).
- Three levels: `info`, `warning`, `error`. Color via `lipgloss.Color` distinct from `TopBarStyle` and `BottomBarStyle` so the toast is visually separated.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/components/toasts.go` | NEW | New package. Exports: `type Toast struct { Level, Message string; Expiry time.Time }`. `type Stack struct { items []Toast; mu sync.Mutex }`. Methods: `(s *Stack) Push(level, message string)` — appends with `Expiry = time.Now().Add(ToastDuration)`; trims to `ToastMaxStack` from the front (oldest dropped). `(s *Stack) Tick(now time.Time)` — drops expired entries. `(s *Stack) DismissTop()` — pop the most recent. `(s *Stack) Render(width, height int) string` — returns the renderable string positioned bottom-right. Renderer uses `lipgloss.Place(width, height, lipgloss.Right, lipgloss.Bottom, stackBox)` to overlay; the stackBox is a `lipgloss.JoinVertical(lipgloss.Right, ...)` of each toast's individual styled box. Color map: info=cyan/39, warning=yellow/214, error=red/196. Net lines: ~+150. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/components/toasts_test.go` | NEW | Five tests: `TestPushTrimsToMaxStack` (push 5, only 3 retained, oldest dropped), `TestTickExpiresOldEntries`, `TestDismissTopRemovesNewest`, `TestRenderEmptyReturnsEmptyString`, `TestRenderProducesBottomRightPlacement` (string-match the lipgloss render output for the placement codes). Net lines: ~+180. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/messages.go` | MODIFIED | New tea.Msgs: `toastPushMsg { Level, Message string }`, `toastTickMsg time.Time`, `toastDismissMsg {}`. Net lines: ~+12. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/model.go` | MODIFIED | (1) New field `toasts components.Stack` on the root `Model` struct. (2) Constructor inits to zero-value Stack. (3) `Init()` adds a `toastTick` timer (1s cadence) alongside the existing 2s `tickPoll`. (4) New cases in `Update`: `toastPushMsg` → `m.toasts.Push(level, msg)`, then schedule the next tick; `toastTickMsg` → `m.toasts.Tick(time.Now())`, schedule next tick; `toastDismissMsg` → `m.toasts.DismissTop()`. (5) `View()` calls `m.toasts.Render(m.width, m.height)` and overlays it on top of the existing `lipgloss.JoinVertical` chrome. Use `lipgloss.PlaceHorizontal` + custom bytewise overlay since lipgloss does not natively support overlay; alternative is to render into the BottomBarStyle area's right side — see §11 Q3 for the render-positioning decision. (6) Five push-call sites: in the existing `case agentsLoadedMsg` after successful refresh (info: not pushed routinely — only on the first successful load post-disconnect; uses a flag to avoid spamming). (7) `case daemonErrMsg`: push warning toast "daemon disconnected". (8) `case chatErrorMsg`: push error toast with the err.Error(). (9) Any successful create / delete / start / stop in the CLI path that surfaces back via the TUI: NOT in beta.3 — TUI only consumes the events; CLI emits them in its own renderer (out of scope; the TUI doesn't run agent.create today). For beta.3 the visible toast surfaces are: daemon connect/disconnect, chat error, logs stream error. Net lines: ~+60. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/keys.go` | MODIFIED | Add `Toast key.Binding` for the `t` dismissal key. Net lines: ~+3. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/styles.go` | MODIFIED | Three new exported styles: `ToastInfoStyle`, `ToastWarningStyle`, `ToastErrorStyle`. Each is a `lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Foreground(...)` with the level-appropriate color. Net lines: ~+15. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/app_test.go` | MODIFIED | New test `TestToastPushedOnDaemonDisconnect`: drive the model with a `daemonErrMsg`, assert `m.toasts` has one entry with level=warning. New test `TestToastTickExpiresStaleEntries`: push, advance time via overriding `time.Now`, send `toastTickMsg`, assert empty. Net lines: ~+80. |

**Test plan:**

- `go test ./internal/tui/components/...` — five new tests.
- `go test ./internal/tui/...` — extended `app_test.go`.
- `go vet ./...`, `go build` clean.
- Manual: launch TUI; kill the daemon mid-session (`pkill dclawd`); observe a warning toast appear bottom-right and fade after 3 seconds. Press `t` to dismiss prematurely.

**Acceptance criteria:**

1. `internal/tui/components/toasts.go` exists with `Push`, `Tick`, `DismissTop`, `Render` methods.
2. `go test ./internal/tui/components/...` passes with five new tests.
3. Bottom-right placement asserted by string-match on the lipgloss render output.
4. Dismissal via `t` works (covered by test).
5. Auto-dismiss after `ToastDuration` (3s) works (covered by test with overridden time).
6. Daemon disconnect produces a visible warning toast (covered by `TestToastPushedOnDaemonDisconnect`).

**Rollout risk:** Low. New TUI surface, additive only. Worst case: the lipgloss bottom-right placement renders incorrectly on certain terminal widths. Mitigation: `Render` falls back to no-op (returns empty string) if `width < toast.Width` so a tiny terminal sees nothing rather than misplaced glyphs.

**Rollback:** Revert. TUI loses transient notifications; the existing in-place error rendering (e.g., `m.chat.AppendError`) is preserved untouched.

---

### 4.4 PR-C — Chat History Persistence

**Goal:** Per Q2 decision: load chat history on every `openChat`. Persist on every chat round-trip (user message + agent reply). New table from PR-Phase0 is the storage; new RPCs from PR-Phase0 are the wire.

**Decision (locked):** Per Q4 cap discussion, `ChatHistoryListParams.Limit = 0` means "all messages for this agent". The TUI passes 0 unconditionally. If history grows large enough to be a perf concern (10k+ messages per agent), Q4 follow-up adds pagination — but for beta.3 the model is "load all on open, render in viewport, viewport handles scrolling." This is the simplest correct shape and matches Q2's "Option A" mandate.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/router.go` | MODIFIED | Two new handlers in the handlers map: `"agent.chat.history.list": r.handleChatHistoryList`, `"agent.chat.history.append": r.handleChatHistoryAppend`. New methods: `handleChatHistoryList` parses `ChatHistoryListParams`, calls `repo.GetAgent(name)` to resolve `agent.ID`, then `repo.ListChatHistory(agentID, limit)`, returns `protocol.ChatHistoryListResult{Messages: ...}`. `handleChatHistoryAppend` is mostly used internally by `ChatHandler` (see below) but also exposed as an RPC for completeness — parses `ChatHistoryAppendParams`, calls `repo.InsertChatMessage`, returns `protocol.AckResult{Ack: true}`. Net lines: ~+50. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/chat.go` | MODIFIED | (1) `ChatHandler.Handle` (line 40) gains a `repo *store.Repo` field already-present-on-struct (line 25). (2) After parsing `req` and validating the agent, BEFORE the exec, write the user-side message: `repo.InsertChatMessage(ctx, store.ChatMessageRecord{ID: store.NewID(), AgentID: rec.ID, Role: "user", Content: req.Content, ParentID: req.ParentID, MessageID: req.MessageID, Sequence: 0, Timestamp: time.Now().Unix()})`. Wrap in `if err := ...; err != nil { /* log warn, continue — don't fail the chat round-trip on a persistence failure */ }`. (3) After the final chunk lands (`exitCode == 0` happy path, around line 152), write the agent-side reply: same shape with `Role: "agent"`, `Content: text`, `ParentID: req.MessageID` (the user message's ID is the agent reply's parent), `MessageID: <new ULID-derived ID, since the chunk's MessageID is empty in the alpha.4 final-chunk shape — see §11 Q6>`, `Sequence: <chunk.Sequence ?: 0>`. Same fail-soft on persistence error. (4) On error paths (`execErr`, `exitCode != 0`), record an `error`-role row so history reflects what happened; matches the in-memory `ChatModel.AppendError` pattern at line 160 of `internal/tui/views/chat.go`. Net lines: ~+45. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/daemon/chat_test.go` | MODIFIED | New tests: `TestChatHandlerPersistsUserAndAgentMessages` (round-trip via mockDockerExec, then `repo.ListChatHistory` returns 2 rows: user + agent), `TestChatHandlerPersistsErrorMessage` (exec fails → 2 rows: user + error), `TestChatHandlerSurvivesPersistenceFailure` (inject a closed-DB error from repo, chat still sends the chunk). Net lines: ~+200. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/client/rpc.go` | MODIFIED | New methods: `ChatHistoryList(ctx, agentName string, limit int) ([]protocol.ChatMessage, error)` — wraps `c.call("agent.chat.history.list", ...)`. `ChatHistoryAppend(ctx, agentName, role, content, parentID, messageID string, sequence int) error` — exposed for symmetry; the TUI doesn't call it (the daemon's `ChatHandler` writes through `repo.InsertChatMessage` directly). Net lines: ~+50. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/client/rpc_chat_test.go` | MODIFIED | Add `TestChatHistoryListRoundTrip` against a mock daemon. Net lines: ~+80. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/views/chat.go` | MODIFIED | (1) New method `(m *ChatModel) LoadHistory(messages []protocol.ChatMessage)` that converts each protocol-shape message to a views-shape `ChatMessage` and appends to `m.messages`. Sets `m.lastMsgID` to the last message's ID so the next user submit threads correctly. (2) `Reset` clears `m.lastMsgID` AND `m.messages` (already does). (3) `(m *ChatModel) HasHistory() bool` for the TUI to know whether to show the "(no prior conversation)" placeholder. Net lines: ~+45. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/messages.go` | MODIFIED | New `chatHistoryLoadedMsg { messages []protocol.ChatMessage }` and `chatHistoryErrorMsg { err error }`. Net lines: ~+10. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/model.go` | MODIFIED | (1) `openChat` (line 423) gains a tea.Cmd that fires the history fetch BEFORE the textarea takes input. New goroutine via tea.Cmd: `func() tea.Msg { msgs, err := m.rpc.ChatHistoryList(ctx, agentName, 0); if err != nil { return chatHistoryErrorMsg{err} }; return chatHistoryLoadedMsg{msgs} }`. (2) New cases in `Update`: `chatHistoryLoadedMsg` → `m.chat.LoadHistory(msg.messages)`; `chatHistoryErrorMsg` → push a warning toast (PR-B integration) and continue with empty history. Net lines: ~+25. |
| `/Users/macmini/workspace/agents/atlas/dclaw/scripts/smoke-daemon.sh` | MODIFIED | Add Test 25 (chat history end-to-end). Body in §4.4a. |

**§4.4a — Test 25 body:**

```bash
echo "--- Test 25: chat history persistence — round-trip + reload (requires ANTHROPIC_API_KEY) ---"
if [ -z "${ANTHROPIC_API_KEY:-}" ] && [ -z "${ANTHROPIC_OAUTH_TOKEN:-}" ]; then
  echo "SKIP: Test 25 requires ANTHROPIC_API_KEY or ANTHROPIC_OAUTH_TOKEN — skipping (set the var to enable)"
else
  STATE_DIR_T25=$(mktemp -d -t dclaw-smoke-t25-XXXXXXXX)
  case "$STATE_DIR_T25" in
    /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
    *) echo "refuse: STATE_DIR_T25=$STATE_DIR_T25 outside expected prefix" >&2; exit 1;;
  esac
  SOCKET_T25="$STATE_DIR_T25/dclaw.sock"
  cleanup_t25() {
    "$DCLAW_BIN" --state-dir "$STATE_DIR_T25" --daemon-socket "$SOCKET_T25" daemon stop >/dev/null 2>&1 || true
    docker rm -f dclaw-smoke-history >/dev/null 2>&1 || true
    rm -rf "${STATE_DIR_T25:?refuse empty}"
  }
  trap cleanup_t25 EXIT
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T25" --daemon-socket "$SOCKET_T25" daemon start || fail "t25-start"
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T25" --daemon-socket "$SOCKET_T25" agent create smoke-history \
    --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T25" || fail "t25-create"
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T25" --daemon-socket "$SOCKET_T25" agent start smoke-history || fail "t25-agent-start"
  # First chat invocation, persists user + agent messages.
  OUT1=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T25" --daemon-socket "$SOCKET_T25" agent chat smoke-history \
    --one-shot "reply with only the word: T25_FIRST" --timeout 90s 2>&1) || fail "t25-chat1"
  echo "$OUT1" | grep -qi "T25_FIRST" || fail "Test 25: round-trip 1 missing T25_FIRST: $OUT1"
  # Second invocation, history must already contain row 1.
  # We use a hidden CLI subcommand `agent chat history <name>` that prints history as JSON.
  HIST=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T25" --daemon-socket "$SOCKET_T25" agent chat history smoke-history -o json 2>&1) || fail "t25-history-cmd"
  echo "$HIST" | grep -q '"role": *"user"' || fail "Test 25: history missing user message: $HIST"
  echo "$HIST" | grep -q '"role": *"agent"' || fail "Test 25: history missing agent message: $HIST"
  echo "$HIST" | grep -q "T25_FIRST" || fail "Test 25: history missing T25_FIRST content: $HIST"
  cleanup_t25
  trap cleanup EXIT
  pass "chat history persisted across round-trip + reload"
fi
```

(Note: Test 25 introduces a `dclaw agent chat history <name>` subcommand. This is OPERATOR-FACING. It's a thin CLI wrapper over `client.ChatHistoryList`, ~30 lines in `internal/cli/agent.go`. Counted in PR-C scope.)

**Test plan:**

- `go test ./internal/store/...` — passes (Phase-0 tests).
- `go test ./internal/daemon/...` — three new chat-handler tests.
- `go test ./internal/client/...` — new history list test.
- `go test ./internal/tui/...` — extended app_test for `openChat` history load.
- `go test ./...` regression.
- `./scripts/smoke-daemon.sh` Tests 1-24 still green plus new Test 25 (skipped without `ANTHROPIC_API_KEY`, mirroring Test 13's pattern).

**Acceptance criteria:**

1. `go test ./internal/daemon/...` passes including the three new persistence tests.
2. After a chat round-trip, `sqlite3 $STATE_DIR/state.db 'SELECT count(*) FROM chat_messages'` returns ≥ 2 (user + agent).
3. Re-opening the TUI chat view for an agent with prior history shows the prior messages BEFORE the user types anything new (asserted by `TestOpenChatLoadsHistory` in `internal/tui/app_test.go`).
4. Smoke Test 25 green when `ANTHROPIC_API_KEY` is set; skip otherwise (matches Test 13 precedent).
5. Persistence failures do NOT break the chat round-trip — covered by `TestChatHandlerSurvivesPersistenceFailure`.
6. `dclaw agent chat history <name>` subcommand exists and emits JSON.

**Rollout risk:** Medium. The `ChatHandler` write path is the hot path for chat traffic; a slow SQLite write could add latency to chat. Mitigation: writes go through `repo.InsertChatMessage` which uses a single-row prepared INSERT against a WAL-mode database with `busy_timeout=5000` (per `internal/store/schema.go:28`). On a SSD this is single-digit milliseconds. Worst case: write hangs for the full 5s busy_timeout — chat reply is already streamed to the user; persistence is fire-and-forget asynchronously? **Decision: synchronous, but fail-soft.** A persistence failure logs at WARN level and the chat continues. Recovery on next round-trip when the DB lock clears.

**Rollback:** Revert. New chat sessions stop being persisted; existing rows remain in `chat_messages` (harmless). On next deploy, persistence resumes seamlessly because the schema is backward-compatible. If reverting Phase-0 too (drops the table), existing rows are lost — but no production deploy depends on them yet.

---

### 4.5 PR-D — Cleanup + Docs

**Goal:** Doc sweep to reflect what beta.3 ships. Plan §0 SHIPPED flip. WORKLOG entry. Tighten any TUI corners exposed by the new components.

**Files changed:**

| File | Kind | Notes |
|---|---|---|
| `/Users/macmini/workspace/agents/atlas/dclaw/docs/phase-3-beta3-wipe-recovery-plan.md` | MODIFIED | Flip §0 Status from `DRAFT` to `SHIPPED (2026-04-DD) as v0.3.0-beta.3-wipe-recovery`. Add the 5-commit table (Phase0/A/B/C/D hashes). Same shape as beta.2 plan §0. Net lines: ~+10. |
| `/Users/macmini/workspace/agents/atlas/dclaw/README.md` | MODIFIED | (1) Version line on the second header (currently `v0.3.0-beta.2.6-platform-port`) → `v0.3.0-beta.3-wipe-recovery`. (2) Code-fence example `dclaw agent attach` flow — add a sentence about the `l` key (open logs view). (3) "Try it" section — add a one-liner about toast notifications. Net lines: ~+8. |
| `/Users/macmini/workspace/agents/atlas/dclaw/docs/architecture.md` | MODIFIED | "Wire protocol — daemon-bound methods" subsection — add three new method names (`agent.logs.stream`, `agent.chat.history.list`, `agent.chat.history.append`) plus the new `ErrChatHistoryUnavailable = -32008` code. Net lines: ~+12. |
| `/Users/macmini/workspace/agents/atlas/dclaw/internal/tui/views/help.go` | MODIFIED | Drop the stale "Coming in beta.1" subsection (lines 47-49). Move `l: open logs view` into the active key list. Add `t: dismiss toast` in the Actions section. Net lines: ~+3 / ~-2. |
| `/Users/macmini/workspace/agents/atlas/dclaw/WORKLOG.md` | MODIFIED | New `## 2026-04-DD — beta.3 wipe-recovery build cycle` section, mirroring the structure of the 2026-04-23 beta.2 entry. Net lines: ~+80. |
| `/Users/macmini/workspace/agents/atlas/dclaw/agent/README.md` | UNCHANGED | Agent-side untouched by beta.3. |
| `/Users/macmini/workspace/agents/atlas/dclaw/docs/workspace-root.md` | UNCHANGED | Workspace policy untouched. |

**Test plan:**

- `make lint` (golangci-lint + shellcheck) green.
- `git grep "v0.3.0-beta.2.6"` in docs returns only WORKLOG entries (intentionally preserved as historical markers).
- `git grep "Coming in beta.1"` returns zero hits in `internal/tui/views/help.go`.

**Acceptance criteria:**

1. `docs/phase-3-beta3-wipe-recovery-plan.md` §0 reads `SHIPPED`.
2. `README.md` version header is `v0.3.0-beta.3-wipe-recovery`.
3. `docs/architecture.md` lists the three new RPC methods.
4. `WORKLOG.md` has a new dated section for beta.3.
5. No stale "Coming in beta.1" / "Coming in alpha.X" residue in `internal/tui/views/help.go`.

**Rollout risk:** None. Documentation only.

**Rollback:** Trivial. Revert the docs commit.

---

## 5. Modified Files Diff Summary

Across all five PRs:

| Path | Kind | PR | LoC delta |
|---|---|---|---|
| `internal/store/migrations/0003_chat_history.sql` | NEW | Phase0 | +30 |
| `internal/store/repo.go` | MODIFIED | Phase0 | +90 |
| `internal/store/repo_test.go` | MODIFIED | Phase0 | +120 |
| `internal/protocol/messages.go` | MODIFIED | Phase0 | +70 |
| `internal/daemon/router.go` | MODIFIED | A, C | +12, +50 |
| `internal/daemon/logs.go` | MODIFIED | A | +95 |
| `internal/daemon/logs_test.go` | NEW | A | +150 |
| `internal/daemon/chat.go` | MODIFIED | C | +45 |
| `internal/daemon/chat_test.go` | MODIFIED | C | +200 |
| `internal/client/rpc.go` | MODIFIED | A, C | +120, +50 |
| `internal/client/rpc_chat_test.go` | MODIFIED | C | +80 |
| `internal/tui/views/view.go` | MODIFIED | A | +3 |
| `internal/tui/views/logs.go` | NEW | A | +180 |
| `internal/tui/views/chat.go` | MODIFIED | C | +45 |
| `internal/tui/views/help.go` | MODIFIED | A, D | +1/-1, +3/-2 |
| `internal/tui/components/toasts.go` | NEW | B | +150 |
| `internal/tui/components/toasts_test.go` | NEW | B | +180 |
| `internal/tui/messages.go` | MODIFIED | A, B, C | +25, +12, +10 |
| `internal/tui/keys.go` | MODIFIED | B | +3 |
| `internal/tui/styles.go` | MODIFIED | B | +15 |
| `internal/tui/model.go` | MODIFIED | A, B, C | +100, +60, +25 |
| `internal/tui/app_test.go` | MODIFIED | A, B, C | (each adds ~20-80) |
| `internal/cli/agent.go` | MODIFIED | C | +30 (history subcmd) |
| `scripts/smoke-daemon.sh` | MODIFIED | A, C | +50 (Test 24), +60 (Test 25) |
| `docs/phase-3-beta3-wipe-recovery-plan.md` | NEW (this file) | — | +700 (initial draft) |
| `docs/phase-3-beta3-wipe-recovery-plan.md` | MODIFIED | D | +10 (status flip) |
| `README.md` | MODIFIED | D | +8 |
| `docs/architecture.md` | MODIFIED | D | +12 |
| `WORKLOG.md` | MODIFIED | D | +80 |

**Estimated total:** ~+2200 lines across PRs Phase0+A+B+C+D + this plan doc.

---

## 6. Threat-or-Concern Model

beta.3 is product code, not security code, so the threat model is shorter than beta.1's or beta.2's. Concerns:

### 6.1 Logs view DoS

**Concern:** A misbehaving agent emits 100k lines/sec. The TUI buffer fills, the renderer falls behind, the user's terminal locks up.

**Mitigation:** `MaxLogLines = 5000` FIFO bound on the in-memory `lines` slice in `LogsModel`. The buffered channel between `sandbox.LogsFollow` (capacity 128) and the consumer drops oldest line on overflow. Renderer scrolls to bottom on each line — no full-history re-render. Lipgloss viewport handles incremental updates. Realistic worst case: TUI shows a partial trail of the 5000 most recent lines, but doesn't crash. For full archival capture, operators redirect `dclaw agent logs -f <name> > log.txt`.

### 6.2 Chat history information leak

**Concern:** `chat_messages.content` may contain secrets that the user pasted into a chat (API keys, etc.). The table is mode-0600 by virtue of the SQLite file permissions inherited from `state.db` — but operators sharing the dclaw state-dir backup unintentionally leak old chat content.

**Mitigation:** Documented in `docs/workspace-root.md` (added in PR-D? — actually no, the existing workspace-root.md is paths-focused; consider a separate `docs/chat-history.md` follow-up. For beta.3 ship, mention in the plan but do not require the separate doc). Existing `state.db` mode 0600 prevents non-owner reads. Future hardening: per-row encryption (KMS-style) is a major follow-up; out of scope.

### 6.3 Toast injection / prompt injection in toast text

**Concern:** A malicious agent emits an error message with ANSI escape sequences that, rendered as a toast, terminal-bombs the user (clear screen, fake prompts).

**Mitigation:** All toast `Message` strings flow through `lipgloss.NewStyle().Render(message)` which strips most control codes. Belt-and-suspenders: a `sanitize` function in `internal/tui/components/toasts.go` strips `\x00` through `\x1f` (except `\n`/`\t`) before rendering. Spec'd in `Push`. No agent-emitted text currently flows into toasts (only chat-error and daemon-disconnect, both come from internal sources), but future surfaces (e.g., a "channel send failed: <reason>" toast) might surface third-party text.

### 6.4 Streaming RPC connection exhaustion

**Concern:** A buggy TUI session opens a logs stream, drops the connection, never closes — daemon side goroutine leak. With many such sessions, the daemon hits its file-descriptor limit.

**Mitigation:** `LogStreamHandler.Handle` honors `ctx.Done()` — when the per-connection `serveConn` ctx is cancelled (which happens on `conn.Close()` either side), the handler returns and the stream goroutine exits. `internal/sandbox/docker.go:LogsFollow` also honors ctx via the `select { case <-ctx.Done(): return ... }` branch. Per-process FD ulimit caps the worst case at ~1024 leaked streams before the daemon refuses new connections — observable, recoverable by daemon restart. No code change needed beyond what's already there for chat streams.

### 6.5 Chat history N+1 query on TUI open

**Concern:** Every `openChat` does a full `SELECT * FROM chat_messages WHERE agent_id=?`. For an agent with thousands of messages, this is non-trivial.

**Mitigation:** Index `chat_messages_agent_ts_idx ON chat_messages(agent_id, timestamp DESC)` covers the query. SQLite can serve `LIMIT 500` from this index in <1ms for any realistic agent. For unbounded `LIMIT 0` (no limit per Q4), the cost is linear in row count — acceptable up to ~10k messages, beyond which the Q4 follow-up adds pagination. Documented as a known limit in the WORKLOG entry.

---

## 7. Smoke-Test Additions

Full list of smoke tests after beta.3 merges. Tests 1-23 existing (beta.1/beta.2). Tests 24-25 new.

| # | Test | Probes | PR |
|---|---|---|---|
| 24 | Logs stream RPC | `agent.logs.stream` returns container stdout via `agent.log.line` notifications | A |
| 25 | Chat history persistence | round-trip + `agent chat history <name>` shows prior turn | C |

All tests follow the existing precedent: isolated `STATE_DIR`, trap-armed cleanup, prefix-whitelist guard, explicit `--state-dir`/`--daemon-socket`/`--workspace` flags. No `$HOME` touching.

**Docker requirement.** Both new tests require Docker reachable + `dclaw-agent:v0.1` built. Test 25 additionally requires `ANTHROPIC_API_KEY` (or the `_OAUTH_TOKEN` variant) per the existing Test 13 pattern. The `docker-smoke` CI workflow (main-push + tag-push triggered, per beta.2.1) covers Test 24; Test 25 will SKIP in CI because the API key is not present in repo secrets — flagged for follow-up to add a mock-Anthropic mode (already considered for Test 13; same trade-off).

**Estimated new run time:** Test 24 ~5s, Test 25 ~10s (with API key) or ~0s (skip). Post-beta.3 docker-smoke total estimated ~95s with API key, ~85s without. Acceptable.

---

## 8. Operational Impact

**User-visible changes:**

- TUI cursor on `ViewList` or `ViewDetail`: pressing `l` opens a live-streaming logs view. Press `esc` to leave; press `t` to dismiss any pending toast.
- TUI shows transient bottom-right toasts on daemon connect/disconnect, chat error, logs stream error. Auto-dismiss after 3 seconds; `t` dismisses early.
- TUI chat view, on first open per agent per session: prior conversation visible above the textarea (paged into the viewport) before the user types. No cursor change — the input area remains focused.
- New CLI subcommand `dclaw agent chat history <name>` prints the persisted history as text (default) or JSON (`-o json`).

**On existing agents:**

- Existing agents in `state.db` (created under beta.1+ posture) gain chat-history persistence on the FIRST chat round-trip post-beta.3. No backfill — pre-beta.3 chat sessions vanished with the TUI process; the table starts empty.
- Existing agents continue to start/stop/run unchanged. Container posture is untouched.

**On existing workflows:**

- `dclaw agent logs -f <name>` (CLI) is unchanged — still uses the alpha.1 polling path. The new streaming RPC is TUI-only by default, with the hidden `--stream` flag (mentioned in §4.2a) reserved for smoke testing. If §11 Q2 lands the operator-facing `--stream`, that's a beta.3.X polish patch.
- `dclaw agent attach <name>` (alpha.3 chat-attach) gains pre-loaded history. Operators who chat with the same agent over multiple sessions see the conversation thread.

**CI impact.** `docker-smoke` runtime grows ~5s on every main-push and tag-push (Test 24). Test 25 is skipped without the API key. Post-beta.3 docker-smoke estimated ~85-95s. No new GitHub Actions secrets needed.

**No new external services.** No new ports, sockets, or connections.

---

## 9. Migration / Backwards Compatibility

**Schema migration `0003_chat_history.sql`:**

```sql
-- +goose Up
-- +goose StatementBegin

CREATE TABLE chat_messages (
  id          TEXT PRIMARY KEY,                                                -- ULID
  agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  role        TEXT NOT NULL CHECK(role IN ('user','agent','system','error')),
  content     TEXT NOT NULL,
  parent_id   TEXT NOT NULL DEFAULT '',                                        -- content-addressed parent message ID
  message_id  TEXT NOT NULL UNIQUE,                                            -- matches client.chatMessageID() hash
  sequence    INTEGER NOT NULL DEFAULT 0,
  timestamp   INTEGER NOT NULL                                                 -- unix seconds
);

CREATE INDEX chat_messages_agent_ts_idx ON chat_messages(agent_id, timestamp DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS chat_messages_agent_ts_idx;
DROP TABLE IF EXISTS chat_messages;
-- +goose StatementEnd
```

Migration number `0003_` is correct because the authoritative baseline (`5b8d46d`) has only `0001_` and `0002_`. The lost pre-wipe Phase-0 tried to use `0003_` — re-using the same number now is intentional; the re-derivation IS the original Phase-0 intent. Documented as a residual-risk note in §11 Q1.

**Grandfathering rule:**

1. On `dclawd` startup, after `repo.Migrate`, the table exists empty. No additional scan or migration needed.
2. Existing agents in `state.db` continue to work; their chat sessions start persisting on the next round-trip.
3. No CASCADE concerns on agent-delete because the `ON DELETE CASCADE` FK declaration handles it.

**Protocol compatibility:**

- `agent.logs.stream` is a new method — JSON-RPC `-32601 method not found` if called against an alpha.4-era daemon. New TUI against old daemon will see this and (per §11 Q7) fall back to the alpha.1 polling path automatically OR show a toast warning "logs stream not supported by this daemon version". Decision pending; see §11 Q7.
- `agent.chat.history.list` and `agent.chat.history.append` are new methods. New TUI against old daemon will see `-32601` from history-list; the TUI handles this by treating it as "no history" and continuing with empty messages (graceful degradation, no error toast).
- New `LogsStreamParams`, `ChatHistoryListParams`, `ChatMessage` types — additive. No existing message shape changes.
- `protocol.Version` stays at 1. No bump.

No breaking wire changes.

---

## 10. Test Strategy

### Automated unit + integration tests

| Test | Location | Exercises | PR |
|---|---|---|---|
| `TestInsertAndListChatHistory` | `internal/store/repo_test.go` | round-trip insert + list | Phase0 |
| `TestInsertChatMessageDuplicateMessageID` | `internal/store/repo_test.go` | UNIQUE constraint trips | Phase0 |
| `TestDeleteChatHistoryForAgent` | `internal/store/repo_test.go` | explicit delete + cascade-delete | Phase0 |
| `TestMigrate0003UpDownUp` | `internal/store/repo_test.go` (extension) | migration roundtrip | Phase0 |
| `TestLogStreamHandlerForwardsLines` | `internal/daemon/logs_test.go` | stub docker → assert agent.log.line notifications | A |
| `TestLogStreamHandlerErrorPath` | `internal/daemon/logs_test.go` | not-found agent → error | A |
| `TestRPCLogsStream` | `internal/client/rpc_logs_test.go` (new) | mock daemon → drain channel | A |
| `TestOpenLogsKeyOpensView` | `internal/tui/app_test.go` | `'l'` key transitions to ViewLogs | A |
| `TestPushTrimsToMaxStack` | `internal/tui/components/toasts_test.go` | stack cap 3 | B |
| `TestTickExpiresOldEntries` | `internal/tui/components/toasts_test.go` | auto-dismiss | B |
| `TestDismissTopRemovesNewest` | `internal/tui/components/toasts_test.go` | manual dismiss | B |
| `TestRenderProducesBottomRightPlacement` | `internal/tui/components/toasts_test.go` | lipgloss render asserts placement | B |
| `TestToastPushedOnDaemonDisconnect` | `internal/tui/app_test.go` | daemonErrMsg → toast | B |
| `TestChatHandlerPersistsUserAndAgentMessages` | `internal/daemon/chat_test.go` | round-trip writes 2 rows | C |
| `TestChatHandlerPersistsErrorMessage` | `internal/daemon/chat_test.go` | error path writes 2 rows | C |
| `TestChatHandlerSurvivesPersistenceFailure` | `internal/daemon/chat_test.go` | DB error doesn't break chat | C |
| `TestChatHistoryListRoundTrip` | `internal/client/rpc_chat_test.go` | RPC call shape | C |
| `TestOpenChatLoadsHistory` | `internal/tui/app_test.go` | openChat fetches + populates messages | C |
| All existing tests | various | regression | Phase0/A/B/C/D |

### Integration tests via smoke-daemon.sh

- Test 24 (new, see §7).
- Test 25 (new, see §7; skipped without API key).
- Tests 1-23 (beta.1+beta.2) must remain green.

### Docker-CI (docker-smoke workflow)

- Runs on every main push and tag push (per beta.2.1 trigger).
- Current run time ~55s. Post-beta.3 estimated ~85s (Test 25 typically skipped in CI; Test 24 adds ~5s; misc Go test growth).

### Manual verification matrix

| Check | Pre-PR | Post-PR |
|---|---|---|
| Press `l` in TUI list | no-op | opens ViewLogs |
| Press `l` in TUI list with no agent selected | no-op | no-op (degraded) |
| Press `l` in TUI on stopped agent | no-op | shows "agent not running" placeholder |
| `agent exec` prints to stdout in container | not visible in TUI | line appears in ViewLogs within 1s |
| Kill daemon (`pkill dclawd`) while TUI running | TUI transitions to ViewNoDaemon | TUI transitions to ViewNoDaemon AND warning toast pushed |
| Chat with agent, leave chat, re-enter chat | empty history (alpha.3 behavior) | prior conversation visible |
| `dclaw agent chat history <name>` | command not found | prints history (text or JSON) |
| Chat with agent, then `agent delete <name>` | history orphaned (no FK to clean) | history cascades-deleted via FK |

---

## 11. Open Questions

### Q1: PR-B and PR-C parallel or serial?

**Decided: serial — branch-B then rebase-C onto B.** beta.1's 2026-04-20 PR-B+C parallel agent run hit a shared-CWD race (per WORKLOG: "PR-B's agent recovered via stash → re-checkout → re-verify"). Even though B and C touch mostly-disjoint files, both modify `internal/tui/messages.go` and `internal/tui/model.go`. Serializing eliminates the race entirely; the 30-minute round-trip cost per PR is cheaper than the recovery saga.

If future phases adopt git worktrees (one worktree per agent), the parallel approach becomes safe again. For beta.3, serial.

### Q2: `dclaw agent logs --stream` operator-facing or smoke-test only?

**Decided: smoke-test only for beta.3, follow-up promotes to operator-facing.** The `--stream` flag is implemented as part of PR-A so smoke Test 24 has a CLI surface to test against, but it's hidden via `cmd.Hidden = true` in the cobra command. Promoting to operator-facing is a cosmetic flip + a doc + a minor behavior consideration: should `--stream` print line attribution (`[stdout]` vs `[stderr]`) like `kubectl logs` does, or just raw lines? Decision deferred to a beta.3.X polish.

The CLI's existing `dclaw agent logs -f <name>` polling path stays the operator-facing default; it works, it's familiar.

### Q3: Toast lifecycle parameters — `ToastDuration` and `ToastMaxStack` values?

**Decided: 3 seconds, max stack 3.** Tradeoffs considered:

- 3s is long enough to read a one-line toast (~30 characters at 10 chars/sec reading speed is 3s).
- 5s would let the operator look up briefly without missing it; but a chatty session (many toasts) gets visually crowded.
- 1.5s is too short for the daemon-disconnect message which has actionable text ("press r to retry" — implicit).
- Max stack 3: more than 3 simultaneous toasts means something is broken; instead of stacking 8 toasts, drop the oldest. Operators see "chained" toast events as the newest 3.

Rendering positioning: bottom-right via `lipgloss.Place(width, height-2, lipgloss.Right, lipgloss.Bottom, stackBox)` over the existing chrome. Subtracting `height-2` because the existing top + bottom chrome reserve 2 rows; toasts overlay the main content area's bottom-right corner. If the active view (chat, logs) wants the bottom-right for its own renderer (chat doesn't; logs scrolls), there's a one-line collision — accepted as polish-pass concern.

### Q4: `ChatHistoryList` cap — load all or last N?

**Decided: load all (`Limit=0`).** Per Q2 of the lost original plan — Hatef-locked Option A "simple, load all". Pragmatic considerations:

- For an agent with <500 messages (the realistic case), loading all is <10ms over the Unix socket and renders in <50ms via the viewport bubble. Imperceptible.
- For an agent with 5000+ messages (synthetic stress), latency grows linearly. The TUI freezes for 100-300ms on chat-open. Visible but acceptable.
- For 50k+ messages (way beyond realistic), pagination becomes mandatory. Q4 follow-up phase tackles it.

Implementation: `ListChatHistory(agentID, 0)` short-circuits the LIMIT clause entirely (no SQLite parameter for unbounded).

### Q5: Logs view scrollback and stdout/stderr split

**Decided: 5000 lines FIFO in TUI; stdout/stderr merged for beta.3.** The current `sandbox.LogsFollow` (line 364-368) merges the demuxed streams into a single pipe (`stdcopy.StdCopy(pw, pw, rc)`). Splitting requires two pipes and a tagged-line shape on the wire. That's a non-trivial change to `LogsFollow`'s signature. For beta.3, all lines have `Stream: "stdout"` on the wire; the TUI shows them as a single column. A follow-up phase splits.

5000 lines is the FIFO bound on the in-memory `LogsModel.lines` slice. Above 5000, oldest lines drop. Operators who need the full archive use `dclaw agent logs -f <name> > /tmp/log.txt`.

### Q6: ChatHandler.Handle currently emits `MessageID` empty on the final chunk — how does the persisted agent message get a unique ID?

**Decided: derive a server-side ULID for the persisted agent reply, distinct from the user's content-hash MessageID.** The user's `MessageID` is the SHA-256 of `agentName|parentID|content` (computed client-side at `internal/client/rpc.go:524`). The agent's reply is content-addressed by ulid: `store.NewID()` generates a fresh ULID at persistence time, which becomes the `chat_messages.message_id` value for the reply row. The `parent_id` of the reply is the user's `MessageID`. This keeps the thread structure intact while avoiding a naming conflict with the user's content-hash IDs.

Alternative considered: hash the agent's reply content to derive its `MessageID`. Rejected — agent replies are non-deterministic (Anthropic API variance), so hashing the response leaks no useful "same input → same ID" property.

### Q7: New TUI against old (pre-beta.3) daemon — graceful degradation or hard error?

**Decided: silent graceful degradation.** When `client.ChatHistoryList` returns `-32601 method not found`, the TUI swallows the error and continues with empty history. No toast. No error visible to the user. Matches the wire-protocol's forward-compat pattern (older daemons silently lack newer features; `protocol.Version` stays at 1 because no breaking change).

For `client.LogsStream` returning `-32601`: fall back to the alpha.1 polling path internally — the user gets logs either way, just via the slower mechanism. Implementation: `LogsStream` catches the error code, calls `AgentLogs(name, 100, true)` instead, returns a wrapped channel. ~20 LoC fallback in `internal/client/rpc.go:LogsStream`.

Decision rationale: a TUI that won't open against a slightly-older daemon is worse UX than a TUI that loses a feature. Hatef can override.

### Q8: `dclaw agent chat history <name>` subcommand — JSON output shape?

**Decided: `[{role, content, message_id, parent_id, sequence, timestamp}, ...]`.** Same as `protocol.ChatMessage` shape. Implementation in `internal/cli/agent.go` — thin wrapper over `client.ChatHistoryList`. Default human-readable format mirrors the chat-view rendering (one indented block per message). `-o json` emits the array.

---

## 12. Follow-Ups (Deferred — Not Shipped in beta.3-wipe-recovery)

1. **Stdout/stderr split in logs view.** Tag each line with a stream identifier, render `[stderr]`-prefixed lines distinctly. See §11 Q5. Requires updating `sandbox.LogsFollow` to expose two channels OR a tagged-line shape on the merged channel.
2. **Multi-agent log multiplexing.** A "system view" showing all running agents' stdout interleaved with attribution. Needs UX design.
3. **Logs view filtering / regex search.** `/regex` to filter visible lines, `n`/`N` to jump matches. Polish pass.
4. **Chat history pagination** for agents with 10k+ messages. Per Q4 — currently we load all; pagination is the natural follow-up when an agent's history exceeds the "imperceptibly fast load" threshold. Adds `?limit=500&before_seq=N` style params to `ChatHistoryList`.
5. **Chat history search / cross-agent search.** Full-text search via SQLite FTS5 module.
6. **Chat history export** — `dclaw agent chat history <name> --output markdown` or `--output html`.
7. **Chat history clear** — `dclaw agent chat history <name> --clear` operator-facing subcommand. Calls `repo.DeleteChatHistoryForAgent`.
8. **Toast click-to-dismiss** when mouse mode is enabled (per `cmd/dclaw/main.go:18 NoMouse`).
9. **Toast colors / icons beyond level.** Per-event glyphs (e.g., 🟢 for "agent created", ⚠️ for "daemon down"). Polish.
10. **Toast persistence** — append to a ring buffer the operator can review later (`dclaw notifications`). Out of scope for beta.3.
11. **Promote `dclaw agent logs --stream` to operator-facing.** Per §11 Q2. Beta.3.X polish patch.
12. **Mock-Anthropic mode for Test 25.** Same problem Test 13 has — CI can't run without an API key. A scriptable mock would make Test 25 always run in `docker-smoke`.
13. **Wire-protocol version bump to v2** for any future breaking change. beta.3 stays at v1.
14. **`docs/chat-history.md` operator runbook** — backup/restore implications, the role of the `state.db`, retention semantics.
15. **Per-agent chat history retention policy.** `chat_messages` grows unbounded. A `[chat] retention-days = 30` config knob (extending the `[audit]` precedent from beta.2.5) bounds growth.

---

## 13. Acceptance Checklist

Hatef ticks these off before tagging.

- [ ] PR-Phase0 merges clean on top of `5b8d46d`; CI green (build + vet + docker-smoke).
- [ ] PR-Phase0: `internal/store/migrations/` has exactly three files (`0001_`, `0002_`, `0003_chat_history.sql`).
- [ ] PR-Phase0: `go test ./internal/store/...` green with three new chat-history tests.
- [ ] PR-Phase0: migration 0003 up→down→up roundtrip verified.
- [ ] PR-A merges clean; CI green.
- [ ] PR-A: `internal/tui/views/logs.go` exists; `internal/tui/views/view.go` declares `ViewLogs`.
- [ ] PR-A: `agent.logs.stream` and `agent.log.line` registered in router (grep returns ≥4 hits across daemon/protocol/client/tui).
- [ ] PR-A: smoke Test 24 green on docker-reachable host (covered by main-push docker-smoke per beta.2.1 trigger).
- [ ] PR-A: existing `agentLogsFollowPoll` retained for CLI fallback.
- [ ] PR-A: pressing `l` in TUI ViewList opens ViewLogs (manual verification).
- [ ] PR-B merges clean; CI green.
- [ ] PR-B: `internal/tui/components/toasts.go` exists with Push/Tick/DismissTop/Render.
- [ ] PR-B: `go test ./internal/tui/components/...` green with five new tests.
- [ ] PR-B: bottom-right placement asserted via lipgloss render output.
- [ ] PR-B: kill daemon mid-session → warning toast appears, auto-dismisses after 3s (manual verification).
- [ ] PR-C merges clean; CI green.
- [ ] PR-C: chat round-trip persists 2 rows (user + agent) — asserted by `TestChatHandlerPersistsUserAndAgentMessages`.
- [ ] PR-C: re-opening chat for an agent shows prior history before user types — asserted by `TestOpenChatLoadsHistory`.
- [ ] PR-C: `dclaw agent chat history <name>` subcommand exists; `-o json` emits the array.
- [ ] PR-C: smoke Test 25 green when `ANTHROPIC_API_KEY` set; SKIPs cleanly otherwise.
- [ ] PR-C: persistence failures don't break chat — asserted by `TestChatHandlerSurvivesPersistenceFailure`.
- [ ] PR-D merges clean; CI green.
- [ ] PR-D: `docs/phase-3-beta3-wipe-recovery-plan.md` §0 reads SHIPPED with the 5-commit table.
- [ ] PR-D: `README.md` version header is `v0.3.0-beta.3-wipe-recovery`.
- [ ] PR-D: `docs/architecture.md` lists the three new RPC methods and the new error code.
- [ ] PR-D: `internal/tui/views/help.go` no longer shows "Coming in beta.1" stale text.
- [ ] PR-D: `WORKLOG.md` has a new dated section for beta.3.
- [ ] All five PRs squash-merged to `main` in order Phase0 → A → B → C → D.
- [ ] `git tag -a v0.3.0-beta.3-wipe-recovery -m "Phase 3 beta.3 wipe-recovery: re-derive logs view + toasts + chat history persistence"`.
- [ ] `git push origin main v0.3.0-beta.3-wipe-recovery`.
- [ ] `docker-smoke` CI workflow green on the new tag (Tests 1-25 all pass; 25 may SKIP).
- [ ] `WORKLOG.md` ship entry written.

---

## 14. References

- `WORKLOG.md` 2026-04-19 session — incident recap. Key paragraph: "Unpushed local work lost: `docs/phase-3-beta1-plan.md` (~1444 lines), Phase 0 commit `d84554d` (migration `0003_` + protocol types + repo methods), Agent A commit `29633a8` (logs view + streaming RPC + Test 14), plus whatever Agents B (toasts) and C (chat history persistence) produced."
- `WORKLOG.md` 2026-04-25 — beta.2.6 series-complete summary. Key bullet: "Original beta.1 content lost in 2026-04-18 wipe: logs view, toasts, chat history persistence. Will need to be re-derived for v0.3.0 GA."
- `docs/phase-3-beta1-paths-hardening-plan.md` — style + structural reference.
- `docs/phase-3-beta2-sandbox-hardening-plan.md` — most-recently-shipped phase doc; structural template (this plan mirrors §0-14 layout exactly).
- `docs/phase-3-alpha3-plan.md` — chat streaming precedent (`agent.chat.send` → `agent.chat.chunk` shape that beta.3 PR-A's `agent.logs.stream` → `agent.log.line` mirrors).
- `internal/daemon/logs.go:38-67` — pre-existing `LogStreamer.Stream` skeleton that beta.3 PR-A wires to a real handler.
- `internal/daemon/chat.go:21-164` — `ChatHandler` precedent for both the streaming-RPC pattern (PR-A reuse) and the persistence hook site (PR-C target).
- `internal/sandbox/docker.go:345-383` — `LogsFollow` channel-pair shape that PR-A consumes.
- `internal/client/rpc.go:411-520 ChatSend` — dedicated-connection drain pattern that PR-A's `LogsStream` and PR-C's history fetch follow.
- `internal/tui/views/chat.go` — alpha.3 chat view; PR-C extends with `LoadHistory`. PR-A's logs view (`views/logs.go`) parallels this file's structure.
- `internal/tui/model.go:423-430 openChat` — extension point for PR-C (history fetch) and parallel construction site for PR-A (`openLogs`).
- `internal/store/repo.go` — repo method site for PR-Phase0 (`InsertChatMessage`, `ListChatHistory`, `DeleteChatHistoryForAgent`).
- `internal/store/migrations/0001_initial.sql` and `0002_workspace_trust.sql` — migration shape precedent for `0003_chat_history.sql`.
- `internal/protocol/messages.go:42-49` — error code precedent (`ErrWorkspaceForbidden = -32007` from beta.1; PR-Phase0 adds `ErrChatHistoryUnavailable = -32008`).
- `scripts/smoke-daemon.sh:166-189` — Test 13 (chat round-trip with `ANTHROPIC_API_KEY`); PR-C Test 25 mirrors this skip-on-no-key pattern.
- `scripts/smoke-daemon.sh:602-670` — Test 23 (full posture probe) shape that Test 24 mirrors for the structure/cleanup.
