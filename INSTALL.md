# Install dclaw from source

This guide installs dclaw from a fresh source checkout. It builds and installs
the two host binaries (`dclaw`, `dclawd`), builds the default agent image
(`dclaw-agent:v0.1`), and configures the first-run workspace root.

## Prerequisites

- Go 1.25+
- `make`
- Docker running locally
- A shell with standard Unix tools, including `install`

The default install is user-local and does not use `sudo`.

## One-command install

From the repository root:

```bash
scripts/install.sh
```

Defaults:

- Installs binaries to `$HOME/.local/bin`.
- Builds `dclaw-agent:v0.1`.
- Runs `dclaw init --yes --workspace-root "$HOME/dclaw"`.
- Runs `dclaw doctor`.
- Does not start the daemon unless `--start-daemon` is passed.

If `$HOME/.local/bin` is not on your `PATH`, add this to your shell profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Common options

```bash
# Pick a different binary directory.
scripts/install.sh --bin-dir "$HOME/bin"

# Pick a different workspace allow-root.
scripts/install.sh --workspace-root "$HOME/dclaw-agents"

# Install binaries and config only; skip Docker image build.
scripts/install.sh --skip-agent-image

# Install and start the daemon immediately.
scripts/install.sh --start-daemon

# Show what would run without changing the machine.
scripts/install.sh --dry-run
```

Environment variables are also supported:

```bash
DCLAW_INSTALL_BIN_DIR="$HOME/bin" \
DCLAW_WORKSPACE_ROOT="$HOME/dclaw-agents" \
DCLAW_AGENT_TAG="dclaw-agent:v0.1" \
scripts/install.sh
```

## Verify

After install:

```bash
dclaw version
dclawd --version
dclaw doctor
```

Start the daemon and create a first agent:

```bash
dclaw daemon start
dclaw agent create foo --image=dclaw-agent:v0.1 --workspace="$HOME/dclaw/foo"
dclaw agent start foo
dclaw agent chat foo --one-shot "hello"
```

Note: bash does not expand `~` inside `--workspace=...`; use `$HOME/...` or an
absolute path.

## What the installer does not do

- It does not install Go or Docker.
- It does not use `sudo`.
- It does not edit shell profile files.
- It does not start the daemon by default.
