<p align="center">
  <img src="logo.png" alt="dclaw" width="300" />
</p>

# dclaw

A container-native multi-agent platform. Lightweight sandboxed agent containers (data plane) + host daemon (control plane). Independently versioned channel plugins, fleet orchestration, and per-agent isolation.

## What is this?

dclaw is a container-native multi-agent platform. Each AI agent runs inside a lightweight, sandboxed Docker container (~100MB) with its own scoped filesystem, network policy, and resource limits. A control-plane daemon on the host manages the fleet.

- **Sandboxed by default** — every agent (brain + tools) runs inside a Docker container. Scoped filesystem, scoped network (iptables allowlist), scoped resources. Nothing escapes the sandbox.
- **Lightweight agent containers** — ~100MB Alpine + Go binary, NOT a 3GB Claude Code install. Starts in 1-2 seconds.
- **Control plane + data plane split** — the `dclaw` daemon manages containers and routes messages (control plane). Agent containers make API calls and execute tools (data plane).
- **Independently versioned channel plugins** — upgrade Discord without touching Slack, roll back WhatsApp without affecting anything else
- **Main agent + ephemeral workers** — one always-on agent, spawns scoped worker containers per task
- **Fleet orchestration** — declarative `fleet.yaml`, `dclaw` CLI, cost tracking, quota enforcement

## Architecture

![dclaw Platform Architecture](docker-claw-canonical.png)

## Project Structure

```
dclaw/
├── cmd/
│   ├── dclaw/           # Daemon + CLI entry point
│   └── dclaw-agent/     # Agent binary (runs inside containers)
├── internal/
│   ├── daemon/          # Control plane (fleet, routing, quota)
│   ├── agent/           # Agent runtime (API calls, conversation, tools)
│   ├── protocol/        # Wire protocol types and serialization
│   └── tools/           # Built-in tool implementations (bash, file, web)
├── pkg/mcp/             # MCP server implementations
├── configs/             # Example fleet configs
├── docs/                # Wire protocol spec, diagrams
├── go.mod
└── README.md
```

## Tech Stack

- **dclaw daemon (control plane)**: Go — fleet management, channel routing, quota enforcement
- **dclaw agent binary (data plane)**: Go — runs inside containers, makes Anthropic API calls, executes tools
- **Agent containers**: Alpine Linux (~100MB) — sandboxed execution environment
- **Web dashboard**: TypeScript (planned)
- **Channel plugins**: Any language — communicate via JSON-RPC over Unix sockets
- **Wire protocol**: JSON-RPC 2.0 over Unix domain sockets

## Status

🚧 **Early development** — Phase 1 (foundations)

## License

Apache-2.0
