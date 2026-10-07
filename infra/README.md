# Infra

What ynh's own development needs outside the code: where Terraform keeps its state, and the
GitHub repository's settings.

| Path | What it is |
|---|---|
| [`terraform-state/`](terraform-state/README.md) | Terraform for the S3 bucket that holds the Terraform state for everything in this repository, and the IAM user that runs it. Applied once to bootstrap, then only to change the bucket or the user. |
| [`github/`](github/README.md) | Terraform for the `eyelock/ynh` repository: settings, rulesets, labels, Actions permissions and secrets, security. |
