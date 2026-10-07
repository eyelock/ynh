terraform {
  required_version = ">= 1.10"

  required_providers {
    github = {
      source  = "integrations/github"
      version = "~> 6.13"
    }
  }

  # State bucket from infra/terraform-state, locked with a .tflock object beside the state. Runs need
  # AWS credentials that can use it (the ynh-terraform profile).
  backend "s3" {
    bucket       = "ynh-terraform-state.eyelock.net"
    key          = "github/terraform.tfstate"
    region       = "us-east-1"
    use_lockfile = true
    encrypt      = true
  }
}

# The token comes from GITHUB_TOKEN (`export GITHUB_TOKEN="$(gh auth token)"`); it needs the
# repo and workflow scopes.
provider "github" {
  owner = var.owner
}
