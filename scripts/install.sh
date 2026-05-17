#!/usr/bin/env bash
# Install dclaw from a source checkout.
#
# This script intentionally avoids sudo and installs into a user-writable bin
# directory by default. Pass --bin-dir /usr/local/bin if you want a system path
# and have already arranged write permissions.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

INSTALL_BIN_DIR="${DCLAW_INSTALL_BIN_DIR:-$HOME/.local/bin}"
WORKSPACE_ROOT="${DCLAW_WORKSPACE_ROOT:-$HOME/dclaw}"
AGENT_TAG="${DCLAW_AGENT_TAG:-dclaw-agent:v0.1}"

BUILD_AGENT_IMAGE=1
RUN_INIT=1
RUN_DOCTOR=1
START_DAEMON=0
DRY_RUN=0

usage() {
  cat <<'EOF'
Install dclaw from source.

Usage:
  scripts/install.sh [options]

Options:
  --bin-dir <dir>          Install dclaw and dclawd here
                           (default: $DCLAW_INSTALL_BIN_DIR or $HOME/.local/bin)
  --workspace-root <dir>   First-run workspace-root for `dclaw init --yes`
                           (default: $DCLAW_WORKSPACE_ROOT or $HOME/dclaw)
  --agent-tag <ref>        Agent image tag to build
                           (default: $DCLAW_AGENT_TAG or dclaw-agent:v0.1)
  --skip-agent-image       Do not build the dclaw-agent Docker image
  --skip-init              Do not run `dclaw init`
  --skip-doctor            Do not run `dclaw doctor`
  --start-daemon           Start dclawd after install
  --dry-run                Print commands without running them
  -h, --help               Show this help

Examples:
  scripts/install.sh
  scripts/install.sh --bin-dir "$HOME/bin" --workspace-root "$HOME/dclaw-agents"
  scripts/install.sh --skip-agent-image --skip-doctor
EOF
}

log() {
  printf '\n==> %s\n' "$*"
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

run() {
  if (( DRY_RUN )); then
    printf '+'
    printf ' %q' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

check_cmd() {
  if command -v "$1" >/dev/null 2>&1; then
    return 0
  fi
  if (( DRY_RUN )); then
    printf 'WARN: missing command in dry-run: %s\n' "$1" >&2
    return 0
  fi
  die "missing required command: $1"
}

path_contains() {
  case ":$PATH:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin-dir)
      [[ $# -ge 2 ]] || die "--bin-dir requires a value"
      INSTALL_BIN_DIR="$2"
      shift 2
      ;;
    --workspace-root)
      [[ $# -ge 2 ]] || die "--workspace-root requires a value"
      WORKSPACE_ROOT="$2"
      shift 2
      ;;
    --agent-tag)
      [[ $# -ge 2 ]] || die "--agent-tag requires a value"
      AGENT_TAG="$2"
      shift 2
      ;;
    --skip-agent-image)
      BUILD_AGENT_IMAGE=0
      shift
      ;;
    --skip-init)
      RUN_INIT=0
      shift
      ;;
    --skip-doctor)
      RUN_DOCTOR=0
      shift
      ;;
    --start-daemon)
      START_DAEMON=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$INSTALL_BIN_DIR" ]] || die "--bin-dir cannot be empty"
[[ -n "$WORKSPACE_ROOT" ]] || die "--workspace-root cannot be empty"
[[ -n "$AGENT_TAG" ]] || die "--agent-tag cannot be empty"
CONFIGURED_WORKSPACE_ROOT="$WORKSPACE_ROOT"

cd "$ROOT_DIR"

log "Checking prerequisites"
check_cmd go
check_cmd install
if (( BUILD_AGENT_IMAGE )); then
  check_cmd docker
fi
if ! command -v make >/dev/null 2>&1; then
  if (( DRY_RUN )); then
    printf 'WARN: missing command in dry-run: make\n' >&2
  else
    die "missing required command: make (the installer uses the repo Makefile for version-stamped binaries)"
  fi
fi

log "Building dclaw binaries"
run make build

log "Installing binaries into $INSTALL_BIN_DIR"
run install -d "$INSTALL_BIN_DIR"
run install -m 0755 "$ROOT_DIR/bin/dclaw" "$INSTALL_BIN_DIR/dclaw"
run install -m 0755 "$ROOT_DIR/bin/dclawd" "$INSTALL_BIN_DIR/dclawd"

log "Verifying installed binaries"
run "$INSTALL_BIN_DIR/dclaw" version
run "$INSTALL_BIN_DIR/dclawd" --version

if (( BUILD_AGENT_IMAGE )); then
  log "Building agent image $AGENT_TAG"
  run env DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}" DCLAW_AGENT_TAG="$AGENT_TAG" "$ROOT_DIR/agent/build.sh"
else
  log "Skipping agent image build"
fi

if (( RUN_INIT )); then
  log "Configuring workspace-root"
  run "$INSTALL_BIN_DIR/dclaw" init --yes --workspace-root "$WORKSPACE_ROOT"
  run "$INSTALL_BIN_DIR/dclaw" config get workspace-root
  if (( ! DRY_RUN )); then
    CONFIGURED_WORKSPACE_ROOT="$("$INSTALL_BIN_DIR/dclaw" config get workspace-root)"
  fi
else
  log "Skipping dclaw init"
fi

if (( START_DAEMON )); then
  log "Starting daemon"
  run "$INSTALL_BIN_DIR/dclaw" daemon start
else
  log "Not starting daemon"
fi

if (( RUN_DOCTOR )); then
  log "Running doctor"
  run "$INSTALL_BIN_DIR/dclaw" doctor
else
  log "Skipping doctor"
fi

cat <<EOF

dclaw install complete.

Installed:
  $INSTALL_BIN_DIR/dclaw
  $INSTALL_BIN_DIR/dclawd

Workspace root:
  $CONFIGURED_WORKSPACE_ROOT

EOF

if ! path_contains "$INSTALL_BIN_DIR"; then
  cat <<EOF
Add this to your shell profile if it is not already present:

  export PATH="$INSTALL_BIN_DIR:\$PATH"

EOF
fi

cat <<EOF
Next commands:
EOF

if (( ! START_DAEMON )); then
  cat <<EOF
  dclaw daemon start
EOF
fi

cat <<EOF
  dclaw doctor
  mkdir -p "$CONFIGURED_WORKSPACE_ROOT/foo"
  dclaw agent create foo --image="$AGENT_TAG" --workspace="$CONFIGURED_WORKSPACE_ROOT/foo"
  dclaw agent start foo
  dclaw agent chat foo --one-shot "hello"

EOF
