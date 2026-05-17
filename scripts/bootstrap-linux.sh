#!/usr/bin/env bash
# Bootstrap a fresh Linux host for dclaw, then hand off to scripts/install.sh.
#
# This script owns OS-level setup and may use sudo. The dclaw source installer
# remains non-sudo and user-local.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

DRY_RUN=0
SKIP_DCLAW_INSTALL=0
INSTALL_ARGS=()

usage() {
  cat <<'EOF'
Bootstrap a Linux host for dclaw, then run scripts/install.sh.

Usage:
  scripts/bootstrap-linux.sh [options]

Options:
  --bin-dir <dir>          Forwarded to scripts/install.sh
  --workspace-root <dir>   Forwarded to scripts/install.sh
  --agent-tag <ref>        Forwarded to scripts/install.sh
  --start-daemon           Forwarded to scripts/install.sh
  --skip-dclaw-install     Install OS prerequisites only
  --dry-run                Print commands without running them
  -h, --help               Show this help

Supported OS package flows:
  Fedora / RHEL-like:      dnf + moby-engine/docker-cli/docker-buildx
  Debian / Ubuntu-like:    apt-get + docker.io/docker-buildx
  Arch / Manjaro-like:     pacman + docker/docker-buildx
  openSUSE-like:           zypper + docker

Dry-run on non-Linux hosts defaults to Fedora command generation. Override
with DCLAW_BOOTSTRAP_DISTRO=ubuntu|debian|fedora|arch|opensuse.
EOF
}

log() {
  printf '\n==> %s\n' "$*"
}

warn() {
  printf 'WARN: %s\n' "$*" >&2
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

sudo_run() {
  if (( EUID == 0 )); then
    run "$@"
    return 0
  fi
  if (( DRY_RUN )); then
    run sudo "$@"
    return 0
  fi
  if ! command -v sudo >/dev/null 2>&1; then
    die "sudo is required for OS package/service setup"
  fi
  run sudo "$@"
}

cmd_exists() {
  command -v "$1" >/dev/null 2>&1
}

require_pkg_mgr() {
  if cmd_exists "$1"; then
    return 0
  fi
  if (( DRY_RUN )); then
    warn "$1 not found in dry-run; printing commands anyway"
    return 0
  fi
  die "$1 not found"
}

quoted_args() {
  local out=()
  local arg
  for arg in "$@"; do
    out+=("$(printf '%q' "$arg")")
  done
  printf '%s' "${out[*]}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin-dir|--workspace-root|--agent-tag)
      [[ $# -ge 2 ]] || die "$1 requires a value"
      INSTALL_ARGS+=("$1" "$2")
      shift 2
      ;;
    --start-daemon)
      INSTALL_ARGS+=("$1")
      shift
      ;;
    --skip-dclaw-install)
      SKIP_DCLAW_INSTALL=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      INSTALL_ARGS+=("--dry-run")
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

detect_distro() {
  OS_ID=""
  OS_ID_LIKE=""

  if [[ "$(uname -s)" != "Linux" ]]; then
    if (( DRY_RUN )); then
      OS_ID="${DCLAW_BOOTSTRAP_DISTRO:-fedora}"
      warn "non-Linux dry-run; using distro=$OS_ID for command generation"
      return 0
    fi
    die "bootstrap-linux.sh must run on Linux"
  fi

  if [[ -r /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    OS_ID="${ID:-}"
    OS_ID_LIKE="${ID_LIKE:-}"
    return 0
  fi

  if (( DRY_RUN )); then
    OS_ID="${DCLAW_BOOTSTRAP_DISTRO:-fedora}"
    warn "missing /etc/os-release in dry-run; using distro=$OS_ID"
    return 0
  fi

  die "cannot detect Linux distribution: /etc/os-release not found"
}

distro_matches() {
  local needle="$1"
  [[ "$OS_ID" == "$needle" ]] || [[ " $OS_ID_LIKE " == *" $needle "* ]]
}

install_packages() {
  if distro_matches fedora || distro_matches rhel || distro_matches centos; then
    require_pkg_mgr dnf
    sudo_run dnf install -y golang make git moby-engine docker-cli docker-buildx
    return 0
  fi

  if distro_matches debian || distro_matches ubuntu; then
    require_pkg_mgr apt-get
    sudo_run apt-get update
    sudo_run apt-get install -y golang-go make git docker.io docker-buildx ca-certificates
    return 0
  fi

  if distro_matches arch || distro_matches manjaro; then
    require_pkg_mgr pacman
    sudo_run pacman -Sy --needed --noconfirm go make git docker docker-buildx
    return 0
  fi

  if distro_matches opensuse || distro_matches suse || distro_matches sles; then
    require_pkg_mgr zypper
    sudo_run zypper --non-interactive install go make git docker
    return 0
  fi

  return 1
}

print_manual_prereqs() {
  cat >&2 <<EOF
Unsupported Linux distribution: ${OS_ID:-unknown}

Install these prerequisites with your distro package manager, then rerun
scripts/bootstrap-linux.sh or scripts/install.sh:

  Go 1.25+
  make
  git
  Docker Engine-compatible daemon and CLI

If those tools are already installed, this script will continue.
EOF
}

missing_prereq_count() {
  local missing=0
  for cmd in go make git docker; do
    if ! cmd_exists "$cmd"; then
      warn "missing prerequisite: $cmd"
      missing=$((missing + 1))
    fi
  done
  return "$missing"
}

start_docker_service() {
  if (( DRY_RUN )); then
    sudo_run systemctl enable --now docker
    return 0
  fi

  if cmd_exists systemctl && [[ -d /run/systemd/system ]]; then
    if systemctl list-unit-files docker.service >/dev/null 2>&1; then
      sudo_run systemctl enable --now docker
      return 0
    fi
    warn "systemd is present, but docker.service was not found"
    return 0
  fi

  if cmd_exists service; then
    if sudo_run service docker start; then
      return 0
    fi
    warn "service docker start failed; continuing to docker info check"
    return 0
  fi

  warn "no systemd/service manager detected; start Docker manually if needed"
}

is_orbstack_vm() {
  if uname -r | grep -qi 'orbstack'; then
    return 0
  fi
  [[ -e /etc/profile.d/000-orbstack.sh || -e /etc/profile.d/999-orbstack.sh ]]
}

current_user() {
  if [[ -n "${SUDO_USER:-}" && "${SUDO_USER:-}" != "root" ]]; then
    printf '%s\n' "$SUDO_USER"
    return 0
  fi
  id -un
}

ensure_subid_entry() {
  local file="$1"
  local user="$2"
  local entry="$user:100000:65536"

  if [[ -r "$file" ]] && grep -q "^$user:" "$file"; then
    return 0
  fi

  if (( DRY_RUN )); then
    run sudo sh -c "grep -q '^$user:' '$file' 2>/dev/null || echo '$entry' >> '$file'"
    return 0
  fi

  sudo_run sh -c "grep -q '^$user:' '$file' 2>/dev/null || echo '$entry' >> '$file'"
}

setup_orbstack_rootless_docker() {
  if ! is_orbstack_vm; then
    return 1
  fi

  if ! cmd_exists dockerd-rootless-setuptool.sh; then
    if (( DRY_RUN )); then
      warn "dockerd-rootless-setuptool.sh not found in dry-run; printing rootful Docker commands"
    else
      warn "OrbStack VM detected, but Docker rootless tooling is not installed; using rootful Docker"
    fi
    return 1
  fi

  local user
  user="$(current_user)"

  log "Configuring rootless Docker for OrbStack"
  ensure_subid_entry /etc/subuid "$user"
  ensure_subid_entry /etc/subgid "$user"

  run dockerd-rootless-setuptool.sh install --force
  if cmd_exists loginctl; then
    sudo_run loginctl enable-linger "$user"
  fi
  run docker context use rootless
  if (( DRY_RUN )); then
    run docker context inspect rootless --format '{{.Endpoints.docker.Host}}'
    return 0
  fi

  local docker_host
  docker_host="$(docker context inspect rootless --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)"
  if [[ -n "$docker_host" && "$docker_host" != "<no value>" ]]; then
    export DOCKER_HOST="$docker_host"
    log "Using rootless Docker host $DOCKER_HOST"
  fi
  return 0
}

ensure_docker_group() {
  if getent group docker >/dev/null 2>&1; then
    return 0
  fi
  sudo_run groupadd docker
}

verify_docker_access() {
  if (( DRY_RUN )); then
    run docker info
    return 0
  fi

  if docker info >/dev/null 2>&1; then
    return 0
  fi

  if sudo docker info >/dev/null 2>&1; then
    local user
    local forwarded_args
    user="$(current_user)"
    forwarded_args="$(quoted_args "${INSTALL_ARGS[@]}")"
    ensure_docker_group
    sudo_run usermod -aG docker "$user"
    cat >&2 <<EOF

Docker is running, but user '$user' cannot access the Docker socket yet.
I added '$user' to the docker group.

Refresh group membership, then rerun the dclaw install step:

  newgrp docker
  scripts/bootstrap-linux.sh $forwarded_args

Or, if OS prerequisites are already done:

  newgrp docker
  scripts/install.sh $forwarded_args

EOF
    exit 0
  fi

  die "Docker is not reachable. Start Docker, then rerun scripts/bootstrap-linux.sh"
}

verify_buildx() {
  if (( DRY_RUN )); then
    run docker buildx version
    return 0
  fi

  if docker buildx version >/dev/null 2>&1; then
    return 0
  fi

  die "Docker Buildx is not available. Install the docker-buildx package, then rerun scripts/bootstrap-linux.sh"
}

verify_basic_tools() {
  if (( DRY_RUN )); then
    run go version
    run make --version
    run git --version
    run docker --version
    return 0
  fi

  local missing=0
  for cmd in go make git docker; do
    if ! cmd_exists "$cmd"; then
      warn "missing prerequisite after package install: $cmd"
      missing=$((missing + 1))
    fi
  done
  if (( missing > 0 )); then
    die "missing $missing required prerequisite(s)"
  fi
}

main() {
  cd "$ROOT_DIR"

  detect_distro
  log "Detected Linux distribution: ${OS_ID:-unknown}${OS_ID_LIKE:+ (like $OS_ID_LIKE)}"

  log "Installing OS prerequisites"
  if ! install_packages; then
    print_manual_prereqs
    if (( DRY_RUN )); then
      warn "unsupported distro in dry-run; skipping prerequisite enforcement"
      return 0
    fi
    if ! missing_prereq_count; then
      die "install prerequisites manually, then rerun this script"
    fi
  fi

  log "Checking required tools"
  verify_basic_tools

  log "Starting Docker"
  if ! setup_orbstack_rootless_docker; then
    start_docker_service
  fi

  log "Checking Docker access"
  verify_docker_access

  log "Checking Docker Buildx"
  verify_buildx

  if (( SKIP_DCLAW_INSTALL )); then
    log "Skipping dclaw source install"
    return 0
  fi

  log "Running dclaw source installer"
  run "$ROOT_DIR/scripts/install.sh" "${INSTALL_ARGS[@]}"
}

main
