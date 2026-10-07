resource "github_actions_repository_permissions" "ynh" {
  repository      = github_repository.ynh.name
  enabled         = true
  allowed_actions = "all"
}

# Workflows get a read-only GITHUB_TOKEN unless a job asks for more, and cannot approve PRs.
resource "github_workflow_repository_permissions" "ynh" {
  repository                       = github_repository.ynh.name
  default_workflow_permissions     = "read"
  can_approve_pull_request_reviews = false
}

# The environment GitHub Pages deploys through: only main may deploy to it.
resource "github_repository_environment" "github_pages" {
  repository  = github_repository.ynh.name
  environment = "github-pages"

  deployment_branch_policy {
    protected_branches     = false
    custom_branch_policies = true
  }
}

resource "github_repository_environment_deployment_policy" "github_pages" {
  repository     = github_repository.ynh.name
  environment    = github_repository_environment.github_pages.environment
  branch_pattern = "main"
}

# Secrets are declared by name only. GitHub never returns a secret's value, so Terraform owns that
# each secret exists and writes a value only when it creates one; after that the value is ignored.
# Each is set with `gh secret set <NAME> -R eyelock/ynh` and adopted by the import in imports.tf, so no value
# passes through Terraform or its state. To rotate one, set it again with gh.
locals {
  actions_secrets = toset([
    "CLAUDE_CODE_OAUTH_TOKEN",
    "RELEASE_TOKEN",
  ])
}

data "github_actions_secrets" "ynh" {
  name = github_repository.ynh.name
}

resource "github_actions_secret" "this" {
  for_each = local.actions_secrets

  repository  = github_repository.ynh.name
  secret_name = each.key
  # The placeholder is never written: the precondition stops a create without a real value, and
  # ignore_changes stops an update.
  value = coalesce(lookup(var.secret_values, each.key, null), "unset")

  lifecycle {
    ignore_changes = [value]

    precondition {
      condition     = contains(keys(var.secret_values), each.key) || contains(data.github_actions_secrets.ynh.secrets[*].name, each.key)
      error_message = "${each.key} does not exist yet: set it with `gh secret set ${each.key} -R eyelock/ynh` (see README.md), then plan again."
    }
  }
}
