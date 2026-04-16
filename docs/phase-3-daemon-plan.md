# Phase 3 Daemon Plan — v0.3.0-daemon

**Goal:** Ship the `dclawd` daemon, the SQLite-backed source of truth, the Docker API integration, a real JSON-RPC client replacing `NoopClient`, and a bubbletea TUI — all staged across five shippable alpha/beta tags, culminating in `v0.3.0` GA. Every subcommand that exited 69 in v0.2.0-cli actually works against a real daemon by the end of alpha.1, and the bare `dclaw` invocation boots a k9s-inspired dashboard by the end of alpha.2.

**Scope:** Control plane daemon + data plane container orchestration + TUI. No Discord plugin yet (v0.4+). No fleet.yaml (v0.4+). Single-host; no auth; no RBAC.

**Timeline:** 10-15 days of focused work across five sub-milestones.

**Out of scope:** see Section 2 for the explicit list.

---

## 0. Status

| Field              | Value                                                                 |
|--------------------|-----------------------------------------------------------------------|
| **Milestone tag**  | `v0.3.0-daemon` (cut as GA after five alpha/beta increments)          |
| **Sub-tags**       | `v0.3.0-alpha.1`, `v0.3.0-alpha.2`, `v0.3.0-alpha.3`, `v0.3.0-beta.1`, `v0.3.0` |
| **Est. duration**  | 10-15 days                                                            |
| **Prereqs**        | Phase 2 complete (`v0.2.0-cli` tagged). Go 1.22+ installed. Docker daemon reachable on host. |
| **Next milestone** | `v0.4.0-channels` — Discord plugin + fleet.yaml declarative mode.     |
| **Binaries**       | `dclaw` (CLI + TUI) and `dclawd` (daemon), both installed under `$GOPATH/bin` or `/usr/local/bin`. |
| **Module path**    | `github.com/itsmehatef/dclaw`                                         |

---

## 1. Definition of Done (GA criteria for v0.3.0)

The following flows work end-to-end on a stock macOS or Linux host with Docker Desktop (or native Docker) running:

```bash
# Start the daemon
dclaw daemon start
# prints: dclaw daemon started (pid 12345, socket /Users/me/.dclaw/dclaw.sock)
# exit 0

dclaw daemon status
# prints: running (pid 12345, uptime 12s, 0 agents, 0 channels)
# exit 0

# Full CRUD against a real container
dclaw agent create foo --image=dclaw-agent:v0.1 --workspace=$HOME/proj
# prints: agent foo created (id 01HKXYZ...)
# exit 0

dclaw agent list
# prints: NAME  IMAGE              STATUS    AGE
#         foo   dclaw-agent:v0.1   created   3s
# exit 0

dclaw agent start foo
# prints: agent foo started (container sha256:abc...)
# exit 0

dclaw agent logs foo --tail=20 -f
# streams stdout/stderr of the container

dclaw agent exec foo -- ls /workspace
# runs ls inside the container, streams stdout, exits with container exit code

dclaw agent stop foo
# prints: agent foo stopped

dclaw agent delete foo
# prints: agent foo deleted

dclaw daemon stop
# prints: dclaw daemon stopped
```

Bare-invocation TUI:

```bash
dclaw
# opens the k9s-style TUI: left pane agent list, main pane detail view,
# top bar daemon status, bottom bar keybindings.
# j/k navigates, enter opens detail, c opens chat, l opens logs,
# : enters command mode, ? shows help, q quits.
```

Chat attach convenience verb:

```bash
dclaw agent attach foo
# opens the TUI directly in chat mode, focused on agent foo.
```

Build + test infrastructure:

```bash
make build         # produces ./bin/dclaw and ./bin/dclawd with version info stamped
make test          # go test ./... passes, including teatest TUI tests
make lint          # golangci-lint run passes
make smoke         # scripts/smoke-daemon.sh end-to-end: daemon up, CRUD, daemon down
make smoke-tui     # scripts/smoke-tui.sh teatest-driven TUI flow
make clean         # rm -rf bin/
```

CI (`.github/workflows/build.yml`) runs `go vet`, `go build`, `go test` on push and pull request; integration / docker smoke runs only on tagged pushes (since GHA docker-in-docker is fragile).

**Non-goals for v0.3.0:** see Section 2.

---

## 2. Explicitly Out of Scope for v0.3.0

| Out of scope                                                 | Deferred to   | Reason                                                                                       |
|--------------------------------------------------------------|---------------|----------------------------------------------------------------------------------------------|
| Discord channel plugin (real message routing)                | v0.4+         | Phase 4. Wire protocol boundary 1 (channel <-> main) is speced but no plugin yet.            |
| `fleet.yaml` + `dclaw apply` + `dclaw export`                | v0.4+         | Declarative layer comes after imperative CRUD is proven.                                     |
| Slack / Teams / WhatsApp / any non-Discord channel           | v0.5+         | Follows the Discord plugin pattern.                                                          |
| Per-agent cost tracking and quota warnings                   | v0.4+         | `quota.warning` message type stubbed but not enforced.                                       |
| Worker-agent spawning (boundary 3: worker <-> dispatcher)    | v0.4+         | Main agents only in Phase 3. Workers land with the discord plugin.                           |
| `dclaw upgrade` / plugin rollback                            | v0.4+         | Single-version agent image assumed.                                                          |
| Auth / multi-tenant / RBAC                                   | Post-v1       | Not a v1 concern.                                                                            |
| Windows support                                              | Post-v1       | Unix sockets are mandatory.                                                                  |
| Web dashboard                                                | Post-v1       | TUI is our dashboard for v0.3.                                                               |
| Encrypted daemon socket                                      | Post-v1       | `0660` perms on `$XDG_RUNTIME_DIR/dclaw.sock` are the v1 story.                              |
| Remote daemon (`--daemon-socket tcp://...`)                  | Post-v1       | Local Unix socket only in v0.3.                                                              |
| Structured-logs-to-file (JSON logs with rotation)            | v0.4+         | Plain-text `~/.dclaw/logs/daemon.log` is enough.                                             |

---

## 3. Philosophy

**k9s-style operator dashboard as the default face.** The bare `dclaw` command, typed into a terminal, opens an interactive dashboard. This is a breaking UX change from v0.2.0-cli, where bare invocation printed cobra help. The reasoning: operators running a fleet of agents want situational awareness first, not a reference card. Cobra help is one flag away (`dclaw --help`), never hidden.

**Control plane / data plane split, strictly enforced.** `dclawd` (Go) manages containers, owns SQLite, listens on the Unix socket, talks to Docker. Agent containers (Node.js + pi-mono) think and act. The daemon never makes an LLM call; the container never talks to Docker. This mirrors the architecture doc (`docs/architecture.md`) and the wire protocol spec's boundary 2.

**One-stop shop.** A single `dclaw` binary is the CLI, the TUI, and (via `dclawd`) the daemon you talk to. No separate `dclaw-tui`. No separate `dclawd-ctl`. Operators install two binaries, run one, use the other.

**Staged shipping with real tags.** Five tags (`alpha.1`, `alpha.2`, `alpha.3`, `beta.1`, `v0.3.0`) mean each increment is a demoable slice. Alpha.1 is "CLI talks to daemon, which drives Docker." Alpha.2 is "look at my fleet." Alpha.3 is "chat with an agent." Beta.1 is "logs + polish." GA is the release cut.

**JSON-RPC 2.0 everywhere.** The wire protocol spec defines 23 message types across three boundaries. v0.3 lights up boundary 2 (main-agent <-> dispatcher) and a new CLI <-> daemon sub-boundary that reuses the same envelope format and handshake. Boundary 1 (channel <-> main) ships in v0.4; boundary 3 (worker <-> dispatcher) ships with worker-agent spawning in v0.4.

**CRUD stays CRUD.** The v0.2.0-cli command surface is the contract. v0.3 removes the `RequireDaemon()` stubs and fills in real RPC calls against the same flag set. Users upgrading from v0.2.0-cli see their existing muscle memory "just work." Exit 69 is never emitted by v0.3 for implemented commands.

---

## 4. Exit Codes

Inherit v0.2.0-cli's codes; add three.

| Code | Meaning                                                                                            |
|------|----------------------------------------------------------------------------------------------------|
| 0    | Success.                                                                                           |
| 1    | Generic error (internal error, I/O failure, parse failure, etc.).                                  |
| 2    | Cobra usage error (bad flag, unknown command, missing required arg).                               |
| 64   | EX_USAGE — reserved for invalid user input that cobra didn't catch (rare; name collisions etc).    |
| 65   | EX_DATAERR — daemon database integrity error (e.g., migration failure).                            |
| 69   | EX_UNAVAILABLE — still reserved. Used when the daemon is not running (`dclaw daemon is not running; run 'dclaw daemon start'`). |
| 70   | EX_SOFTWARE — internal daemon error returned as JSON-RPC `-32603` on the wire.                     |
| 75   | EX_TEMPFAIL — transient (socket busy, DB locked, Docker rate limited). Client should retry.        |
| 77   | EX_NOPERM — Docker socket unreachable due to permissions (`permission denied on /var/run/docker.sock`). |

Rationale: the BSD `sysexits.h` numbers are the closest thing to a Unix convention for "service-layer" errors. Keeping 69 as "daemon unreachable" (rather than "feature not ready") preserves the semantic — the daemon exists now, but the caller's daemon isn't running. Scripts that keyed off 69 in v0.2.0-cli will still get 69 in v0.3.0 if the daemon isn't started, with a different but compatible message.

---

## 5. Command Surface

Every command supports `--help`. All subcommands in `agent`, `channel`, `daemon`, and `status` that previously stubbed to exit 69 now execute against the daemon and return real results.

### 5.1 Bare invocation (NEW breaking UX)

| Invocation            | Behavior                                                                                  |
|-----------------------|-------------------------------------------------------------------------------------------|
| `dclaw`               | No args, no flags, stdin is a TTY: launches the TUI.                                      |
| `dclaw` (non-TTY)     | Prints cobra help to stdout, exits 0. (Pipes, CI, tests get help, not an interactive UI.) |
| `dclaw --help`        | Cobra help. Never launches the TUI.                                                       |
| `dclaw help`          | Cobra help. Never launches the TUI.                                                       |
| `dclaw <unknown>`     | Cobra usage error. Exits 2.                                                               |
| `dclaw --output=...`  | With no subcommand: cobra help with the flag accepted silently. Never launches the TUI.   |

Detection logic: in `cmd/dclaw/main.go`, if `len(os.Args) == 1` and `isatty(os.Stdin)` and `isatty(os.Stdout)`, route into `internal/tui.Run()` instead of calling `cli.Execute()`.

### 5.2 Agent subtree (now fully wired)

| Command                                  | Status in v0.3.0 | Notes                                                              |
|------------------------------------------|------------------|--------------------------------------------------------------------|
| `dclaw agent create <name>`              | Fully wired      | `client.AgentCreate()` -> daemon `agent.create` -> Docker create. |
| `dclaw agent list`                       | Fully wired      | `agent.list` -> SQLite + Docker inspect for live status.          |
| `dclaw agent get <name>`                 | Fully wired      | `agent.get`                                                        |
| `dclaw agent describe <name>`            | Fully wired      | `agent.describe` — verbose human-readable.                         |
| `dclaw agent update <name>`              | Fully wired      | `agent.update`                                                     |
| `dclaw agent delete <name>` (alias `rm`) | Fully wired      | `agent.delete` — forcibly stops container first.                  |
| `dclaw agent start <name>`               | Fully wired      | `agent.start`                                                      |
| `dclaw agent stop <name>`                | Fully wired      | `agent.stop` — SIGTERM then SIGKILL after 10s.                    |
| `dclaw agent restart <name>`             | Fully wired      | `agent.restart` — stop + start.                                    |
| `dclaw agent logs <name>`                | Fully wired      | `agent.logs` — streaming via notifications.                        |
| `dclaw agent exec <name> -- <cmd>...`    | Fully wired      | `agent.exec` — docker exec, stdio attached.                        |
| `dclaw agent attach <name>` (NEW)        | Fully wired      | Launches TUI in chat mode for this agent.                          |

### 5.3 Channel subtree (CRUD wired, routing deferred)

`channel create/list/get/delete/attach/detach` persist records in SQLite but do not actually route messages; they are metadata-only until v0.4 lands the Discord plugin. The commands exit 0 and produce the expected table/json/yaml output — they just don't cause any traffic to flow. This is explicit in the `--help` long description for each command.

| Command                                              | Status in v0.3.0                                          |
|------------------------------------------------------|-----------------------------------------------------------|
| `dclaw channel create <name>`                        | Fully wired (record only).                                |
| `dclaw channel list`                                 | Fully wired.                                              |
| `dclaw channel get <name>`                           | Fully wired.                                              |
| `dclaw channel delete <name>`                        | Fully wired.                                              |
| `dclaw channel attach <agent-name> <channel-name>`   | Fully wired (record only; no routing).                    |
| `dclaw channel detach <agent-name> <channel-name>`   | Fully wired.                                              |

### 5.4 System

| Command                 | Status in v0.3.0                                                                   |
|-------------------------|------------------------------------------------------------------------------------|
| `dclaw status`          | Daemon health + fleet overview. Table output default; `-o json\|yaml` supported.   |
| `dclaw daemon start`    | Forks `dclawd` as a background process. Writes pidfile. Exits once socket reachable.|
| `dclaw daemon stop`     | Reads pidfile, sends SIGTERM, waits up to 10s.                                    |
| `dclaw daemon status`   | Prints pid, uptime, socket path, agent count, channel count, version.              |
| `dclaw version`         | Unchanged from v0.2.0-cli.                                                         |

### 5.5 Global flags (unchanged; default for `--daemon-socket` updated)

| Flag                  | Type     | Default                                            | Description                                                                 |
|-----------------------|----------|----------------------------------------------------|-----------------------------------------------------------------------------|
| `-o`, `--output`      | string   | `table`                                            | Output format for list/get/status. One of `table`, `json`, `yaml`.          |
| `--daemon-socket`     | string   | auto: `$XDG_RUNTIME_DIR/dclaw.sock` or `~/.dclaw/dclaw.sock` | Path to the daemon Unix socket. Default computed at runtime (see 7.x).      |
| `-v`, `--verbose`     | bool     | `false`                                            | Verbose logging to stderr.                                                  |

---

## 6. Directory Layout

After this phase:

```
dclaw/
├── cmd/
│   ├── dclaw/
│   │   └── main.go                    # MODIFIED — bare-TUI-dispatch; subcommands still work
│   └── dclawd/
│       └── main.go                    # NEW — daemon entrypoint
├── internal/
│   ├── cli/
│   │   ├── root.go                    # MODIFIED — client factory, default socket path
│   │   ├── version.go                 # unchanged
│   │   ├── agent.go                   # MODIFIED — RequireDaemon -> client calls
│   │   ├── agent_attach.go            # NEW — launches TUI in chat mode
│   │   ├── channel.go                 # MODIFIED — same
│   │   ├── daemon.go                  # MODIFIED — real start/stop/status
│   │   ├── status.go                  # MODIFIED — real fleet summary
│   │   ├── exit.go                    # MODIFIED — new exit codes + helpers
│   │   └── output.go                  # NEW — table/json/yaml formatters
│   ├── client/
│   │   ├── client.go                  # unchanged (interface + types)
│   │   └── rpc.go                     # NEW — real Unix-socket JSON-RPC client
│   ├── daemon/
│   │   ├── server.go                  # NEW — socket listener + accept loop
│   │   ├── router.go                  # NEW — JSON-RPC method dispatch
│   │   ├── lifecycle.go               # NEW — agent CRUD + start/stop orchestration
│   │   ├── logs.go                    # NEW — docker log tail/stream
│   │   ├── config.go                  # NEW — state dir + socket path resolution
│   │   └── server_test.go             # NEW — in-process server smoke
│   ├── protocol/
│   │   ├── protocol.go                # unchanged (handshake)
│   │   ├── messages.go                # NEW — 23 message types
│   │   └── encoding.go                # NEW — JSON-RPC envelope encode/decode
│   ├── sandbox/
│   │   ├── docker.go                  # NEW — docker API wrapper
│   │   └── docker_test.go             # NEW — table-driven unit tests (no network)
│   ├── store/
│   │   ├── repo.go                    # NEW — sqlite repo
│   │   ├── schema.go                  # NEW — //go:embed migrations/*.sql
│   │   ├── repo_test.go               # NEW — in-memory sqlite tests
│   │   └── migrations/
│   │       └── 0001_initial.sql       # NEW — agents + events tables
│   ├── tui/
│   │   ├── app.go                     # NEW — bubbletea root Model
│   │   ├── keys.go                    # NEW — keymaps
│   │   ├── styles.go                  # NEW — lipgloss styles
│   │   ├── app_test.go                # NEW — teatest smoke
│   │   └── views/
│   │       ├── list.go                # NEW — agent list view
│   │       ├── detail.go              # NEW — detail view
│   │       ├── describe.go            # NEW — describe view
│   │       ├── chat.go                # NEW — chat view
│   │       ├── logs.go                # NEW — logs view
│   │       └── help.go                # NEW — help overlay
│   └── version/
│       └── version.go                 # unchanged
├── agent/                             # Phase 1 artifacts — unchanged
├── configs/                           # Phase 1 artifacts — unchanged
├── docs/
│   ├── architecture.md
│   ├── phase-1-plan.md
│   ├── phase-2-cli-plan.md
│   ├── phase-3-daemon-plan.md         # THIS DOC
│   └── wire-protocol-spec.md
├── plugins/discord/                   # still a .gitkeep stub
├── pkg/mcp/                           # still a .gitkeep stub
├── scripts/
│   ├── smoke-cli.sh                   # unchanged from v0.2.0-cli
│   ├── smoke-daemon.sh                # NEW — integration smoke against real daemon
│   └── smoke-tui.sh                   # NEW — teatest-driven TUI smoke
├── .github/
│   └── workflows/
│       ├── build.yml                  # MODIFIED — teatest added; docker smoke gated by tag
│       └── release.yml                # NEW — tag-triggered release builds
├── Makefile                           # MODIFIED — dclawd target, tui target, migrate target
├── go.mod                             # MODIFIED — new deps
├── go.sum                             # regenerated
└── README.md                          # MODIFIED — screenshots, demo, updated install
```

If `internal/daemon/lifecycle.go` grows past ~500 lines, split into `lifecycle_agent.go`, `lifecycle_channel.go`, `lifecycle_exec.go`. Start monolithic; split on pain.

---

## 7. Exact File Contents

Each subsection is copy-paste ready. Where a later-alpha file is speced at skeleton granularity (e.g., `logs.go` for beta.1), that's called out explicitly.

### 7.1 `go.mod`

```
module github.com/itsmehatef/dclaw

go 1.22

require (
	github.com/charmbracelet/bubbles v0.18.0
	github.com/charmbracelet/bubbletea v0.25.0
	github.com/charmbracelet/lipgloss v0.10.0
	github.com/charmbracelet/x/exp/teatest v0.0.0-20240229115032-4b47b6fdaf28
	github.com/docker/docker v26.1.3+incompatible
	github.com/docker/go-connections v0.5.0
	github.com/mattn/go-isatty v0.0.20
	github.com/oklog/ulid/v2 v2.1.0
	github.com/pressly/goose/v3 v3.21.1
	github.com/spf13/cobra v1.8.1
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.30.1
)
```

Indirect deps are populated by `go mod tidy` in Step 1 of alpha.1. The exact `// indirect` block below will land in `go.sum` and the sibling `require (...)` block in `go.mod`; don't hand-write it.

### 7.2 `cmd/dclawd/main.go`

```go
// dclawd is the dclaw daemon: the host-side control plane. It listens on a
// Unix domain socket, speaks JSON-RPC 2.0 to the dclaw CLI (and eventually
// to channel plugins and main-agent containers), and drives Docker via the
// official API client.
//
// Flags:
//   --socket <path>   Override the socket path (default: $XDG_RUNTIME_DIR/dclaw.sock).
//   --state-dir <d>   Override the state directory (default: ~/.dclaw).
//   --log-level lvl   debug|info|warn|error (default: info).
//   --foreground      Stay in the foreground; don't detach. Default when run from dclaw daemon start.
//   --migrate-only    Run pending SQLite migrations and exit 0. Used by `make migrate`.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
	"github.com/itsmehatef/dclaw/internal/version"
)

func main() {
	var (
		socketPath  = flag.String("socket", "", "Unix socket path (default: auto)")
		stateDir    = flag.String("state-dir", "", "state directory (default: ~/.dclaw)")
		logLevel    = flag.String("log-level", "info", "log level: debug|info|warn|error")
		foreground  = flag.Bool("foreground", true, "run in foreground (default: true)")
		showVer     = flag.Bool("version", false, "print version and exit")
		migrateOnly = flag.Bool("migrate-only", false, "run pending migrations and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("dclawd version %s (commit %s, built %s, %s)\n",
			version.Version, version.Commit, version.BuildDate, version.GoVersion())
		return
	}

	cfg, err := daemon.LoadConfig(*socketPath, *stateDir, *logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dclawd: config error: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel, cfg.LogPath)
	logger.Info("dclawd starting",
		"version", version.Version,
		"socket", cfg.SocketPath,
		"state_dir", cfg.StateDir,
	)

	// Initialize SQLite store + run embedded migrations.
	repo, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("store open failed", "err", err)
		os.Exit(65) // EX_DATAERR
	}
	defer repo.Close()
	if err := repo.Migrate(context.Background()); err != nil {
		logger.Error("migration failed", "err", err)
		os.Exit(65)
	}

	// --migrate-only: run migrations and exit. No daemon, no Docker, no socket.
	// Invoked by `make migrate` and by operators who want to run migrations
	// before starting the daemon (e.g. during upgrades).
	if *migrateOnly {
		logger.Info("migrate-only: migrations complete; exiting")
		return
	}

	// Initialize Docker client.
	docker, err := sandbox.NewDockerClient()
	if err != nil {
		logger.Error("docker connect failed", "err", err)
		os.Exit(77) // EX_NOPERM
	}
	defer docker.Close()

	// Wire and run the server.
	srv := daemon.NewServer(cfg, logger, repo, docker)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Write pidfile for `dclaw daemon stop`.
	if err := cfg.WritePIDFile(os.Getpid()); err != nil {
		logger.Error("pidfile write failed", "err", err)
		os.Exit(1)
	}
	defer cfg.RemovePIDFile()

	if _ = foreground; true {
		if err := srv.Run(ctx); err != nil {
			logger.Error("server stopped with error", "err", err)
			os.Exit(70) // EX_SOFTWARE
		}
	}

	logger.Info("dclawd stopped cleanly")
}

// newLogger constructs a slog.Logger writing to cfg.LogPath (falls back to
// stderr if the file can't be opened). Level is parsed from cfg.LogLevel.
func newLogger(levelStr, path string) *slog.Logger {
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var w *os.File = os.Stderr
	if path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			w = f
		}
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
```

Rationale: tiny main, all real work delegated to `internal/daemon`. Foreground-only for v0.3; `dclaw daemon start` invokes `dclawd --foreground` in a child process and detaches on the CLI side (documented in 7.10).

### 7.3 `internal/daemon/config.go`

```go
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// Config holds the daemon's resolved paths and settings. Constructed once at
// startup via LoadConfig; immutable thereafter.
type Config struct {
	SocketPath string // $XDG_RUNTIME_DIR/dclaw.sock or ~/.dclaw/dclaw.sock
	StateDir   string // ~/.dclaw (created with mode 0700)
	DBPath     string // <StateDir>/state.db
	LogDir     string // <StateDir>/logs
	LogPath    string // <LogDir>/daemon.log
	PIDPath    string // <StateDir>/dclawd.pid
	LogLevel   string // debug|info|warn|error
}

// LoadConfig resolves default paths and validates the runtime environment.
// socketOverride / stateDirOverride / logLevel may be empty.
func LoadConfig(socketOverride, stateDirOverride, logLevel string) (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	stateDir := stateDirOverride
	if stateDir == "" {
		stateDir = filepath.Join(home, ".dclaw")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir state dir %q: %w", stateDir, err)
	}

	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir log dir %q: %w", logDir, err)
	}

	socketPath := socketOverride
	if socketPath == "" {
		socketPath = DefaultSocketPath(stateDir)
	}

	if logLevel == "" {
		logLevel = "info"
	}

	return &Config{
		SocketPath: socketPath,
		StateDir:   stateDir,
		DBPath:     filepath.Join(stateDir, "state.db"),
		LogDir:     logDir,
		LogPath:    filepath.Join(logDir, "daemon.log"),
		PIDPath:    filepath.Join(stateDir, "dclawd.pid"),
		LogLevel:   logLevel,
	}, nil
}

// DefaultSocketPath returns the resolved socket path for this host.
//
// On Linux, prefer $XDG_RUNTIME_DIR/dclaw.sock (typically /run/user/<uid>).
// If XDG_RUNTIME_DIR is unset or not writable, fall back to <stateDir>/dclaw.sock.
// On macOS, XDG_RUNTIME_DIR is rarely set; always use <stateDir>/dclaw.sock.
func DefaultSocketPath(stateDir string) string {
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			if fi, err := os.Stat(xdg); err == nil && fi.IsDir() {
				return filepath.Join(xdg, "dclaw.sock")
			}
		}
	}
	return filepath.Join(stateDir, "dclaw.sock")
}

// WritePIDFile atomically writes pid to PIDPath with mode 0600.
func (c *Config) WritePIDFile(pid int) error {
	tmp := c.PIDPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.PIDPath)
}

// RemovePIDFile deletes the pidfile. Safe to call multiple times.
func (c *Config) RemovePIDFile() {
	_ = os.Remove(c.PIDPath)
}

// ReadPIDFile returns the pid from PIDPath, or 0 if no pidfile exists.
func (c *Config) ReadPIDFile() (int, error) {
	b, err := os.ReadFile(c.PIDPath)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(trimSpace(b)))
	if err != nil {
		return 0, fmt.Errorf("invalid pidfile contents: %w", err)
	}
	return pid, nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
```

### 7.4 `internal/daemon/server.go`

```go
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

// Server is the dclawd Unix-socket listener. One listener, many connections.
// Each connection is handled in its own goroutine; each message on a
// connection is processed sequentially (per the wire protocol's v1
// one-request-at-a-time rule).
type Server struct {
	cfg    *Config
	log    *slog.Logger
	repo   *store.Repo
	docker *sandbox.DockerClient

	router *Router

	mu       sync.Mutex
	listener net.Listener
	closed   bool
}

// NewServer wires up a Server. Call Run to start it.
func NewServer(cfg *Config, log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *Server {
	s := &Server{cfg: cfg, log: log, repo: repo, docker: docker}
	s.router = NewRouter(log, repo, docker)
	return s
}

// Run starts listening on cfg.SocketPath and serves connections until ctx is
// cancelled. Returns the first non-normal error.
func (s *Server) Run(ctx context.Context) error {
	// Remove stale socket from a crashed previous run.
	_ = os.Remove(s.cfg.SocketPath)

	ln, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %q: %w", s.cfg.SocketPath, err)
	}
	if err := os.Chmod(s.cfg.SocketPath, 0o660); err != nil {
		ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		ln.Close()
		_ = os.Remove(s.cfg.SocketPath)
	}()

	// Cancel blocks when ctx is done.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	s.log.Info("dclawd listening", "socket", s.cfg.SocketPath)

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				break
			}
			s.log.Warn("accept error", "err", err)
			continue
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			s.serveConn(ctx, c)
		}(conn)
	}

	wg.Wait()
	return nil
}

// serveConn handles one connection for its lifetime: handshake, then
// per-message dispatch until EOF or ctx cancellation.
func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	// 1. Handshake required.
	var hs protocol.Envelope
	if err := dec.Decode(&hs); err != nil {
		s.log.Warn("handshake decode failed", "err", err)
		return
	}
	if hs.Method != "dclaw.handshake" {
		_ = enc.Encode(protocol.ErrorResponse(hs.ID, protocol.ErrInvalidRequest, "first message must be dclaw.handshake", nil))
		return
	}
	var hreq protocol.Handshake
	if err := json.Unmarshal(hs.Params, &hreq); err != nil {
		_ = enc.Encode(protocol.ErrorResponse(hs.ID, protocol.ErrInvalidParams, "handshake params invalid", err.Error()))
		return
	}
	if hreq.ProtocolVersion != protocol.Version {
		_ = enc.Encode(protocol.ErrorResponse(hs.ID, protocol.ErrInvalidRequest, "unsupported protocol version",
			map[string]any{"requested": hreq.ProtocolVersion, "supported": []int{protocol.Version}}))
		return
	}
	if err := enc.Encode(protocol.SuccessResponse(hs.ID, protocol.HandshakeResult{Accepted: true, NegotiatedVersion: protocol.Version})); err != nil {
		return
	}
	s.log.Debug("handshake ok", "component", hreq.ComponentType, "id", hreq.ComponentID)

	// 2. Main message loop.
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
		resp := s.router.Dispatch(ctx, &env)
		if resp != nil {
			if err := enc.Encode(resp); err != nil {
				return
			}
		}
	}
}
```

### 7.5 `internal/daemon/router.go`

```go
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
```

### 7.6 `internal/daemon/lifecycle.go`

```go
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

// Lifecycle owns the real work of CRUD + start/stop for agents and channels.
// It sits between the Router (pure RPC dispatch) and the store + docker
// layers. All public methods return domain errors; the router maps those to
// RPC error envelopes.
type Lifecycle struct {
	log    *slog.Logger
	repo   *store.Repo
	docker *sandbox.DockerClient
}

// NewLifecycle constructs a Lifecycle around an existing store + docker
// client.
func NewLifecycle(log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *Lifecycle {
	return &Lifecycle{log: log, repo: repo, docker: docker}
}

// ---------- agent ----------

// AgentCreate inserts a new agent record and (if Docker reachable) creates
// the container in "created" state (not started). Returns the populated
// record.
func (l *Lifecycle) AgentCreate(ctx context.Context, req protocol.AgentCreateParams) (protocol.Agent, error) {
	if strings.TrimSpace(req.Name) == "" {
		return protocol.Agent{}, fmt.Errorf("agent name required")
	}
	if strings.TrimSpace(req.Image) == "" {
		return protocol.Agent{}, fmt.Errorf("agent image required")
	}

	if _, err := l.repo.GetAgent(ctx, req.Name); err == nil {
		return protocol.Agent{}, fmt.Errorf("agent %q already exists", req.Name)
	}

	now := time.Now().Unix()
	id := ulid.Make().String()

	envMap := parseKVList(req.Env)
	labelMap := parseKVList(req.Labels)

	containerID, err := l.docker.CreateAgent(ctx, sandbox.CreateSpec{
		Name:      fmt.Sprintf("dclaw-%s", req.Name),
		Image:     req.Image,
		Env:       envMap,
		Labels:    labelMap,
		Workspace: req.Workspace,
	})
	if err != nil {
		return protocol.Agent{}, fmt.Errorf("docker create: %w", err)
	}

	rec := store.AgentRecord{
		ID:          id,
		Name:        req.Name,
		Image:       req.Image,
		Status:      "created",
		ContainerID: containerID,
		Workspace:   req.Workspace,
		Env:         jsonMustMarshal(envMap),
		Labels:      jsonMustMarshal(labelMap),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := l.repo.InsertAgent(ctx, rec); err != nil {
		// Best-effort cleanup of the orphaned container.
		_ = l.docker.DeleteAgent(ctx, containerID)
		return protocol.Agent{}, fmt.Errorf("store insert: %w", err)
	}

	l.log.Info("agent created", "name", req.Name, "image", req.Image, "container_id", containerID)
	return agentToWire(rec), nil
}

// AgentList returns all agents, enriching each record with the live Docker
// status if the container is known.
func (l *Lifecycle) AgentList(ctx context.Context) ([]protocol.Agent, error) {
	recs, err := l.repo.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Agent, 0, len(recs))
	for _, r := range recs {
		live := r
		if r.ContainerID != "" {
			if st, err := l.docker.InspectStatus(ctx, r.ContainerID); err == nil {
				live.Status = st
			}
		}
		out = append(out, agentToWire(live))
	}
	return out, nil
}

// AgentGet fetches a single agent by name with live status.
func (l *Lifecycle) AgentGet(ctx context.Context, name string) (protocol.Agent, error) {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return protocol.Agent{}, fmt.Errorf("agent %q not found", name)
	}
	if rec.ContainerID != "" {
		if st, err := l.docker.InspectStatus(ctx, rec.ContainerID); err == nil {
			rec.Status = st
		}
	}
	return agentToWire(rec), nil
}

// AgentDescribe returns a verbose per-agent projection including recent events.
func (l *Lifecycle) AgentDescribe(ctx context.Context, name string) (protocol.AgentDescribeResult, error) {
	a, err := l.AgentGet(ctx, name)
	if err != nil {
		return protocol.AgentDescribeResult{}, err
	}
	events, err := l.repo.RecentEvents(ctx, a.ID, 20)
	if err != nil {
		return protocol.AgentDescribeResult{}, err
	}
	return protocol.AgentDescribeResult{
		Agent:  a,
		Events: events,
	}, nil
}

// AgentUpdate mutates image/env/labels. Image change requires the container to
// be recreated; v0.3 requires the agent to be in "stopped" or "created" state
// first.
func (l *Lifecycle) AgentUpdate(ctx context.Context, req protocol.AgentUpdateParams) (protocol.Agent, error) {
	rec, err := l.repo.GetAgent(ctx, req.Name)
	if err != nil {
		return protocol.Agent{}, fmt.Errorf("agent %q not found", req.Name)
	}
	if req.Image != "" && (rec.Status != "created" && rec.Status != "stopped" && rec.Status != "exited") {
		return protocol.Agent{}, fmt.Errorf("cannot update image while agent is %s; stop it first", rec.Status)
	}
	if req.Image != "" {
		rec.Image = req.Image
	}
	if req.Env != nil {
		rec.Env = jsonMustMarshal(parseKVList(req.Env))
	}
	if req.Labels != nil {
		rec.Labels = jsonMustMarshal(parseKVList(req.Labels))
	}
	rec.UpdatedAt = time.Now().Unix()
	if err := l.repo.UpdateAgent(ctx, rec); err != nil {
		return protocol.Agent{}, err
	}
	return agentToWire(rec), nil
}

// AgentDelete stops (if running), removes the container, deletes the DB
// record.
func (l *Lifecycle) AgentDelete(ctx context.Context, name string) error {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return fmt.Errorf("agent %q not found", name)
	}
	if rec.ContainerID != "" {
		_ = l.docker.StopAgent(ctx, rec.ContainerID, 10*time.Second)
		_ = l.docker.DeleteAgent(ctx, rec.ContainerID)
	}
	if err := l.repo.DeleteAgent(ctx, name); err != nil {
		return err
	}
	l.log.Info("agent deleted", "name", name)
	return nil
}

// AgentStart starts the container (if not already running) and flips the DB
// status to "running".
func (l *Lifecycle) AgentStart(ctx context.Context, name string) error {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return fmt.Errorf("agent %q not found", name)
	}
	if rec.ContainerID == "" {
		return fmt.Errorf("agent %q has no container", name)
	}
	if err := l.docker.StartAgent(ctx, rec.ContainerID); err != nil {
		return fmt.Errorf("docker start: %w", err)
	}
	rec.Status = "running"
	rec.UpdatedAt = time.Now().Unix()
	if err := l.repo.UpdateAgent(ctx, rec); err != nil {
		return err
	}
	_ = l.repo.InsertEvent(ctx, store.EventRecord{AgentID: rec.ID, Type: "started", Data: "", Timestamp: time.Now().Unix()})
	return nil
}

// AgentStop sends SIGTERM, waits 10s, then SIGKILL if still alive.
func (l *Lifecycle) AgentStop(ctx context.Context, name string) error {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return fmt.Errorf("agent %q not found", name)
	}
	if rec.ContainerID == "" {
		return fmt.Errorf("agent %q has no container", name)
	}
	if err := l.docker.StopAgent(ctx, rec.ContainerID, 10*time.Second); err != nil {
		return fmt.Errorf("docker stop: %w", err)
	}
	rec.Status = "stopped"
	rec.UpdatedAt = time.Now().Unix()
	if err := l.repo.UpdateAgent(ctx, rec); err != nil {
		return err
	}
	_ = l.repo.InsertEvent(ctx, store.EventRecord{AgentID: rec.ID, Type: "stopped", Data: "", Timestamp: time.Now().Unix()})
	return nil
}

// AgentRestart = stop + start.
func (l *Lifecycle) AgentRestart(ctx context.Context, name string) error {
	if err := l.AgentStop(ctx, name); err != nil {
		// If the agent was already stopped, fall through.
		if !strings.Contains(err.Error(), "not running") {
			return err
		}
	}
	return l.AgentStart(ctx, name)
}

// AgentLogsBulk returns the last N log lines (stdout + stderr interleaved).
func (l *Lifecycle) AgentLogsBulk(ctx context.Context, name string, tail int) ([]string, error) {
	rec, err := l.repo.GetAgent(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	if rec.ContainerID == "" {
		return nil, fmt.Errorf("agent %q has no container", name)
	}
	if tail <= 0 {
		tail = 100
	}
	return l.docker.LogsTail(ctx, rec.ContainerID, tail)
}

// AgentExec runs a command inside the agent container synchronously.
func (l *Lifecycle) AgentExec(ctx context.Context, req protocol.AgentExecParams) (protocol.AgentExecResult, error) {
	rec, err := l.repo.GetAgent(ctx, req.Name)
	if err != nil {
		return protocol.AgentExecResult{}, fmt.Errorf("agent %q not found", req.Name)
	}
	if rec.ContainerID == "" {
		return protocol.AgentExecResult{}, fmt.Errorf("agent %q has no container", req.Name)
	}
	stdout, stderr, code, err := l.docker.ExecIn(ctx, rec.ContainerID, req.Argv)
	if err != nil {
		return protocol.AgentExecResult{}, err
	}
	return protocol.AgentExecResult{
		ExitCode: code,
		Stdout:   stdout,
		Stderr:   stderr,
	}, nil
}

// ---------- channel ----------

func (l *Lifecycle) ChannelCreate(ctx context.Context, req protocol.ChannelCreateParams) (protocol.Channel, error) {
	if req.Name == "" || req.Type == "" {
		return protocol.Channel{}, fmt.Errorf("channel name and type required")
	}
	id := ulid.Make().String()
	now := time.Now().Unix()
	rec := store.ChannelRecord{
		ID:        id,
		Name:      req.Name,
		Type:      req.Type,
		Config:    req.Config,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := l.repo.InsertChannel(ctx, rec); err != nil {
		return protocol.Channel{}, err
	}
	return protocol.Channel{Name: req.Name, Type: req.Type, Config: req.Config}, nil
}

// ---------- helpers ----------

func parseKVList(items []string) map[string]string {
	out := make(map[string]string, len(items))
	for _, kv := range items {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out[kv] = ""
			continue
		}
		out[kv[:eq]] = kv[eq+1:]
	}
	return out
}

func jsonMustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func agentToWire(rec store.AgentRecord) protocol.Agent {
	var env, labels map[string]string
	if rec.Env != "" {
		_ = json.Unmarshal([]byte(rec.Env), &env)
	}
	if rec.Labels != "" {
		_ = json.Unmarshal([]byte(rec.Labels), &labels)
	}
	return protocol.Agent{
		ID:          rec.ID,
		Name:        rec.Name,
		Image:       rec.Image,
		Status:      rec.Status,
		ContainerID: rec.ContainerID,
		Workspace:   rec.Workspace,
		Env:         env,
		Labels:      labels,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
	}
}
```

### 7.7 `internal/daemon/logs.go`

```go
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
```

### 7.8 `internal/store/schema.go`

```go
// Package store is the SQLite-backed source of truth for the daemon. It
// wraps modernc.org/sqlite (pure-Go driver; no cgo) and uses goose with
// embedded migrations.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Repo is the daemon's persistence layer. Construct with Open, close via
// Close.
type Repo struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and returns a *Repo.
// It does NOT run migrations; call Migrate for that.
func Open(path string) (*Repo, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("db.Ping: %w", err)
	}
	// SQLite is single-writer; a small pool is fine and avoids lock churn.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return &Repo{db: db}, nil
}

// Close shuts down the underlying *sql.DB.
func (r *Repo) Close() error { return r.db.Close() }

// Migrate runs the embedded migrations. Safe to call on every boot.
func (r *Repo) Migrate(ctx context.Context) error {
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("sqlite"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, r.db, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
```

### 7.9 `internal/store/migrations/0001_initial.sql`

```sql
-- +goose Up
-- +goose StatementBegin

CREATE TABLE agents (
  id             TEXT PRIMARY KEY,           -- ULID
  name           TEXT NOT NULL UNIQUE,
  image          TEXT NOT NULL,
  status         TEXT NOT NULL,              -- created|running|stopped|exited|errored
  container_id   TEXT,
  workspace_path TEXT,
  labels         TEXT NOT NULL DEFAULT '{}', -- JSON object
  env            TEXT NOT NULL DEFAULT '{}', -- JSON object
  created_at     INTEGER NOT NULL,           -- unix seconds
  updated_at     INTEGER NOT NULL
);

CREATE INDEX agents_status_idx ON agents(status);
CREATE INDEX agents_updated_at_idx ON agents(updated_at);

CREATE TABLE channels (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL UNIQUE,
  type           TEXT NOT NULL,              -- discord|slack|cli|...
  config         TEXT NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL
);

CREATE INDEX channels_type_idx ON channels(type);

CREATE TABLE channel_bindings (
  agent_id       TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  channel_id     TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  created_at     INTEGER NOT NULL,
  PRIMARY KEY (agent_id, channel_id)
);

CREATE TABLE events (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id       TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  type           TEXT NOT NULL,              -- started|stopped|errored|log|...
  data           TEXT NOT NULL DEFAULT '',   -- JSON payload, free-form
  timestamp      INTEGER NOT NULL
);

CREATE INDEX events_agent_timestamp_idx ON events(agent_id, timestamp DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS events_agent_timestamp_idx;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS channel_bindings;
DROP INDEX IF EXISTS channels_type_idx;
DROP TABLE IF EXISTS channels;
DROP INDEX IF EXISTS agents_updated_at_idx;
DROP INDEX IF EXISTS agents_status_idx;
DROP TABLE IF EXISTS agents;
-- +goose StatementEnd
```

### 7.10 `internal/store/repo.go`

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/itsmehatef/dclaw/internal/protocol"
)

// AgentRecord is the on-disk shape of an agent row. Env and Labels are raw
// JSON text; the Lifecycle layer marshals/unmarshals.
type AgentRecord struct {
	ID           string
	Name         string
	Image        string
	Status       string
	ContainerID  string
	Workspace    string
	Labels       string
	Env          string
	CreatedAt    int64
	UpdatedAt    int64
}

// ChannelRecord is the on-disk shape of a channel row.
type ChannelRecord struct {
	ID        string
	Name      string
	Type      string
	Config    string
	CreatedAt int64
	UpdatedAt int64
}

// EventRecord is the on-disk shape of an event row.
type EventRecord struct {
	ID        int64
	AgentID   string
	Type      string
	Data      string
	Timestamp int64
}

// InsertAgent inserts a new row. Returns an error if the name is not unique.
func (r *Repo) InsertAgent(ctx context.Context, rec AgentRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agents (id, name, image, status, container_id, workspace_path, labels, env, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.Name, rec.Image, rec.Status, rec.ContainerID, rec.Workspace, rec.Labels, rec.Env, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// GetAgent returns the agent with the given name, or an error if none exists.
func (r *Repo) GetAgent(ctx context.Context, name string) (AgentRecord, error) {
	var rec AgentRecord
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, image, status, COALESCE(container_id, ''), COALESCE(workspace_path, ''), labels, env, created_at, updated_at
		FROM agents WHERE name = ?
	`, name).Scan(&rec.ID, &rec.Name, &rec.Image, &rec.Status, &rec.ContainerID, &rec.Workspace, &rec.Labels, &rec.Env, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentRecord{}, fmt.Errorf("agent %q not found", name)
	}
	return rec, err
}

// ListAgents returns all agents ordered by created_at desc.
func (r *Repo) ListAgents(ctx context.Context) ([]AgentRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, image, status, COALESCE(container_id, ''), COALESCE(workspace_path, ''), labels, env, created_at, updated_at
		FROM agents ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentRecord
	for rows.Next() {
		var rec AgentRecord
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Image, &rec.Status, &rec.ContainerID, &rec.Workspace, &rec.Labels, &rec.Env, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// UpdateAgent replaces an existing row by name. Returns an error if no row
// was matched.
func (r *Repo) UpdateAgent(ctx context.Context, rec AgentRecord) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE agents SET image=?, status=?, container_id=?, workspace_path=?, labels=?, env=?, updated_at=?
		WHERE name = ?
	`, rec.Image, rec.Status, rec.ContainerID, rec.Workspace, rec.Labels, rec.Env, rec.UpdatedAt, rec.Name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q not found", rec.Name)
	}
	return nil
}

// DeleteAgent removes a row by name.
func (r *Repo) DeleteAgent(ctx context.Context, name string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM agents WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q not found", name)
	}
	return nil
}

// InsertChannel stores a channel record.
func (r *Repo) InsertChannel(ctx context.Context, rec ChannelRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO channels (id, name, type, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.Name, rec.Type, rec.Config, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// GetChannel returns the channel with the given name.
func (r *Repo) GetChannel(ctx context.Context, name string) (protocol.Channel, error) {
	var c protocol.Channel
	err := r.db.QueryRowContext(ctx, `SELECT name, type, config FROM channels WHERE name = ?`, name).
		Scan(&c.Name, &c.Type, &c.Config)
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("channel %q not found", name)
	}
	return c, err
}

// ListChannels returns all channels.
func (r *Repo) ListChannels(ctx context.Context) ([]protocol.Channel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name, type, config FROM channels ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Channel
	for rows.Next() {
		var c protocol.Channel
		if err := rows.Scan(&c.Name, &c.Type, &c.Config); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteChannel removes a channel by name.
func (r *Repo) DeleteChannel(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channels WHERE name = ?`, name)
	return err
}

// AttachChannel creates a binding row between agent and channel.
func (r *Repo) AttachChannel(ctx context.Context, agentName, channelName string) error {
	aID, cID, err := r.lookupBindingIDs(ctx, agentName, channelName)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO channel_bindings (agent_id, channel_id, created_at) VALUES (?, ?, ?)
	`, aID, cID, time.Now().Unix())
	return err
}

// DetachChannel deletes a binding row.
func (r *Repo) DetachChannel(ctx context.Context, agentName, channelName string) error {
	aID, cID, err := r.lookupBindingIDs(ctx, agentName, channelName)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM channel_bindings WHERE agent_id = ? AND channel_id = ?`, aID, cID)
	return err
}

func (r *Repo) lookupBindingIDs(ctx context.Context, agentName, channelName string) (string, string, error) {
	var aID, cID string
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM agents WHERE name = ?`, agentName).Scan(&aID); err != nil {
		return "", "", fmt.Errorf("agent %q not found", agentName)
	}
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM channels WHERE name = ?`, channelName).Scan(&cID); err != nil {
		return "", "", fmt.Errorf("channel %q not found", channelName)
	}
	return aID, cID, nil
}

// InsertEvent appends an event row.
func (r *Repo) InsertEvent(ctx context.Context, rec EventRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO events (agent_id, type, data, timestamp) VALUES (?, ?, ?, ?)
	`, rec.AgentID, rec.Type, rec.Data, rec.Timestamp)
	return err
}

// RecentEvents returns up to `limit` most recent events for the given agent.
func (r *Repo) RecentEvents(ctx context.Context, agentID string, limit int) ([]protocol.Event, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT type, data, timestamp FROM events
		WHERE agent_id = ? ORDER BY timestamp DESC LIMIT ?
	`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Event
	for rows.Next() {
		var e protocol.Event
		if err := rows.Scan(&e.Type, &e.Data, &e.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// NewID returns a new ULID string suitable for id columns.
func NewID() string { return ulid.Make().String() }
```

### 7.11 `internal/protocol/messages.go`

```go
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
```

### 7.12 `internal/protocol/encoding.go`

```go
package protocol

import (
	"encoding/json"
)

// SuccessResponse builds a JSON-RPC 2.0 success envelope from a result value.
func SuccessResponse(id any, result any) *Envelope {
	raw, _ := json.Marshal(result)
	return &Envelope{JSONRPC: "2.0", Result: raw, ID: id}
}

// ErrorResponse builds a JSON-RPC 2.0 error envelope.
func ErrorResponse(id any, code int, msg string, data any) *Envelope {
	return &Envelope{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: msg, Data: data},
		ID:      id,
	}
}

// Request builds a JSON-RPC 2.0 request envelope from a method + params.
// id should be the caller's monotonic counter.
func Request(id int, method string, params any) *Envelope {
	raw, _ := json.Marshal(params)
	return &Envelope{JSONRPC: "2.0", Method: method, Params: raw, ID: id}
}

// Notification builds a JSON-RPC 2.0 notification envelope.
func Notification(method string, params any) *Envelope {
	raw, _ := json.Marshal(params)
	return &Envelope{JSONRPC: "2.0", Method: method, Params: raw}
}

// DecodeResult unmarshals env.Result into v. Returns env.Error unwrapped if
// the response is an error.
func DecodeResult(env *Envelope, v any) error {
	if env.Error != nil {
		return env.Error
	}
	if len(env.Result) == 0 {
		return nil
	}
	return json.Unmarshal(env.Result, v)
}

// Error implements error on RPCError so DecodeResult can return it directly.
func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}
```

### 7.13 `internal/sandbox/docker.go`

```go
// Package sandbox wraps the official Docker Engine API client with a
// dclaw-shaped surface. This is the only place in the codebase that imports
// github.com/docker/docker; daemon code talks to DockerClient methods, not to
// docker types directly.
package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerClient is a thin wrapper around the docker SDK's client.Client.
type DockerClient struct {
	cli *client.Client
}

// CreateSpec captures everything the daemon needs to create a new agent
// container.
type CreateSpec struct {
	Name      string
	Image     string
	Env       map[string]string
	Labels    map[string]string
	Workspace string
}

// NewDockerClient connects to the docker daemon using default env resolution.
// Returns an error if the socket is unreachable.
func NewDockerClient() (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	// Round-trip a Ping so a startup-time error surfaces before anything else.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("docker ping: %w", err)
	}
	return &DockerClient{cli: cli}, nil
}

// Close shuts down the underlying client.
func (d *DockerClient) Close() error {
	if d == nil || d.cli == nil {
		return nil
	}
	return d.cli.Close()
}

// CreateAgent creates (but does not start) a container for the given spec.
// Returns the docker container ID.
func (d *DockerClient) CreateAgent(ctx context.Context, spec CreateSpec) (string, error) {
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	labels := make(map[string]string, len(spec.Labels)+1)
	for k, v := range spec.Labels {
		labels[k] = v
	}
	labels["dclaw.managed"] = "true"
	labels["dclaw.name"] = spec.Name

	var mounts []mount.Mount
	if spec.Workspace != "" {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: spec.Workspace,
			Target: "/workspace",
		})
	}

	cfg := &container.Config{
		Image:  spec.Image,
		Env:    env,
		Labels: labels,
		Tty:    false,
	}
	hostCfg := &container.HostConfig{
		Mounts: mounts,
		RestartPolicy: container.RestartPolicy{Name: "no"},
	}

	resp, err := d.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("ContainerCreate: %w", err)
	}
	return resp.ID, nil
}

// StartAgent starts a created container.
func (d *DockerClient) StartAgent(ctx context.Context, id string) error {
	return d.cli.ContainerStart(ctx, id, container.StartOptions{})
}

// StopAgent stops the container with a graceful SIGTERM grace period.
func (d *DockerClient) StopAgent(ctx context.Context, id string, grace time.Duration) error {
	secs := int(grace.Seconds())
	return d.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &secs})
}

// DeleteAgent removes a container (stopping it first if running).
func (d *DockerClient) DeleteAgent(ctx context.Context, id string) error {
	return d.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: false})
}

// InspectStatus returns the container's current status string, or an error
// if it no longer exists.
func (d *DockerClient) InspectStatus(ctx context.Context, id string) (string, error) {
	info, err := d.cli.ContainerInspect(ctx, id)
	if err != nil {
		return "", err
	}
	if info.State == nil {
		return "unknown", nil
	}
	switch {
	case info.State.Running:
		return "running", nil
	case info.State.Paused:
		return "paused", nil
	case info.State.Restarting:
		return "restarting", nil
	case info.State.Dead:
		return "dead", nil
	case info.State.OOMKilled:
		return "oomkilled", nil
	case info.State.ExitCode != 0:
		return "exited", nil
	case info.State.Status == "created":
		return "created", nil
	default:
		return info.State.Status, nil
	}
}

// LogsTail returns the last N combined stdout+stderr lines, newest-last.
func (d *DockerClient) LogsTail(ctx context.Context, id string, tail int) ([]string, error) {
	rc, err := d.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", tail),
		Timestamps: true,
	})
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var buf bytes.Buffer
	// Demux the multiplexed stdout/stderr stream.
	if _, err := stdcopy.StdCopy(&buf, &buf, rc); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	return lines, nil
}

// LogsFollow returns a channel that streams log lines until ctx is cancelled
// or the container exits. The second channel yields a terminal error (or is
// closed cleanly on EOF).
func (d *DockerClient) LogsFollow(ctx context.Context, id string) (<-chan string, <-chan error) {
	lines := make(chan string, 128)
	errs := make(chan error, 1)
	go func() {
		defer close(lines)
		defer close(errs)
		rc, err := d.cli.ContainerLogs(ctx, id, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
			Timestamps: true,
			Tail:       "all",
		})
		if err != nil {
			errs <- err
			return
		}
		defer rc.Close()

		// Demux asynchronously via a pipe + scanner.
		pr, pw := io.Pipe()
		go func() {
			_, _ = stdcopy.StdCopy(pw, pw, rc)
			pw.Close()
		}()
		scan := bufio.NewScanner(pr)
		for scan.Scan() {
			select {
			case <-ctx.Done():
				return
			case lines <- scan.Text():
			}
		}
		if err := scan.Err(); err != nil {
			errs <- err
		}
	}()
	return lines, errs
}

// ExecIn runs argv inside the container and returns buffered stdout, stderr,
// and exit code.
func (d *DockerClient) ExecIn(ctx context.Context, id string, argv []string) (string, string, int, error) {
	ex, err := d.cli.ContainerExecCreate(ctx, id, types.ExecConfig{
		Cmd:          argv,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", "", 1, err
	}
	att, err := d.cli.ContainerExecAttach(ctx, ex.ID, types.ExecStartCheck{})
	if err != nil {
		return "", "", 1, err
	}
	defer att.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, att.Reader); err != nil && !errors.Is(err, io.EOF) {
		return stdout.String(), stderr.String(), 1, err
	}

	ins, err := d.cli.ContainerExecInspect(ctx, ex.ID)
	if err != nil {
		return stdout.String(), stderr.String(), 1, err
	}
	return stdout.String(), stderr.String(), ins.ExitCode, nil
}
```

### 7.14 `internal/client/rpc.go`

```go
// rpc.go is the real implementation of client.Client for v0.3+. It opens a
// Unix-domain-socket connection to dclawd, performs the JSON-RPC handshake,
// and exposes method wrappers that match the Client interface.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

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
		Name:      a.Name,
		Image:     a.Image,
		Workspace: a.Workspace,
		Env:       mapToKVList(a.Env),
		Labels:    mapToKVList(a.Labels),
		Channel:   a.Channel,
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
		Name:      a.Name,
		Image:     a.Image,
		Channel:   "",
		Workspace: a.Workspace,
		Env:       a.Env,
		Labels:    a.Labels,
		Status:    a.Status,
	}
}
```

Add to `go.mod` the two extra deps referenced above:

```
require (
	github.com/google/uuid v1.6.0
)
```

Add `import "io"` and `import "time"` at the top.

### 7.15 `cmd/dclaw/main.go` (modified)

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

	// Bare invocation (`dclaw` alone, no subcommand, interactive TTY) =
	// launch TUI. Everything else flows through cobra.
	if shouldLaunchTUI(os.Args, os.Stdin, os.Stdout) {
		if err := tui.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "dclaw tui: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

// shouldLaunchTUI returns true only for the literal bare invocation on an
// interactive terminal. Any args, any flags, any non-TTY stdio falls through
// to cobra.
func shouldLaunchTUI(argv []string, stdin, stdout *os.File) bool {
	if len(argv) != 1 {
		return false
	}
	if !isatty.IsTerminal(stdin.Fd()) || !isatty.IsTerminal(stdout.Fd()) {
		return false
	}
	return true
}
```

### 7.16 `internal/cli/root.go` (modified)

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
)

var (
	outputFormat string
	daemonSocket string
	verbose      bool
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

// defaultSocketPath resolves the default daemon socket path at process start.
// Mirrors daemon.DefaultSocketPath so CLI and daemon agree.
func defaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/dclaw.sock"
	}
	return daemon.DefaultSocketPath(filepath.Join(home, ".dclaw"))
}

// newClient constructs an RPCClient at the resolved socket path. If the
// daemon isn't listening, Dial will return an error that the caller can map
// to exit 69.
func newClient(ctx context.Context) (*client.RPCClient, error) {
	c := client.NewRPCClient(daemonSocket)
	if err := c.Dial(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// withClient is a helper: opens a client, runs fn, closes the client.
func withClient(ctx context.Context, fn func(c *client.RPCClient) error) error {
	c, err := newClient(ctx)
	if err != nil {
		return DaemonUnreachable(err)
	}
	defer c.Close()
	return fn(c)
}
```

### 7.17 `internal/cli/exit.go` (modified)

```go
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes (see Section 4 of phase-3-daemon-plan.md).
const (
	ExitOK              = 0
	ExitGeneric         = 1
	ExitUsage           = 2
	ExitInputErr        = 64
	ExitDataErr         = 65
	ExitDaemonDown      = 69 // reused from v0.2.0-cli
	ExitInternal        = 70
	ExitTempFail        = 75
	ExitNoPerm          = 77
)

// DaemonUnreachable converts a low-level dial error into a CLI-facing error
// with a standardized message and the daemon-down exit code.
func DaemonUnreachable(err error) error {
	msg := "dclaw daemon is not running; run 'dclaw daemon start'"
	if err != nil {
		msg = fmt.Sprintf("%s (%v)", msg, err)
	}
	if outputFormat == "json" {
		payload := map[string]any{
			"error":     "daemon_unreachable",
			"message":   msg,
			"exit_code": ExitDaemonDown,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
	os.Exit(ExitDaemonDown)
	return nil
}

// HandleRPCError formats an RPC error for the CLI. Returns a cobra-friendly
// error after calling os.Exit internally for fatal cases.
func HandleRPCError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	// A bare "connection refused" means the daemon died mid-operation.
	var nerr *net.OpError
	if errors.As(err, &nerr) {
		return DaemonUnreachable(err)
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
	os.Exit(ExitInternal)
	return nil
}

// Legacy helpers kept for any v0.2.0-cli stubs still in place during the
// transition. After alpha.1 completes these should have no live callers.
const NextMilestone = "v0.3.0-daemon"
const ExitCodeNeedsDaemon = ExitDaemonDown

type NotReadyPayload struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	ExitCode  int    `json:"exit_code"`
	Milestone string `json:"milestone"`
	Command   string `json:"command"`
}
```

### 7.18 `internal/cli/output.go` (NEW)

```go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/itsmehatef/dclaw/internal/client"
)

// PrintAgents renders a list of agents in the currently selected output
// format (table, json, yaml). Called by `dclaw agent list`.
func PrintAgents(w io.Writer, agents []client.Agent) error {
	switch outputFormat {
	case "json":
		return encodeJSON(w, agents)
	case "yaml":
		return encodeYAML(w, agents)
	default:
		return encodeAgentTable(w, agents)
	}
}

// PrintAgent renders a single agent.
func PrintAgent(w io.Writer, a client.Agent) error {
	switch outputFormat {
	case "json":
		return encodeJSON(w, a)
	case "yaml":
		return encodeYAML(w, a)
	default:
		return encodeAgentTable(w, []client.Agent{a})
	}
}

// PrintChannels renders a list of channels.
func PrintChannels(w io.Writer, channels []client.Channel) error {
	switch outputFormat {
	case "json":
		return encodeJSON(w, channels)
	case "yaml":
		return encodeYAML(w, channels)
	default:
		return encodeChannelTable(w, channels)
	}
}

// PrintStatus renders a fleet status summary (string from DaemonStatus for
// now; structured upgrade in beta.1).
func PrintStatus(w io.Writer, status string) error {
	switch outputFormat {
	case "json":
		return encodeJSON(w, map[string]string{"status": status})
	case "yaml":
		return encodeYAML(w, map[string]string{"status": status})
	default:
		_, err := fmt.Fprintln(w, status)
		return err
	}
}

// ----- format-specific helpers -----

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func encodeYAML(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(v)
}

func encodeAgentTable(w io.Writer, agents []client.Agent) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, "NAME\tIMAGE\tSTATUS\tWORKSPACE")
	for _, a := range agents {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Name, a.Image, a.Status, a.Workspace)
	}
	return nil
}

func encodeChannelTable(w io.Writer, channels []client.Channel) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, "NAME\tTYPE\tCONFIG")
	for _, c := range channels {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, c.Type, c.Config)
	}
	return nil
}

// humanTime renders a unix-seconds timestamp as a relative duration when recent
// or a short ISO date when older. Used by the TUI.
func humanTime(unix int64) string {
	if unix == 0 {
		return "-"
	}
	t := time.Unix(unix, 0)
	age := time.Since(t)
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return t.Format("2006-01-02")
	}
}

// Kept explicit to silence the linter on unused helpers during staged dev.
var _ = os.Stdout
```

### 7.19 `internal/cli/agent.go` (modified)

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents (create, list, start, stop, ...)",
	Long:  `Manage dclaw agents.`,
}

// ---------- create ----------

var (
	agentCreateImage     string
	agentCreateChannel   string
	agentCreateWorkspace string
	agentCreateEnv       []string
	agentCreateLabel     []string
)

var agentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			a := client.Agent{
				Name:      args[0],
				Image:     agentCreateImage,
				Channel:   agentCreateChannel,
				Workspace: agentCreateWorkspace,
				Env:       kvSliceToMap(agentCreateEnv),
				Labels:    kvSliceToMap(agentCreateLabel),
			}
			if err := c.AgentCreate(ctx, a); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s created\n", args[0])
			return nil
		})
	},
}

// ---------- list ----------

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agents",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			agents, err := c.AgentList(ctx)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintAgents(cmd.OutOrStdout(), agents)
		})
	},
}

// ---------- get ----------

var agentGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get a single agent by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			a, err := c.AgentGet(ctx, args[0])
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintAgent(cmd.OutOrStdout(), a)
		})
	},
}

// ---------- describe ----------

var agentDescribeCmd = &cobra.Command{
	Use:   "describe <name>",
	Short: "Describe an agent in human-readable form",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			a, err := c.AgentGet(ctx, args[0])
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Name:      %s\n", a.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Image:     %s\n", a.Image)
			fmt.Fprintf(cmd.OutOrStdout(), "Status:    %s\n", a.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", a.Workspace)
			if len(a.Env) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Env:")
				for k, v := range a.Env {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s\n", k, v)
				}
			}
			if len(a.Labels) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Labels:")
				for k, v := range a.Labels {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s\n", k, v)
				}
			}
			return nil
		})
	},
}

// ---------- update ----------

var (
	agentUpdateImage string
	agentUpdateEnv   []string
	agentUpdateLabel []string
)

var agentUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an agent's image, env, or labels",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentUpdateImage == "" && len(agentUpdateEnv) == 0 && len(agentUpdateLabel) == 0 {
			return fmt.Errorf("at least one of --image, --env, --label must be provided")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			a := client.Agent{
				Name:   args[0],
				Image:  agentUpdateImage,
				Env:    kvSliceToMap(agentUpdateEnv),
				Labels: kvSliceToMap(agentUpdateLabel),
			}
			if err := c.AgentUpdate(ctx, a); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s updated\n", args[0])
			return nil
		})
	},
}

// ---------- delete ----------

var agentDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete an agent",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.AgentDelete(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s deleted\n", args[0])
			return nil
		})
	},
}

// ---------- start / stop / restart ----------

var agentStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.AgentStart(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s started\n", args[0])
			return nil
		})
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.AgentStop(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s stopped\n", args[0])
			return nil
		})
	},
}

var agentRestartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.AgentRestart(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent %s restarted\n", args[0])
			return nil
		})
	},
}

// ---------- logs ----------

var (
	agentLogsFollow bool
	agentLogsTail   int
)

var agentLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Show logs for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			ch, err := c.AgentLogs(ctx, args[0], agentLogsTail, agentLogsFollow)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			for line := range ch {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		})
	},
}

// ---------- exec ----------

var agentExecCmd = &cobra.Command{
	Use:                   "exec <name> -- <cmd>...",
	Short:                 "Exec a command inside an agent container",
	Args:                  cobra.MinimumNArgs(1),
	DisableFlagsInUseLine: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dash := cmd.ArgsLenAtDash()
		if dash < 0 || dash >= len(args) {
			return fmt.Errorf("usage: dclaw agent exec <name> -- <cmd>...")
		}
		name := args[0]
		argv := args[dash:]

		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			// Wire stdio sinks.
			client.ExecStdoutWriter = cmd.OutOrStdout()
			client.ExecStderrWriter = cmd.ErrOrStderr()
			code, err := c.AgentExec(ctx, name, argv)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			os.Exit(code)
			return nil
		})
	},
}

// ---------- attach (NEW) ----------
// See internal/cli/agent_attach.go.

// ---------- init ----------

func init() {
	agentCreateCmd.Flags().StringVar(&agentCreateImage, "image", "", "container image for the agent (required)")
	agentCreateCmd.Flags().StringVar(&agentCreateChannel, "channel", "", "channel to bind to")
	agentCreateCmd.Flags().StringVar(&agentCreateWorkspace, "workspace", "", "host path to bind as /workspace")
	agentCreateCmd.Flags().StringArrayVar(&agentCreateEnv, "env", nil, "set env var KEY=VAL (repeatable)")
	agentCreateCmd.Flags().StringArrayVar(&agentCreateLabel, "label", nil, "set label KEY=VAL (repeatable)")
	_ = agentCreateCmd.MarkFlagRequired("image")

	agentUpdateCmd.Flags().StringVar(&agentUpdateImage, "image", "", "new container image")
	agentUpdateCmd.Flags().StringArrayVar(&agentUpdateEnv, "env", nil, "set env var KEY=VAL (repeatable)")
	agentUpdateCmd.Flags().StringArrayVar(&agentUpdateLabel, "label", nil, "set label KEY=VAL (repeatable)")

	agentLogsCmd.Flags().BoolVarP(&agentLogsFollow, "follow", "f", false, "stream new log output")
	agentLogsCmd.Flags().IntVar(&agentLogsTail, "tail", 100, "number of lines to show from the end of the logs")

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
		agentAttachCmd,
	)
}

func kvSliceToMap(items []string) map[string]string {
	out := make(map[string]string, len(items))
	for _, kv := range items {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}
```

### 7.20 `internal/cli/agent_attach.go` (NEW)

```go
package cli

import (
	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/tui"
)

var agentAttachCmd = &cobra.Command{
	Use:   "attach <name>",
	Short: "Attach to an agent in the TUI chat view",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.RunAttached(args[0])
	},
}
```

### 7.21 `internal/cli/channel.go` (modified)

```go
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Manage channels and bindings to agents",
	Long: `Manage dclaw channels.

NOTE: in v0.3.0 channel commands persist records in the daemon database but
do NOT route messages. Real message routing lands in v0.4+ (Discord plugin).`,
}

var (
	channelCreateType   string
	channelCreateConfig string
)

var channelCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if channelCreateType == "" {
			return fmt.Errorf("--type is required")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			ch := client.Channel{Name: args[0], Type: channelCreateType, Config: channelCreateConfig}
			if err := c.ChannelCreate(ctx, ch); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "channel %s created (record only; no routing)\n", args[0])
			return nil
		})
	},
}

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			chs, err := c.ChannelList(ctx)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintChannels(cmd.OutOrStdout(), chs)
		})
	},
}

var channelGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get a single channel by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			ch, err := c.ChannelGet(ctx, args[0])
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintChannels(cmd.OutOrStdout(), []client.Channel{ch})
		})
	},
}

var channelDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.ChannelDelete(ctx, args[0]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "channel %s deleted\n", args[0])
			return nil
		})
	},
}

var channelAttachCmd = &cobra.Command{
	Use:   "attach <agent-name> <channel-name>",
	Short: "Attach a channel to an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.ChannelAttach(ctx, args[0], args[1]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "channel %s attached to %s\n", args[1], args[0])
			return nil
		})
	},
}

var channelDetachCmd = &cobra.Command{
	Use:   "detach <agent-name> <channel-name>",
	Short: "Detach a channel from an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			if err := c.ChannelDetach(ctx, args[0], args[1]); err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "channel %s detached from %s\n", args[1], args[0])
			return nil
		})
	},
}

func init() {
	channelCreateCmd.Flags().StringVar(&channelCreateType, "type", "", "channel type: discord, slack, cli (required)")
	channelCreateCmd.Flags().StringVar(&channelCreateConfig, "config", "", "path to channel config file")
	_ = channelCreateCmd.MarkFlagRequired("type")

	channelCmd.AddCommand(
		channelCreateCmd,
		channelListCmd,
		channelGetCmd,
		channelDeleteCmd,
		channelAttachCmd,
		channelDetachCmd,
	)
}
```

### 7.22 `internal/cli/daemon.go` (modified)

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the dclaw daemon (control plane)",
	Long:  `Start, stop, and inspect the dclawd daemon.`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the dclaw daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		cfg, err := daemon.LoadConfig(daemonSocket, filepath.Join(home, ".dclaw"), "info")
		if err != nil {
			return err
		}

		// Already running?
		if pid, _ := cfg.ReadPIDFile(); pid > 0 {
			if err := syscall.Kill(pid, 0); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "daemon already running (pid %d)\n", pid)
				return nil
			}
		}

		// Locate the dclawd binary: prefer the one next to the dclaw binary.
		dclawdPath, err := locateDclawd()
		if err != nil {
			return err
		}

		daemonProc := exec.Command(dclawdPath,
			"--socket", cfg.SocketPath,
			"--state-dir", cfg.StateDir,
		)
		// Detach: new process group, discard stdio.
		daemonProc.Stdin = nil
		daemonProc.Stdout, _ = os.OpenFile(cfg.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		daemonProc.Stderr = daemonProc.Stdout
		daemonProc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := daemonProc.Start(); err != nil {
			return fmt.Errorf("fork dclawd: %w", err)
		}
		// Don't wait; detach.
		_ = daemonProc.Process.Release()

		// Poll until the socket is reachable (up to 5s).
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(cfg.SocketPath); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "dclaw daemon started (pid %d, socket %s)\n",
					daemonProc.Process.Pid, cfg.SocketPath)
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return errors.New("daemon did not become ready within 5s (check ~/.dclaw/logs/daemon.log)")
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the dclaw daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		cfg, err := daemon.LoadConfig(daemonSocket, filepath.Join(home, ".dclaw"), "info")
		if err != nil {
			return err
		}
		pid, err := cfg.ReadPIDFile()
		if err != nil || pid == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "daemon not running")
			return nil
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("kill pid %d: %w", pid, err)
		}
		// Wait up to 10s for exit.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if err := syscall.Kill(pid, 0); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "dclaw daemon stopped (pid %d)\n", pid)
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		return fmt.Errorf("daemon did not exit in 10s; sent SIGKILL")
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the dclaw daemon status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			s, err := c.DaemonStatus(ctx)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), s)
			return nil
		})
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd, daemonStopCmd, daemonStatusCmd)
}

// locateDclawd returns the path to the dclawd binary. Search order:
//   1. $DCLAWD_BIN
//   2. a sibling named "dclawd" next to the current dclaw binary
//   3. PATH lookup
func locateDclawd() (string, error) {
	if env := os.Getenv("DCLAWD_BIN"); env != "" {
		return env, nil
	}
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "dclawd")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return exec.LookPath("dclawd")
}
```

### 7.23 `internal/cli/status.go` (modified)

```go
package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/itsmehatef/dclaw/internal/client"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon health and fleet overview",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFormat(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		return withClient(ctx, func(c *client.RPCClient) error {
			s, err := c.DaemonStatus(ctx)
			if err != nil {
				return HandleRPCError(cmd, err)
			}
			return PrintStatus(cmd.OutOrStdout(), s)
		})
	},
}
```


### 7.24 `internal/tui/app.go` (NEW)

```go
// Package tui is the bubbletea root: single Model, dispatched messages, view
// routing. Entry points: Run() (bare-dclaw launch) and RunAttached(name)
// (dclaw agent attach).
package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsmehatef/dclaw/internal/client"
	"github.com/itsmehatef/dclaw/internal/tui/views"
)

// MouseEnabled is set by cmd/dclaw/main.go based on the `--no-mouse` CLI flag
// (defaults to true; false disables `tea.WithMouseCellMotion()`). Exposed as
// a package var so both Run() and RunAttached() pick it up. A future
// `~/.dclaw/config.toml` will feed into this via the flag plumbing; for v0.3
// the flag is the only control surface.
var MouseEnabled = true

// Run launches the TUI on the default view (agent list).
func Run() error {
	return run(nil)
}

// RunAttached launches the TUI pre-focused on the chat view for the given
// agent.
func RunAttached(agentName string) error {
	return run(&attachTarget{Agent: agentName, View: views.ViewChat})
}

type attachTarget struct {
	Agent string
	View  views.View
}

// run is the shared implementation. It constructs the root Model, the
// bubbletea program, and returns the program's run error.
func run(target *attachTarget) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Default socket path from the CLI flag plumbing. In bare TUI launch, we
	// re-resolve here because cobra hasn't parsed flags.
	socket := resolveSocketPath()
	rpc := client.NewRPCClient(socket)
	if err := rpc.Dial(ctx); err != nil {
		return fmt.Errorf("cannot reach dclaw daemon at %s: %w\n\nrun 'dclaw daemon start' first", socket, err)
	}

	m := NewModel(ctx, rpc, target)

	// Build bubbletea options conditionally. Mouse support is on by default;
	// users can disable it via `dclaw --no-mouse` when their terminal (e.g.
	// stock macOS Terminal.app) mishandles mouse events.
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if MouseEnabled {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(m, opts...)
	_, err := p.Run()
	return err
}

// Model is the root bubbletea.Model for the dclaw TUI.
type Model struct {
	ctx    context.Context
	rpc    *client.RPCClient
	now    time.Time

	// View state
	current views.View
	list    views.ListModel
	detail  views.DetailModel
	chat    views.ChatModel
	logs    views.LogsModel
	desc    views.DescribeModel
	help    views.HelpModel

	// Chrome
	topBar    string
	bottomBar string
	width     int
	height    int

	// Selection
	selected string

	// Command mode
	cmdMode bool
	cmdBuf  string

	// Error toast
	toast    string
	toastTTL time.Time
}

// NewModel builds the initial Model.
func NewModel(ctx context.Context, rpc *client.RPCClient, target *attachTarget) *Model {
	m := &Model{
		ctx:     ctx,
		rpc:     rpc,
		current: views.ViewList,
		list:    views.NewListModel(),
		detail:  views.NewDetailModel(),
		chat:    views.NewChatModel(),
		logs:    views.NewLogsModel(),
		desc:    views.NewDescribeModel(),
		help:    views.NewHelpModel(),
	}
	if target != nil {
		m.selected = target.Agent
		m.current = target.View
	}
	return m
}

// Init kicks off the initial fetch.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		fetchAgents(m.ctx, m.rpc),
		tickStatus(),
	)
}

// Update is the main message dispatch.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case agentsFetchedMsg:
		m.list.SetAgents(msg.agents)
		m.topBar = fmt.Sprintf("dclawd OK  %d agents, %d running", len(msg.agents), countRunning(msg.agents))
		return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return refreshMsg(t) })
	case refreshMsg:
		return m, fetchAgents(m.ctx, m.rpc)
	case errorMsg:
		m.toast = string(msg)
		m.toastTTL = time.Now().Add(5 * time.Second)
		return m, nil
	}
	return m, nil
}

// View composes top bar + main pane + bottom bar.
func (m *Model) View() string {
	if m.help.Active() {
		return m.help.View()
	}
	var main string
	switch m.current {
	case views.ViewList:
		main = m.list.View(m.width, m.height-2)
	case views.ViewDetail:
		main = m.detail.View(m.width, m.height-2, m.selected)
	case views.ViewChat:
		main = m.chat.View(m.width, m.height-2, m.selected)
	case views.ViewLogs:
		main = m.logs.View(m.width, m.height-2, m.selected)
	case views.ViewDescribe:
		main = m.desc.View(m.width, m.height-2, m.selected)
	}
	return m.topBar + "\n" + main + "\n" + m.bottomBar
}

// handleKey dispatches KeyMsg across the keymap + per-view key handlers.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.cmdMode {
		return m.handleCmdKey(msg)
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help.Toggle()
		return m, nil
	case ":":
		m.cmdMode = true
		m.cmdBuf = ""
		return m, nil
	case "j", "down":
		m.list.Down()
		return m, nil
	case "k", "up":
		m.list.Up()
		return m, nil
	case "enter":
		if m.current == views.ViewList {
			m.selected = m.list.SelectedName()
			m.current = views.ViewDetail
		}
		return m, nil
	case "c":
		if m.selected != "" {
			m.current = views.ViewChat
		}
		return m, nil
	case "l":
		if m.selected != "" {
			m.current = views.ViewLogs
		}
		return m, nil
	case "d":
		if m.selected != "" {
			m.current = views.ViewDescribe
		}
		return m, nil
	case "esc":
		m.current = views.ViewList
		return m, nil
	}
	return m, nil
}

func (m *Model) handleCmdKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.cmdMode = false
		return m.runCmd(m.cmdBuf)
	case "esc":
		m.cmdMode = false
		m.cmdBuf = ""
		return m, nil
	case "backspace":
		if len(m.cmdBuf) > 0 {
			m.cmdBuf = m.cmdBuf[:len(m.cmdBuf)-1]
		}
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.cmdBuf += string(msg.Runes)
		}
		return m, nil
	}
}

func (m *Model) runCmd(cmd string) (tea.Model, tea.Cmd) {
	switch cmd {
	case "q", "quit":
		return m, tea.Quit
	case "help":
		m.help.Toggle()
		return m, nil
	case "refresh":
		return m, fetchAgents(m.ctx, m.rpc)
	}
	m.toast = fmt.Sprintf("unknown command: %s", cmd)
	m.toastTTL = time.Now().Add(3 * time.Second)
	return m, nil
}

// ---------- messages ----------

type agentsFetchedMsg struct{ agents []client.Agent }
type refreshMsg time.Time
type errorMsg string

func fetchAgents(ctx context.Context, rpc *client.RPCClient) tea.Cmd {
	return func() tea.Msg {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		items, err := rpc.AgentList(cctx)
		if err != nil {
			return errorMsg(err.Error())
		}
		return agentsFetchedMsg{agents: items}
	}
}

func tickStatus() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return refreshMsg(t) })
}

func countRunning(agents []client.Agent) int {
	n := 0
	for _, a := range agents {
		if a.Status == "running" {
			n++
		}
	}
	return n
}

// resolveSocketPath mirrors defaultSocketPath() in internal/cli/root.go. We
// duplicate it here to avoid circular imports.
func resolveSocketPath() string {
	// The CLI sets this via flag; if we're being called from bare-TUI launch,
	// use the OS default.
	return client.DefaultSocketPath()
}
```

Note: the `DefaultSocketPath()` helper on the client package is a wrapper over `daemon.DefaultSocketPath` — add a tiny indirection at the top of `internal/client/rpc.go`:

```go
// DefaultSocketPath returns the resolved socket path for this host.
func DefaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/dclaw.sock"
	}
	return daemon.DefaultSocketPath(filepath.Join(home, ".dclaw"))
}
```

(Import `os`, `path/filepath`, and `github.com/itsmehatef/dclaw/internal/daemon` in `rpc.go`.)

### 7.25 `internal/tui/keys.go` (NEW)

```go
package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap is the centralized keymap for the TUI. Per-view overrides extend
// this via embedding.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Chat     key.Binding
	Logs     key.Binding
	Describe key.Binding
	Cmd      key.Binding
	Help     key.Binding
	Quit     key.Binding
}

// DefaultKeys returns the shared global keymap.
func DefaultKeys() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Chat:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "chat")),
		Logs:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
		Describe: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "describe")),
		Cmd:      key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "cmd")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}
```

### 7.26 `internal/tui/styles.go` (NEW)

```go
package tui

import "github.com/charmbracelet/lipgloss"

// Shared lipgloss styles. Exported for cross-view use.
var (
	TopBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1).
			Bold(true)

	BottomBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("240")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1)

	ListHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	SelectedRowStyle = lipgloss.NewStyle().
			Reverse(true).
			Bold(true)

	DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	ToastStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("196")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1)
)
```

### 7.27 `internal/tui/views/list.go` (NEW)

```go
package views

import (
	"fmt"
	"strings"

	"github.com/itsmehatef/dclaw/internal/client"
)

// ListModel is the left-pane agent list.
type ListModel struct {
	items    []client.Agent
	cursor   int
}

// NewListModel returns an empty list.
func NewListModel() ListModel { return ListModel{} }

// SetAgents replaces the backing slice.
func (m *ListModel) SetAgents(items []client.Agent) {
	m.items = items
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Up / Down moves the cursor.
func (m *ListModel) Up() {
	if m.cursor > 0 {
		m.cursor--
	}
}
func (m *ListModel) Down() {
	if m.cursor < len(m.items)-1 {
		m.cursor++
	}
}

// SelectedName returns the currently highlighted agent's name (empty if list
// is empty).
func (m *ListModel) SelectedName() string {
	if len(m.items) == 0 {
		return ""
	}
	return m.items[m.cursor].Name
}

// View renders the list into width x height.
func (m *ListModel) View(width, height int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-20s %-24s %-10s\n", "NAME", "IMAGE", "STATUS"))
	b.WriteString(strings.Repeat("-", width) + "\n")
	for i, a := range m.items {
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		b.WriteString(fmt.Sprintf("%s%-18s %-24s %-10s\n", marker, a.Name, a.Image, a.Status))
	}
	return b.String()
}
```

### 7.28 `internal/tui/views/detail.go` (NEW)

```go
package views

import (
	"fmt"
	"strings"

	"github.com/itsmehatef/dclaw/internal/client"
)

// DetailModel is the right-pane single-agent detail view.
type DetailModel struct {
	agent client.Agent
}

// NewDetailModel returns an empty detail view.
func NewDetailModel() DetailModel { return DetailModel{} }

// SetAgent replaces the backing record.
func (m *DetailModel) SetAgent(a client.Agent) { m.agent = a }

// View renders the detail view.
func (m *DetailModel) View(width, height int, name string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== agent: %s ===\n", name))
	b.WriteString(fmt.Sprintf("  image:     %s\n", m.agent.Image))
	b.WriteString(fmt.Sprintf("  status:    %s\n", m.agent.Status))
	b.WriteString(fmt.Sprintf("  workspace: %s\n", m.agent.Workspace))
	if len(m.agent.Env) > 0 {
		b.WriteString("  env:\n")
		for k, v := range m.agent.Env {
			b.WriteString(fmt.Sprintf("    %s=%s\n", k, v))
		}
	}
	if len(m.agent.Labels) > 0 {
		b.WriteString("  labels:\n")
		for k, v := range m.agent.Labels {
			b.WriteString(fmt.Sprintf("    %s=%s\n", k, v))
		}
	}
	return b.String()
}
```

### 7.29 `internal/tui/views/chat.go` (alpha.3)

```go
package views

import (
	"fmt"
	"strings"
	"time"
)

// ChatModel renders the chat turn log and input buffer for a single agent.
// alpha.3 ships a local-echo-only skeleton that stores messages in-memory
// and posts them via `agent.exec` as a stopgap until worker-agent messaging
// arrives in v0.4. beta.1 wires in a real websocket-style stream.
type ChatModel struct {
	turns     []chatTurn
	input     string
	lastError string
}

type chatTurn struct {
	Role string // "user" or "agent"
	Text string
	Time time.Time
}

// NewChatModel returns an empty chat model.
func NewChatModel() ChatModel { return ChatModel{} }

// Append records a new turn.
func (m *ChatModel) Append(role, text string) {
	m.turns = append(m.turns, chatTurn{Role: role, Text: text, Time: time.Now()})
}

// SetInput replaces the input buffer.
func (m *ChatModel) SetInput(s string) { m.input = s }

// AppendInput appends to the input buffer (called on every keystroke).
func (m *ChatModel) AppendInput(s string) { m.input += s }

// ClearInput wipes the input buffer.
func (m *ChatModel) ClearInput() string {
	out := m.input
	m.input = ""
	return out
}

// SetError stashes a per-view error string for rendering.
func (m *ChatModel) SetError(err string) { m.lastError = err }

// View renders the chat log.
func (m *ChatModel) View(width, height int, agentName string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== chat: %s ===\n", agentName))
	for _, t := range m.turns {
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n", t.Time.Format("15:04:05"), t.Role, t.Text))
	}
	if m.lastError != "" {
		b.WriteString("\n! " + m.lastError + "\n")
	}
	b.WriteString("\n> " + m.input)
	return b.String()
}
```

### 7.30 `internal/tui/views/logs.go` (beta.1 skeleton)

```go
package views

import (
	"fmt"
	"strings"
)

// LogsModel renders a scrollable view over container logs. alpha.2 ships a
// static render of the last N lines fetched via bulk; beta.1 replaces the
// backend with the LogStreamer notification stream.
type LogsModel struct {
	lines  []string
	offset int
}

// NewLogsModel returns an empty logs model.
func NewLogsModel() LogsModel { return LogsModel{} }

// SetLines replaces the backing slice (bulk fetch).
func (m *LogsModel) SetLines(lines []string) { m.lines = lines }

// Append adds a new line at the end (streaming).
func (m *LogsModel) Append(line string) { m.lines = append(m.lines, line) }

// ScrollUp / ScrollDown moves the view.
func (m *LogsModel) ScrollUp()   { if m.offset > 0 { m.offset-- } }
func (m *LogsModel) ScrollDown() { m.offset++ }

// View renders the logs pane.
func (m *LogsModel) View(width, height int, agentName string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== logs: %s ===\n", agentName))
	start := m.offset
	if start >= len(m.lines) {
		start = len(m.lines)
	}
	end := start + height - 2
	if end > len(m.lines) {
		end = len(m.lines)
	}
	for _, l := range m.lines[start:end] {
		b.WriteString(l + "\n")
	}
	return b.String()
}
```

### 7.31 `internal/tui/views/describe.go` (beta.1)

```go
package views

import (
	"fmt"
	"strings"

	"github.com/itsmehatef/dclaw/internal/client"
)

// DescribeModel renders a kubectl-describe-style verbose view.
type DescribeModel struct {
	agent  client.Agent
	events []string // preformatted event rows; beta.1 replaces with structured events
}

// NewDescribeModel returns an empty describe model.
func NewDescribeModel() DescribeModel { return DescribeModel{} }

// SetAgent replaces the backing record.
func (m *DescribeModel) SetAgent(a client.Agent) { m.agent = a }

// SetEvents replaces the preformatted event rows.
func (m *DescribeModel) SetEvents(events []string) { m.events = events }

// View renders the describe view.
func (m *DescribeModel) View(width, height int, name string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== describe: %s ===\n\n", name))
	b.WriteString(fmt.Sprintf("  image:     %s\n", m.agent.Image))
	b.WriteString(fmt.Sprintf("  status:    %s\n", m.agent.Status))
	b.WriteString(fmt.Sprintf("  workspace: %s\n\n", m.agent.Workspace))
	b.WriteString("Events:\n")
	for _, e := range m.events {
		b.WriteString("  " + e + "\n")
	}
	return b.String()
}
```

### 7.32 `internal/tui/views/help.go` (beta.1)

```go
package views

import "strings"

// HelpModel toggles a full-screen help overlay.
type HelpModel struct {
	active bool
}

// NewHelpModel returns a help model in the inactive state.
func NewHelpModel() HelpModel { return HelpModel{} }

// Toggle flips visibility.
func (m *HelpModel) Toggle() { m.active = !m.active }

// Active reports whether the overlay is open.
func (m *HelpModel) Active() bool { return m.active }

// View renders the overlay contents.
func (m *HelpModel) View() string {
	var b strings.Builder
	b.WriteString("dclaw TUI help — press ? to close\n\n")
	b.WriteString("Navigation:\n")
	b.WriteString("  j/k or up/down  select previous/next agent\n")
	b.WriteString("  enter           open detail view\n")
	b.WriteString("  esc             return to list\n")
	b.WriteString("\nViews:\n")
	b.WriteString("  c               chat view\n")
	b.WriteString("  l               logs view\n")
	b.WriteString("  d               describe view\n")
	b.WriteString("\nCommand mode:\n")
	b.WriteString("  :               enter command mode\n")
	b.WriteString("  :quit | :q      exit dclaw\n")
	b.WriteString("  :refresh        force refresh agent list\n")
	b.WriteString("  :help           toggle this help\n")
	b.WriteString("\nGlobal:\n")
	b.WriteString("  ?               toggle help\n")
	b.WriteString("  q, ctrl+c       quit\n")
	return b.String()
}
```

### 7.33 `internal/tui/views/view.go` (NEW — shared View enum)

```go
package views

// View identifies the current main-pane content.
type View int

const (
	ViewList View = iota
	ViewDetail
	ViewChat
	ViewLogs
	ViewDescribe
)
```

### 7.34 `Makefile` (modified)

```makefile
# dclaw Makefile — v0.3.0-daemon

BINARY_CLI  := dclaw
BINARY_D    := dclawd
PKG         := github.com/itsmehatef/dclaw
CMD_CLI     := ./cmd/dclaw
CMD_D       := ./cmd/dclawd
BIN_DIR     := ./bin

VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.3.0-dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS     := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.BuildDate=$(BUILD_DATE)

GO          ?= go
GOFLAGS     ?=

.PHONY: all build cli daemon tui test vet lint fmt install clean tidy smoke smoke-daemon smoke-tui migrate help

all: build

build: cli daemon ## Build both binaries

cli: ## Build dclaw
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY_CLI) $(CMD_CLI)

daemon: ## Build dclawd
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY_D) $(CMD_D)

tui: cli ## Convenience: build and run the TUI in dev mode
	DCLAWD_BIN=$(BIN_DIR)/$(BINARY_D) $(BIN_DIR)/$(BINARY_CLI)

test: ## Run unit tests
	$(GO) test $(GOFLAGS) ./...

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint (no-op if not installed)
	@command -v golangci-lint >/dev/null 2>&1 \
		&& golangci-lint run \
		|| echo "golangci-lint not installed; skipping"

fmt: ## Format code
	$(GO) fmt ./...
	gofmt -s -w .

install: build ## go install both binaries with version ldflags
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD_CLI)
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD_D)

tidy: ## go mod tidy
	$(GO) mod tidy

smoke: smoke-daemon smoke-tui ## Run all smoke suites

smoke-daemon: build ## Integration smoke against a real daemon + docker
	DCLAW_BIN=$(BIN_DIR)/$(BINARY_CLI) DCLAWD_BIN=$(BIN_DIR)/$(BINARY_D) \
		./scripts/smoke-daemon.sh

smoke-tui: build ## teatest-driven TUI smoke
	DCLAW_BIN=$(BIN_DIR)/$(BINARY_CLI) DCLAWD_BIN=$(BIN_DIR)/$(BINARY_D) \
		./scripts/smoke-tui.sh

migrate: build ## Apply embedded migrations to ~/.dclaw/state.db (useful in dev)
	$(BIN_DIR)/$(BINARY_D) --migrate-only

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
```

### 7.35 `scripts/smoke-daemon.sh` (NEW)

```bash
#!/usr/bin/env bash
# Phase 3 integration smoke: spin up dclawd, exercise full CRUD, tear down.
# Requires docker reachable on the host and dclaw-agent:v0.1 built (phase 1).
set -euo pipefail

DCLAW_BIN="${DCLAW_BIN:-./bin/dclaw}"
DCLAWD_BIN="${DCLAWD_BIN:-./bin/dclawd}"
STATE_DIR="${STATE_DIR:-$(mktemp -d -t dclaw-smoke-XXXX)}"
SOCKET="$STATE_DIR/dclaw.sock"

export DCLAWD_BIN
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

cleanup() {
  "$DCLAW_BIN" --daemon-socket "$SOCKET" daemon stop >/dev/null 2>&1 || true
  rm -rf "$STATE_DIR" || true
}
trap cleanup EXIT

echo "--- Test 1: daemon start ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" daemon start || fail "daemon start"
test -S "$SOCKET" || fail "socket not created"
pass "daemon start"

echo "--- Test 2: daemon status ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" daemon status | grep -q "agents=0" || fail "status lacks agents=0"
pass "daemon status"

echo "--- Test 3: agent create ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" agent create smokey \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR" || fail "create"
pass "agent create"

echo "--- Test 4: agent list shows smokey ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" agent list | grep -q smokey || fail "list missing smokey"
pass "agent list"

echo "--- Test 5: agent get smokey ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" agent get smokey -o json | grep -q '"name": *"smokey"' || fail "get json"
pass "agent get"

echo "--- Test 6: agent delete ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" agent delete smokey || fail "delete"
pass "agent delete"

echo "--- Test 7: daemon stop ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" daemon stop || fail "stop"
pass "daemon stop"

echo "All daemon smoke tests passed."
```

Make executable: `chmod +x scripts/smoke-daemon.sh`.

### 7.36 `scripts/smoke-tui.sh` (NEW)

```bash
#!/usr/bin/env bash
# Invoke the teatest-driven TUI smoke (lives in internal/tui/app_test.go).
# This script is a thin wrapper so make targets can call it uniformly.
set -euo pipefail

go test -run TestTUISmoke -v ./internal/tui/... -timeout 60s
```

Make executable: `chmod +x scripts/smoke-tui.sh`.

The actual teatest harness lives in `internal/tui/app_test.go`:

```go
package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

func TestTUISmoke(t *testing.T) {
	m := &Model{}                               // no rpc — pre-populated via injection below
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}
```

Beta.1 expands this with a mock RPC backend to exercise list -> chat -> logs navigation.

### 7.37 `.github/workflows/build.yml` (modified)

```yaml
name: build

on:
  push:
    branches: [main]
    tags: ['v*']
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
      - name: vet
        run: go vet ./...
      - name: build
        run: make build
      - name: test
        run: go test ./...
      - name: cli smoke
        run: ./scripts/smoke-cli.sh
      - name: tui smoke
        run: ./scripts/smoke-tui.sh

  docker-smoke:
    # Integration with real docker. Only run on tag push so PRs don't flake.
    if: startsWith(github.ref, 'refs/tags/v')
    runs-on: ubuntu-latest
    services:
      docker:
        image: docker:26-dind
        options: --privileged
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: build
        run: make build
      - name: build agent image
        run: bash agent/build.sh
      - name: daemon smoke
        run: ./scripts/smoke-daemon.sh
```

### 7.38 `.github/workflows/release.yml` (NEW)

```yaml
name: release

on:
  push:
    tags: ['v0.3.*']

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: build darwin/amd64
        run: GOOS=darwin GOARCH=amd64 make build
      - name: build darwin/arm64
        run: GOOS=darwin GOARCH=arm64 make build
      - name: build linux/amd64
        run: GOOS=linux GOARCH=amd64 make build
      - name: build linux/arm64
        run: GOOS=linux GOARCH=arm64 make build
      - name: archive
        run: |
          for os in darwin linux; do
            for arch in amd64 arm64; do
              tar czf dclaw-${{ github.ref_name }}-${os}-${arch}.tgz -C bin dclaw dclawd
            done
          done
      - name: release
        uses: softprops/action-gh-release@v2
        with:
          files: dclaw-*.tgz
```

---

## 8. Staging & Implementation Steps

Five sub-milestones, each with its own Definition of Done + numbered implementation order + Release Checklist. Do NOT collapse. Each alpha is a demoable slice.

### 8a. v0.3.0-alpha.1 — Daemon backbone + SQLite + Docker + CLI CRUD

**Definition of Done for alpha.1:**

```bash
dclaw daemon start   # starts dclawd in background, writes pidfile
dclaw daemon status  # talks over socket, returns agents=0 running=0 channels=0
dclaw agent create foo --image=dclaw-agent:v0.1 --workspace=/tmp/ws
dclaw agent list     # prints foo
dclaw agent start foo
dclaw agent stop foo
dclaw agent delete foo
dclaw daemon stop    # stops daemon, removes socket + pidfile
```

All above produce their real effects against real Docker. No TUI in alpha.1.

**Implementation order (alpha.1):**

1. **Update `go.mod`**: add dependencies from Section 7.1. Run `go mod tidy`. Commit.
2. **Write `internal/store/schema.go`**: copy 7.8 verbatim.
3. **Write `internal/store/migrations/0001_initial.sql`**: copy 7.9 verbatim.
4. **Write `internal/store/repo.go`**: copy 7.10 verbatim.
5. **Write `internal/store/repo_test.go`** (table-driven: insert, get, list, update, delete agent; same for channel; basic event insert). Skeleton:

   ```go
   package store

   import (
     "context"
     "os"
     "path/filepath"
     "testing"
     "time"
   )

   func openTemp(t *testing.T) *Repo {
     t.Helper()
     dir, err := os.MkdirTemp("", "store-test-*")
     if err != nil { t.Fatal(err) }
     t.Cleanup(func() { _ = os.RemoveAll(dir) })
     r, err := Open(filepath.Join(dir, "test.db"))
     if err != nil { t.Fatal(err) }
     if err := r.Migrate(context.Background()); err != nil { t.Fatal(err) }
     return r
   }

   func TestAgentCRUD(t *testing.T) {
     r := openTemp(t)
     ctx := context.Background()
     now := time.Now().Unix()
     rec := AgentRecord{ID: "id-1", Name: "foo", Image: "img:latest", Status: "created", Labels: "{}", Env: "{}", CreatedAt: now, UpdatedAt: now}
     if err := r.InsertAgent(ctx, rec); err != nil { t.Fatal(err) }
     got, err := r.GetAgent(ctx, "foo")
     if err != nil { t.Fatal(err) }
     if got.Name != "foo" { t.Fatalf("name=%q", got.Name) }
     got.Status = "running"
     got.UpdatedAt = time.Now().Unix()
     if err := r.UpdateAgent(ctx, got); err != nil { t.Fatal(err) }
     if err := r.DeleteAgent(ctx, "foo"); err != nil { t.Fatal(err) }
   }
   ```
6. **Write `internal/protocol/messages.go`**: copy 7.11.
7. **Write `internal/protocol/encoding.go`**: copy 7.12.
8. **Write `internal/sandbox/docker.go`**: copy 7.13.
9. **Write `internal/daemon/config.go`**: copy 7.3.
10. **Write `internal/daemon/lifecycle.go`**: copy 7.6.
11. **Write `internal/daemon/logs.go`**: copy 7.7 (skeleton; bulk-only path is exercised at this alpha).
12. **Write `internal/daemon/router.go`**: copy 7.5.
13. **Write `internal/daemon/server.go`**: copy 7.4.
14. **Write `cmd/dclawd/main.go`**: copy 7.2. This includes the `--migrate-only` flag (added in the 2026-04-16 review for decision Q6): the daemon runs pending SQLite migrations then exits 0, for use by `make migrate` and by operators who want to migrate before starting the daemon.
15. **First daemon compile**: `go build ./cmd/dclawd`. Fix any import mismatches.
16. **Smoke daemon boot manually**:
    ```bash
    ./bin/dclawd --socket /tmp/dc.sock --state-dir /tmp/dc &
    sleep 1
    ls -la /tmp/dc.sock
    kill %1
    ```
    Expect the socket file to exist while the daemon is running.
16a. **Smoke `--migrate-only` manually**:
    ```bash
    rm -f /tmp/dc2.db
    ./bin/dclawd --state-dir /tmp/dc2 --migrate-only
    echo $?          # expect 0
    ls -la /tmp/dc2/state.db   # migration must have created the file
    ```
    Expect exit 0 and the SQLite DB file to exist with the schema applied.
17. **Write `internal/client/rpc.go`**: copy 7.14.
18. **Write `internal/cli/exit.go`** (modified): copy 7.17.
19. **Write `internal/cli/root.go`** (modified): copy 7.16.
20. **Write `internal/cli/output.go`**: copy 7.18.
21. **Write `internal/cli/agent.go`** (modified): copy 7.19. Don't add agent_attach.go yet — that's alpha.2.
22. **Write `internal/cli/channel.go`** (modified): copy 7.21.
23. **Write `internal/cli/daemon.go`** (modified): copy 7.22.
24. **Write `internal/cli/status.go`** (modified): copy 7.23.
25. **Write `Makefile`** (modified): copy 7.34 — but temporarily omit the `tui` target and `smoke-tui` target until alpha.2. Leave stubs:
    ```makefile
    tui: cli
    	@echo "tui ships in v0.3.0-alpha.2"
    	@exit 1
    smoke-tui: cli
    	@echo "smoke-tui ships in v0.3.0-alpha.2"
    	@exit 0
    ```
26. **First full compile**: `make build`. Expect both binaries. Fix up any import-path or duplicate-declaration errors.
27. **Write `scripts/smoke-daemon.sh`**: copy 7.35. `chmod +x scripts/smoke-daemon.sh`.
28. **Run the smoke**:
    ```bash
    make build
    ./scripts/smoke-daemon.sh
    ```
    All 7 sub-tests must pass against a Docker daemon with `dclaw-agent:v0.1` available locally.
29. **Write `internal/daemon/server_test.go`** — in-process server + RPCClient round-trip (no Docker). Skeleton:
    ```go
    package daemon

    import (
      "context"
      "log/slog"
      "os"
      "path/filepath"
      "testing"
      "time"

      "github.com/itsmehatef/dclaw/internal/client"
      "github.com/itsmehatef/dclaw/internal/store"
    )

    func TestServerPing(t *testing.T) {
      dir := t.TempDir()
      repo, err := store.Open(filepath.Join(dir, "t.db"))
      if err != nil { t.Fatal(err) }
      if err := repo.Migrate(context.Background()); err != nil { t.Fatal(err) }
      cfg := &Config{SocketPath: filepath.Join(dir, "dc.sock"), StateDir: dir, DBPath: filepath.Join(dir, "t.db")}
      log := slog.New(slog.NewTextHandler(os.Stderr, nil))
      srv := NewServer(cfg, log, repo, nil) // nil docker → handlers return ErrInternal on agent ops
      ctx, cancel := context.WithCancel(context.Background())
      defer cancel()
      go func() { _ = srv.Run(ctx) }()
      time.Sleep(200 * time.Millisecond)

      c := client.NewRPCClient(cfg.SocketPath)
      if err := c.Dial(ctx); err != nil { t.Fatal(err) }
      defer c.Close()
      if _, err := c.DaemonStatus(ctx); err != nil { t.Fatal(err) }
    }
    ```
    Note: if nil-docker crashes, add a guard check at the top of each docker-dependent handler in lifecycle.go; simpler is to inject a fake DockerClient interface. Either approach is acceptable for alpha.1; choose whichever is faster.
30. **Write `.github/workflows/build.yml`** (modified): copy 7.37 but for alpha.1 comment out the `docker-smoke` job (we'll run it locally). Replace the line `if: startsWith(github.ref, 'refs/tags/v')` with `if: false`.
31. **Update README.md** — add a "Running the daemon" section right above the Phase 2 CLI section:

    ~~~markdown
    ## Running the daemon (v0.3.0-alpha.1)

    ```bash
    make build
    ./bin/dclaw daemon start
    ./bin/dclaw agent create foo --image=dclaw-agent:v0.1 --workspace=$(pwd)
    ./bin/dclaw agent list
    ```
    ~~~
32. **Commit**:
    ```bash
    git add go.mod go.sum \
      cmd/dclawd/ \
      internal/daemon/ internal/sandbox/ internal/store/ \
      internal/protocol/messages.go internal/protocol/encoding.go \
      internal/client/rpc.go \
      internal/cli/root.go internal/cli/agent.go internal/cli/channel.go \
      internal/cli/daemon.go internal/cli/status.go internal/cli/exit.go \
      internal/cli/output.go \
      Makefile scripts/smoke-daemon.sh .github/workflows/build.yml README.md \
      docs/phase-3-daemon-plan.md
    git commit -m "Phase 3 alpha.1: daemon + sqlite + docker + CLI CRUD (v0.3.0-alpha.1)"
    ```
33. **Tag and push**:
    ```bash
    git tag -a v0.3.0-alpha.1 -m "Phase 3 alpha.1: daemon + CRUD"
    git push origin main v0.3.0-alpha.1
    ```
34. **Update handoff doc**: bump `~/.claude/projects/-Users-hatef-workspace-agents-atlas/handoff/dclaw.md` with alpha.1 state.

**Release Checklist for v0.3.0-alpha.1:**

1. [ ] `make build` produces both `./bin/dclaw` and `./bin/dclawd`
2. [ ] `./bin/dclawd --version` prints a version with the tag stamped in
3. [ ] `./scripts/smoke-daemon.sh` passes all 7 sub-tests
4. [ ] `go test ./...` passes (includes `store`, `protocol`, daemon in-process)
5. [ ] `go vet ./...` clean
6. [ ] README.md updated with Phase 3 alpha.1 section
7. [ ] Commit tagged `v0.3.0-alpha.1`
8. [ ] Handoff doc updated
9. [ ] No TUI code landed (that's alpha.2)

### 8b. v0.3.0-alpha.2 — TUI dashboard (list + detail + describe)

**Definition of Done for alpha.2:**

```bash
dclaw daemon start
dclaw agent create foo --image=dclaw-agent:v0.1
dclaw        # opens TUI. Left pane shows foo. j/k navigates (if multiple agents). enter shows detail. d shows describe. esc back. q quits.
```

No chat, no logs streaming. "Look at my fleet."

**Implementation order (alpha.2):**

1. **Add bubbletea deps** — already in `go.mod` from alpha.1; run `go mod tidy` just to be sure.
2. **Write `internal/tui/views/view.go`**: copy 7.33.
3. **Write `internal/tui/styles.go`**: copy 7.26.
4. **Write `internal/tui/keys.go`**: copy 7.25.
5. **Write `internal/tui/views/list.go`**: copy 7.27.
6. **Write `internal/tui/views/detail.go`**: copy 7.28.
7. **Write `internal/tui/views/describe.go`**: copy 7.31 — alpha.2 ships with empty Events slice; beta.1 populates from daemon events.
8. **Write `internal/tui/views/chat.go`**: copy 7.29 as a STUB — include the file but route `c` to a "coming in alpha.3" toast.
9. **Write `internal/tui/views/logs.go`**: copy 7.30 as a STUB — route `l` to a toast.
10. **Write `internal/tui/views/help.go`**: copy 7.32.
11. **Write `internal/tui/app.go`**: copy 7.24. This includes the conditional `tea.WithMouseCellMotion()` wiring (added in the 2026-04-16 review for decision Q12): the package-level `MouseEnabled` var is set from the CLI `--no-mouse` flag in step 12 below, and the TUI only enables mouse cell motion when `MouseEnabled == true`.
12. **Modify `cmd/dclaw/main.go`**: copy 7.15. Make sure the `go-isatty` import resolves. Also add a root-level `--no-mouse` bool flag that, when set, flips `tui.MouseEnabled = false` before the TUI launches:
    ```go
    rootCmd.PersistentFlags().Bool("no-mouse", false, "disable mouse support in the TUI (useful for stock macOS Terminal.app)")
    ```
    In the persistent pre-run, propagate the flag value: `if v, _ := cmd.Flags().GetBool("no-mouse"); v { tui.MouseEnabled = false }`. The future `~/.dclaw/config.toml` `mouse: true|false` field will layer on top of this; v0.3 ships the flag only.
13. **Modify `internal/cli/agent.go`**: add agent_attach.go from 7.20 and include `agentAttachCmd` in the `agentCmd.AddCommand(...)` list.
14. **Write `internal/tui/app_test.go`**: copy skeleton from 7.36.
15. **Write `scripts/smoke-tui.sh`**: copy 7.36's shell wrapper. `chmod +x`.
16. **Enable `smoke-tui` in the Makefile**: replace the alpha.1 stub with the real target from 7.34.
17. **Compile + smoke**:
    ```bash
    make build
    ./bin/dclaw          # manual check: TUI opens (mouse on)
    ./bin/dclaw --no-mouse   # manual check: TUI opens without mouse
    make smoke-tui       # teatest assertion
    ```
18. **Update README** — add TUI screenshot placeholder + short usage blurb. Mention `--no-mouse` in the "troubleshooting on stock Terminal.app" note.
19. **Commit + tag `v0.3.0-alpha.2`.**
20. **Update handoff doc.**

**Release Checklist for v0.3.0-alpha.2:**

1. [ ] All alpha.1 checklist items still green
2. [ ] `./bin/dclaw` with no args launches TUI (verified manually on a TTY)
3. [ ] `./bin/dclaw agent attach <name>` opens TUI in (stubbed-for-now) chat view
4. [ ] `make smoke-tui` passes
5. [ ] `q`, `j`/`k`, `enter`, `esc`, `?`, `:q` all wired
6. [ ] `l` and `c` route to "coming in alpha.3" toasts (not crashes)
7. [ ] Commit tagged `v0.3.0-alpha.2`

### 8c. v0.3.0-alpha.3 — TUI chat mode + wire protocol streaming

**Definition of Done for alpha.3:**

```bash
dclaw agent attach foo   # opens TUI in chat mode
# Type a message, press enter
# Streamed response appears line-by-line
```

**Implementation order (alpha.3):**

1. **Extend `internal/protocol/messages.go`** — add two new notification types for chat streaming:
   ```go
   type AgentChatSendParams struct {
     Name    string `json:"name"`
     Message string `json:"message"`
   }
   type AgentChatChunk struct {
     Name  string `json:"name"`
     Role  string `json:"role"` // "agent"
     Text  string `json:"text"`
     Final bool   `json:"final"`
   }
   ```
2. **Extend `internal/daemon/router.go`** — register `agent.chat.send` method + `agent.chat.chunk` notification emitter. The alpha.3 implementation pipes through `docker exec -it` a single-turn pi-mono call (`pi -p --no-session "<message>"`) inside the agent container; stream stdout line-by-line back as `agent.chat.chunk` notifications on the caller's connection.
3. **Extend `internal/client/rpc.go`** — add a `AgentChatSend(ctx, name, message string, out chan<- string)` helper that sends the request and blocks reading `agent.chat.chunk` notifications until one has `Final: true`.
4. **Wire `internal/tui/views/chat.go`** — replace the stub: on enter, drain `ClearInput()`, call `rpc.AgentChatSend`, push each chunk into `Append("agent", chunk)`.
5. **Update `internal/tui/app.go`** — add `chatSendMsg` / `chatChunkMsg` / `chatDoneMsg` plumbing; route keypresses in chat view to the ChatModel's input buffer.
6. **Add `internal/tui/views/chat_test.go`** — teatest drives a chat turn against a mocked RPC.
7. **Update `scripts/smoke-daemon.sh`** — add one more test:
   ```bash
   echo "--- Test 8: agent chat round-trip ---"
   "$DCLAW_BIN" --daemon-socket "$SOCKET" agent start smokey
   echo "hi" | "$DCLAW_BIN" --daemon-socket "$SOCKET" agent exec smokey -- pi -p --no-session "say hi back in one word"
   # (exec path proxied as the chat backend for alpha.3)
   pass "chat round-trip via exec path"
   ```
8. **Manual demo**: spin up agent, `dclaw agent attach foo`, type "what is 2+2?", see `4` stream back.
9. **Commit + tag `v0.3.0-alpha.3`.**

**Release Checklist for v0.3.0-alpha.3:**

1. [ ] All alpha.2 checklist items still green
2. [ ] `dclaw agent attach <name>` + type + enter produces streamed response
3. [ ] Round-trip latency < 5s for a trivial prompt (local Docker)
4. [ ] Commit tagged `v0.3.0-alpha.3`

### 8d. v0.3.0-beta.1 — TUI logs view + logs tail from daemon + polish

**Definition of Done for beta.1:**

```bash
dclaw        # TUI opens
# navigate to foo, press 'l' — logs view opens, live tailing
# press ':' — command mode; type 'refresh' and enter
# press '?' — help overlay
# resize terminal — layout reflows cleanly
```

**Implementation order (beta.1):**

1. **Finish `internal/daemon/logs.go`** — wire `LogStreamer.Stream` into the router as the `agent.logs.stream` method. Emit `agent.log_line` notifications (`{"name": "...", "line": "..."}`).
2. **Finish `internal/client/rpc.go`** — replace the poll-based `agentLogsFollowPoll` with a real subscribe path that reads notifications until ctx done.
3. **Finish `internal/tui/views/logs.go`** — replace the alpha.2 stub with a scrolling viewport (bubbletea `bubbles/viewport`). Wire `j`/`k` within logs view to scroll.
4. **Finish `internal/tui/views/describe.go`** — populate Events from `agent.describe` response.
5. **Wire command mode** — the three commands `:q`, `:help`, `:refresh` already exist; add `:logs <agent>`, `:chat <agent>`, `:describe <agent>` for quick navigation.
6. **Add error toasts** — convert `m.toast` into a lipgloss-styled bottom-right floating box using `ToastStyle`.
7. **Handle `tea.WindowSizeMsg` aggressively** — recompute left-pane width, main-pane width, re-wrap contents.
8. **Harden `scripts/smoke-tui.sh`** — exercise chat + logs + describe transitions.
9. **Polish: keyboard cheat-sheet in the bottom bar** — dynamic to current view via the view's `KeyHelp()` method (add to each view).
10. **Commit + tag `v0.3.0-beta.1`.**

**Release Checklist for v0.3.0-beta.1:**

1. [ ] All alpha.3 checklist items still green
2. [ ] Live log tail works for a running agent
3. [ ] Describe view shows real events
4. [ ] Command mode verbs all function
5. [ ] Help overlay opens and closes cleanly
6. [ ] Terminal resize produces no visual corruption
7. [ ] Commit tagged `v0.3.0-beta.1`

### 8e. v0.3.0 — GA release cut

**Definition of Done for GA:**

- Everything from alpha.1 through beta.1, polished
- README has screenshots + a terminal-recorded demo gif / svg
- Handoff doc reflects Phase 3 complete
- Phase 4 plan doc seeded (just a stub `docs/phase-4-channels-plan.md` is fine)

**Implementation order (GA):**

1. **Bug-bash pass** — run `./scripts/smoke-daemon.sh` and `./scripts/smoke-tui.sh` 10x back-to-back. Fix any flake. Look especially at: sqlite busy timeouts under TUI's 3s refresh + concurrent CRUD, socket perms on macOS, container cleanup on `agent delete`.
2. **Record a demo** — use `vhs` or `asciinema` to record `dclaw daemon start` → `agent create foo` → bare `dclaw` TUI with live data. Commit as `docs/demo.gif` or `docs/demo.cast`.
3. **Update README** — top-of-file hero screenshot + demo gif + quickstart.
4. **Write `docs/phase-4-channels-plan.md`** — at minimum a stub with "Goal: Discord plugin + fleet.yaml + per-agent cost tracking" and a copy of the bullet points from Section 13 of this doc.
5. **Run `.github/workflows/release.yml` via tag push** — produce the darwin/linux amd64/arm64 tarballs as a GitHub release.
6. **Close out handoff doc** — Phase 3 marked complete; pointer to `docs/phase-3-daemon-plan.md`; Phase 4 state set to "design scoped; implementation next".
7. **Final commit + tag `v0.3.0`.**

**Release Checklist for v0.3.0 GA:**

1. [ ] All beta.1 checklist items still green after bug-bash
2. [ ] README has hero screenshot + demo gif/svg
3. [ ] `docs/phase-4-channels-plan.md` exists (even if just a stub)
4. [ ] GitHub release contains darwin-amd64, darwin-arm64, linux-amd64, linux-arm64 tarballs
5. [ ] Handoff doc reflects Phase 3 complete + Phase 4 ready
6. [ ] Commit tagged `v0.3.0` (GA)
7. [ ] `v0.3.0` tag pushed to origin

---

## 9. Testing Strategy

### 9.1 Unit tests

| Package                  | What it tests                                                                                    |
|--------------------------|--------------------------------------------------------------------------------------------------|
| `internal/store`         | CRUD on agents / channels / events; migration idempotence (up -> down -> up).                   |
| `internal/protocol`      | Envelope round-trip (encode -> decode) for every wire spec type; error envelope shape.           |
| `internal/sandbox`       | Smoke against real Docker if reachable (short-circuit if not); pure unit tests for CreateSpec   |
|                          | validation and label merging.                                                                    |
| `internal/daemon`        | Router dispatch (method routing, error codes for unknown methods, notification handling).        |
|                          | Server handshake (accepts v1, rejects v2, rejects non-handshake first message).                  |
|                          | Lifecycle with mocked docker + real sqlite.                                                      |
| `internal/client`        | RPC client round-trip against an in-process server (see `internal/daemon/server_test.go`).       |
| `internal/cli`           | Cobra help doesn't error for every subcommand; `validateOutputFormat`; `DaemonUnreachable` exits |
|                          | 69 with the right payload on `-o json`.                                                          |
| `internal/tui/views`     | Each view model's `SetAgents` / `Up` / `Down` / `SelectedName` invariants.                      |
| `internal/tui`           | teatest: key dispatch for `j`, `k`, `enter`, `esc`, `q`, `?`, `:`.                              |

### 9.2 Integration tests (smoke scripts)

| Script                         | Runs in CI?       | What it exercises                                                     |
|--------------------------------|-------------------|-----------------------------------------------------------------------|
| `scripts/smoke-cli.sh`         | Always            | Existing v0.2.0-cli smoke. Still passes because help/version paths unchanged. |
| `scripts/smoke-daemon.sh`      | Tag pushes only   | Full daemon + CRUD + docker round-trip.                               |
| `scripts/smoke-tui.sh`         | Always            | teatest-driven bubbletea key sequence + render snapshot.              |

CI decision: docker-in-GHA is fragile. `docker-smoke` only runs on tag push, where flakes are worth the signal. PRs get CLI + TUI smoke which needs no Docker.

### 9.3 Non-functional

- **Binary size**: `./bin/dclaw` < 30 MB stripped (cobra + bubbletea + lipgloss). `./bin/dclawd` < 50 MB (includes docker SDK).
- **Daemon startup**: socket reachable within 1s of `dclaw daemon start` on a clean host.
- **TUI cold start**: `time dclaw` until first render < 300 ms.
- **CRUD round-trip**: `dclaw agent list` against a fleet of 10 agents < 200 ms.
- **SQLite WAL**: confirm `~/.dclaw/state.db-wal` appears on first write.

### 9.4 What we're NOT testing in Phase 3

- Discord plugin (none yet).
- Worker-agent spawning (none yet).
- Remote daemon (local socket only).
- Concurrent-CLI stress (single client assumed; SQLite busy_timeout handles occasional contention).

---

## 10. Known Gotchas

1. **Docker socket permissions** — on macOS, `~/.docker/run/docker.sock` is the default; `client.FromEnv` respects `DOCKER_HOST`. On Linux, users may need to be in the `docker` group or use rootless Docker. If `NewDockerClient()` returns a permission error, exit 77 and print a clear remediation message. This is doubly important for the daemon because it runs as the login user, not root.

2. **XDG_RUNTIME_DIR portability** — on macOS this is almost never set. We default to `~/.dclaw/dclaw.sock` on macOS unconditionally. On Linux, we prefer `$XDG_RUNTIME_DIR/dclaw.sock` (typically `/run/user/<uid>/dclaw.sock`) because tmpfs-on-tmpfs yields zero residue on reboot. Test both paths in `internal/daemon/config_test.go`.

3. **SQLite busy under concurrent writes** — the daemon serializes writes (single-writer pool, `busy_timeout=5000`), but a CLI doing `agent create` concurrent with the TUI's 3s poll can collide. WAL mode + single-writer pool makes this safe but adds latency. Acceptable for v0.3; beware if we ever add multi-process writers.

4. **bubbletea terminal resize** — `tea.WindowSizeMsg` arrives asynchronously and views must handle it. Our Update() branch does `m.width = msg.Width` and that's it; individual views re-wrap on every View() call. This is inefficient but correct. Beta.1 adds a cached layout pass if profiling shows it matters.

5. **JSON-RPC ID types** — the spec says IDs are monotonic integers per connection, but the envelope declares `ID any` so that responses from an RPC library that uses strings still round-trip. Our implementation always sends int64 and decodes whatever comes back — tested in `protocol/encoding_test.go`.

6. **`syscall.Kill(pid, 0)` as a liveness probe** — this returns nil if the process exists and is reachable; errors otherwise. Works on macOS + Linux. Windows: not supported (but we don't target Windows).

7. **`os/exec`'s Setsid + Release pattern for detachment** — the CLI fork-and-forget for `daemon start` uses `SysProcAttr{Setsid: true}` to create a new session, then `Process.Release()` to tell the Go runtime "don't wait on this child." If you forget `Release()`, the parent process zombies on exit.

8. **Docker API version negotiation** — `client.WithAPIVersionNegotiation()` handles Docker Engine API version mismatches at Ping time. Don't pin a version in code — it'll break against older Docker Desktop releases.

9. **`goose` embedded migrations** — the `//go:embed migrations/*.sql` directive requires the `migrations/` directory to sit next to `schema.go`. Moving either file breaks the embed. Pin the `goose` version in go.mod (`v3.21.1` in 7.1); newer majors may change SetBaseFS.

10. **lipgloss color fallback on non-color terminals** — detection happens automatically; if the user has `NO_COLOR=1` set, lipgloss degrades gracefully. Test this path by running `NO_COLOR=1 dclaw`.

11. **bubbletea `tea.WithAltScreen()` + stdout captures** — smoke-tui must use teatest's test harness, not spawn a real subprocess, because teatest intercepts the alt-screen sequences. A naive `echo | dclaw` in a script would hang.

12. **stdio hand-off in `agent exec`** — v0.3 buffers the entire stdout/stderr in the JSON-RPC response envelope. For commands that produce > 1MB the envelope limit bites. Beta.1 upgrades to a streaming variant; for GA, document the 1MB limit under Known Limitations in the README.

13. **`ULID` vs `UUID`** — we use ULID for agent IDs (sortable, 26 char string) but UUID for component IDs in the handshake (spec requirement). Both live in the import list of `internal/client/rpc.go`.

14. **Detection of bare invocation vs piped** — `isatty.IsTerminal(os.Stdin.Fd())` + `isatty.IsTerminal(os.Stdout.Fd())` must BOTH be true. If stdout is piped (`dclaw | grep foo`), we want cobra help, not a TUI that emits alt-screen escape sequences. This is the single most subtle UX test.

15. **Log file rotation** — there isn't any. `~/.dclaw/logs/daemon.log` grows without bound. Acceptable for v0.3. Phase 4 adds `lumberjack` or similar.

---

## 11. Error Handling

### 11.1 JSON-RPC error envelope

The daemon always returns errors as JSON-RPC 2.0 error objects per the wire spec (Section 8). The `Code` is one of:

| Code    | Meaning                                                                                 |
|---------|-----------------------------------------------------------------------------------------|
| -32700  | Parse error (invalid JSON).                                                             |
| -32600  | Invalid request (jsonrpc field missing / wrong type).                                   |
| -32601  | Method not found.                                                                        |
| -32602  | Invalid params.                                                                          |
| -32603  | Internal error.                                                                          |
| -32001  | Agent not found (dclaw-custom).                                                          |
| -32002  | Agent not running (dclaw-custom).                                                        |
| -32003  | Quota exceeded (dclaw-custom; reserved, not emitted in v0.3).                            |
| -32004  | Spawn failed (dclaw-custom; bubbles up docker create/start errors).                      |
| -32005  | Timeout (dclaw-custom; not emitted in v0.3).                                             |
| -32006  | Channel plugin not connected (dclaw-custom; reserved, not emitted in v0.3).              |

### 11.2 CLI output on error

Human mode (`-o table`): single line to stderr, no cobra `Error:` prefix, exit with a `sysexits.h` code.

JSON mode (`-o json`): structured object to stdout:
```json
{
  "error": "agent_not_found",
  "message": "agent \"foo\" not found",
  "exit_code": 70,
  "code": -32001
}
```

YAML mode (`-o yaml`): analogous YAML shape.

Mapping from JSON-RPC code to exit code is centralized in `internal/cli/exit.go`:

| JSON-RPC code | Exit code | Label               |
|---------------|-----------|---------------------|
| -32700        | 1         | parse               |
| -32600        | 1         | invalid_request     |
| -32601        | 1         | method_not_found    |
| -32602        | 2         | invalid_params      |
| -32603        | 70        | internal            |
| -32001        | 70        | agent_not_found     |
| -32002        | 70        | agent_not_running   |
| -32003        | 75        | quota_exceeded      |
| -32004        | 70        | spawn_failed        |
| -32005        | 75        | timeout             |
| -32006        | 70        | channel_not_ready   |

### 11.3 TUI error toasts

Errors during a fetch produce a red `ToastStyle` box at the bottom-right that dismisses after 5 seconds or on keypress. The underlying error is appended to `~/.dclaw/logs/daemon.log` (for user-visible diagnosis) via the daemon's logger, which is piped through the RPC connection. The TUI does NOT attempt to retry transient errors automatically; the user's next `:refresh` command or the 3s auto-poll handles recovery.

---

## 12. Release Checklist

### 12.1 Per alpha tag

Every alpha tag (alpha.1, alpha.2, alpha.3, beta.1) must pass its own Release Checklist from Section 8a-8d, AND:

1. [ ] All previous alpha tags' checklists still pass (regression bar)
2. [ ] `git tag -a v0.3.0-<label> -m "..."` pushed to origin
3. [ ] Handoff doc at `~/.claude/projects/-Users-hatef-workspace-agents-atlas/handoff/dclaw.md` updated with:
     - "Last updated" = today's date
     - New sub-milestone bullet under Phase 3
     - Pointer to `docs/phase-3-daemon-plan.md`

### 12.2 For v0.3.0 GA

1. [ ] Every alpha + beta checklist is green, re-run
2. [ ] `make test` + `make vet` + `make lint` all clean
3. [ ] `./scripts/smoke-cli.sh` + `./scripts/smoke-daemon.sh` + `./scripts/smoke-tui.sh` all pass 3x consecutively (flake check)
4. [ ] `./bin/dclaw version` and `./bin/dclawd --version` both print `v0.3.0`
5. [ ] Binary sizes within Section 9.3 budgets
6. [ ] TUI cold start < 300 ms measured locally
7. [ ] README updated with hero screenshot + demo gif/svg
8. [ ] `docs/phase-4-channels-plan.md` stub exists
9. [ ] GitHub release cut with darwin/linux, amd64/arm64 tarballs
10. [ ] Tag `v0.3.0` pushed to origin
11. [ ] Handoff doc marks Phase 3 complete

---

## 13. What Phase 4 Adds (preview)

Phase 4 (`v0.4.0-channels`) picks up where v0.3 stops:

- **Discord channel plugin** — a separate container at `plugins/discord/`. Owns a Discord bot token. Implements wire-protocol boundary 1 (`channel.*` message types) by translating Discord gateway events. The daemon registers the plugin via `channel.register` on its own socket, spins up a per-plugin socket at `$XDG_RUNTIME_DIR/dclaw-channel-discord.sock`, and routes attached-agent messages through.
- **`fleet.yaml` declarative config + `dclaw apply -f fleet.yaml`** — the agent/channel CRUD surface becomes the imperative base; `apply` diffs a yaml spec against the current fleet and executes the delta. No namespaces; a single flat fleet.
- **`dclaw export` + `dclaw diff`** — round-trip the current fleet to yaml; show pending changes.
- **Per-agent cost tracking** — agents emit `cost_usd` via `worker.status_changed`; daemon accumulates in SQLite; TUI shows a per-agent $/day column.
- **Worker-agent spawning** — boundary 3 (`worker.*` message types). Main agents get a new `spawn_worker` tool. Dispatcher creates ephemeral worker containers.
- **Slack channel plugin** — second channel for proof of multi-plugin.
- **Quota warnings on the wire** — `quota.warning` notifications surfaced in the TUI as orange toasts.
- **Chat history persistence** (deferred from v0.3, Q14) — save chat messages to the `events` table with `type='chat'`; TUI reads recent N on chat-mode entry so scrollback survives TUI and daemon restarts.
- **Daemon backup/restore verbs + migration rollback** (deferred from v0.3, Q8) — add `dclaw daemon backup <path>` and `dclaw daemon restore <path>` CLI verbs. Establishes the full migration-rollback story; the v0.3 "move aside state.db" workaround retires.
- **macOS launchd plist + Linux systemd unit examples** (deferred from v0.3, Q10) — ship sample artifacts so users can run `dclawd` at login / at boot without hand-rolling service files.
- **Socket auth / tokens** (deferred from v0.3, Q11) — add an auth handshake on the CLI<->Daemon socket for multi-user or remote daemon access scenarios. v0.3's `0660` + user-group is kept as the default for single-user local.
- **Wire up worker/channel protocol types** (deferred from v0.3, Q7) — the unused `protocol/messages.go` types for boundaries 1 and 3 get consumed by the Discord plugin, worker dispatcher, and channel router. Clears the intentional linter warnings from v0.3.
- **Wire up `daemon.ping`** (deferred from v0.3, Q15) — hook `daemon.ping` into systemd/launchd readiness probes and the TUI's top-bar health indicator.
- **`docker.cli` upgrade** (deferred from v0.3, Q13) — revisit the `v26.1.3+incompatible` pin when docker v27 stable lands. Phase 4's Discord plugin also exercises the docker API more thoroughly, so a version refresh is warranted.

Phase 3's wire protocol types for boundaries 1 and 3 are declared in `internal/protocol/messages.go` now, so Phase 4 wiring is plumbing, not design.

---

## 14. Open Questions

Anything the implementer should escalate instead of silently deciding. Most of these are things I had to invent because the locked spec was silent. All 15 resolved in the 2026-04-16 review; original discussion preserved below so the audit trail survives.

1. **Wire-protocol spec socket paths vs ours** — the spec says `/var/run/dclaw/dispatcher.sock` (requires root or sudo). The locked Phase 3 decision says `$XDG_RUNTIME_DIR/dclaw.sock` (user-owned). I went with the Phase 3 decision and flagged the spec as stale. Before we tag GA we should update `docs/wire-protocol-spec.md` Section 2.1 to reflect the new paths so the spec and code agree. Escalation: "do we care that the authoritative spec says `/var/run/dclaw/`?"

   DECIDED (2026-04-16): spec is stale; update the wire spec to `$XDG_RUNTIME_DIR/dclaw.sock` (Linux, fallback `~/.dclaw/dclaw.sock`) and `~/.dclaw/dclaw.sock` (macOS). No sudo, no `/var/run/`. Spec edited in this same review.

2. **CLI <-> daemon is a FOURTH boundary, not in the wire spec** — the spec covers three boundaries (channel<->main, main<->dispatcher, worker<->dispatcher). v0.3 adds a fourth: CLI <-> daemon. Methods `agent.*`, `channel.*`, `daemon.*` are dclaw-specific and not numbered in the spec's 23-message table. I included them alongside the 23 on a separate "CLI<->daemon" axis. Escalation: "should these be added to the spec document? If yes, Section 12.1 of the spec needs a fourth boundary row."

   DECIDED (2026-04-16): add a fourth boundary (`CLI <-> Daemon`) to the wire spec. New section 7a covers all 23 methods + 3 streaming notifications. Section 12.1 table extended; 12.2 counts updated (was 23 total, now 49 with boundary 4 included).

3. **`agent.chat.send` method is my invention for alpha.3** — the wire protocol spec's message types all assume main-agent routing. There is no spec-level "CLI types a message and gets streamed agent response" flow. Alpha.3's stopgap is to reuse `agent.exec` to pipe prompts through `pi -p --no-session "<msg>"` inside the agent container. Escalation: "is this a hack that should be upstreamed into the wire spec as a new message type?"

   DECIDED (2026-04-16): `agent.chat.send` is captured by the new boundary 4 in the wire spec -- no separate change needed beyond decision 2. Spec now explicitly notes that `agent.chat.send` is content-addressed (targets any agent by name) and is NOT main-agent-routed; the main-agent-behavior is a Phase 4+ convention of Boundary 2, not a constraint of Boundary 4.

4. **`daemon.shutdown` RPC vs SIGTERM to pidfile** — I implemented both (RPC notification for clean shutdown, SIGTERM fallback). This is redundant. Escalation: "pick one for GA and remove the other." My preference: keep SIGTERM-to-pidfile (simpler, always works even if the socket is wedged).

   DECIDED (2026-04-16): keep BOTH. This is intentional redundancy, not a bug: `daemon.shutdown` RPC for clean-shutdown tooling and scripted lifecycle control; SIGTERM-to-pidfile as the bulletproof fallback when the socket is wedged. Remove the "pick one" escalation language -- both paths are load-bearing.

5. **`dclaw agent attach` semantics** — if the agent is `stopped`, does attach auto-start it? I said no: attach opens the TUI chat view; user runs `:start` or `dclaw agent start` themselves. This matches `kubectl attach` / `docker attach` behavior. Escalation: "reasonable?"

   DECIDED (2026-04-16): attach does NOT auto-start a stopped agent. User runs `:start` or `dclaw agent start` themselves. Matches `kubectl attach` / `docker attach` behavior; principle of least surprise. Locked.

6. **`--migrate-only` flag on dclawd** — referenced in the Makefile's `migrate` target but I didn't add it to `cmd/dclawd/main.go`. Alpha.1 should add this as a simple `flag.Bool` that runs `repo.Migrate` then exits. Flagging so it's not forgotten.

   DECIDED (2026-04-16): add `--migrate-only` to `cmd/dclawd/main.go` in alpha.1. Invokes `repo.Migrate()` then exits 0. §7.2 exact-file-contents and §8a implementation steps updated in this review.

7. **Worker and channel wire types are declared but unused** — I included all 23 spec message types in `internal/protocol/messages.go` (for completeness + to let tests unmarshal spec examples). The linter will report them as unused until Phase 4. Accept the linter warning; don't silence it with `//nolint` comments (we want the warning to push us to wire them up in v0.4).

   DECIDED (2026-04-16): accept the linter warnings; wire up in v0.4. No `//nolint` suppressions. The unused-symbol noise is load-bearing -- it's what pushes us to actually wire boundaries 1 and 3 in Phase 4.

8. **sqlite migration rollback story** — I speced `+goose Down` blocks for 0001 but the daemon never calls Down. If a migration breaks a user's DB, the recovery path is "move aside `~/.dclaw/state.db` and restart." Is that acceptable? Phase 4 should add a `dclaw daemon restore <backup>` verb.

   DECIDED (2026-04-16): defer to Phase 4. v0.3 accepts the "move aside state.db and restart" recovery path. Phase 4 adds `dclaw daemon backup <path>` and `dclaw daemon restore <path>` verbs + the full migration-rollback story. See §13 Phase 4 preview.

9. **TUI's `chat.go` v0.3 vs the eventual protocol.worker.message design** — alpha.3 ships a CLI-only chat that doesn't touch the main-agent / worker split. When Phase 4 wires worker spawning, chat becomes "send message to main agent; main agent spawns workers; main agent reports back." The alpha.3 chat UX should survive that transition — just the backend RPC changes. Verify this assumption before alpha.3 ships.

   DECIDED (2026-04-16): the assumption holds. Alpha.3's chat UX survives the Phase 4 main-agent/worker transition untouched; only the backend RPC routing changes (from `docker exec pi -p` to `agent.chat.send -> worker.spawn -> worker.report` chain). Locked.

10. **macOS `dclawd` signal handling under launchctl** — if users want `dclawd` to run at login, they'll wrap it in a launchd plist. The daemon needs clean SIGTERM handling, which `signal.NotifyContext` provides. I did NOT write a sample launchd plist; Phase 4 can.

    DECIDED (2026-04-16): defer plist/systemd-unit artifacts to Phase 4. v0.3's clean-SIGTERM handling is sufficient; the documentation artifacts (macOS launchd plist + Linux systemd unit examples) ship with Phase 4. See §13 Phase 4 preview.

11. **No auth on the socket** — `0660` perms + the login-user group mean any user in the `dclaw` group (typically just the owner) can talk to the daemon. No token, no challenge-response. Acceptable for v0.3 single-user local; revisit for multi-user or remote.

    DECIDED (2026-04-16): no socket auth for v0.3. `0660` perms + user group are sufficient for single-user local deployments (the overwhelming v0.3 use case). Socket auth / tokens land in Phase 4 for multi-user or remote daemon scenarios. See §13 Phase 4 preview.

12. **`tea.WithMouseCellMotion()` on macOS Terminal.app** — bubbletea's mouse support works in iTerm2 / Alacritty / Kitty / Ghostty. Apple's Terminal.app is spottier. If users complain, we can drop `WithMouseCellMotion()` without losing keyboard functionality.

    DECIDED (2026-04-16): make mouse support opt-out via a `--no-mouse` flag on `dclaw` invocation in v0.3 (included in alpha.2 scope). A future `~/.dclaw/config.toml` will add a `mouse: true|false` field, but for v0.3 we ship the flag only. §7.24 (`internal/tui/app.go`) updated to conditionally call `tea.WithMouseCellMotion()` based on the flag. §8b alpha.2 implementation steps updated.

13. **`docker.cli` version pinning in go.mod** — I picked `github.com/docker/docker v26.1.3+incompatible` which is a common LTS. The `+incompatible` suffix is because the docker module's module path predates Go modules. Upgrade path: when docker v27 goes stable, revisit; don't chase minor versions.

    DECIDED (2026-04-16): keep `v26.1.3+incompatible` for v0.3. Don't chase minor versions during the v0.3 release cycle. Revisit the pin when docker v27 stable lands. See §13 Phase 4 preview.

14. **Chat history persistence** — v0.3 chat history is in-memory only (lost when the TUI closes). Should we persist it in SQLite (`events` table with `type='chat'`)? I left it out of scope for v0.3 but it's an obvious Phase 4 win; flagging here.

    DECIDED (2026-04-16): defer to Phase 4. Chat history persistence goes in the `events` table with `type='chat'`; TUI will read recent N on chat-mode entry for scrollback that survives TUI/daemon restarts. Clear Phase 4 deliverable. See §13 Phase 4 preview.

15. **The `daemon.ping` method has no caller** — I registered it for future-use (health checks, readiness probes). Linter will flag. Keep it; it costs nothing.

    DECIDED (2026-04-16): keep `daemon.ping` registered in v0.3 even though it's unused. Phase 4 wires it up as a health-check probe (readiness endpoint for systemd/launchd integration). See §13 Phase 4 preview.

---

**End of Phase 3 Daemon Plan.**
