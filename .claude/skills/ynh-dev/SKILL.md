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

## Common issues

- **Tool not found**: The Makefile uses full paths to GOPATH/bin for go-installed tools. Run `make deps` if tools are missing.
- **Lint errors**: `errcheck` is strict - all returned errors must be handled, even in tests
- **Test isolation**: Always use `t.TempDir()` and `t.Setenv("YNH_HOME", "")` to avoid leaking state
