#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

TAG="${DCLAW_AGENT_TAG:-dclaw-agent:v0.1}"

echo "Building ${TAG}..."
docker build -t "${TAG}" .

echo ""
echo "Built ${TAG}"
docker image inspect "${TAG}" --format 'Size: {{.Size | printf "%d bytes (%.1f MB)" (div . 1048576)}}' 2>/dev/null || \
  docker images "${TAG}" --format "Size: {{.Size}}"
