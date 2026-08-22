# Branching Model

This project uses Gitflow. All feature and fix work targets `develop`. Only `develop` and `hotfix/*` branches may PR into `main`.

## NEVER commit directly to `main` or `develop`

Every change goes through a branch and PR — no exceptions.

## Branch targets

| Work type | Branch from | PR into |
|-----------|-------------|---------|
| Feature / fix / docs / ci | `develop` | `develop` |
| Release promotion | `develop` | `release/vX.Y.Z` → `main` (never `develop` → `main` directly) |
| Hotfix | release tag | `main` (forward-port to `develop` after) |

## Branch naming

Use slashes for all branches:

```
feat/<description>
fix/<description>
docs/<description>
ci/<description>
refactor/<description>
test/<description>
hotfix/<description>
```

CI enforces `hotfix/*` for hotfixes targeting `main`. Release tags are cut from `main` after a `release/vX.Y.Z` branch is merged in — see below.

## Release branches and mandatory back-merge

`develop` and `main` diverged for ~6 weeks once (#54 → v0.2.3) because a release landed on `main` without back-merging to `develop`. To prevent recurrence:

- Every release cuts a `release/vX.Y.Z` branch from `develop`, PRs into `main`, and is merged with a **true merge** (not squash) — never `develop` → `main` directly.
- After tagging, the release branch (plus any conflict resolutions made on it) is **mandatorily back-merged into `develop`** via its own PR before the release branch is deleted.
- Any auto-generated files CI writes directly to `main` after a release must also be forward-ported to `develop` in the same or a follow-up PR.
- Hotfixes off `main` back-merge to `develop` the same way.

Full step-by-step procedure lives in the `release` skill (`references/stable.md`).

## Workflow

```bash
# Start work
git checkout develop
git pull origin develop
git checkout -b feat/my-thing

# Before opening a PR — sync with develop
git fetch origin develop
git merge origin/develop
git push

# Merge (squash only)
gh pr merge --squash
```

**Never use `gh pr merge --admin`** — it bypasses CI and is forbidden.

## Post-merge cleanup

```bash
git branch -d <branch-name>
git push origin --delete <branch-name>
```
