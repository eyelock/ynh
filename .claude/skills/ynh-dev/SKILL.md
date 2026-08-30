---
name: ynh-dev
description: Development workflow for the ynh codebase. Build, test, lint, and format in the right order.
---

# ynh Development Workflow

You are helping a developer work on the ynh codebase.

## References

Read these before starting work:

- `references/architecture.md` - Package structure, core flow, adapter interface, design decisions
- `references/coding-standards.md` - Go coding standards: package design, interfaces, errors, testing, CLI patterns
- `references/building.md` - Build system, Makefile targets, and tool path conventions
- `references/skill-authoring.md` - Required reading (https://agentskills.io/) before creating or modifying skills

## Quick checks

Run the full CI pipeline:

```bash
make check
```

This runs deps, format, lint, test, and build in sequence. Fix any issues before committing.

## Individual steps

If you need to run steps individually:

```bash
make deps      # install prerequisites (goimports, golangci-lint)
make format    # goimports + gofmt
make lint      # golangci-lint
make test      # go test with race detection and coverage
make build     # build binary to bin/ynh
```

Target a specific package:

```bash
make test FILE=./cmd/ynh
make test FILE=./internal/assembler
```

## Before committing

1. Run `make check` - all steps must pass
2. Check test coverage - new features should include tests
3. Review the test matrix in `references/architecture.md` if touching assembler/resolver logic

## Manual testing

After code changes, verify against the relevant tutorial in `docs/tutorial/`.
`docs/tutorial/manual-test-plan.md` adds edge cases the tutorials do not cover.

Tutorial filenames are descriptive, not numbered — do not cite them by number.

| Package / area | Tutorial |
|---|---|
| `cmd/ynh` (install, run, ls, uninstall) | `first-harness.md` |
| `internal/vendor`, `internal/symlink` | `vendors-and-symlinks.md` |
| `internal/resolver` (includes, Git sources) | `composition.md` |
| `internal/assembler` (delegates) | `delegation.md` |
| `internal/exporter` | `export.md` |
| `internal/marketplace` | `marketplace.md` |
| `internal/registry`, `internal/sources` | `registry-and-discovery.md` |
| `cmd/ynd` (create, lint, validate, fmt, compress) | `developer-tools.md` |
| `cmd/ynd` (preview, compose, diff) | `developer-preview.md` |
| Docker image build | `docker-image.md` |
| Hooks | `hooks.md` |
| MCP servers | `mcp-servers.md` |
| Profiles | `profiles.md` |
| Focus | `focus.md` |
| Project-local config | `project-local-config.md` |
| `internal/clischema`, `internal/jsonschema` | `structured-output.md` |
| `ynh include` editing | `include-editing.md` |
| `internal/namespace`, `internal/migration` | `namespacing-and-migration.md` |
| `internal/plugin` (sensor declarations) | `sensors.md` |
| `internal/gate`, `internal/baseline` | `check.md` |
| `internal/agent` | `agent-loop.md` |
| `internal/freshness`, shadow mode | `shadow-mode.md` |

Run the relevant tutorial steps end-to-end before committing. Build first with
`make build` so the binaries reflect your changes. The tutorials use `/tmp/` as
a scratch directory; the `evals` agent has the isolation rules.

## Common issues

- **Tool not found**: The Makefile uses full paths to GOPATH/bin for go-installed tools. Run `make deps` if tools are missing.
- **Lint errors**: `errcheck` is strict - all returned errors must be handled, even in tests
- **Test isolation**: Always use `t.TempDir()` and `t.Setenv("YNH_HOME", "")` to avoid leaking state
