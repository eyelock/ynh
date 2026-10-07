terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  # This configuration's own state lives in the bucket it creates. The first apply runs on local
  # state and then moves it here (README.md, "Bootstrap").
  backend "s3" {
    bucket       = "ynh-terraform-state.eyelock.net"
    key          = "terraform-state/terraform.tfstate"
    region       = "us-east-1"
    use_lockfile = true
    encrypt      = true
  }
}

provider "aws" {
  region = "us-east-1"

  default_tags {
    tags = {
      Project   = "ynh"
      Owner     = "eyelock"
      ManagedBy = "terraform"
    }
  }
}
