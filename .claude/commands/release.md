Cut a ynh release. A semver tag on `main` triggers goreleaser via GitHub Actions
to build binaries and Docker images and update the Homebrew tap.

**Read `.claude/rules/branching.md` § "Release branches and mandatory back-merge"
before starting.** It is the authority; this file is the step-by-step. If the two
ever disagree, the rule wins and this file is the bug.

The shape, and why: `develop` and `main` diverged for ~6 weeks once (#54 →
v0.2.3) because a release landed on `main` and was never back-merged. Every step
below that looks like ceremony exists to stop that recurring.

```
develop → release/vX.Y.Z → (true merge) → main → tag → back-merge → develop
```

## Pre-flight — mandatory, skip nothing

### 1. Evals gate

Run `/evals` and confirm `EVALS: PASS`. If evals have not run in this
conversation, or the last run failed, **stop** and say:

> Release blocked: evals have not passed in this conversation. Run /evals first.

Do not proceed under any circumstances until they pass.

### 2. Full check

`make check` — format, lint, test, build, and check-artifacts. If anything
fails, stop and report.

### 3. Start from a clean, current `develop`

```bash
git status                      # must be clean
git checkout develop
git fetch origin
git rev-list --count HEAD..origin/develop    # must be 0
```

**The release is cut from `develop`, not from `main`.** If you are on `main`,
you are about to do the thing that caused #54.

## Version

1. Latest tag: `git tag --sort=-version:refname | head -1`
2. Ask the user: **MAJOR, MINOR, or PATCH?**
   - MAJOR — breaking changes
   - MINOR — new features, backward compatible
   - PATCH — bug fixes only
   - If `config.CapabilitiesVersion` changed this cycle, that is a strong signal
     for at least MINOR. Check `git log` for a bump.
3. Confirm explicitly before proceeding:

   > Release **vX.Y.Z**? This tags `main` and triggers goreleaser to publish
   > binaries, Docker images, and the Homebrew formula. Proceed? [y/N]

Wait for a clear yes. Do not proceed on silence or ambiguity.

## Cut the release branch

```bash
git checkout -b release/vX.Y.Z develop
```

### Stamp the harness version

The harness manifests and marketplace indexes carry their own version, and it
is what a marketplace browser sees. It sat at `0.1.0` across eight files while
the product reached `v0.6.0`, because each was hand-maintained and nothing
compared them — a plugin that looked abandoned.

```bash
make stamp-version VERSION=X.Y.Z
git commit -am "chore: stamp harness manifests at vX.Y.Z"
```

`make check-marketplace` fails if the eight disagree, so a forgotten stamp is
caught by CI rather than by whoever installs the release. Run it on the release
branch, before the PR, so `main` and the tag carry the right number.

```bash
git push -u origin release/vX.Y.Z
gh pr create --base main --head release/vX.Y.Z \
  --title "release: vX.Y.Z" --body "<summary of what is in this release>"
```

Wait for green CI:

```bash
gh pr checks <pr> --watch
```

## Merge into `main` — true merge, never squash

```bash
gh pr merge <pr> --merge          # NOT --squash, NOT --rebase
```

A squash would collapse the release into a single commit with no shared history,
which is what makes the back-merge conflict later. **Never `--admin`** — it
bypasses CI and is forbidden by the rules.

Do **not** pass `--delete-branch`. The branch is still needed for the
back-merge, and deleting a base branch closes any PR stacked on it.

## Tag from `main`

```bash
git checkout main
git pull origin main
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
gh run watch --exit-status
gh release view vX.Y.Z
```

## Back-merge into `develop` — mandatory, before deleting anything

This is the step that was skipped in #54.

```bash
git checkout -b backmerge/vX.Y.Z release/vX.Y.Z
git push -u origin backmerge/vX.Y.Z
gh pr create --base develop --head backmerge/vX.Y.Z \
  --title "chore: back-merge release/vX.Y.Z into develop" \
  --body "Mandatory back-merge per .claude/rules/branching.md."
```

It must carry any conflict resolutions made on the release branch. Merge it once
CI is green.

Then check whether CI wrote anything directly to `main` after the release —
generated changelogs, formula updates, docs. Anything it wrote must be
forward-ported to `develop` in this PR or an immediate follow-up.

## Only now, clean up

```bash
git push origin --delete release/vX.Y.Z
git push origin --delete backmerge/vX.Y.Z
```

## Verify

- `gh release view vX.Y.Z` shows binaries and checksums
- `ynh version` reports the new version
- `git rev-list --count develop..main` is 0 — if it is not, the back-merge did
  not land and `develop` is already drifting

Report the release URL when done.
