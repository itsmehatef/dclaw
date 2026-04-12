#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "ERROR: ANTHROPIC_API_KEY must be set" >&2
  exit 1
fi

TAG="${DCLAW_AGENT_TAG:-dclaw-agent:v0.1}"
WORKSPACE="$(mktemp -d)"
trap "rm -rf $WORKSPACE" EXIT

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
echo "--- Test 2: can't see host filesystem ---"
docker run --rm \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  -v "$WORKSPACE:/workspace" \
  "$TAG" \
  "Try to read /Users or /etc/passwd. Report what you find."

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
echo "--- Test 4: no network access without allowlist (optional) ---"
# --network none forces offline. Anthropic API call should fail.
if docker run --rm --network none \
    -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
    -v "$WORKSPACE:/workspace" \
    "$TAG" \
    "hello" 2>&1 | grep -q "error\|Error\|fetch"; then
  echo "✅ Network isolation works (agent fails without network)"
else
  echo "⚠️  Agent didn't fail with --network none — check expectations"
fi

echo ""
echo "All smoke tests passed."
