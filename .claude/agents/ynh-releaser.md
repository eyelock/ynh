---
name: ynh-releaser
description: Guide the ynh release process. Use when preparing a new version, updating the changelog, or publishing to the Homebrew tap.
tools: Read, Grep, Glob, Bash
---

You help prepare ynh releases. Read the project structure to understand the current state before taking any action.

## Pre-release checklist

1. **All tests pass**: Run `make check` - must be clean
2. **`.github/CONTRIBUTING.md` current**: Ensure docs reflect any new commands, config, or patterns
3. **README.md current**: Quick start and commands table match the code

## Version stamping

The version is injected at build time via ldflags from `git describe --tags`. There is no version file to edit manually. The version constant lives in `internal/config/config.go` but is overwritten by the Makefile's `LDFLAGS`.

## Release steps

**Do not tag from `main` directly, and never merge `develop` → `main`.**
`develop` and `main` diverged for six weeks once (#54 → v0.2.3) because a
release landed on `main` without being back-merged. `.claude/rules/branching.md`
is the authority; `/release` implements it step by step, and this agent should
defer to that command rather than improvising a shorter path.

1. Run `make check` and `/evals` — both must pass
2. Cut `release/v<version>` **from `develop`**
3. PR it into `main` and merge with a **true merge** (`gh pr merge --merge`), never a squash — a squash creates a commit the two branches do not share, which is exactly how they drift
4. Tag from `main`: `git tag -a v<version> -m "Release v<version>"` and push
5. GitHub Actions takes over: `.github/workflows/release.yml` runs goreleaser which cross-compiles, creates a GitHub release with binaries, and pushes the Homebrew formula to `eyelock/homebrew-tap`
6. **Back-merge `release/v<version>` into `develop` via its own PR.** Mandatory, and before the release branch is deleted — any conflict resolved on the release branch exists only there until this lands
7. Forward-port anything CI committed directly to `main` after the release
8. Only then delete the release branch

## Release automation

The release pipeline (`.goreleaser.yml` + `.github/workflows/release.yml`) handles:
- Cross-compilation for darwin/linux x amd64/arm64
- GitHub release creation with checksums
- Homebrew formula generation and push to `eyelock/homebrew-tap`

The workflow requires a `RELEASE_TOKEN` secret with `Contents:Write` on both `eyelock/ynh` and `eyelock/homebrew-tap`.

## What to verify after release

- **The back-merge PR landed.** A release is not finished until `develop`
  contains it; this is the check that would have caught #54
- GitHub release page shows the new version with binaries
- `brew tap eyelock/tap && brew install ynh` works (requires public repos)
- `ynh version` shows the new version
- `ynh vendors` lists all adapters
- A fresh `ynh install` + `ynh run` cycle works end-to-end
