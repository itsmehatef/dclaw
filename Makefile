# dclaw Makefile — v0.2.0-cli

BINARY      := dclaw
PKG         := github.com/itsmehatef/dclaw
CMD         := ./cmd/dclaw
BIN_DIR     := ./bin

# Version info stamped via -ldflags.
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.2.0-cli-dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS     := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.BuildDate=$(BUILD_DATE)

GO          ?= go
GOFLAGS     ?=

.PHONY: all build test lint fmt vet install clean tidy smoke help

all: build

build: ## Build dclaw into ./bin/dclaw
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(CMD)
	@echo "Built $(BIN_DIR)/$(BINARY) ($(VERSION))"

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

install: ## go install dclaw with version ldflags
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD)

tidy: ## go mod tidy
	$(GO) mod tidy

smoke: build ## Run the smoke test script against the freshly-built binary
	DCLAW_BIN=$(BIN_DIR)/$(BINARY) ./scripts/smoke-cli.sh

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
