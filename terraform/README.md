# Terraform Infrastructure

Terraform configuration for ynh's distribution infrastructure.

## What This Manages

This creates the **Homebrew tap repository** (`homebrew_tap`) on GitHub. When users run `brew tap <owner>/tap`, Homebrew clones this repository to find formula files.

This Terraform does **not** manage:

- The main ynh repository (it already exists)
- Formula file contents (managed by the release pipeline on each release)
- CI/CD pipelines (managed in the main repo)

Terraform handles the one-time infrastructure setup. The release process handles ongoing content.

## Prerequisites

- [Terraform](https://developer.hashicorp.com/terraform/install) >= 1.5
- A GitHub personal access token with `repo` scope
  - Generate at https://github.com/settings/tokens
  - The token needs permission to create repositories in your org

## Quick Start

```bash
cd terraform/eyelock_tap

# Create your variable file from the example
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your GitHub token and org

# Initialize Terraform (downloads the GitHub provider)
terraform init

# Preview what will be created
terraform plan

# Create the repository
terraform apply
```

After applying, Terraform outputs the `brew tap` command that users need.

## Structure

```
terraform/
  homebrew_tap/          Reusable module: creates a Homebrew tap repository
    main.tf              Repository and file resources
    variables.tf         Module inputs (tap name, description, etc.)
    outputs.tf           Module outputs (URLs, tap command)
  eyelock_tap/           Root module: eyelock-specific configuration
    main.tf              Provider setup and module call
    variables.tf         GitHub token and org
    outputs.tf           Outputs passed through from module
    terraform.tfvars.example
```

## Using This for Your Own Org

If you've forked ynh and want your own Homebrew tap, the `homebrew_tap` module is reusable.

### Option 1: Reference the Module Directly

Create your own root module:

```hcl
module "homebrew_tap" {
  source = "github.com/eyelock/ynh//terraform/homebrew_tap"

  github_owner  = "your-org"
  tap_name      = "tap"
  description   = "Homebrew tap for my-tool"
  homepage_url  = "https://github.com/your-org/your-project"
  formula_names = ["my-tool"]
}
```

### Option 2: Copy and Customize

Copy `terraform/eyelock_tap/` and update the module call with your org's values.

### Module Inputs

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `github_owner` | string | required | GitHub org or username |
| `tap_name` | string | `"tap"` | Tap name (repo becomes `homebrew-<tap_name>`) |
| `description` | string | `"Homebrew tap"` | Repository description |
| `homepage_url` | string | `""` | Project homepage URL |
| `topics` | list(string) | `["homebrew", "homebrew_tap"]` | Repo topics |
| `formula_names` | list(string) | `[]` | Formula names for the README |
| `default_branch` | string | `"main"` | Default branch |
| `visibility` | string | `"public"` | Must be "public" (Homebrew requirement) |

### Module Outputs

| Output | Description |
|--------|-------------|
| `repository_name` | Repo name (e.g., `homebrew_tap`) |
| `repository_full_name` | Full name (e.g., `eyelock/homebrew_tap`) |
| `clone_url` | HTTPS clone URL |
| `html_url` | Browser URL |
| `tap_command` | The `brew tap` command for users |

## State Management

This configuration uses **local state** by default, which is appropriate for one-time infrastructure bootstrapping. The state file (`terraform.tfstate`) is git-ignored because it may contain sensitive data.

For team environments, configure a remote backend in `eyelock_tap/main.tf`. See the commented examples for S3 or Terraform Cloud.

## After Applying

Once the tap repository exists, the release pipeline takes over:

1. Tag a release in the main ynh repository
2. goreleaser (or your release tool) builds binaries and generates a Homebrew formula
3. The formula is pushed to the tap repository's `Formula/` directory
4. Users run `brew tap eyelock/tap && brew install ynh`

Terraform's job is done after the initial `apply`. You only need to run it again if you want to change repository settings (description, topics, etc.).
