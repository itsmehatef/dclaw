# dclaw Makefile — v0.3.0-daemon

BINARY_CLI  := dclaw
BINARY_D    := dclawd
PKG         := github.com/itsmehatef/dclaw
CMD_CLI     := ./cmd/dclaw
CMD_D       := ./cmd/dclawd
BIN_DIR     := ./bin

VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.3.0-dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS     := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.BuildDate=$(BUILD_DATE)

GO          ?= go
GOFLAGS     ?=

.PHONY: all build cli daemon tui test vet lint fmt install clean tidy smoke smoke-daemon smoke-tui migrate help

all: build

build: cli daemon ## Build both binaries

cli: ## Build dclaw
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY_CLI) $(CMD_CLI)

daemon: ## Build dclawd
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY_D) $(CMD_D)

tui: cli ## Convenience: build and run the TUI in dev mode
	DCLAWD_BIN=$(BIN_DIR)/$(BINARY_D) $(BIN_DIR)/$(BINARY_CLI)

test: ## Run unit tests
	$(GO) test $(GOFLAGS) ./...

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint (no-op if not installed)
	@command -v golangci-lint >/dev/null 2>&1 \
		&& golangci-lint run \
		|| echo "golangci-lint not installed; skipping"

fmt: ## Format code
	$(GO) fmt ./...
	gofmt -s -w .

install: build ## go install both binaries with version ldflags
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD_CLI)
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD_D)

tidy: ## go mod tidy
	$(GO) mod tidy

smoke: smoke-daemon smoke-tui ## Run all smoke suites

smoke-daemon: build ## Integration smoke against a real daemon + docker
	DCLAW_BIN=$(BIN_DIR)/$(BINARY_CLI) DCLAWD_BIN=$(BIN_DIR)/$(BINARY_D) \
		./scripts/smoke-daemon.sh

smoke-tui: build ## teatest-driven TUI smoke
	DCLAW_BIN=$(BIN_DIR)/$(BINARY_CLI) DCLAWD_BIN=$(BIN_DIR)/$(BINARY_D) \
		./scripts/smoke-tui.sh

migrate: build ## Apply embedded migrations to ~/.dclaw/state.db (useful in dev)
	$(BIN_DIR)/$(BINARY_D) --migrate-only

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
