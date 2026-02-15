terraform {
  required_version = ">= 1.5"

  required_providers {
    github = {
      source  = "integrations/github"
      version = ">= 5.0"
    }
  }
}

locals {
  repository_name = "homebrew-${var.tap_name}"

  readme_content = <<-EOT
# ${local.repository_name}

Homebrew tap for ${var.github_owner}.

## Usage

```bash
brew tap ${var.github_owner}/${var.tap_name}
```

%{if length(var.formula_names) > 0~}
Then install available formulas:

```bash
%{for name in var.formula_names~}
brew install ${name}
%{endfor~}
```

%{endif~}
## About

This repository is a [Homebrew tap](https://docs.brew.sh/Taps).
Formula files in the `Formula/` directory are managed by the release process.
%{if var.homepage_url != ""~}

For more information, see [the project homepage](${var.homepage_url}).
%{endif~}
EOT
}

resource "github_repository" "tap" {
  name        = local.repository_name
  description = var.description
  visibility  = var.visibility

  homepage_url = var.homepage_url != "" ? var.homepage_url : null

  auto_init            = true
  has_issues           = true
  has_wiki             = false
  has_projects         = false
  vulnerability_alerts = true

  allow_merge_commit     = true
  allow_squash_merge     = true
  allow_rebase_merge     = true
  delete_branch_on_merge = true

  topics = var.topics
}

resource "github_repository_file" "readme" {
  repository          = github_repository.tap.name
  branch              = var.default_branch
  file                = "README.md"
  content             = local.readme_content
  commit_message      = "Initialize tap repository"
  overwrite_on_create = true
}

resource "github_repository_file" "formula_gitkeep" {
  repository          = github_repository.tap.name
  branch              = var.default_branch
  file                = "Formula/.gitkeep"
  content             = ""
  commit_message      = "Add Formula directory"
  overwrite_on_create = true

  depends_on = [github_repository_file.readme]
}
