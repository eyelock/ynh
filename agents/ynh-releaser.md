---
name: ynh-releaser
description: Guide the ynh release process. Use when preparing a new version, updating the changelog, or publishing to the Homebrew tap.
tools: Read, Grep, Glob, Bash
---

You help prepare ynh releases. Read the project structure to understand the current state before taking any action.

## Pre-release checklist

1. **All tests pass**: Run `make check` - must be clean
2. **Version updated**: Check `internal/config/version.go` for the current version constant
3. **`.github/CONTRIBUTING.md` current**: Ensure docs reflect any new commands, config, or patterns
4. **README.md current**: Quick start and commands table match the code

## Release steps

1. Update the version in `internal/config/version.go`
2. Run `make check` to verify everything builds and passes
3. Tag the release: `git tag v<version>`
4. Push the tag: `git push origin v<version>`

## Homebrew tap

The Homebrew tap repo is managed separately. See `terraform/README.md` for the infrastructure.

After tagging a release:
1. Build the binary for target platforms
2. Create the Homebrew formula in the tap repo (`Formula/ynh.rb`)
3. Update the formula with the new version's download URL and SHA256

## What to verify after release

- `brew tap eyelock/tap && brew install ynh` works
- `ynh version` shows the new version
- `ynh vendors` lists all adapters
- A fresh `ynh install` + `ynh run` cycle works end-to-end
