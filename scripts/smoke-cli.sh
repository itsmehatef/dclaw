#!/usr/bin/env bash
set -euo pipefail

DCLAW_BIN="${DCLAW_BIN:-./bin/dclaw}"

# Make the script self-sufficient: build the binary if it's missing or if the
# caller didn't set DCLAW_BIN to a pre-built binary. Prefer `make build` to
# keep version ldflags consistent; fall back to `go build` if Make isn't available.
if [[ ! -x "$DCLAW_BIN" ]]; then
  echo "--- Building $DCLAW_BIN (not found) ---"
  if command -v make >/dev/null 2>&1 && [[ -f Makefile ]]; then
    if ! make build; then
      echo "ERROR: 'make build' failed — cannot run smoke tests" >&2
      exit 1
    fi
  else
    mkdir -p "$(dirname "$DCLAW_BIN")"
    if ! go build -o "$DCLAW_BIN" ./cmd/dclaw; then
      echo "ERROR: 'go build' failed — cannot run smoke tests" >&2
      exit 1
    fi
  fi
  if [[ ! -x "$DCLAW_BIN" ]]; then
    echo "ERROR: $DCLAW_BIN still not found/executable after build" >&2
    exit 1
  fi
fi

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

echo "--- Test 1: dclaw version exits 0 ---"
out="$("$DCLAW_BIN" version)"
echo "  $out"
[[ "$out" == dclaw\ version\ * ]] || fail "unexpected version output: $out"
pass "version output"

echo "--- Test 2: dclaw --help exits 0 ---"
"$DCLAW_BIN" --help >/dev/null
pass "dclaw --help"

echo "--- Test 3: dclaw agent --help exits 0 ---"
"$DCLAW_BIN" agent --help >/dev/null
pass "dclaw agent --help"

echo "--- Test 4: dclaw agent list exits 69 ---"
set +e
"$DCLAW_BIN" agent list >/dev/null 2>/tmp/dclaw-smoke-stderr
code=$?
set -e
(( code == 69 )) || fail "expected exit 69, got $code"
grep -q "dclaw daemon" /tmp/dclaw-smoke-stderr || fail "expected 'dclaw daemon' in stderr"
pass "dclaw agent list exits 69 with daemon message"

echo "--- Test 5: dclaw agent list -o json emits structured error ---"
set +e
json="$("$DCLAW_BIN" agent list -o json 2>/dev/null)"
code=$?
set -e
(( code == 69 )) || fail "expected exit 69, got $code"
echo "$json" | grep -q '"error": *"feature_not_ready"' || fail "expected feature_not_ready in JSON"
echo "$json" | grep -q '"exit_code": *69' || fail "expected exit_code 69 in JSON"
pass "dclaw agent list -o json emits feature_not_ready"

echo "--- Test 6: dclaw agent create without --image hits cobra's required-flag error path (exit 1) ---"
set +e
"$DCLAW_BIN" agent create foo >/dev/null 2>&1
code=$?
set -e
(( code == 1 )) || fail "expected cobra required-flag exit 1, got $code"
pass "dclaw agent create without --image exits 1 (required-flag error)"

echo "--- Test 7: dclaw agent list -o bogus fails ---"
set +e
"$DCLAW_BIN" agent list -o bogus >/dev/null 2>&1
code=$?
set -e
(( code == 1 )) || fail "expected exit 1, got $code"
pass "invalid -o rejected"

# Note: dclaw's cmd/dclaw/main.go currently maps all cobra errors to os.Exit(1),
# so there is no exit-2 path to test at this stage. If we later differentiate
# usage errors (unknown subcommand, required-flag) from runtime errors, add a
# test here asserting exit 2 for `dclaw bogus-subcommand`.
echo "--- Test 8: dclaw bogus-subcommand is a cobra error (exit 1 under current main.go) ---"
set +e
"$DCLAW_BIN" bogus-subcommand >/dev/null 2>&1
code=$?
set -e
(( code == 1 )) || fail "expected exit 1 for unknown subcommand, got $code"
pass "unknown subcommand exits 1 (current main.go maps all cobra errors to 1)"

echo ""
echo "All 8 CLI smoke tests passed."
