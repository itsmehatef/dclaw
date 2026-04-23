# dclaw-agent (Phase 1)

A Docker container that runs pi-mono's coding agent. Supports two modes:
persistent (default) for `dclaw` chat/exec dispatch, and one-shot for direct
prompt-in/response-out runs.

**Image contract:** the container's PID 1 must be long-running. dclaw execs
into running containers to dispatch chat messages via `pi -p --no-session`, so
the default `CMD` is `tail -f /dev/null` — tini stays as PID 1 for signal
handling and zombie reaping, and the container stays up until `dclaw agent
stop` (or `docker stop`). A container whose entrypoint exits immediately will
be rejected by `dclaw agent start` with a clear error.

## Build

```bash
./build.sh
```

Produces `dclaw-agent:v0.1` locally. First build takes ~2 minutes (apt + npm); subsequent builds use BuildKit cache.

## Run

Persistent mode (what `dclaw agent start` uses):

```bash
docker run -d --name my-agent \
  -e ANTHROPIC_API_KEY=sk-ant-... \
  -v "$(pwd):/workspace" \
  dclaw-agent:v0.1
# then: docker exec my-agent pi -p --no-session "your prompt here"
```

One-shot mode (overrides `CMD` to run pi and exit):

```bash
docker run --rm \
  -e ANTHROPIC_API_KEY=sk-ant-... \
  -v "$(pwd):/workspace" \
  dclaw-agent:v0.1 \
  node /app/run.mjs "your prompt here"
```

The agent can read, write, edit, and run bash commands inside the container. It cannot see anything outside `/workspace` except the container's own rootfs. It can reach the Anthropic API but nothing else (in Phase 1; firewall allowlist is Phase 2).

## Smoke test

```bash
ANTHROPIC_API_KEY=sk-... ./smoke-test.sh
```

Runs 4 end-to-end tests validating basic operation, sandboxing, workspace persistence, and network isolation.

## What's inside

- `node:22-bookworm-slim` base image
- `@mariozechner/pi-coding-agent` (pi-mono's headless coding agent)
- Runtime tools: bash, git, ripgrep, fd, jq, curl, tini
- Non-root `node` user (uid 1000)
- `tini` as PID 1 for signal handling
- `/workspace` as the only writable host-mounted directory

## Current scope (as of v0.3.0-beta.2-sandbox-hardening)

Fully wired:

- Multi-turn chat via `dclaw agent chat` (alpha.3).
- Streaming output (alpha.3).
- Workspace bind-mount at `/workspace` (phase-1 baseline).
- Daemon-enforced container posture (beta.2): `CapDrop: ALL`, `no-new-privileges`, `seccomp=default`, `ReadonlyRootfs: true` with `/tmp` + `/run` tmpfs, `User: 1000:1000`, `PidsLimit: 256`, docker.sock denylist.

Still open:

- Per-agent network egress allowlist (`protocol.AgentCreateParams.EgressAllowlist` exists but unwired).
- Worker spawning (parallel sub-agents).
- Channel integration (daemon-managed agent-to-agent message channels).
