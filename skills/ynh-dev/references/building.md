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
| `make check` | Full CI pipeline: install, format, lint, test, build |
| `make install` | Install prerequisites (goimports, golangci-lint) |
| `make build` | Build binary to `bin/ynh` |
| `make test` | Run all tests with race detection and coverage |
| `make test FILE=./cmd/ynh` | Run tests for a specific package (verbose) |
| `make format` | Run goimports + gofmt |
| `make lint` | Run golangci-lint |
| `make clean` | Remove build artifacts and caches |
| `make bin` | Build and copy binary to `~/.ynh/bin` |
| `make help` | List all targets |

## Permissions

All `make` commands are pre-approved in `.claude/settings.json`. You should never need to ask for permission to run them.

## Version stamping

The binary version is injected at build time via ldflags from `git describe`:

```makefile
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/eyelock/ynh/internal/config.Version=$(VERSION)"
```

## Conventions

- **Always use `make check` before committing** - it runs the full pipeline in the correct order
- **Use `make test FILE=...`** to iterate on a specific package during development
- **Never bypass the Makefile** to run tools directly - paths may not resolve
