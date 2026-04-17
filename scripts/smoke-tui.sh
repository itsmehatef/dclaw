#!/usr/bin/env bash
set -euo pipefail
go test -run TestTUISmoke              -v ./internal/tui/...    -timeout 60s
go test -run TestTUIChatOpenFromList   -v ./internal/tui/...    -timeout 60s
go test -run TestTUIChatEscReturns     -v ./internal/tui/...    -timeout 60s
go test -run TestChatMessageIDDeterministic -v ./internal/client/... -timeout 30s
