# dclaw-agent

A Docker container that runs pi-mono's coding agent. Supports two modes:
persistent (default) for `dclaw` chat/exec dispatch, and one-shot for direct
prompt-in/response-out runs.

**Image contract:** the container's PID 1 must be long-running. dclaw execs
into running containers to dispatch chat messages via `node /app/run.mjs`, so
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
# then: docker exec my-agent node /app/run.mjs "your prompt here"
```

One-shot mode (overrides `CMD` to run the wrapper and exit):

```bash
docker run --rm \
  -e ANTHROPIC_API_KEY=sk-ant-... \
  -v "$(pwd):/workspace" \
  dclaw-agent:v0.1 \
  node /app/run.mjs "your prompt here"
```

The default path uses pi-mono, so the agent can read, write, edit, and run bash
commands inside the container. beta.3.1 also supports a lightweight DeepSeek
chat path for prompt/response smoke tests:

```bash
docker run --rm \
  -e DEEPSEEK_API_KEY=sk-... \
  -v "$(pwd):/workspace" \
  dclaw-agent:v0.1 \
  node /app/run.mjs "reply with only the word: OK"
```

The DeepSeek path calls `https://api.deepseek.com/chat/completions` directly
with `DEEPSEEK_MODEL=deepseek-v4-flash` by default. It is simple chat, not a full
pi-mono coding-agent tool loop.

The container cannot see anything outside `/workspace` except its own rootfs.
Per-agent network egress allowlist exists on the wire protocol but is not yet
enforced — see "Still open" below.

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

## Current scope (as of v0.3.0-beta.3.1)

Fully wired:

- Multi-turn chat via `dclaw agent chat` (alpha.3).
- Streaming output (alpha.3).
- DeepSeek-backed simple chat for `dclaw agent chat` smoke/history checks
  (beta.3.1).
- Workspace bind-mount at `/workspace` (phase-1 baseline).
- Daemon-enforced container posture (beta.2): `CapDrop: ALL`, `no-new-privileges`, Docker default seccomp (auto-applied; not pinned), `ReadonlyRootfs: true` with `/tmp` + `/run` tmpfs, `User: 1000:1000`, `PidsLimit: 256`, docker.sock denylist.

Still open:

- Per-agent network egress allowlist (`protocol.AgentCreateParams.EgressAllowlist` exists but unwired).
- Worker spawning (parallel sub-agents).
- Channel integration (daemon-managed agent-to-agent message channels).
