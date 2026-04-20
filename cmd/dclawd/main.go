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
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"syscall"

	"github.com/itsmehatef/dclaw/internal/audit"
	"github.com/itsmehatef/dclaw/internal/config"
	"github.com/itsmehatef/dclaw/internal/daemon"
	"github.com/itsmehatef/dclaw/internal/paths"
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

	// beta.1-paths-hardening: load the workspace-root allow policy and
	// open the audit log. Both are daemon-lifetime objects; the audit
	// logger is closed on shutdown. Failure to open the audit log is
	// fatal — we refuse to run without an audit trail for agent-create.
	policy := buildPolicy(logger, cfg.StateDir)
	auditLog, err := audit.New(cfg.StateDir)
	if err != nil {
		logger.Error("audit open failed", "err", err)
		os.Exit(65)
	}
	defer auditLog.Close()

	// Build context that cancels on SIGTERM/SIGINT.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Write pidfile for `dclaw daemon stop`.
	if err := cfg.WritePIDFile(os.Getpid()); err != nil {
		logger.Error("pidfile write failed", "err", err)
		os.Exit(1)
	}
	defer cfg.RemovePIDFile()

	// Legacy-scan: warn once per pre-beta.1 agent with an out-of-root or
	// unvalidated workspace. Skips agents that already carry a trust
	// reason. Does NOT block startup — operators need to be able to
	// start dclawd even if every legacy agent fails the current policy,
	// because `agent delete` is the prescribed remediation.
	legacyScan(ctx, logger, repo, policy)

	// Start background status reconciler.
	// Run initial sync immediately (before the first 2s tick) so the DB is
	// accurate as soon as the daemon starts — e.g. after a daemon restart where
	// containers may have exited while dclawd was down. ReconcileOnce logs
	// warnings on individual failures but does not abort daemon startup.
	reconciler := daemon.NewStatusReconciler(logger, repo, docker)
	reconciler.ReconcileOnce(ctx) // synchronous initial pass
	go reconciler.Run(ctx)

	// Wire and run the server.
	srv := daemon.NewServer(cfg, logger, repo, docker, policy, auditLog)

	if _ = foreground; true {
		if err := srv.Run(ctx); err != nil {
			logger.Error("server stopped with error", "err", err)
			os.Exit(70) // EX_SOFTWARE
		}
	}

	logger.Info("dclawd stopped cleanly")
}

// buildPolicy constructs the runtime paths.Policy from the resolved config.
// Starts from paths.DefaultDenylist and appends the daemon user's $HOME so
// a workspace pointing at the operator's home dir directly is refused.
// On config read failure we log but keep going with an empty AllowRoot —
// the validator will then reject every non-trust path with the "not
// configured" message, which is the desired fail-closed behavior.
func buildPolicy(logger *slog.Logger, stateDir string) paths.Policy {
	dl := append([]string(nil), paths.DefaultDenylist...)
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		dl = append(dl, u.HomeDir)
	}

	fc, err := config.ReadConfigFile(stateDir)
	if err != nil {
		logger.Warn("config.toml read failed; running with empty allow-root (all agent.create without --workspace-trust will be rejected)", "err", err)
		return paths.Policy{Denylist: dl}
	}
	return paths.Policy{AllowRoot: fc.WorkspaceRoot, Denylist: dl}
}

// legacyScan logs one Warn per pre-beta.1 agent whose workspace does not
// validate under the current policy and has no WorkspaceTrustReason set.
// Intentionally non-blocking: the scan surfaces hazards but does not
// modify state or refuse startup.
func legacyScan(ctx context.Context, logger *slog.Logger, repo *store.Repo, policy paths.Policy) {
	rows, err := repo.ListAgents(ctx)
	if err != nil {
		logger.Warn("legacy scan: list agents failed", "err", err)
		return
	}
	for _, r := range rows {
		if r.Workspace == "" {
			continue
		}
		if r.WorkspaceTrustReason != "" {
			continue
		}
		if _, verr := policy.Validate(r.Workspace); verr != nil {
			// Only log truly forbidden cases; a benign error path here
			// (e.g., config missing) would fire a warning for every
			// agent, which is noisy and not actionable.
			if errors.Is(verr, paths.ErrWorkspaceForbidden) {
				logger.Warn("legacy agent with unverified workspace path",
					"name", r.Name,
					"workspace", r.Workspace,
					"reason", verr.Error(),
				)
			}
		}
	}
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
