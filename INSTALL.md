# Install dclaw from source

This guide installs dclaw from a fresh source checkout. It builds and installs
the two host binaries (`dclaw`, `dclawd`), builds the default agent image
(`dclaw-agent:v0.1`), and configures the first-run workspace root.

## Linux bootstrap

On Linux, use the bootstrap script first. It installs OS prerequisites with the
native package manager, starts Docker when possible, checks Docker socket
access, then calls `scripts/install.sh`.

Supported package-manager flows:

- Fedora / RHEL-like: `dnf` + `moby-engine` / `docker-cli` / `docker-buildx`
- Debian / Ubuntu-like: `apt-get` + `docker.io` / `docker-buildx`
- Arch / Manjaro-like: `pacman` + `docker` / `docker-buildx`
- openSUSE-like: `zypper` + `docker`

On OrbStack Linux machines, the bootstrap uses rootless Docker when the distro
package provides Docker's rootless setup tool. This avoids nested rootful
container failures on some OrbStack distro images while still using Docker
Engine-compatible tooling. The bootstrap exports the rootless `DOCKER_HOST` for
the install run; add the same export to your shell profile if Docker commands
outside the bootstrap do not find the daemon.

If `git` is not installed yet, install it before cloning this repo:

```bash
# Fedora / RHEL-like
sudo dnf install -y git

# Debian / Ubuntu-like
sudo apt-get update
sudo apt-get install -y git
```

Then clone and bootstrap:

```bash
git clone https://github.com/itsmehatef/dclaw.git
cd dclaw
scripts/bootstrap-linux.sh --start-daemon
```

Common bootstrap options:

```bash
scripts/bootstrap-linux.sh --workspace-root "$HOME/dclaw-agents"
scripts/bootstrap-linux.sh --bin-dir "$HOME/bin"
scripts/bootstrap-linux.sh --skip-dclaw-install
scripts/bootstrap-linux.sh --dry-run
```

If Docker works with `sudo` but not as your user, the bootstrap adds your user
to the `docker` group and stops. Refresh group membership, then run the dclaw
install step:

```bash
newgrp docker
scripts/install.sh --start-daemon
```

## Source installer prerequisites

- Go 1.25+
- `make`
- Docker running locally with Buildx available
- A shell with standard Unix tools, including `install`

The source installer is user-local and does not use `sudo`. Use it directly on
non-Linux systems, or after the Linux bootstrap has prepared the machine.

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
mkdir -p "$HOME/dclaw/foo"
dclaw agent create foo --image=dclaw-agent:v0.1 --workspace="$HOME/dclaw/foo"
dclaw agent start foo
dclaw agent chat foo --one-shot "hello"
```

Set `ANTHROPIC_API_KEY` or `ANTHROPIC_OAUTH_TOKEN` before `agent create` if
you want real LLM chat. dclaw automatically inherits those two keys into the
agent environment when they are present.

Note: bash does not expand `~` inside `--workspace=...`; use `$HOME/...` or an
absolute path.

## What the installer does not do

- It does not install Go or Docker.
- It does not use `sudo`.
- It does not edit shell profile files.
- It does not start the daemon by default.
