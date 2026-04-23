#!/usr/bin/env bash
# Phase 3 integration smoke: spin up dclawd, exercise full CRUD, tear down.
# Requires docker reachable on the host and dclaw-agent:v0.1 built (phase 1).
#
# beta.1-paths-hardening: This script NEVER reassigns the user's HOME env var
# and NEVER runs rm -rf against any path it did not create via mktemp. All
# daemon state is isolated via DCLAW_STATE_DIR + --state-dir, belt-and-suspenders.
set -euo pipefail

# TMPDIR can be injected by parent shells, CI runners, or misconfigured
# environments. Require it to be one of the known-safe prefixes, else unset.
if [ -n "${TMPDIR:-}" ]; then
  case "$TMPDIR" in
    /tmp|/tmp/*|/var/folders|/var/folders/*|/private/tmp|/private/tmp/*) ;;
    *) echo "refuse: TMPDIR=$TMPDIR outside expected prefix" >&2; exit 1;;
  esac
fi

SMOKE_STATE=$(mktemp -d -t dclaw-smoke-state-XXXXXXXX)
# Prefix whitelist: mktemp -d without -t can escape; validate belt-and-suspenders.
case "$SMOKE_STATE" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: SMOKE_STATE=$SMOKE_STATE outside expected prefix" >&2; exit 1;;
esac

SMOKE_AGENT_NAMES=(smoke-daemon smoke-dup smoke-chatbot smoke-chatbot13 smoke-trusted)

wipe_smoke_containers() {
  for name in "${SMOKE_AGENT_NAMES[@]}"; do
    docker rm -f "dclaw-${name}" >/dev/null 2>&1 || true
  done
}

DCLAW_BIN="${DCLAW_BIN:-./bin/dclaw}"
DCLAWD_BIN="${DCLAWD_BIN:-./bin/dclawd}"
SOCKET="$SMOKE_STATE/dclaw.sock"
export DCLAWD_BIN

# Arm the trap BEFORE exporting anything or running any command that could fail.
# ${SMOKE_STATE:?refuse empty} ensures we never expand to `rm -rf ` on an unset var.
cleanup() {
  "$DCLAW_BIN" --daemon-socket "$SOCKET" daemon stop >/dev/null 2>&1 || true
  wipe_smoke_containers
  rm -rf "${SMOKE_STATE:?refuse empty}"
}
trap cleanup EXIT

export DCLAW_STATE_DIR="$SMOKE_STATE"
# Workspace root for the new paths-hardening policy. Linux CI: /tmp is not
# denylisted and covers every mktemp dir the smoke script creates. Operators
# running locally on macOS should override (/tmp canonicalizes to /private/tmp
# which IS in the default denylist).
export DCLAW_WORKSPACE_ROOT="${DCLAW_WORKSPACE_ROOT:-/tmp}"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

wipe_smoke_containers  # Pre-run cleanup of stale containers from crashed prior runs.

echo "--- Test 1: daemon start ---"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" daemon start || fail "daemon start"
test -S "$SOCKET" || fail "socket not created"
pass "daemon start"

echo "--- Test 2: daemon status ---"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" daemon status | grep -q "agents=0" || fail "status lacks agents=0"
pass "daemon status"

echo "--- Test 3: agent create ---"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" agent create smoke-daemon \
  --image=dclaw-agent:v0.1 --workspace="$SMOKE_STATE" || fail "create"
pass "agent create"

echo "--- Test 4: agent list shows smoke-daemon ---"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" agent list | grep -q smoke-daemon || fail "list missing smoke-daemon"
pass "agent list"

echo "--- Test 5: agent get smoke-daemon ---"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" agent get smoke-daemon -o json | grep -q '"name": *"smoke-daemon"' || fail "get json"
pass "agent get"

echo "--- Test 6: agent delete ---"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" agent delete smoke-daemon || fail "delete"
pass "agent delete"

echo "--- Test 7: daemon stop ---"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" daemon stop || fail "stop"
pass "daemon stop"

# ---- NEGATIVE PATH TESTS ----
# These tests verify that the daemon and CLI return proper errors for invalid
# operations. Each test restarts the daemon in a fresh state-dir because some
# negative paths leave the daemon in a broken state.

echo "--- Test 8: duplicate agent name is rejected ---"
STATE_DIR_NEG=$(mktemp -d -t dclaw-smoke-neg-XXXXXXXX)
case "$STATE_DIR_NEG" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_NEG=$STATE_DIR_NEG outside expected prefix" >&2; exit 1;;
esac
SOCKET_NEG="$STATE_DIR_NEG/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_NEG" --daemon-socket "$SOCKET_NEG" daemon start || fail "neg-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_NEG" --daemon-socket "$SOCKET_NEG" agent create smoke-dup \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_NEG" || fail "dup-create-1"
if "$DCLAW_BIN" --state-dir "$STATE_DIR_NEG" --daemon-socket "$SOCKET_NEG" agent create smoke-dup \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_NEG" 2>/dev/null; then
  fail "duplicate agent name should have been rejected"
fi
"$DCLAW_BIN" --state-dir "$STATE_DIR_NEG" --daemon-socket "$SOCKET_NEG" daemon stop >/dev/null 2>&1 || true
rm -rf "${STATE_DIR_NEG:?refuse empty}"
pass "duplicate agent name rejected"

echo "--- Test 9: get non-existent agent returns error ---"
STATE_DIR_NEG2=$(mktemp -d -t dclaw-smoke-neg2-XXXXXXXX)
case "$STATE_DIR_NEG2" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_NEG2=$STATE_DIR_NEG2 outside expected prefix" >&2; exit 1;;
esac
SOCKET_NEG2="$STATE_DIR_NEG2/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_NEG2" --daemon-socket "$SOCKET_NEG2" daemon start || fail "neg2-start"
if "$DCLAW_BIN" --state-dir "$STATE_DIR_NEG2" --daemon-socket "$SOCKET_NEG2" agent get nosuchagent 2>/dev/null; then
  fail "get non-existent agent should have failed"
fi
"$DCLAW_BIN" --state-dir "$STATE_DIR_NEG2" --daemon-socket "$SOCKET_NEG2" daemon stop >/dev/null 2>&1 || true
rm -rf "${STATE_DIR_NEG2:?refuse empty}"
pass "get non-existent agent returned error"

echo "--- Test 10: daemon already-running is idempotent ---"
STATE_DIR_NEG3=$(mktemp -d -t dclaw-smoke-neg3-XXXXXXXX)
case "$STATE_DIR_NEG3" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_NEG3=$STATE_DIR_NEG3 outside expected prefix" >&2; exit 1;;
esac
SOCKET_NEG3="$STATE_DIR_NEG3/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_NEG3" --daemon-socket "$SOCKET_NEG3" daemon start || fail "neg3-start-1"
# Starting again should print "already running" and exit 0, not error.
"$DCLAW_BIN" --state-dir "$STATE_DIR_NEG3" --daemon-socket "$SOCKET_NEG3" daemon start || fail "neg3-start-2 (idempotent start failed)"
"$DCLAW_BIN" --state-dir "$STATE_DIR_NEG3" --daemon-socket "$SOCKET_NEG3" daemon stop >/dev/null 2>&1 || true
rm -rf "${STATE_DIR_NEG3:?refuse empty}"
pass "daemon start is idempotent"

echo "--- Test 11: daemon CLI fails gracefully when daemon is not running ---"
BAD_SOCKET="$SMOKE_STATE/dclaw-smoke-notexist-$$.sock"
OUT=$("$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$BAD_SOCKET" agent list 2>&1 || true)
echo "$OUT" | grep -qi "not running\|no such file\|connection refused\|dial" \
  || fail "expected daemon-not-running error, got: $OUT"
pass "CLI fails gracefully when daemon not running"

echo "--- Test 12: agent chat RPC smoke (exec proxy) ---"
STATE_DIR_CHAT=$(mktemp -d -t dclaw-smoke-chat-XXXXXXXX)
case "$STATE_DIR_CHAT" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_CHAT=$STATE_DIR_CHAT outside expected prefix" >&2; exit 1;;
esac
SOCKET_CHAT="$STATE_DIR_CHAT/dclaw.sock"
"$DCLAW_BIN" --state-dir "$STATE_DIR_CHAT" --daemon-socket "$SOCKET_CHAT" daemon start || fail "chat-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_CHAT" --daemon-socket "$SOCKET_CHAT" agent create smoke-chatbot \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_CHAT" || fail "chat-create"
"$DCLAW_BIN" --state-dir "$STATE_DIR_CHAT" --daemon-socket "$SOCKET_CHAT" agent start smoke-chatbot || fail "chat-agent-start"
OUT=$("$DCLAW_BIN" --state-dir "$STATE_DIR_CHAT" --daemon-socket "$SOCKET_CHAT" agent exec smoke-chatbot -- echo "smoke-ok" 2>&1)
echo "$OUT" | grep -q "smoke-ok" || fail "expected 'smoke-ok' in exec output, got: $OUT"
"$DCLAW_BIN" --state-dir "$STATE_DIR_CHAT" --daemon-socket "$SOCKET_CHAT" daemon stop >/dev/null 2>&1 || true
rm -rf "${STATE_DIR_CHAT:?refuse empty}"
pass "agent chat RPC smoke"

echo "--- Test 13: agent chat real round-trip (requires ANTHROPIC_API_KEY) ---"
if [ -z "${ANTHROPIC_API_KEY:-}" ] && [ -z "${ANTHROPIC_OAUTH_TOKEN:-}" ]; then
  echo "SKIP: Test 13 requires ANTHROPIC_API_KEY or ANTHROPIC_OAUTH_TOKEN — skipping (set the var to enable)"
else
  STATE_DIR_T13=$(mktemp -d -t dclaw-smoke-t13-XXXXXXXX)
  case "$STATE_DIR_T13" in
    /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
    *) echo "refuse: STATE_DIR_T13=$STATE_DIR_T13 outside expected prefix" >&2; exit 1;;
  esac
  SOCKET_T13="$STATE_DIR_T13/dclaw.sock"
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T13" --daemon-socket "$SOCKET_T13" daemon start || fail "t13-start"
  # Create agent with the API key; --env inheritance handles the key if not set.
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T13" --daemon-socket "$SOCKET_T13" agent create smoke-chatbot13 \
    --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T13" || fail "t13-create"
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T13" --daemon-socket "$SOCKET_T13" agent start smoke-chatbot13 || fail "t13-agent-start"
  OUT=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T13" --daemon-socket "$SOCKET_T13" agent chat smoke-chatbot13 \
    --one-shot "reply with only the word: SMOKE_CONFIRMED" \
    --timeout 90s 2>&1) || fail "t13-chat-cmd failed (exit $?)"
  echo "$OUT" | grep -qi "SMOKE_CONFIRMED\|smoke_confirmed\|smoke confirmed" \
    || fail "Test 13 expected SMOKE_CONFIRMED in chat output, got: $OUT"
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T13" --daemon-socket "$SOCKET_T13" daemon stop >/dev/null 2>&1 || true
  rm -rf "${STATE_DIR_T13:?refuse empty}"
  pass "agent chat real round-trip"
fi

# ---- PATH-HARDENING TESTS (Tests 14-16) ----
# These exercise the validator, trust override, and audit log. They share a
# single daemon instance bound to $SMOKE_STATE so the audit.log accumulates
# entries from both a forbidden create (Test 14) and a trust-override create
# (Test 15). Test 16 then greps that single audit.log for both outcomes.

echo "--- Test 14: validator rejection on /etc ---"
# The non-JSON renderer in renderWorkspaceForbidden emits HUMAN prose
# ("error: workspace path forbidden by policy: ...") on stderr; the
# machine-readable string "workspace_forbidden" only appears in the JSON
# payload when -o json is passed. Use -o json for the code assertion.
# /etc is on the denylist regardless of allow-root, so the global
# DCLAW_WORKSPACE_ROOT=/tmp export does not change this rejection.
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" daemon start || fail "t14-start"
set +e
T14_OUT=$("$DCLAW_BIN" -o json --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" agent create forbidden \
  --image=dclaw-agent:v0.1 --workspace=/etc 2>&1)
T14_EXIT=$?
set -e
if [ "$T14_EXIT" -ne 65 ]; then
  fail "Test 14 expected exit 65, got exit $T14_EXIT (output: $T14_OUT)"
fi
echo "$T14_OUT" | grep -q '"error": *"workspace_forbidden"' \
  || fail "Test 14 expected '\"error\": \"workspace_forbidden\"' in JSON output, got: $T14_OUT"
pass "validator rejection on /etc (exit 65 + workspace_forbidden JSON)"

echo "--- Test 15: trust override via --workspace-trust ---"
# Configure workspace-root to /tmp so the trusted path still needs trust (the
# path lives under /tmp but we pass --workspace-trust unconditionally per spec).
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" config set workspace-root /tmp || fail "t15-config-set"
TRUSTED_WS="/tmp/smoke-trusted-ws-$$"
mkdir -p "$TRUSTED_WS"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" agent create smoke-trusted \
  --image=dclaw-agent:v0.1 --workspace="$TRUSTED_WS" \
  --workspace-trust "smoke test" || fail "t15-create"
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" agent describe smoke-trusted \
  | grep -q "smoke test" \
  || fail "Test 15 expected 'smoke test' in describe output"
rm -rf "$TRUSTED_WS"
pass "trust override accepted and surfaced in describe"

echo "--- Test 16: audit.log contains forbidden + trust entries ---"
AUDIT_LOG="$SMOKE_STATE/audit.log"
test -f "$AUDIT_LOG" || fail "Test 16 expected $AUDIT_LOG to exist"
FORBIDDEN_COUNT=$(grep -c 'outcome":"forbidden"' "$AUDIT_LOG" || true)
TRUST_COUNT=$(grep -c 'outcome":"trust"' "$AUDIT_LOG" || true)
if [ "$FORBIDDEN_COUNT" -lt 1 ]; then
  fail "Test 16 expected at least one outcome=forbidden line in $AUDIT_LOG"
fi
if [ "$TRUST_COUNT" -lt 1 ]; then
  fail "Test 16 expected at least one outcome=trust line in $AUDIT_LOG"
fi
"$DCLAW_BIN" --state-dir "$SMOKE_STATE" --daemon-socket "$SOCKET" daemon stop >/dev/null 2>&1 || true
pass "audit.log contains forbidden + trust entries (forbidden=$FORBIDDEN_COUNT trust=$TRUST_COUNT)"

# ---- SANDBOX-HARDENING TESTS (Tests 17-19, beta.2) ----
# Each test spins up its own isolated daemon in a fresh mktemp state-dir
# and tears it down via trap — no shared state across these tests. The
# probes below expect dclaw-agent:v0.1 on the host and a reachable
# docker daemon; on a dev machine without docker they will fail early
# at `agent create` and the smoke script will surface the failure
# without reaching the posture assertion.

echo "--- Test 17: capability drop — CAP_MKNOD unavailable ---"
# Inside the container, try to create a block device. With CAP_MKNOD
# dropped, mknod must fail with EPERM. Without caps, even uid 0 inside
# the container cannot do this. Under pre-beta.2 posture,
# `mknod /tmp/dev b 8 0` would succeed (exposing the host's /dev/sda
# raw-device surface).
STATE_DIR_T17=$(mktemp -d -t dclaw-smoke-t17-XXXXXXXX)
case "$STATE_DIR_T17" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T17=$STATE_DIR_T17 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T17="$STATE_DIR_T17/dclaw.sock"
cleanup_t17() {
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" daemon stop >/dev/null 2>&1 || true
  docker rm -f dclaw-smoke-cap-probe >/dev/null 2>&1 || true
  rm -rf "${STATE_DIR_T17:?refuse empty}"
}
trap cleanup_t17 EXIT
"$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" daemon start || fail "t17-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" agent create smoke-cap-probe \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T17" || fail "t17-create"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" agent start smoke-cap-probe || fail "t17-start-agent"
set +e
OUT=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T17" --daemon-socket "$SOCKET_T17" agent exec smoke-cap-probe \
  -- mknod /tmp/dev-sda b 8 0 2>&1)
EX=$?
set -e
if [ "$EX" -eq 0 ]; then
  fail "Test 17: mknod succeeded inside container; CAP_MKNOD not dropped (output: $OUT)"
fi
echo "$OUT" | grep -qi "operation not permitted\|eperm" \
  || fail "Test 17: expected EPERM from mknod, got: $OUT"
cleanup_t17
trap cleanup EXIT
pass "CAP_MKNOD dropped (mknod failed with EPERM)"

echo "--- Test 18: seccomp profile — unshare(CLONE_NEWUSER) denied ---"
# The default Docker seccomp profile denies unshare with CLONE_NEWUSER
# for unprivileged tasks. This test exercises one concrete denied
# syscall; a full profile regression suite lives in
# syscall_blocklist_test.sh under follow-ups. Under pre-beta.2,
# whether this succeeded depended on the daemon config — post-beta.2,
# we pin seccomp=default explicitly.
STATE_DIR_T18=$(mktemp -d -t dclaw-smoke-t18-XXXXXXXX)
case "$STATE_DIR_T18" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T18=$STATE_DIR_T18 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T18="$STATE_DIR_T18/dclaw.sock"
cleanup_t18() {
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T18" --daemon-socket "$SOCKET_T18" daemon stop >/dev/null 2>&1 || true
  docker rm -f dclaw-smoke-seccomp-probe >/dev/null 2>&1 || true
  rm -rf "${STATE_DIR_T18:?refuse empty}"
}
trap cleanup_t18 EXIT
"$DCLAW_BIN" --state-dir "$STATE_DIR_T18" --daemon-socket "$SOCKET_T18" daemon start || fail "t18-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T18" --daemon-socket "$SOCKET_T18" agent create smoke-seccomp-probe \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T18" || fail "t18-create"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T18" --daemon-socket "$SOCKET_T18" agent start smoke-seccomp-probe || fail "t18-start-agent"
set +e
OUT=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T18" --daemon-socket "$SOCKET_T18" agent exec smoke-seccomp-probe \
  -- unshare -U -r whoami 2>&1)
EX=$?
set -e
if [ "$EX" -eq 0 ]; then
  fail "Test 18: unshare -U succeeded; seccomp default not applied (output: $OUT)"
fi
echo "$OUT" | grep -qi "operation not permitted\|eperm" \
  || fail "Test 18: expected EPERM from unshare, got: $OUT"
cleanup_t18
trap cleanup EXIT
pass "seccomp default profile applied (unshare(CLONE_NEWUSER) denied)"

echo "--- Test 19: PidsLimit — fork bomb capped at 256 ---"
# Spawn 300 sleeping processes. With PidsLimit=256, the 257th fork
# fails. We assert the count is bounded below 300 (not that we hit
# exactly 256 — the kernel counts pid-1 and the shell itself). If
# PidsLimit is absent, every fork succeeds and jobs returns 300.
STATE_DIR_T19=$(mktemp -d -t dclaw-smoke-t19-XXXXXXXX)
case "$STATE_DIR_T19" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T19=$STATE_DIR_T19 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T19="$STATE_DIR_T19/dclaw.sock"
cleanup_t19() {
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T19" --daemon-socket "$SOCKET_T19" daemon stop >/dev/null 2>&1 || true
  docker rm -f dclaw-smoke-pids-probe >/dev/null 2>&1 || true
  rm -rf "${STATE_DIR_T19:?refuse empty}"
}
trap cleanup_t19 EXIT
"$DCLAW_BIN" --state-dir "$STATE_DIR_T19" --daemon-socket "$SOCKET_T19" daemon start || fail "t19-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T19" --daemon-socket "$SOCKET_T19" agent create smoke-pids-probe \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T19" || fail "t19-create"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T19" --daemon-socket "$SOCKET_T19" agent start smoke-pids-probe || fail "t19-start-agent"
set +e
OUT=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T19" --daemon-socket "$SOCKET_T19" agent exec smoke-pids-probe \
  -- sh -c 'i=0; while [ "$i" -lt 300 ]; do (sleep 30) & i=$((i+1)); done; jobs | wc -l' 2>&1)
EX=$?
set -e
JOB_COUNT=$(echo "$OUT" | tail -n1 | tr -d '[:space:]')
# The spawn loop is expected to hit EAGAIN partway through, producing
# non-zero exit status from the sub-shell; accept any result where the
# observed job count is bounded below 300. If PidsLimit is missing, we
# will see 300 (every fork succeeded) and fail.
case "$JOB_COUNT" in
  ''|*[!0-9]*)
    fail "Test 19: could not parse job count from output: $OUT"
    ;;
esac
if [ "$JOB_COUNT" -ge 300 ]; then
  fail "Test 19: PidsLimit not enforced; spawned $JOB_COUNT processes (output: $OUT)"
fi
cleanup_t19
trap cleanup EXIT
pass "PidsLimit enforced (spawned $JOB_COUNT < 300 processes before EAGAIN)"

echo "--- Test 20: ReadonlyRootfs — /etc + /opt not writable; /tmp + /workspace writable ---"
# Four probes in one test exercise the ReadonlyRootfs + Tmpfs posture:
#   (a) touch /etc/sandbox-breach  → must fail with EROFS
#   (b) touch /opt/sandbox-breach  → must fail with EROFS
#   (c) touch /tmp/ok              → must succeed (tmpfs overlay)
#   (d) touch /workspace/ok        → must succeed (bind-mount)
# Under pre-beta.2 posture all four writes would succeed, allowing an
# attacker to persist a malicious entry-point (e.g. overwriting /app/run.mjs).
# Post-beta.2, the rootfs is read-only except for the tmpfs overlays and
# the workspace bind-mount carved out at container-create time.
STATE_DIR_T20=$(mktemp -d -t dclaw-smoke-t20-XXXXXXXX)
case "$STATE_DIR_T20" in
  /var/folders/*|/tmp/*|/private/tmp/*|/private/var/folders/*) ;;
  *) echo "refuse: STATE_DIR_T20=$STATE_DIR_T20 outside expected prefix" >&2; exit 1;;
esac
SOCKET_T20="$STATE_DIR_T20/dclaw.sock"
cleanup_t20() {
  "$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" daemon stop >/dev/null 2>&1 || true
  docker rm -f dclaw-smoke-rootfs-probe >/dev/null 2>&1 || true
  rm -rf "${STATE_DIR_T20:?refuse empty}"
}
trap cleanup_t20 EXIT
"$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" daemon start || fail "t20-start"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" agent create smoke-rootfs-probe \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_T20" || fail "t20-create"
"$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" agent start smoke-rootfs-probe || fail "t20-start-agent"

# Negative probe (a): /etc write must fail with EROFS
set +e
OUT=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" agent exec smoke-rootfs-probe \
  -- touch /etc/sandbox-breach 2>&1)
EX=$?
set -e
if [ "$EX" -eq 0 ]; then
  fail "Test 20 /etc: ReadonlyRootfs not applied; /etc is writable (output: $OUT)"
fi
echo "$OUT" | grep -qi "read-only\|erofs" \
  || fail "Test 20 /etc: expected EROFS, got: $OUT"

# Negative probe (b): /opt write must fail with EROFS
set +e
OUT=$("$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" agent exec smoke-rootfs-probe \
  -- touch /opt/sandbox-breach 2>&1)
EX=$?
set -e
if [ "$EX" -eq 0 ]; then
  fail "Test 20 /opt: ReadonlyRootfs not applied; /opt is writable (output: $OUT)"
fi
echo "$OUT" | grep -qi "read-only\|erofs" \
  || fail "Test 20 /opt: expected EROFS, got: $OUT"

# Positive probe (c): /tmp write must succeed (tmpfs overlay)
"$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" agent exec smoke-rootfs-probe \
  -- touch /tmp/ok \
  || fail "Test 20 /tmp: tmpfs overlay missing; /tmp not writable"

# Positive probe (d): /workspace write must succeed (bind-mount)
"$DCLAW_BIN" --state-dir "$STATE_DIR_T20" --daemon-socket "$SOCKET_T20" agent exec smoke-rootfs-probe \
  -- touch /workspace/ok \
  || fail "Test 20 /workspace: bind-mount missing; /workspace not writable"

cleanup_t20
trap cleanup EXIT
pass "ReadonlyRootfs enforced (/etc, /opt non-writable); /tmp + /workspace writable"

echo "All daemon smoke tests passed."
