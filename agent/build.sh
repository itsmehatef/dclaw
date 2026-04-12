#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

TAG="${DCLAW_AGENT_TAG:-dclaw-agent:v0.1}"

echo "Building ${TAG}..."
docker build -t "${TAG}" .

echo ""
echo "Built ${TAG}"
docker images "${TAG}" --format "Size: {{.Size}}"
