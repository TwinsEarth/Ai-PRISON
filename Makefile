# AI Prison Makefile
# Pure Go standard library; no code generation or external tooling required.
GO ?= go
GOFLAGS ?= -mod=mod

# Release metadata, injectable at link time.
VERSION    ?= 1.0.0
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Native binary by default; cross-compile with e.g. `GOOS=windows make build`.
ifeq ($(GOOS),windows)
BIN_OUT := bin/aiprison.exe
else
BIN_OUT := bin/aiprison
endif

# -trimpath + stripped symbol/DWARF tables; version metadata injected into the
# cmd/aiprison package variables. NOTE: package main's linker symbol prefix is
# `main` (verified via go tool nm), so -X must target main.* — the
# module-path form only works for non-main packages.
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildTime=$(BUILD_TIME)

.PHONY: all build build-examples vet test fmt check demo clean tidy smoke

all: build

build: ## Build the real CLI binary into bin/ (version metadata injected)
	mkdir -p bin
	$(GO) build $(GOFLAGS) -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_OUT) ./cmd/aiprison

build-examples: ## Build the basic example into bin/examples-basic (never leaves a root-level `basic`)
	mkdir -p bin
	$(GO) build $(GOFLAGS) -o bin/examples-basic ./examples/basic

vet: ## Run go vet
	$(GO) vet $(GOFLAGS) ./...

test: ## Run the full test suite (artifacts land in temp dirs; nothing in the repo root)
	$(GO) test $(GOFLAGS) -race ./...

test-cover: ## Run tests with coverage
	$(GO) test $(GOFLAGS) -race -coverprofile=coverage.txt ./...
	$(GO) tool cover -func=coverage.txt

fmt: ## Format source
	$(GO) fmt ./...

check: vet test build ## The per-version gate: vet + test + build must all pass

demo: ## Run the end-to-end CLI demo (populated in v0.9.5)
	$(GO) run $(GOFLAGS) ./cmd/aiprison demo

smoke: build ## End-to-end smoke on the real binary: temp data dirs, cross-process replay, zero residue
	./scripts/smoke.sh

tidy:
	$(GO) mod tidy

clean:
	rm -rf bin dist coverage.txt coverage.html

release-check: ## Run the pre-release allow-list gate (files, version consistency, hygiene, no secrets)
	bash scripts/release_check.sh

.PHONY: release-check
