Release a new version of ynh. This pushes a semver tag which triggers goreleaser via GitHub Actions to build binaries, Docker images, and update the Homebrew tap.

## Pre-flight (mandatory — do not skip any step)

### 1. Evals gate

Run `/evals` and confirm the verdict is `EVALS: PASS`. If evals have not been run in this conversation, or the last run produced `EVALS: FAIL`, **STOP immediately** and tell the user:

> Release blocked: evals have not passed in this conversation. Run /evals first.

Do not proceed with the release under any circumstances until evals pass.

### 2. CI check

Run `make check` to verify format, lint, test, and build all pass. If any step fails, stop and report.

### 3. Clean working tree, on `develop`

Verify ALL of the following:
- `git status` shows a clean working tree (no uncommitted changes)
- Current branch is **`develop`** — a release is cut *from* develop, not from main
- Local develop is up to date with `origin/develop` (`git pull origin develop`)

If any condition fails, stop and tell the user what needs to be resolved.

**Never tag from `main` directly, and never merge `develop` → `main`.**
`develop` and `main` diverged for six weeks once (#54 → v0.2.3) because a
release landed on `main` without being back-merged. `.claude/rules/branching.md`
is the authority here; this command implements it.

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

### 1. Cut the release branch from `develop`

```bash
git checkout develop && git pull origin develop
git checkout -b release/v<new-version>
git push -u origin release/v<new-version>
```

### 2. PR into `main`, and merge it as a TRUE merge

```bash
gh pr create --base main --head release/v<new-version> \
  --title "release: v<new-version>"
gh pr checks <number> --watch          # green before merging
gh pr merge <number> --merge           # --merge, NOT --squash
```

**`--merge`, never `--squash`.** A squash creates a new commit `main` and
`develop` do not share, which is what makes the two branches drift apart. Do
not pass `--delete-branch` — the branch is still needed for step 4.

### 3. Tag from `main`

```bash
git checkout main && git pull origin main
git tag v<new-version>
git push origin v<new-version>
gh run watch --exit-status
gh release view v<new-version>
```

### 4. Back-merge into `develop` — mandatory, before deleting anything

This is the step whose absence caused #54. Any conflict resolved on the release
branch exists only there until this lands.

```bash
gh pr create --base develop --head release/v<new-version> \
  --title "chore: back-merge release/v<new-version> into develop"
gh pr merge <number> --merge
```

### 5. Forward-port anything CI wrote to `main`

The release workflow may commit generated files directly to `main`. Check and
carry them to `develop` in the same PR or an immediate follow-up:

```bash
git log origin/main --oneline -5      # anything not from the release merge?
```

### 6. Only now, delete the release branch

```bash
git push origin --delete release/v<new-version>
```

Report the release URL when complete, and confirm explicitly that the
back-merge PR landed. A release is not finished until `develop` contains it.
