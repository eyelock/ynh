# Authoritative: a label created in the UI and not listed here is removed on apply. GitHub's
# defaults plus the three Dependabot creates for its pull requests.
locals {
  labels = {
    "bug"              = { color = "d73a4a", description = "Something isn't working" }
    "dependencies"     = { color = "0366d6", description = "Pull requests that update a dependency file" }
    "documentation"    = { color = "0075ca", description = "Improvements or additions to documentation" }
    "duplicate"        = { color = "cfd3d7", description = "This issue or pull request already exists" }
    "enhancement"      = { color = "a2eeef", description = "New feature or request" }
    "github_actions"   = { color = "000000", description = "Pull requests that update GitHub Actions code" }
    "go"               = { color = "16e2e2", description = "Pull requests that update go code" }
    "good first issue" = { color = "7057ff", description = "Good for newcomers" }
    "help wanted"      = { color = "008672", description = "Extra attention is needed" }
    "invalid"          = { color = "e4e669", description = "This doesn't seem right" }
    "question"         = { color = "d876e3", description = "Further information is requested" }
    "wontfix"          = { color = "ffffff", description = "This will not be worked on" }
  }
}

resource "github_issue_labels" "ynh" {
  repository = github_repository.ynh.name

  dynamic "label" {
    for_each = local.labels
    content {
      name        = label.key
      color       = label.value.color
      description = label.value.description
    }
  }
}
