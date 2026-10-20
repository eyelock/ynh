# Adopt the live repository into state. On a fresh state these import; once imported they are
# no-ops. To set the repository up from nothing (a new owner or name), delete this file first.
# The rulesets have nothing to import: the first apply creates them, and removes the classic
# branch protection they replace (branches.tf).

import {
  to = github_repository.ynh
  id = "ynh"
}

import {
  to = github_repository_pages.ynh
  id = "ynh"
}

import {
  to = github_branch_default.develop
  id = "ynh"
}

import {
  to = github_issue_labels.ynh
  id = "ynh"
}

import {
  to = github_actions_repository_permissions.ynh
  id = "ynh"
}

import {
  to = github_workflow_repository_permissions.ynh
  id = "ynh"
}

import {
  to = github_repository_vulnerability_alerts.ynh
  id = "ynh"
}

import {
  to = github_repository_dependabot_security_updates.ynh
  id = "ynh"
}

import {
  to = github_repository_environment.github_pages
  id = "ynh:github-pages"
}

# The id is the branch policy's, from
# `gh api repos/eyelock/ynh/environments/github-pages/deployment-branch-policies`.
import {
  to = github_repository_environment_deployment_policy.github_pages
  id = "ynh:github-pages:43137793"
}

# Set by hand with gh, so no value enters Terraform state; these adopt them.
import {
  for_each = local.actions_secrets

  to = github_actions_secret.this[each.key]
  id = "ynh:${each.key}"
}

# Created by hand in every YN repository (2026-10-08); this adopts it without recreating it.
import {
  to = github_repository_ruleset.never_delete_main_or_develop
  id = "ynh:24699968"
}
