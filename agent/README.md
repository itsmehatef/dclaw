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

## Known limitations (v0.1)

- Single-turn only (one prompt, one response)
- No conversation history (--no-session)
- No streaming output (buffered final text)
- No per-agent network allowlist (relies on Docker default bridge)
- No worker spawning (single agent per container)
- No channel integration (CLI input only)
- Workspace ownership is implicit — /workspace is created by WORKDIR when the container builds, owned by the node user because USER node precedes WORKDIR. If the upstream node:22-bookworm-slim image ever pre-creates /workspace as root, the agent will silently fail to write. Phase 2 will add an explicit runtime check.

All of the above are addressed in Phase 2+.
