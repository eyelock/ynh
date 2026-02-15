terraform {
  required_version = ">= 1.5"

  required_providers {
    github = {
      source  = "integrations/github"
      version = "~> 6.0"
    }
  }

  # Local state is appropriate for infrastructure bootstrapping.
  # For team use, uncomment a remote backend:
  #
  # backend "s3" {
  #   bucket = "your-terraform-state"
  #   key    = "ynh/homebrew_tap.tfstate"
  #   region = "us-east-1"
  # }
}

provider "github" {
  token = var.github_token
  owner = var.github_owner
}

module "homebrew_tap" {
  source = "../homebrew_tap"

  github_owner  = var.github_owner
  tap_name      = "tap"
  description   = "Homebrew tap for ynh (ynh) - persona manager for AI coding assistants"
  homepage_url  = "https://github.com/${var.github_owner}/ynh"
  formula_names = ["ynh"]
  topics        = ["homebrew", "homebrew_tap", "ynh", "ynh"]
}
