# ynh — Project Context

## What This Is

ynh is a persona manager for AI coding assistants (Claude Code, OpenAI Codex, Cursor). No runtime — resolves config, assembles vendor-specific layout, launches the vendor CLI.

## Two Binaries

- **`ynh`** (`cmd/ynh/`) — Persona manager: install, run, update, uninstall
- **`ynd`** (`cmd/ynd/`) — Developer tools: create, lint, validate, fmt, compress, inspect

Both built by `make build`, released together via goreleaser (single `v*` tag).

## Build Commands

**Always use Make targets, not raw `go`/`golangci-lint` commands.** The Makefile wraps them with correct flags (race detection, coverage, ldflags, version injection).

```bash
make check              # full CI: deps, format, lint, test, build
make build              # build both binaries to bin/
make test               # all tests with race detection and coverage
make test FILE=./cmd/ynd  # test specific package
make lint               # golangci-lint
make format             # goimports + gofmt
make test-coverage          # tests with coverage profile + per-function report
make test-coverage FILE=./cmd/ynd  # coverage for specific package
make clean              # remove build artifacts and caches
make clean && make build  # fresh build (use when binary seems stale)
```

**Do not use:** `go build ./...`, `go test ./...`, `golangci-lint run` directly. They miss flags, ldflags, or tool paths that the Makefile provides.

## Architecture & Code Patterns

Read `.github/CONTRIBUTING.md` — it has the full architecture, package structure, vendor adapter guide, testing patterns, and test matrix. Don't duplicate it here.

## ynd-Specific Patterns

- **LLM mocking**: `queryLLMFunc` in `cmd/ynd/llm.go` — replaceable function variable for tests
- **Prompt mocking**: `promptActionFunc` / `promptInputFunc` in `cmd/ynd/inspect.go` — replaceable for stdin-dependent tests
- **Backup system**: `~/.ynd/backups/` mirroring absolute paths. Override with `YND_BACKUP_DIR` in tests
- **Vendor-aware output**: `ynd inspect` writes to `.{vendor}/` by default, override with `-o`

## Artifact Format

Follows the [Agent Skills](https://agentskills.io) open standard. See `docs/skills-standard.md` for spec details and known issues (especially: don't use `compatibility`, `license`, or `metadata` frontmatter in persona skills — Claude Code's plugin loader demotes them).

## Key Files

| File | Purpose |
|------|---------|
| `.github/CONTRIBUTING.md` | Architecture, code patterns, vendor adapters, test matrix |
| `.goreleaser.yml` | Release config (both binaries, brew tap) |
| `.github/workflows/release.yml` | Tag-triggered release |
| `.claude/plans/ynd-manual-test-plan.md` | Manual test script for all ynd features |
| `docs/walkthrough.md` | End-to-end walkthrough (ynh + ynd) |
| `docs/skills-standard.md` | Agent Skills spec, cross-platform compat, known issues |
| `docs/ynd.md` | ynd command reference |

## Code Conventions

- Go 1.25+, standard library only (zero external dependencies)
- Errors returned, not panicked. Wrap: `fmt.Errorf("context: %w", err)`
- Standard `testing` package, `t.TempDir()`, `t.Setenv()` for isolation
- errcheck is strict — all returned errors must be checked
- Coverage target: 90%+ on testable code

## Environment Variables

| Variable | Default | Used by |
|----------|---------|---------|
| `YNH_HOME` | `~/.ynh` | ynh |
| `YND_BACKUP_DIR` | `~/.ynd/backups` | ynd compress |
