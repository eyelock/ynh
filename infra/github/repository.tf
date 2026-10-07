resource "github_repository" "ynh" {
  name         = var.repository
  description  = "Harness template manager for AI coding agents. Compose skills, rules and context from any Git repo, then assemble one harness for Claude Code, Codex or Cursor."
  homepage_url = "https://eyelock.github.io/ynh"
  visibility   = var.visibility
  topics = [
    "agent-skills",
    "ai-agents",
    "claude-code",
    "codex",
    "cursor",
    "developer-tools",
    "golang",
    "harness-engineering",
  ]

  has_issues      = true
  has_discussions = true
  has_projects    = true
  has_wiki        = false
  is_template     = false

  # Gitflow (branches.tf): feature PRs are squash-merged into develop, release and hotfix PRs into
  # main are true merges so the back-merge is clean, and rebase is off so SHAs are never rewritten.
  allow_squash_merge          = true
  allow_merge_commit          = true
  allow_rebase_merge          = false
  allow_auto_merge            = false
  allow_update_branch         = false
  delete_branch_on_merge      = true
  squash_merge_commit_title   = "COMMIT_OR_PR_TITLE"
  squash_merge_commit_message = "COMMIT_MESSAGES"
  merge_commit_title          = "MERGE_MESSAGE"
  merge_commit_message        = "PR_TITLE"
  web_commit_signoff_required = false

  # Secret scanning and push protection only work on a public repository (security.tf).
  dynamic "security_and_analysis" {
    for_each = var.visibility == "public" ? [1] : []
    content {
      secret_scanning {
        status = "enabled"
      }
      secret_scanning_push_protection {
        status = "enabled"
      }
    }
  }

  # A destroy archives the repository instead of deleting it.
  archive_on_destroy = true

  lifecycle {
    prevent_destroy = true
  }
}

# The docs site, built by GitHub from /docs on main: it is the homepage above.
resource "github_repository_pages" "ynh" {
  repository = github_repository.ynh.name
  build_type = "legacy"

  source {
    branch = "main"
    path   = "/docs"
  }
}
