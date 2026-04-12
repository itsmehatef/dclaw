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

See [docs/architecture.md](docs/architecture.md) for the full architecture document covering core principles, sandboxing model, threat model, dependency decisions, and build phases.

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

## Tech Stack

- **dclaw daemon (control plane)**: Go — fleet management, channel routing, quota enforcement, CLI
- **Agent runtime (data plane)**: pi-mono (`@mariozechner/pi-coding-agent`, TypeScript) — runs inside containers
- **Agent containers**: Alpine + Node.js + pi-mono (~200-300MB) — sandboxed execution environment
- **Web dashboard**: TypeScript (planned)
- **Channel plugins**: Any language — independently versioned containers, JSON-RPC over Unix sockets
- **Wire protocol**: JSON-RPC 2.0 over Unix domain sockets

## Status

Early development — Phase 1 (one agent loop working inside a container)

## License

Apache-2.0
