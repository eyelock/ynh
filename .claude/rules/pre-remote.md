Before ANY remote operation (`git push`, `gh pr create`, `gh pr merge`), ALL of the following gates must pass. No exceptions. If blocked, stop and ask the user for help.

**PR target:** feature and fix branches always PR into `develop`, not `main`. Only `develop` and `hotfix/*` may target `main`.

## Gate 1: make check

Run `make check`. It must pass with zero issues. If it fails, fix the issue and re-run until green.

## Gate 2: Evals (behavioral changes only)

If the changeset touches ANY of these, run `/evals` and it must PASS:
- `cmd/ynh/` or `cmd/ynd/` (CLI behavior)
- `internal/` (core logic)
- `docs/tutorial/` (tutorial content that evals verify)

Skip evals only for pure documentation changes that don't touch tutorials (e.g., CONTRIBUTING.md, README.md, profiles.md).

## Gate 3: Manual spot-check (output-affecting changes)

If the changeset affects assembled output (`internal/assembler/`, `internal/harness/`, `internal/vendor/`, `internal/exporter/`), do a manual spot-check:
- Build fresh binaries: `make build`
- Create a test harness in `/tmp/` that exercises the changed behavior
- Run `ynd preview` or equivalent and verify the output looks correct
- Clean up the test harness

## Gate 4: CI after push

After pushing and creating a PR, run `gh pr checks <number> --watch` and wait for CI to pass before merging. If CI is still running, tell the user and wait. Never merge without green CI.

## What to do when blocked

If any gate fails and you cannot resolve it, stop and tell the user:
- Which gate failed
- What the error or unexpected output was
- What you've tried so far
