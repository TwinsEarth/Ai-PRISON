# AI Prison Makefile
# Pure Go standard library; no code generation or external tooling required.
GO ?= go
GOFLAGS ?= -mod=mod

.PHONY: all build vet test fmt check demo clean tidy

all: build

build: ## Build all packages
	$(GO) build $(GOFLAGS) ./...

vet: ## Run go vet
	$(GO) vet $(GOFLAGS) ./...

test: ## Run the full test suite
	$(GO) test $(GOFLAGS) -race ./...

test-cover: ## Run tests with coverage
	$(GO) test $(GOFLAGS) -race -coverprofile=coverage.txt ./...
	$(GO) tool cover -func=coverage.txt

fmt: ## Format source
	$(GO) fmt ./...

check: vet test build ## The per-version gate: vet + test + build must all pass

demo: ## Run the end-to-end CLI demo (populated in v0.9.5)
	$(GO) run $(GOFLAGS) ./cmd/aiprison demo

tidy:
	$(GO) mod tidy

clean:
	rm -rf bin dist coverage.txt
