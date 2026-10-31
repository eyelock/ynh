.PHONY: clean deps install build test test-coverage e2e format lint check check-tidy check-artifacts check-vendor-parity check-marketplace check-eval-stand-ins scan-artifacts run docs help docker-build docker-push vulncheck

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

deps: ## Install prerequisites (Go, linter, formatter, jq)
	@echo "Checking prerequisites..."
	@command -v go >/dev/null 2>&1 || { echo "Installing Go..."; brew install go; }
	@command -v golangci-lint >/dev/null 2>&1 || { echo "Installing golangci-lint..."; brew install golangci-lint; }
	@test -x $(GOIMPORTS) || { echo "Installing goimports..."; go install golang.org/x/tools/cmd/goimports@latest; }
	@command -v jq >/dev/null 2>&1 || { echo "Installing jq..."; brew install jq; }
	@echo "All prerequisites installed."
	@echo ""
	@echo "Run 'make install' to build and install binaries to ~/.ynh/bin/"

build: ## Build all binaries
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/ynh
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME_DEV) ./cmd/ynd

install: build ## Build and install binaries to ~/.ynh/bin
	@mkdir -p $(INSTALL_DIR)
	@# Copy to a temporary name, then rename over the old binary. Overwriting a
	@# binary in place with cp keeps the same file, and on macOS the kernel's
	@# cached code-signature check for that file can go stale: the next launch
	@# is killed with SIGKILL (exit 137) and no message. A rename puts a new
	@# file in place, and anything launching mid-install sees old or new, never
	@# a partial copy.
	cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/.$(BINARY_NAME).new
	mv -f $(INSTALL_DIR)/.$(BINARY_NAME).new $(INSTALL_DIR)/$(BINARY_NAME)
	cp $(BUILD_DIR)/$(BINARY_NAME_DEV) $(INSTALL_DIR)/.$(BINARY_NAME_DEV).new
	mv -f $(INSTALL_DIR)/.$(BINARY_NAME_DEV).new $(INSTALL_DIR)/$(BINARY_NAME_DEV)
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

check-tidy: ## Fail if go.mod or go.sum is not tidy (read-only; CI's `go mod tidy -diff`)
	@# A direct import marked `// indirect` is invisible to build, test and lint
	@# alike; only this sees it. -diff prints what tidy would change and exits
	@# non-zero, writing nothing. Like build and test, it needs the network only
	@# when the module cache is cold.
	$(GO) mod tidy -diff

# The harness artifacts this repo ships (skills/ agents/ rules/) plus the one it
# uses on itself (.claude/). testdata/ is deliberately excluded: its fixtures are
# malformed on purpose, so `ynd lint .` at the repo root can never be green.
ARTIFACT_DIRS := skills agents rules .claude

# NVIDIA SkillSpector — security scanner for agent skills — run through the
# eyelock/actions/skillspector gate image, the same gate CI runs. Pinned by
# digest: an upstream push must not change what gates our merges, which is the
# exact supply-chain risk the scanner exists to catch. The image records the
# SkillSpector release it carries (`docker run IMAGE --version`).
SKILLSPECTOR_IMAGE := ghcr.io/eyelock/skillspector:1.1.0@sha256:729992e6f4e82c269330affe6e3f189a1395ae15022e60a50fe70e4fd6d7b31c
# Skills are found wherever a SKILL.md is; these are scanned as one unit each.
# Keep in step with the Skill Security job in .github/workflows/ci.yml.
SKILLSPECTOR_ARGS := --include agents --include rules --include .claude --exclude 'testdata/*'

# govulncheck. Pinned so a toolchain-side change cannot silently alter what
# gates a merge, the same reasoning as the SkillSpector pin above.
GOVULNCHECK_VERSION := v1.7.0

vulncheck: ## Scan the Go code and its dependencies for known vulnerabilities
	@# Deliberately NOT part of `make check`, for the same reason as
	@# scan-artifacts but a different cause: govulncheck queries a live
	@# vulnerability database, so an unchanged tree can pass today and fail
	@# tomorrow. A non-deterministic step does not belong in the check people
	@# run before pushing. It gates every PR and runs nightly instead, in
	@# .github/workflows/vulncheck.yml.
	@#
	@# govulncheck reports against the toolchain that builds the code, so the
	@# `toolchain` directive in go.mod is what actually fixes a standard-library
	@# advisory; this target is what notices one. Symbol-level, so it reports
	@# only vulnerabilities the code genuinely reaches.
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

scan-artifacts: ## Security-scan the harness artifacts with SkillSpector (needs Docker)
	@scripts/scan-artifacts.sh $(SKILLSPECTOR_IMAGE) $(SKILLSPECTOR_ARGS)

check-marketplace: ## Assert the committed marketplace indexes match the plugin manifests
	@./scripts/marketplace-consistency.sh

check-vendor-parity: build ## Assert every vendor is documented, assembles the same artifacts and is stubbed in evals
	@./scripts/vendor-parity.sh

check-eval-stand-ins: ## Compare the evals' stand-in harnesses with the real ones on GitHub (needs the network)
	@# Not part of `make check`: it clones eyelock/assistants, so its result
	@# depends on the network and on a repository this one does not control.
	@# It runs daily and on changes to the stand-ins, in
	@# .github/workflows/eval-stand-ins.yml. On a failure, update the stand-in in
	@# scripts/eval-remotes.sh, never the real harness.
	@./scripts/eval-stand-ins.sh

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
	docker buildx build --load --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE):$(DOCKER_TAG) -t $(DOCKER_IMAGE):latest .

docker-push: ## Push base Docker image to GHCR
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

# Every check CI's CI workflow runs that needs neither Docker nor the network
# (beyond the module downloads build and test already need on a cold cache).
# A green `make check` is not a promise of a green CI. Left out on purpose:
#   scan-artifacts                  Skill Security job (Docker)
#   the container image build       Packaging Config job (Docker)
#   vulncheck                       vulncheck.yml (network: live vuln database)
#   check-eval-stand-ins            eval-stand-ins.yml (network: GitHub)
#   e2e                             e2e.yml (release gate)
# `format` rewrites files where CI's Format Check only reports them, so commit
# what it changes.
check: deps format lint check-tidy test build check-artifacts check-vendor-parity check-marketplace ## Run every CI check that needs neither Docker nor the network
