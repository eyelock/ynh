# Branching Model

This project uses Gitflow. All feature and fix work targets `develop`. Only `develop` and `hotfix/*` branches may PR into `main`.

## NEVER commit directly to `main` or `develop`

Every change goes through a branch and PR — no exceptions.

## Branch targets

| Work type | Branch from | PR into |
|-----------|-------------|---------|
| Feature / fix / docs / ci | `develop` | `develop` |
| Release promotion | `develop` | `main` |
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

CI enforces `hotfix/*` for hotfixes targeting `main`. There are no release branches — release is tag-driven.

## Workflow

```bash
# Start work
git checkout develop
git pull origin develop
git checkout -b feat-my-thing

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
