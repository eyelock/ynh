# GitHub repository

Terraform for the `eyelock/ynh` repository's settings, so the repository can be checked for drift
and set up again from nothing.

| File | What it manages |
|---|---|
| `repository.tf` | The repository: description, topics, homepage, visibility, features, merge options, secret scanning and push protection (public only), and the GitHub Pages site |
| `branches.tf` | Gitflow: `develop` as the default branch, and a ruleset on each of `main` and `develop`: no deletion or force-push, a pull request with conversations resolved, "All Clear" green and up to date (plus "Verify PR source branch" into `main`), repository admins may bypass. Also removes the classic protection the rulesets replace |
| `labels.tf` | Issue and PR labels, authoritatively: a label not listed is removed |
| `actions.tf` | Actions permissions, the read-only default `GITHUB_TOKEN`, the `github-pages` environment, and that the Actions and Dependabot secrets exist (names only) |
| `security.tf` | Dependabot alerts and security updates, and private vulnerability reporting |
| `imports.tf` | Import blocks that adopt the live repository into a fresh state |

Not managed here: anything committed to the repository (`.github/`).

## Settings

Per the public-ready standard: discussions, projects and issues on, wiki off, branches deleted on
merge, auto-merge off, no sign-off requirement. Squash merge (title from the commit or PR title,
message from the commits) and merge commits (for release and back-merges) are allowed; rebase merge
is off. Secret scanning and push protection are on while `visibility` is `public`, which they need.

## The rulesets

`develop` and `main` are protected by two repository rulesets, "Develop Branch Protection" and
"Main Branch Protection", not by classic branch protection: with both, a merge must satisfy two
lists of required checks, and they drift. Both require the one check **All Clear** (the final job
in `ci.yml`), strictly; `main` also requires **Verify PR source branch**, so it takes only
`develop`, `release/*` and `hotfix/*`. Repository admins (role 5) can bypass.

### Replacing the classic protection

`main` and `develop` still carry classic protection that was set by hand and is in no state. The
first `terraform apply` creates the rulesets and then deletes that protection in the same run
(`terraform_data.remove_classic_protection`, which calls `gh api -X DELETE` once the rulesets
exist). It is done that way because Terraform cannot import a resource and remove it in one plan:
an `import` block needs its target in the configuration, and a `removed` block takes it out
(`Configuration for import target does not exist`). The plan lists it as two resources to add; it
does not show the deletion, which the apply log prints (`removed classic protection from main`).
A branch with no classic protection is treated as done, so a repeat run changes nothing. Once
applied, delete the `terraform_data.remove_classic_protection` resource from `branches.tf`.

## Private vulnerability reporting

The GitHub provider has no resource for it, so `terraform_data.private_vulnerability_reporting`
calls the API with `gh`, only while `visibility` is `public`, the only repositories GitHub offers it on. Terraform cannot see it turned off in the UI; to set it again run
`terraform apply -replace='terraform_data.private_vulnerability_reporting[0]'`.

## Secrets

Declared by name only; GitHub never returns a value, so Terraform tracks that each exists and never
holds a value. They are set with gh and adopted by the imports in `imports.tf`:

| Secret | Kind | Used for |
|---|---|---|
| `RELEASE_TOKEN` | Actions | Publishing the release and pushing the formula to `eyelock/homebrew-tap`: a fine-grained token limited to those repositories with **Contents: Read and write** |
| `CLAUDE_CODE_OAUTH_TOKEN` | Actions | The Claude Code workflow |
| `YNR_READ_PACKAGES` | Actions | Reading the private `ynr` module |
| `YNR_READ_REPO` | Actions | Reading the private `ynr` repository |
| `YNR_READ_REPO` | Dependabot | The same, for Dependabot's own runs |

```bash
gh secret set RELEASE_TOKEN -R eyelock/ynh
gh secret set YNR_READ_REPO --app dependabot -R eyelock/ynh
```

The last two go once `ynr` is public. Remove them from `local.actions_secrets` and
`local.dependabot_secrets` in `actions.tf` and apply; that deletes the secrets from GitHub.

## Use

Needs Terraform 1.10 or later, the `gh` CLI (logged in), a GitHub token with the `repo` and
`workflow` scopes, and the `ynh-terraform` AWS profile. State is in S3 at
`s3://ynh-terraform-state.eyelock.net/github/terraform.tfstate`, locked with a `.tflock` object
beside it; the bucket comes from [`../terraform-state`](../terraform-state/README.md). It holds no
secrets, and if it is ever lost the import blocks rebuild it from the live repository.

```bash
cd infra/github
export AWS_PROFILE=ynh-terraform
export GITHUB_TOKEN="$(gh auth token)"
terraform init
terraform plan     # no changes means the repository matches this configuration
terraform apply
```

To change a setting, edit the `.tf` file, `plan`, then `apply`. A change made in the GitHub UI
shows as drift in the next `plan`; either copy it into the configuration or `apply` to undo it.

The repository has `prevent_destroy` and `archive_on_destroy`, so `terraform destroy` stops, and
removing that guard archives the repository rather than deleting it.

## Set up from nothing

For a new owner or name, set `owner` and `repository`, then:

1. Delete `imports.tf`: there is nothing to import.
2. Create only the repository: `terraform apply -target=github_repository.ynh`.
3. Push `main` and `develop` from a clone. The rulesets and the default branch need them to exist.
4. Set the secrets (above), then apply the rest: `terraform apply`.
