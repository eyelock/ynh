# Gitflow: develop is the default branch and takes feature PRs; main moves only by release,
# hotfix or develop PRs and carries the release tags. Two repository rulesets protect them
# (docs: .github/BRANCH_PROTECTION.md). Classic branch protection is not used: with both, a merge
# must satisfy both lists of checks, and they drift. The branches themselves are git history,
# pushed from a clone, not created here.

resource "github_branch_default" "develop" {
  repository = github_repository.ynh.name
  branch     = "develop"
}

locals {
  # Status checks each protected branch requires before a PR can merge. "All Clear" is the ci
  # workflow's aggregator job; "Verify PR source branch" (protect-main.yml) runs only on PRs into
  # main, so it takes only develop, release/* and hotfix/*.
  protected_branches = {
    main = {
      name   = "Main Branch Protection"
      checks = ["All Clear", "Verify PR source branch"]
    }
    develop = {
      name   = "Develop Branch Protection"
      checks = ["All Clear"]
    }
  }
}

resource "github_repository_ruleset" "this" {
  for_each = local.protected_branches

  repository  = github_repository.ynh.name
  name        = each.value.name
  target      = "branch"
  enforcement = "active"

  conditions {
    ref_name {
      include = ["refs/heads/${each.key}"]
      exclude = []
    }
  }

  # Repository admins (role 5) can bypass in an emergency; use sparingly.
  bypass_actors {
    actor_id    = 5
    actor_type  = "RepositoryRole"
    bypass_mode = "always"
  }

  rules {
    deletion         = true
    non_fast_forward = true

    # A pull request is required, with no approving review: changes go through a PR and green CI,
    # and every review thread is resolved before merge.
    pull_request {
      required_approving_review_count   = 0
      dismiss_stale_reviews_on_push     = false
      require_code_owner_review         = false
      require_last_push_approval        = false
      required_review_thread_resolution = true
    }

    required_status_checks {
      strict_required_status_checks_policy = true
      do_not_enforce_on_create             = false

      dynamic "required_check" {
        for_each = each.value.checks
        content {
          context = required_check.value
        }
      }
    }
  }
}

# The classic branch protection on main and develop that the rulesets replace. It was set by hand
# and is in no state, and Terraform cannot import a resource and remove it in the same plan (an
# import block needs its target in the configuration, which a removed block takes out), so the
# same apply deletes it through the API once the rulesets exist: the new rulesets are always in
# force before the old protection goes. A branch with no protection is already as intended (404),
# so this is safe to run again and does nothing after the first apply; delete it then.
resource "terraform_data" "remove_classic_protection" {
  for_each = local.protected_branches

  triggers_replace = [github_repository_ruleset.this[each.key].ruleset_id]

  provisioner "local-exec" {
    command = <<-EOT
      if out=$(gh api -X DELETE "repos/$OWNER/$REPOSITORY/branches/$BRANCH/protection" 2>&1); then
        echo "removed classic protection from $BRANCH"
      elif [[ "$out" == *"HTTP 404"* ]]; then
        echo "$BRANCH has no classic protection"
      else
        echo "$out" >&2
        exit 1
      fi
    EOT

    interpreter = ["bash", "-c"]

    environment = {
      OWNER      = var.owner
      REPOSITORY = github_repository.ynh.name
      BRANCH     = each.key
    }
  }
}
