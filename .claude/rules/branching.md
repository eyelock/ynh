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

## Stacked PRs

A change too large to review in one PR is split into a stack: each branch is
based on the previous one, and each PR targets the branch below it rather than
`develop`. Only the bottom PR targets `develop`.

```
#208  feat/check-baseline  →  feat/ynh-check     (top)
#207  feat/ynh-check       →  develop            (bottom)
```

Each PR's diff then shows only its own work.

```bash
git checkout -b feat/thing-one develop
# … work, push, open PR into develop …

git checkout -b feat/thing-two feat/thing-one    # stack on top
# … work, push …
gh pr create --base feat/thing-one
```

### Rebase after each merge — this is the part that bites

Merges here are **squash** merges, so when the bottom PR lands, its commits
collapse into a single new commit on `develop` that the branch above has never
seen. GitHub retargets the upper PR automatically, but its diff will then
re-include the lower PR's changes and conflict.

Immediately after the lower PR merges, rebase the branch above onto `develop`:

```bash
git fetch origin develop
git rebase --onto origin/develop feat/thing-one feat/thing-two
git push --force-with-lease
```

That replays only the upper branch's own commits. Do it before touching the
upper PR, and repeat down the stack for each merge.

### CI runs on stacked PRs

`ci.yml` has no branch filter on `pull_request`, so a PR based on a feature
branch is gated exactly like one based on `develop`. This is deliberate: the
pre-remote gates require green CI, and a stacked PR that cannot run CI cannot
be gated. Do not add a branch filter back.

### Rules that still apply

- The bottom of the stack targets `develop`. Nothing in a stack targets `main`.
- Every PR in the stack passes the pre-remote gates on its own.
- Merge bottom-up, never out of order.
- Say in the PR body what it is stacked on, so a reviewer knows which diff is
  theirs to read.

## Post-merge cleanup

```bash
git branch -d <branch-name>
git push origin --delete <branch-name>
```
