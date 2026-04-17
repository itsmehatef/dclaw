#!/usr/bin/env bash
# Wrapper that runs the teatest TUI smoke via go test.
set -euo pipefail
go test -run TestTUISmoke -v ./internal/tui/... -timeout 60s
