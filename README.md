<p align="center">
  <img src="logo.png" alt="dclaw" width="300" />
</p>

# dclaw

A container-native multi-agent platform. Lightweight sandboxed agent containers (data plane) + host daemon (control plane). Independently versioned channel plugins, fleet orchestration, and per-agent isolation.

## What is this?

dclaw is a container-native multi-agent platform. Each AI agent runs inside a lightweight, sandboxed Docker container (~200-300MB) with its own scoped filesystem, network policy, and resource limits. A control-plane daemon on the host manages the fleet.

The agent runtime is powered by [pi-mono](https://github.com/badlogic/pi-mono) (`@mariozechner/pi-coding-agent`, MIT, 34.6k stars) — the same TypeScript agent SDK that OpenClaw builds on. dclaw does NOT rewrite the agentic loop. It wraps pi-mono with mandatory sandboxing, fleet management, and channel plugins.

- **Sandboxing is mandatory, not optional** — every agent (brain + tools) runs inside a Docker container. Scoped filesystem, scoped network (iptables allowlist), scoped resources. Nothing escapes the sandbox. There is no `sandbox.mode: "off"`.
- **Container-native agent runtime** — Debian (bookworm-slim) + Node.js 22 + pi-mono (~200-300MB). The full agent (LLM calls + tool execution) runs inside the container.
- **Control plane + data plane split** — the `dclaw` daemon manages containers and routes messages (control plane). Agent containers make API calls and execute tools (data plane).
- **Independently versioned channel plugins** — upgrade Discord without touching Slack, roll back WhatsApp without affecting anything else
- **Main agent + ephemeral workers** — one always-on agent, spawns scoped worker containers per task
- **Fleet orchestration** — declarative `fleet.yaml`, `dclaw` CLI, cost tracking, quota enforcement

## Architecture

![dclaw Platform Architecture](docker-claw-canonical.png)

See [docs/architecture.md](docs/architecture.md) for the full architecture document covering core principles, sandboxing model, threat model, dependency decisions, and build phases. See [docs/workspace-root.md](docs/workspace-root.md) for the workspace-path validator runbook (how to configure `workspace-root`, `--workspace-trust` escape hatch, audit log format, common errors).

## Project Structure

```
dclaw/
├── cmd/dclaw/           # Daemon + CLI entry point (Go)
├── internal/
│   ├── daemon/          # Control plane (fleet, routing, quota)
│   ├── protocol/        # Wire protocol types and serialization
│   └── sandbox/         # Container management, network policies
├── agent/               # Agent container build (Dockerfile, wrapper, configs)
├── plugins/             # Channel plugin containers
│   └── discord/         # Discord channel plugin
├── configs/             # Example fleet configs
├── docs/                # Wire protocol spec, architecture docs, diagrams
├── go.mod
└── README.md
```

## Building the CLI and Daemon (v0.3.0-beta.2.6-platform-port)

Requires Go 1.25+.

```bash
# Build the binary into ./bin/dclaw
make build

# Install into $GOPATH/bin
make install

# Check the build
./bin/dclaw version
# dclaw version 0.3.0-beta.2.6-platform-port (commit abc1234, built 2026-05-01T...Z, go1.25.x)
```

### Running

dclaw is a container-native multi-agent platform. The CLI, daemon (`dclawd`),
TUI, and workspace validator all ship together.

First-time setup, then start the daemon, create an agent, and chat.

State-dir defaults: macOS uses `~/.dclaw`. Linux honors XDG — if `$XDG_STATE_HOME` is set, the daemon stores state under `$XDG_STATE_HOME/dclaw`; otherwise `~/.local/state/dclaw` if that tree exists; otherwise `~/.dclaw` (the legacy default — existing installs keep working). See [`docs/workspace-root.md`](docs/workspace-root.md) Cross-platform notes for the full table.

```bash
# One-time: configure workspace-root in a single step. Interactive prompt
# defaults to $HOME/dclaw and creates the directory at mode 0700. Use
# --yes to accept the default non-interactively, or --workspace-root <path>
# to pick an explicit path.
dclaw init

# Or set it explicitly without the wizard:
#   dclaw config set workspace-root ~/dclaw-agents

# Pre-flight diagnostics — run `dclaw doctor` if anything goes sideways.
# Reports daemon, docker, config, image, audit-log, and workspace-root state.
dclaw doctor

# Start the background daemon.
dclaw daemon start

# Create and start an agent whose workspace sits under the allow-root.
# Note: bash does NOT expand ~ inside --workspace=, so use $HOME for absolute paths.
dclaw agent create foo --image=dclaw-agent:v0.1 --workspace="$HOME/dclaw/foo"
dclaw agent start foo

# One-shot chat round-trip.
dclaw agent chat foo --one-shot "hello"

# See all commands: dclaw --help
```

### All commands

Major verbs at a glance:

- `dclaw init` — first-run wizard; prompts for `workspace-root` (defaults to `$HOME/dclaw`) and writes `config.toml`.
- `dclaw doctor` — pre-flight diagnostics across config, daemon, docker, image, audit-log, and workspace-root.
- `dclaw config get|set workspace-root` — read/write the workspace allow-root in `config.toml`.
- `dclaw daemon start|stop|status` — manage the background `dclawd` process.
- `dclaw agent create|list|describe|start|stop|delete|chat` — full agent lifecycle.
- `dclaw version` — print build version, commit, build time, Go toolchain.

Run bare `dclaw` (no subcommand) to enter the interactive TUI.

Commands that need the daemon exit with code 69 and structured JSON
(`--output json`) containing `"error": "daemon_unreachable"` when it's not
running.

See [`docs/workspace-root.md`](docs/workspace-root.md) for the workspace-path
validator runbook — allow-root configuration, the `--workspace-trust=<reason>`
escape hatch, and the append-only audit-log format.

## Tech Stack

- **dclaw daemon (control plane)**: Go — fleet management, channel routing, quota enforcement, CLI
- **Agent runtime (data plane)**: pi-mono (`@mariozechner/pi-coding-agent`, TypeScript) — runs inside containers
- **Agent containers**: Debian (bookworm-slim) + Node.js 22 + pi-mono (~200-300MB) — sandboxed execution environment
- **Web dashboard**: TypeScript (planned)
- **Channel plugins**: Any language — independently versioned containers, JSON-RPC over Unix sockets
- **Wire protocol**: JSON-RPC 2.0 over Unix domain sockets

## Status

Early development — v0.3.0-beta.2.6-platform-port: paths-hardening + container posture + first-run UX + audit rotation + doctor + TOML config + cross-platform scaffolding shipped. See [WORKLOG.md](WORKLOG.md) for the full release history.

## Security posture

dclaw layers two enforcement boundaries — workspace path validation (host side) and container posture (sandbox side) — plus an append-only audit log over both. Each item below has a runbook entry under `docs/` if you need the operational detail.

- **Workspace path validation** (beta.1): a hard-coded denylist of system paths (`/`, `/etc`, `/usr`, `/var`, `/private/tmp`, `/Library`, `/Applications`, `/opt/homebrew`, plus `C:\Windows`/`Program Files`/`ProgramData` on Windows builds) is checked first; then a `filepath.Rel`-based containment check against `workspace-root` (set via `dclaw config set workspace-root <path>` or `dclaw init`). The `--workspace-trust=<reason>` escape hatch bypasses the allow-root check (NOT the denylist), persists per-agent in `state.db`, and writes an `outcome=trust` audit record.
- **Container posture** (beta.2): every agent container runs with `CapDrop: ALL`, `SecurityOpt: no-new-privileges`, Docker's default seccomp profile (auto-applied — not pinned, see beta.2 plan §11 Q2 for why), `ReadonlyRootfs: true` with tmpfs overlays at `/tmp` (64m noexec) and `/run` (8m noexec), `User: 1000:1000`, `PidsLimit: 256`. The Docker control socket is denylisted as a workspace target across all three common locations (Linux `/var/run/docker.sock`, systemd `/run/docker.sock`, Docker Desktop on macOS).
- **Audit log**: every workspace decision is logged to `$STATE_DIR/audit.log` as NDJSON. The file is opened `O_APPEND|O_CREATE|O_SYNC` at mode 0600, mutex-serialized, and size-rotated (10MB threshold / 5 files retained by default; tunable in `config.toml`'s `[audit]` table).
- **Not yet enforced** (beta.3+): per-agent network egress allowlist (`EgressAllowlist` exists on the protocol but isn't wired through), custom seccomp profile (tighter than Docker's default), per-agent memory + CPU limits, agent image security rebase. Kernel-level CVEs (Dirty Pipe etc.) are explicitly out of scope — keep the host kernel patched.

See [docs/architecture.md](docs/architecture.md) §"Threat Model" and [docs/workspace-root.md](docs/workspace-root.md) for the full operational matrix.

## License

Apache-2.0
