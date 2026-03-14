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

1. Run `make check` to verify everything builds and passes
2. Tag the release: `git tag -a v<version> -m "Release v<version>"`
3. Push the tag: `git push origin v<version>`
4. GitHub Actions takes over: `.github/workflows/release.yml` runs goreleaser which cross-compiles, creates a GitHub release with binaries, and pushes the Homebrew formula to `eyelock/homebrew-tap`

## Release automation

The release pipeline (`.goreleaser.yml` + `.github/workflows/release.yml`) handles:
- Cross-compilation for darwin/linux x amd64/arm64
- GitHub release creation with checksums
- Homebrew formula generation and push to `eyelock/homebrew-tap`

The workflow requires a `RELEASE_TOKEN` secret with `Contents:Write` on both `eyelock/ynh` and `eyelock/homebrew-tap`.

## What to verify after release

- GitHub release page shows the new version with binaries
- `brew tap eyelock/tap && brew install ynh` works (requires public repos)
- `ynh version` shows the new version
- `ynh vendors` lists all adapters
- A fresh `ynh install` + `ynh run` cycle works end-to-end
