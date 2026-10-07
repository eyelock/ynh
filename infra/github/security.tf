# Secret scanning and push protection are in repository.tf: they work only on a public repository.
# Dependabot version updates come from .github/dependabot.yml and need none of these.
resource "github_repository_vulnerability_alerts" "ynh" {
  repository = github_repository.ynh.name
  enabled    = true
}

resource "github_repository_dependabot_security_updates" "ynh" {
  repository = github_repository.ynh.name
  enabled    = true

  # Security updates need the alerts.
  depends_on = [github_repository_vulnerability_alerts.ynh]
}

# Private vulnerability reporting, which SECURITY.md points reporters at. The GitHub provider has no
# resource for it, so this calls the API with gh; the PUT is idempotent. Terraform cannot see it
# turned off in the UI: run `terraform apply -replace='terraform_data.private_vulnerability_reporting[0]'`
# to set it again.
resource "terraform_data" "private_vulnerability_reporting" {
  # GitHub offers private vulnerability reporting only on public repositories.
  count = var.visibility == "public" ? 1 : 0

  triggers_replace = [github_repository.ynh.repo_id]

  provisioner "local-exec" {
    command = "gh api -X PUT repos/$OWNER/$REPOSITORY/private-vulnerability-reporting"

    environment = {
      OWNER      = var.owner
      REPOSITORY = github_repository.ynh.name
    }
  }
}
