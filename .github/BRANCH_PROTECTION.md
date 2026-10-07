# Branch Protection Configuration

`develop` and `main` are each protected by one repository ruleset. There is no classic branch
protection: the rulesets are the single source of truth, and they are managed as code in
`infra/github` (the rulesets are in [`branches.tf`](../infra/github/branches.tf)), so a change to them is a reviewed pull request followed by `terraform apply`,
never a click in the settings page.

| Ruleset | Branch | Purpose |
|---------|--------|---------|
| Develop Branch Protection | `develop` | Where feature and fix work lands (the default branch) |
| Main Branch Protection | `main` | Moves only by release: PRs from `develop`, `release/*` or `hotfix/*` |

## Required checks

Both rulesets require the status check **All Clear**. It is the last job of the CI workflow,
depends on every other job in `ci.yml` and fails if any of them failed or was cancelled. Add or
rename CI jobs freely; only All Clear is required, and a new job must be added to its `needs`.

`main` additionally requires **Verify PR source branch**, from `protect-main.yml`, which rejects
a PR into `main` unless its source is `develop`, `release/*` or `hotfix/*`.

## Rules (both rulesets)

- Changes reach the branch through a pull request, with zero required approvals
- All review conversations must be resolved
- The branch need not be up to date before merging (required status checks are not strict)
- Force pushes blocked
- Branch deletion blocked
- Repository admins can bypass in emergencies

Squash merges and merge commits are allowed, rebase merges are not. Merge commits are for
release and back-merge PRs; everything else is squashed.
