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

	// Build context that cancels on SIGTERM/SIGINT.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Write pidfile for `dclaw daemon stop`.
	if err := cfg.WritePIDFile(os.Getpid()); err != nil {
		logger.Error("pidfile write failed", "err", err)
		os.Exit(1)
	}
	defer cfg.RemovePIDFile()

	// Start background status reconciler.
	// Run initial sync immediately (before the first 2s tick) so the DB is
	// accurate as soon as the daemon starts — e.g. after a daemon restart where
	// containers may have exited while dclawd was down. ReconcileOnce logs
	// warnings on individual failures but does not abort daemon startup.
	reconciler := daemon.NewStatusReconciler(logger, repo, docker)
	reconciler.ReconcileOnce(ctx) // synchronous initial pass
	go reconciler.Run(ctx)

	// Wire and run the server.
	srv := daemon.NewServer(cfg, logger, repo, docker)

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
