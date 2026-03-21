---
name: ynh-dev
description: Development workflow for the ynh codebase. Build, test, lint, and format in the right order.
---

# ynh Development Workflow

You are helping a developer work on the ynh codebase.

## References

Read these before starting work:

- `references/architecture.md` - Package structure, core flow, adapter interface, design decisions
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

After code changes, verify against the relevant tutorial in `docs/tutorial/`. The full test matrix is in `docs/tutorial/manual-test-plan.md` (85 tests across 8 tutorials + edge cases).

To find which tutorials cover a changed area:

| Package / area | Tutorial |
|---|---|
| `cmd/ynh` (install, run, list, uninstall) | Tutorial 1, 2 |
| `internal/resolver` (includes, Git sources) | Tutorial 3 |
| `internal/assembler` (delegates) | Tutorial 4 |
| `internal/exporter` | Tutorial 5 |
| `internal/marketplace` | Tutorial 6 |
| `internal/registry` | Tutorial 7 |
| `cmd/ynd` (create, lint, validate, fmt, compress, inspect) | Tutorial 8 |

Run the relevant tutorial steps end-to-end before committing. Build first with `make build` so the binaries reflect your changes, then walk through the tutorial steps. The tutorials use `/tmp/ynh-tutorial/` as a scratch directory.

## Common issues

- **Tool not found**: The Makefile uses full paths to GOPATH/bin for go-installed tools. Run `make deps` if tools are missing.
- **Lint errors**: `errcheck` is strict - all returned errors must be handled, even in tests
- **Test isolation**: Always use `t.TempDir()` and `t.Setenv("YNH_HOME", "")` to avoid leaking state
