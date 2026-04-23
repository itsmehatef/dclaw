<p align="center">
  <img src="logo.png" alt="dclaw" width="300" />
</p>

# dclaw

A container-native multi-agent platform. Lightweight sandboxed agent containers (data plane) + host daemon (control plane). Independently versioned channel plugins, fleet orchestration, and per-agent isolation.

## What is this?

dclaw is a container-native multi-agent platform. Each AI agent runs inside a lightweight, sandboxed Docker container (~200-300MB) with its own scoped filesystem, network policy, and resource limits. A control-plane daemon on the host manages the fleet.

The agent runtime is powered by [pi-mono](https://github.com/badlogic/pi-mono) (`@mariozechner/pi-coding-agent`, MIT, 34.6k stars) — the same TypeScript agent SDK that OpenClaw builds on. dclaw does NOT rewrite the agentic loop. It wraps pi-mono with mandatory sandboxing, fleet management, and channel plugins.

- **Sandboxing is mandatory, not optional** — every agent (brain + tools) runs inside a Docker container. Scoped filesystem, scoped network (iptables allowlist), scoped resources. Nothing escapes the sandbox. There is no `sandbox.mode: "off"`.
- **Container-native agent runtime** — Alpine + Node.js + pi-mono (~200-300MB). The full agent (LLM calls + tool execution) runs inside the container.
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

## Building the CLI and Daemon (v0.3.0-beta.2-sandbox-hardening)

Requires Go 1.25+.

```bash
# Build the binary into ./bin/dclaw
make build

# Install into $GOPATH/bin
make install

# Check the build
./bin/dclaw version
# dclaw version 0.3.0-beta.2-sandbox-hardening (commit abc1234, built 2026-04-24T...Z, go1.25.x)
```

### Running

dclaw is a container-native multi-agent platform. The CLI, daemon (`dclawd`),
TUI, and workspace validator all ship together.

First-time setup, then start the daemon, create an agent, and chat:

```bash
# One-time: tell dclaw which host directory is the agent-workspace root.
dclaw config set workspace-root ~/dclaw-agents

# Start the background daemon.
dclaw daemon start

# Create and start an agent whose workspace sits under the allow-root.
dclaw agent create foo --image=dclaw-agent:v0.1 --workspace=~/dclaw-agents/foo
dclaw agent start foo

# One-shot chat round-trip.
dclaw agent chat foo --one-shot "hello"
```

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
- **Agent containers**: Alpine + Node.js + pi-mono (~200-300MB) — sandboxed execution environment
- **Web dashboard**: TypeScript (planned)
- **Channel plugins**: Any language — independently versioned containers, JSON-RPC over Unix sockets
- **Wire protocol**: JSON-RPC 2.0 over Unix domain sockets

## Status

Early development — v0.3.0-beta.2-sandbox-hardening: container posture hardened (cap drop, seccomp, ReadonlyRootfs, non-root UID, docker.sock denylist) on top of paths-hardening.

## License

Apache-2.0
