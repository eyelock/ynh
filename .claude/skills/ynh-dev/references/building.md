# Building ynh

## Makefile is the single entry point

All build, test, lint, and format commands go through `make`. Never run raw `go build`, `goimports`, or `golangci-lint` directly - the Makefile resolves tool paths and flags correctly.

## Tool path resolution

Go-installed tools (like `goimports`) live in `$(go env GOPATH)/bin`, which may not be on the shell's PATH. The Makefile handles this with explicit full-path variables:

```makefile
GOBIN := $(shell go env GOPATH)/bin
GOIMPORTS := $(GOBIN)/goimports
```

Recipes reference `$(GOIMPORTS)` instead of bare `goimports`. This means `make format` works without any PATH exports.

## Available targets

| Command | What it does |
|---------|-------------|
| `make check` | Full CI pipeline: deps, format, lint, test, build, **check-artifacts** |
| `make deps` | Install prerequisites (goimports, golangci-lint) |
| `make build` | Build **both** binaries to `bin/ynh` and `bin/ynd` |
| `make install` | Build and copy both binaries to `~/.ynh/bin` |
| `make test` | Run all tests with race detection and coverage |
| `make test FILE=./cmd/ynh` | Run tests for a specific package (verbose) |
| `make test-coverage` | Tests with coverage profile + per-function report |
| `make format` | Run goimports + gofmt |
| `make lint` | Run golangci-lint |
| `make check-artifacts` | `ynd validate` + `ynd lint` over `skills/ agents/ rules/ .claude/` |
| `make check-vendor-parity` | Every vendor documented and assembling the same artifacts (needs jq) |
| `make scan-artifacts` | SkillSpector security scan (needs Python; not part of `make check`) |
| `make e2e` | E2E suite — release gate, not part of `make test` or `make check` |
| `make clean` | Remove build artifacts and caches |
| `make docs` | Serve docs locally (needs npx) |
| `make docker-build` / `docker-push` | Base Docker image |
| `make help` | List all targets |

`make help` is generated from the Makefile itself, so it cannot go stale —
prefer it over this table.

## Permissions

All `make` commands are pre-approved in `.claude/settings.json`. You should never need to ask for permission to run them.

## Version stamping

The version is injected at build time via ldflags. An exact tag is used only
when the tree is clean *and* sits on that tag; otherwise the build is stamped
with branch and short SHA, so a dev binary always says which commit it came
from:

```makefile
# simplified — see Makefile for the error-fallback shells
DEV_VERSION := dev-<branch with / as ->-<short sha>[-dirty]
VERSION := <exact tag if clean and on it> else $(DEV_VERSION)
LDFLAGS := -ldflags "-X github.com/eyelock/ynh/internal/config.Version=$(VERSION)"
```

So `ynh version` on a feature branch reads like
`dev-fix-dev-harness-accuracy-f344f20`, gaining a `-dirty` suffix when the tree
has uncommitted changes. That is deliberate: it makes "which binary am I
actually running" answerable during an eval or a bug report.

## Conventions

- **Always use `make check` before committing** - it runs the full pipeline in the correct order
- **Use `make test FILE=...`** to iterate on a specific package during development
- **Never bypass the Makefile** to run tools directly - paths may not resolve
