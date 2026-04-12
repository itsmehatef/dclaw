<p align="center">
  <img src="logo.png" alt="Docker Claw" width="300" />
</p>

# Docker Claw (dclaw)

A container-native multi-agent platform with independently versioned channel plugins, sandboxed agent execution, and fleet orchestration.

## What is this?

Docker Claw runs AI agents (Claude Code) inside isolated Docker containers with:

- **Container-per-agent isolation** — each agent runs in its own sandbox with scoped filesystem, network, and tool access
- **Independently versioned channel plugins** — upgrade Discord without touching Slack, roll back WhatsApp without affecting anything else
- **Main agent + ephemeral workers** — one always-on coordinator spawns scoped worker containers per task
- **Fleet orchestration** — declarative `fleet.yaml` config, `dclaw` CLI, cost tracking, quota enforcement
- **Egress observability** — audit every outbound request your agents make

## Architecture

![Docker Claw Platform Architecture](docker-claw-canonical.png)

## Project Structure

```
dclaw/
├── cmd/dclaw/           # CLI entry point
├── internal/
│   ├── dispatcher/      # Dispatcher daemon (worker registry, routing, lifecycle)
│   ├── protocol/        # Wire protocol types and serialization
│   └── worker/          # Worker container management
├── pkg/mcp/             # MCP server implementations (agent-controller, worker)
├── configs/             # Example fleet configs
├── docs/                # Wire protocol spec, diagrams
│   ├── wire-protocol-spec.md
│   └── wire-protocol-spec.pdf
├── go.mod
└── README.md
```

## Tech Stack

- **Dispatcher daemon + CLI**: Go
- **Web dashboard**: TypeScript (planned)
- **Channel plugins**: Any language (communicate via JSON-RPC over Unix sockets)
- **Wire protocol**: JSON-RPC 2.0 over Unix domain sockets

## Status

🚧 **Early development** — Phase 1 (foundations)

## License

Apache-2.0
