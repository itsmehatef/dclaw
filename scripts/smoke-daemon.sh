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

# ---- NEGATIVE PATH TESTS ----
# These tests verify that the daemon and CLI return proper errors for invalid
# operations. Each test restarts the daemon in a fresh STATE_DIR because some
# negative paths leave the daemon in a broken state.

echo "--- Test 8: duplicate agent name is rejected ---"
# Restart daemon for this test.
STATE_DIR_NEG=$(mktemp -d -t dclaw-smoke-neg-XXXX)
SOCKET_NEG="$STATE_DIR_NEG/dclaw.sock"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG" daemon start || fail "neg-start"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG" agent create dup \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_NEG" || fail "dup-create-1"
if "$DCLAW_BIN" --daemon-socket "$SOCKET_NEG" agent create dup \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_NEG" 2>/dev/null; then
  fail "duplicate agent name should have been rejected"
fi
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG" daemon stop >/dev/null 2>&1 || true
rm -rf "$STATE_DIR_NEG"
pass "duplicate agent name rejected"

echo "--- Test 9: get non-existent agent returns error ---"
STATE_DIR_NEG2=$(mktemp -d -t dclaw-smoke-neg2-XXXX)
SOCKET_NEG2="$STATE_DIR_NEG2/dclaw.sock"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG2" daemon start || fail "neg2-start"
if "$DCLAW_BIN" --daemon-socket "$SOCKET_NEG2" agent get nosuchagent 2>/dev/null; then
  fail "get non-existent agent should have failed"
fi
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG2" daemon stop >/dev/null 2>&1 || true
rm -rf "$STATE_DIR_NEG2"
pass "get non-existent agent returned error"

echo "--- Test 10: daemon already-running is idempotent ---"
STATE_DIR_NEG3=$(mktemp -d -t dclaw-smoke-neg3-XXXX)
SOCKET_NEG3="$STATE_DIR_NEG3/dclaw.sock"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG3" daemon start || fail "neg3-start-1"
# Starting again should print "already running" and exit 0, not error.
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG3" daemon start || fail "neg3-start-2 (idempotent start failed)"
"$DCLAW_BIN" --daemon-socket "$SOCKET_NEG3" daemon stop >/dev/null 2>&1 || true
rm -rf "$STATE_DIR_NEG3"
pass "daemon start is idempotent"

echo "--- Test 11: daemon CLI fails gracefully when daemon is not running ---"
BAD_SOCKET="/tmp/dclaw-smoke-notexist-$$.sock"
OUT=$("$DCLAW_BIN" --daemon-socket "$BAD_SOCKET" agent list 2>&1 || true)
echo "$OUT" | grep -qi "not running\|no such file\|connection refused\|dial" \
  || fail "expected daemon-not-running error, got: $OUT"
pass "CLI fails gracefully when daemon not running"

echo "--- Test 12: agent chat RPC smoke (exec proxy) ---"
STATE_DIR_CHAT=$(mktemp -d -t dclaw-smoke-chat-XXXX)
SOCKET_CHAT="$STATE_DIR_CHAT/dclaw.sock"
"$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" daemon start || fail "chat-start"
"$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" agent create chatbot \
  --image=dclaw-agent:v0.1 --workspace="$STATE_DIR_CHAT" || fail "chat-create"
"$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" agent start chatbot || fail "chat-agent-start"
OUT=$("$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" agent exec chatbot -- echo "smoke-ok" 2>&1)
echo "$OUT" | grep -q "smoke-ok" || fail "expected 'smoke-ok' in exec output, got: $OUT"
"$DCLAW_BIN" --daemon-socket "$SOCKET_CHAT" daemon stop >/dev/null 2>&1 || true
rm -rf "$STATE_DIR_CHAT"
pass "agent chat RPC smoke"

echo "All daemon smoke tests passed."
