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

	// beta.1-paths-hardening: resolve the workspace-root allow policy
	// BEFORE the --migrate-only fast-path. buildPolicy is pure
	// config-file-plus-env (no Docker, no socket), so running it here is
	// free and also gives operators a way to verify which source resolved
	// the allow-root from a `dclawd --migrate-only` probe (e.g. in CI).
	policy := buildPolicy(logger, cfg.StateDir)

	// beta.2.5: resolve effective audit-rotation parameters from
	// config.toml on top of the audit package defaults (10 MB / 5 files
	// in beta.2.3). Computed BEFORE the --migrate-only fast-path so an
	// operator running `dclawd --migrate-only` to dry-run the boot
	// sequence can read the same `audit log configured` line they would
	// see at production startup. The values are applied to the open
	// auditLog handle further down — migrate-only does not open the file.
	auditMaxSize, auditMaxFiles := resolveAuditConfig(logger, cfg.StateDir)
	logger.Info("audit log configured",
		"max_size", auditMaxSize,
		"max_files", auditMaxFiles,
	)

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

	// Open the daemon-lifetime audit log. Closed on shutdown. Failure to
	// open is fatal — we refuse to run without an audit trail for
	// agent-create.
	auditLog, err := audit.New(cfg.StateDir)
	if err != nil {
		logger.Error("audit open failed", "err", err)
		os.Exit(65)
	}
	defer auditLog.Close()
	// beta.2.5: apply the [audit] overrides resolved up top onto the
	// open Logger handle. Logger.MaxSize / MaxFiles are exported (per
	// beta.2.3's deviation note), so it's a one-liner copy.
	auditLog.MaxSize = auditMaxSize
	auditLog.MaxFiles = auditMaxFiles

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
	legacyScan(ctx, logger, repo, policy, docker)

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

// resolveAuditConfig returns the effective (max_size, max_files) for the
// audit logger after layering [audit] overrides from $stateDir/config.toml
// on top of audit.Default{MaxSize,MaxFiles} (10 MB / 5 files in beta.2.3).
//
// Behavior:
//   - Missing config.toml or unset [audit] table → defaults.
//   - Read error → defaults; one Warn line is logged so operators see why
//     their tuning didn't take effect.
//   - Non-positive MaxSizeBytes or MaxFiles in the file → defaults (zero
//     values are the "leave it alone" sentinel; deliberate disable lives
//     behind a separate future flag, not the config file).
//
// Pure config + standard library, called BEFORE --migrate-only so the
// info line shows up in dry-run boots too.
func resolveAuditConfig(logger *slog.Logger, stateDir string) (int64, int) {
	maxSize := audit.DefaultMaxSize
	maxFiles := audit.DefaultMaxFiles
	fc, err := config.ReadConfigFile(stateDir)
	if err != nil {
		logger.Warn("config.toml read failed; audit log will use defaults", "err", err)
		return maxSize, maxFiles
	}
	if fc.Audit.MaxSizeBytes > 0 {
		maxSize = fc.Audit.MaxSizeBytes
	}
	if fc.Audit.MaxFiles > 0 {
		maxFiles = fc.Audit.MaxFiles
	}
	return maxSize, maxFiles
}

// buildPolicy constructs the runtime paths.Policy from the resolved config.
// Starts from paths.DefaultDenylist and appends the daemon user's $HOME so
// a workspace pointing at the operator's home dir directly is refused.
// On config read failure we log but keep going with an empty AllowRoot —
// the validator will then reject every non-trust path with the "not
// configured" message, which is the desired fail-closed behavior.
//
// Precedence for the allow-root is: config file > DCLAW_WORKSPACE_ROOT env
// var > unconfigured. The env var fallback makes single-invocation
// overrides (e.g. container-ized tests, `make smoke`) work without
// mutating the on-disk config.toml.
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

	allowRoot := fc.WorkspaceRoot
	if allowRoot == "" {
		if env := os.Getenv(config.EnvWorkspaceRoot); env != "" {
			logger.Info("using DCLAW_WORKSPACE_ROOT env var for workspace-root (config.toml unset)", "workspace_root", env)
			allowRoot = env
		}
	}
	return paths.Policy{AllowRoot: allowRoot, Denylist: dl}
}

// legacyScan logs one Warn per pre-beta.1 agent whose workspace does not
// validate under the current policy and has no WorkspaceTrustReason set,
// AND one Warn per pre-beta.2 agent whose container posture is missing
// any of: CapDrop(ALL), ReadonlyRootfs, User=1000:1000. Intentionally
// non-blocking: the scan surfaces hazards but does not modify state or
// refuse startup.
//
// When policy.AllowRoot is empty ("not configured"), every legacy agent
// will trip ErrWorkspaceForbidden with the "no workspace-root configured"
// reason — that warning has no per-agent signal because the remediation
// is a single `dclaw config set workspace-root`, not `agent delete`. We
// skip the workspace-path portion of the scan in that case and log one
// info-level line instead. The per-agent workspace warnings only fire
// when an allow-root IS configured and specific agents fall outside it.
//
// The beta.2 container-posture portion runs unconditionally — posture
// weaknesses are independent of workspace-root config. ContainerInspect
// failures (container removed externally, docker daemon glitch) are
// silently skipped per agent: this is an advisory scan, not a gate.
func legacyScan(ctx context.Context, logger *slog.Logger, repo *store.Repo, policy paths.Policy, docker *sandbox.DockerClient) {
	rows, err := repo.ListAgents(ctx)
	if err != nil {
		logger.Warn("legacy scan: list agents failed", "err", err)
		return
	}
	// Workspace-path portion (beta.1 carryover). Requires AllowRoot configured.
	if policy.AllowRoot == "" {
		logger.Info("workspace-root not configured; skipping legacy-agent workspace scan (set with 'dclaw config set workspace-root <path>')")
	} else {
		for _, r := range rows {
			if r.Workspace == "" {
				continue
			}
			if r.WorkspaceTrustReason != "" {
				continue
			}
			if _, verr := policy.Validate(r.Workspace); verr != nil {
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
	// Container-posture portion (beta.2 PR-D). Inspects every existing
	// container and warns once per agent whose posture is missing any
	// of the three hardening dimensions. Agents with empty ContainerID
	// have never been created at the Docker layer (create-time failure
	// or pre-lifecycle state) and are skipped. Inspect errors (container
	// removed externally) are silently ignored — the reconciler handles
	// state drift separately.
	for _, r := range rows {
		if r.ContainerID == "" {
			continue
		}
		posture, perr := docker.InspectPosture(ctx, r.ContainerID)
		if perr != nil {
			continue
		}
		if !posture.CapDropAll || !posture.ReadonlyRootfs || posture.User != "1000:1000" {
			logger.Warn("agent has pre-beta.2 weak container posture; recreate with 'agent delete' + 'agent create' to apply the hardening",
				"name", r.Name,
				"cap_drop_all", posture.CapDropAll,
				"readonly_rootfs", posture.ReadonlyRootfs,
				"user", posture.User,
			)
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
