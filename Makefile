.PHONY: clean deps install build test test-coverage format lint check run docs help docker-build docker-push

BINARY_NAME := ynh
BINARY_NAME_DEV := ynd
BUILD_DIR := bin
GO := go
GOFLAGS := -v
INSTALL_DIR := $(HOME)/.ynh/bin

# Tool paths - use full paths so go-installed tools are found without PATH hacks
GOBIN := $(shell go env GOPATH)/bin
GOIMPORTS := $(GOBIN)/goimports

# Version from git: use exact tag only if clean and on that exact commit, otherwise branch+sha
DEV_VERSION := dev-$(shell git rev-parse --abbrev-ref HEAD 2>/dev/null | tr '/' '-' || echo unknown)-$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet 2>/dev/null || echo '-dirty')
VERSION := $(shell git diff --quiet 2>/dev/null && git describe --tags --exact-match 2>/dev/null || echo "$(DEV_VERSION)")
LDFLAGS := -ldflags "-X github.com/eyelock/ynh/internal/config.Version=$(VERSION)"

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

deps: ## Install prerequisites (Go, linter, formatter)
	@echo "Checking prerequisites..."
	@command -v go >/dev/null 2>&1 || { echo "Installing Go..."; brew install go; }
	@command -v golangci-lint >/dev/null 2>&1 || { echo "Installing golangci-lint..."; brew install golangci-lint; }
	@test -x $(GOIMPORTS) || { echo "Installing goimports..."; go install golang.org/x/tools/cmd/goimports@latest; }
	@echo "All prerequisites installed."
	@echo ""
	@echo "Run 'make install' to build and install binaries to ~/.ynh/bin/"

build: ## Build all binaries
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/ynh
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME_DEV) ./cmd/ynd

install: build ## Build and install binaries to ~/.ynh/bin
	@mkdir -p $(INSTALL_DIR)
	cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	cp $(BUILD_DIR)/$(BINARY_NAME_DEV) $(INSTALL_DIR)/$(BINARY_NAME_DEV)
	@echo "Installed $(BINARY_NAME) and $(BINARY_NAME_DEV) to $(INSTALL_DIR)"
	@command -v $(BINARY_NAME_DEV) >/dev/null 2>&1 || echo "Run: export PATH=\"$(INSTALL_DIR):\$$PATH\""

test: ## Run tests with coverage (use FILE=./path/to/pkg to target specific package)
ifdef FILE
	$(GO) test $(FILE) -cover -race -v
else
	$(GO) test ./... -cover -race
endif

test-coverage: ## Run tests with coverage profile (use FILE=./path/to/pkg to target specific package)
ifdef FILE
	$(GO) test $(FILE) -coverprofile=coverage.out -count=1
	$(GO) tool cover -func=coverage.out
else
	$(GO) test ./... -coverprofile=coverage.out -count=1
	$(GO) tool cover -func=coverage.out
endif

format: ## Format code
	$(GOIMPORTS) -w .
	gofmt -s -w .

lint: ## Lint code
	golangci-lint run ./...

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
	$(GO) clean -cache -testcache

docs: ## Serve docs locally (requires npx)
	@command -v npx >/dev/null 2>&1 || { echo "npx not found. Install Node.js to browse docs locally."; exit 1; }
	@echo "Starting docs server at http://localhost:3000"
	@npx --yes docsify-cli serve docs

DOCKER_IMAGE := ghcr.io/eyelock/ynh
DOCKER_TAG := $(VERSION)

docker-build: ## Build base Docker image
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE):$(DOCKER_TAG) -t $(DOCKER_IMAGE):latest .

docker-push: ## Push base Docker image to GHCR
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

check: deps format lint test build ## Run full CI pipeline
