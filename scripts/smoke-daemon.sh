#!/usr/bin/env bash
# Phase 3 integration smoke: spin up dclawd, exercise full CRUD, tear down.
# Requires docker reachable on the host and dclaw-agent:v0.1 built (phase 1).
set -euo pipefail

DCLAW_BIN="${DCLAW_BIN:-./bin/dclaw}"
DCLAWD_BIN="${DCLAWD_BIN:-./bin/dclawd}"
STATE_DIR="${STATE_DIR:-$(mktemp -d -t dclaw-smoke-XXXX)}"
SOCKET="$STATE_DIR/dclaw.sock"

export DCLAWD_BIN
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

cleanup() {
  "$DCLAW_BIN" --daemon-socket "$SOCKET" daemon stop >/dev/null 2>&1 || true
  rm -rf "$STATE_DIR" || true
}
trap cleanup EXIT

echo "--- Test 1: daemon start ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" daemon start || fail "daemon start"
test -S "$SOCKET" || fail "socket not created"
pass "daemon start"

echo "--- Test 2: daemon status ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" daemon status | grep -q "agents=0" || fail "status lacks agents=0"
pass "daemon status"

echo "--- Test 3: agent create ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" agent create smokey \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR" || fail "create"
pass "agent create"

echo "--- Test 4: agent list shows smokey ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" agent list | grep -q smokey || fail "list missing smokey"
pass "agent list"

echo "--- Test 5: agent get smokey ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" agent get smokey -o json | grep -q '"name": *"smokey"' || fail "get json"
pass "agent get"

echo "--- Test 6: agent delete ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" agent delete smokey || fail "delete"
pass "agent delete"

echo "--- Test 7: daemon stop ---"
"$DCLAW_BIN" --daemon-socket "$SOCKET" daemon stop || fail "stop"
pass "daemon stop"

echo "All daemon smoke tests passed."
