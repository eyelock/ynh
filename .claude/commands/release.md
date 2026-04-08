Release a new version of ynh. This pushes a semver tag which triggers goreleaser via GitHub Actions to build binaries, Docker images, and update the Homebrew tap.

## Pre-flight (mandatory — do not skip any step)

### 1. Evals gate

Run `/evals` and confirm the verdict is `EVALS: PASS`. If evals have not been run in this conversation, or the last run produced `EVALS: FAIL`, **STOP immediately** and tell the user:

> Release blocked: evals have not passed in this conversation. Run /evals first.

Do not proceed with the release under any circumstances until evals pass.

### 2. CI check

Run `make check` to verify format, lint, test, and build all pass. If any step fails, stop and report.

### 3. Clean working tree

Verify ALL of the following:
- `git status` shows a clean working tree (no uncommitted changes)
- Current branch is `main`
- Local main is up to date with `origin/main` (`git pull origin main`)

If any condition fails, stop and tell the user what needs to be resolved.

## Version bump

1. Get the latest tag: `git tag --sort=-version:refname | head -1`
2. Ask the user: **MAJOR, MINOR, or PATCH?**
   - MAJOR: breaking changes (v1.2.3 → v2.0.0)
   - MINOR: new features, backward-compatible (v1.2.3 → v1.3.0)
   - PATCH: bug fixes only (v1.2.3 → v1.2.4)
3. Compute the new version from the latest tag
4. Confirm with the user before proceeding:
   > Release **v0.1.0**? This will trigger goreleaser to build and publish binaries, Docker images, and update the Homebrew tap. Proceed? [y/N]

Wait for explicit confirmation. Do not proceed on silence or ambiguity.

## Release

1. Create and push the tag:
   ```
   git tag v<new-version>
   git push origin v<new-version>
   ```
2. Monitor the release workflow:
   ```
   gh run watch --exit-status
   ```
3. Verify the release exists:
   ```
   gh release view v<new-version>
   ```

Report the release URL when complete.
