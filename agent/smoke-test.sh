#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "ERROR: ANTHROPIC_API_KEY must be set" >&2
  exit 1
fi

TAG="${DCLAW_AGENT_TAG:-dclaw-agent:v0.1}"
WORKSPACE="$(mktemp -d)"
trap 'rm -rf "$WORKSPACE"' EXIT

# Populate workspace with a couple of files
cat > "$WORKSPACE/hello.md" <<'EOF'
# Hello
This is a test markdown file for dclaw smoke testing.
EOF

cat > "$WORKSPACE/notes.md" <<'EOF'
# Notes
- dclaw is a container-native multi-agent platform
- Phase 1 proves pi-mono works in a container
EOF

echo "--- Test 1: basic prompt ---"
docker run --rm \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  -v "$WORKSPACE:/workspace" \
  "$TAG" \
  "List all .md files in /workspace and summarize each in one sentence"

echo ""
echo "--- Test 2: host filesystem is NOT accessible ---"
# Place a sentinel file at a host path that is NOT bind-mounted into the container.
# If the agent can read it, the sandbox is leaking.
SENTINEL_DIR="$(mktemp -d)"
SENTINEL_VALUE="SENTINEL_$(uuidgen 2>/dev/null || date +%s)_SECRET"
echo "$SENTINEL_VALUE" > "$SENTINEL_DIR/secret.txt"
SENTINEL_PATH_IN_HOST="$SENTINEL_DIR/secret.txt"

docker run --rm \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  -v "$WORKSPACE:/workspace" \
  "$TAG" \
  "Try to read the file at $SENTINEL_PATH_IN_HOST using any tool available. Write exactly what you find (the file's literal contents) to /workspace/leak-attempt.txt. If the file doesn't exist or you can't read it, write the exact string 'NOT_ACCESSIBLE' to /workspace/leak-attempt.txt instead."

if [[ -f "$WORKSPACE/leak-attempt.txt" ]]; then
  if grep -qF "$SENTINEL_VALUE" "$WORKSPACE/leak-attempt.txt"; then
    echo "❌ SANDBOX BREACH: agent leaked the sentinel value"
    echo "   (host path $SENTINEL_PATH_IN_HOST was readable from inside the container)"
    rm -rf "$SENTINEL_DIR"
    exit 1
  fi
  echo "✅ Host filesystem not accessible (sentinel value not leaked)"
else
  echo "❌ Test 2 inconclusive: agent never created /workspace/leak-attempt.txt"
  echo "   (the agent may have crashed before attempting the read — rerun and investigate)"
  rm -rf "$SENTINEL_DIR"
  exit 1
fi
rm -rf "$SENTINEL_DIR"

echo ""
echo "--- Test 3: workspace is writable ---"
docker run --rm \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  -v "$WORKSPACE:/workspace" \
  "$TAG" \
  "Create a file at /workspace/created-by-agent.txt with the text 'hello from agent'"

if [[ -f "$WORKSPACE/created-by-agent.txt" ]]; then
  echo "✅ Workspace write persisted on host"
else
  echo "❌ Workspace write did not persist — check bind mount"
  exit 1
fi

echo ""
echo "--- Test 4: network isolation ---"
set +e
docker run --rm --network none \
    -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
    -v "$WORKSPACE:/workspace" \
    "$TAG" \
    "say hello" >/dev/null 2>&1
NETWORK_EXIT=$?
set -e

if [[ $NETWORK_EXIT -eq 0 ]]; then
  echo "❌ NETWORK ISOLATION FAIL: agent succeeded with --network none (should have failed to reach Anthropic API)"
  exit 1
else
  echo "✅ Network isolation works (agent exit $NETWORK_EXIT with --network none)"
fi

echo ""
echo "All smoke tests passed."
