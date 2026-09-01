.PHONY: clean deps install build test test-coverage e2e format lint check check-artifacts check-vendor-parity check-marketplace scan-artifacts run docs help docker-build docker-push

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

e2e: build ## Run the E2E test suite (release gate; not part of `make test` or `make check`)
	$(GO) test -tags=e2e -count=1 -timeout=10m ./test/e2e/...

format: ## Format code
	$(GOIMPORTS) -w .
	gofmt -s -w .

lint: ## Lint code
	golangci-lint run ./...

# The harness artifacts this repo ships (skills/ agents/ rules/) plus the one it
# uses on itself (.claude/). testdata/ is deliberately excluded: its fixtures are
# malformed on purpose, so `ynd lint .` at the repo root can never be green.
ARTIFACT_DIRS := skills agents rules .claude

# NVIDIA SkillSpector — security scanner for agent skills. Pinned: an unpinned
# git install would let an upstream push change what gates our merges, which is
# the exact supply-chain risk the scanner exists to catch.
SKILLSPECTOR ?= skillspector
SKILLSPECTOR_VERSION := v2.11.0
SKILLSPECTOR_BASELINE := .skillspector-baseline.yaml
SARIF_DIR := .skillspector-sarif

scan-artifacts: ## Security-scan the harness artifacts with SkillSpector
	@command -v $(SKILLSPECTOR) >/dev/null 2>&1 || { \
		echo "skillspector not found. Install with:"; \
		echo "  pip install 'git+https://github.com/NVIDIA/SkillSpector.git@$(SKILLSPECTOR_VERSION)'"; \
		exit 1; }
	@rm -rf $(SARIF_DIR) && mkdir -p $(SARIF_DIR)
	@for d in $(ARTIFACT_DIRS); do \
		echo "==> skillspector scan $$d"; \
		$(SKILLSPECTOR) scan ./$$d --no-llm --baseline $(SKILLSPECTOR_BASELINE) \
			-f sarif -o $(SARIF_DIR)/`echo $$d | tr -d .`.sarif || exit 1; \
	done
	@# `skillspector scan` exits 0 whatever it finds, so the verdict comes from
	@# the SARIF rather than the exit code. See the script header.
	@scripts/skillspector-findings.sh $(SARIF_DIR)

check-marketplace: ## Assert the committed marketplace indexes match the plugin manifests
	@./scripts/marketplace-consistency.sh

check-vendor-parity: build ## Assert every vendor is documented and assembles the same artifacts
	@./scripts/vendor-parity.sh

stamp-version: ## Stamp the harness version into every manifest (VERSION=X.Y.Z, or the latest tag)
	@./scripts/stamp-harness-version.sh $(VERSION)

check-artifacts: build ## Validate and lint the harness artifacts this repo ships
	@echo "==> ynd validate ."
	@$(BUILD_DIR)/$(BINARY_NAME_DEV) validate .
	@echo "==> ynd lint $(ARTIFACT_DIRS)"
	@$(BUILD_DIR)/$(BINARY_NAME_DEV) lint $(ARTIFACT_DIRS)
	@echo "Harness artifacts OK."

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

check: deps format lint test build check-artifacts ## Run full CI pipeline
