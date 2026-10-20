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
      strict_required_status_checks_policy = false
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

# The first apply (October 2026) deleted the hand-set classic branch protection that the rulesets
# replace, through a one-off terraform_data step. It has done its job; this forgets it without
# running anything against GitHub.
removed {
  from = terraform_data.remove_classic_protection

  lifecycle {
    destroy = false
  }
}

# main and develop can never be deleted or force-pushed, by anyone: no bypass actors, so not even
# repository admins (who can bypass the two rulesets above). Created by hand and adopted through
# imports.tf.
resource "github_repository_ruleset" "never_delete_main_or_develop" {
  repository  = github_repository.ynh.name
  name        = "Never Delete Main or Develop"
  target      = "branch"
  enforcement = "active"

  conditions {
    ref_name {
      include = ["refs/heads/main", "refs/heads/develop"]
      exclude = []
    }
  }

  rules {
    deletion         = true
    non_fast_forward = true
  }
}
