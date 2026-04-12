# dclaw Architecture

## Overview

dclaw is a container-native multi-agent platform. It runs AI agents inside mandatory Docker sandboxes with per-agent isolation, fleet management, and independently versioned channel plugins. The architecture splits cleanly into a Go control plane (the dclaw daemon) and a Node.js data plane (pi-mono agent containers).

dclaw does NOT rewrite the agentic loop. It wraps pi-mono (`@mariozechner/pi-coding-agent`) with sandboxing, fleet management, and channel plugins. This is the same agent SDK that OpenClaw builds on.

## Core Principles

1. **Sandboxing is mandatory, not optional.** Every agent runs inside a container. There is no `sandbox.mode: "off"`. This is dclaw's core differentiator vs OpenClaw.
2. **Control plane / data plane split.** The daemon manages and routes; the agent containers think and act. These never mix.
3. **Independently versioned channel plugins.** Each channel adapter (Discord, Slack, etc.) is its own container with its own release cycle.
4. **pi-mono as the agent engine.** Don't reinvent the wheel. Use a proven, MIT-licensed agent SDK with multi-model support and 34.6k stars.

## Architecture Layers

### Control Plane: dclaw Daemon (Go)

The daemon runs on the host (not in a container). It is the orchestrator and traffic cop.

**What it does:**
- Fleet management (start, stop, monitor agent containers)
- Channel routing (receive messages from channel plugins, route to the correct agent)
- Quota enforcement (token budgets, rate limits, cost tracking)
- CLI interface (`dclaw up`, `dclaw status`, `dclaw logs`, etc.)
- Web UI (planned)

**What it does NOT do:**
- Run agent logic
- Make LLM API calls
- Execute tools (bash, file I/O, web fetch, etc.)

### Data Plane: Agent Containers (Node.js + pi-mono)

Each agent runs inside a Docker container built on Alpine + Node.js + pi-mono. The full agent (brain + all tools) lives inside the container.

**What it does:**
- Runs the complete agent loop (prompt -> LLM call -> tool execution -> response)
- Makes its own API calls to the LLM provider (Anthropic, OpenAI, etc.)
- Executes all tools locally inside the container (bash, file read/write, web fetch)
- Manages its own conversation state and session

**Each agent container gets:**
- Its own API key (passed via environment variable, never written to disk)
- A scoped filesystem (only the bind-mounted workspace is visible)
- A scoped network (per-agent iptables allowlist)
- Resource limits (CPU, memory, via Docker cgroup controls)

**Container image:**
- Base: Alpine Linux + Node.js
- Agent SDK: `@mariozechner/pi-coding-agent` from pi-mono
- Image size: ~200-300MB
- Built from `agent/Dockerfile`

**Agent types:**
- **Main agent** (always-on): receives channel messages, long-running, persistent state
- **Worker agents** (ephemeral): spawned per-task by the main agent via the daemon, destroyed when done

### Channel Layer: Plugin Containers

Each channel adapter is its own container, independently versioned and deployed.

**Wire protocol:** JSON-RPC 2.0 over Unix domain sockets

**v1:** Discord only. Additional channel plugins (Slack, Teams, WhatsApp, web chat) are added as independent containers later, with no changes required to the daemon or agent containers.

**Plugin responsibilities:**
- Connect to the external service (Discord API, Slack API, etc.)
- Translate platform-specific events into dclaw wire protocol messages
- Route responses back to the originating channel/thread

## Sandboxing Model

### What's Sandboxed

Every agent runs inside a mandatory Docker container. There is no option to run agents on bare metal.

| Layer | Sandbox boundary |
|---|---|
| **Agent loop** (LLM API calls) | Inside container. Agent makes its own API calls. |
| **Tool execution** (bash, file, web) | Inside container. All tools execute locally. |
| **Network egress** | Per-agent iptables allowlist. Only whitelisted endpoints reachable. |
| **Filesystem access** | Only the bind-mounted workspace directory is visible. |
| **Credentials** | Per-agent API key via environment variable. Never written to disk. |
| **Resources** | CPU and memory limits via Docker cgroup controls. |

### Comparison with OpenClaw

| Dimension | OpenClaw | dclaw |
|---|---|---|
| Sandbox scope | Optional, bash-only | Mandatory, full-agent |
| Agent loop | Runs on bare metal | Runs inside container |
| File operations | Bare metal | Inside container |
| Network access | Bare metal | Inside container, per-agent allowlist |
| Agent isolation | All agents share one process | Each agent has its own container |
| Blast radius | One compromise = full system compromise | One compromise = one agent only |

### Threat Model

**Prompt injection:** An attacker who compromises an agent via prompt injection is confined to that one container. They cannot access other agents, the host filesystem, or the daemon.

**Credential theft:** Only one agent's API key is exposed per container. Other agents' keys, the daemon's credentials, and host secrets are not accessible.

**Filesystem damage:** Damage is limited to the workspace mount for that agent. The host filesystem and other agents' workspaces are not reachable.

**Network exfiltration:** Blocked by per-agent iptables allowlist. The agent can only reach endpoints explicitly whitelisted in its configuration.

## Dependencies

### pi-mono

- **What:** TypeScript agent SDK by Mario Zechner
- **Repository:** [github.com/badlogic/pi-mono](https://github.com/badlogic/pi-mono)
- **License:** MIT
- **Stars:** 34.6k
- **What we use:** `@mariozechner/pi-coding-agent` (agent loop, tool execution, session management)
- **Why this:** Proven in production, same engine OpenClaw uses, multi-model support (Claude, GPT, Gemini, etc.), active development, MIT license
- **Future option:** Port the agent loop to Go if container image size (~200-300MB) or startup latency becomes a real problem. This is Option C from the architecture evaluation — not a v1 concern. Node.js + pi-mono inside containers is fine for now.

### OpenClaw (Reference, Not Dependency)

dclaw does NOT depend on OpenClaw at runtime or build time.

OpenClaw's codebase serves as a reference implementation for:
- Gateway architecture patterns (`src/gateway/`)
- Channel adapter patterns (`extensions/discord/`)
- Sandbox integration patterns (`src/agents/sandbox/`)

## Build Phases

### Phase 1: One Agent in a Container (Weeks 1-3)

**Goal:** Prove that pi-mono works inside a Docker container with full tool sandboxing.

**Deliverables:**
- `agent/Dockerfile`: Alpine + Node.js + `@mariozechner/pi-coding-agent`
- Thin wrapper script that starts the agent with a system prompt
- Run command: `docker run -e ANTHROPIC_API_KEY=... -v $(pwd):/workspace dclaw-agent:dev "prompt"`

**Success criteria:**
- pi-mono agent loop runs inside Docker
- Tools (bash, file I/O) are sandboxed to the container
- Workspace is scoped to the bind mount
- Agent can complete a coding task end-to-end

### Phase 2: Daemon + Fleet Management (Weeks 4-9)

**Goal:** Build the control plane and wire it to agent containers and a Discord channel plugin.

**Deliverables:**
- dclaw daemon (Go): fleet manager, channel router, quota enforcement
- Wire protocol implementation (JSON-RPC 2.0 over Unix domain sockets)
- Discord channel plugin (one container)
- End-to-end flow: user messages on Discord -> daemon routes -> agent container processes -> response on Discord

### Phase 3: Operational Surface (Weeks 10-16)

**Goal:** Make dclaw operable and configurable for real deployments.

**Deliverables:**
- dclaw CLI (`up`, `status`, `logs`, `upgrade`, `rollback`)
- `fleet.yaml` declarative configuration
- Plugin versioning conventions
- Documentation site

### Phase 4: Polish and Distribution (Weeks 17-24)

**Goal:** Production readiness and public launch.

**Deliverables:**
- Web dashboard
- Additional channel plugins (Slack, Teams, etc.)
- Egress audit log
- Public launch
