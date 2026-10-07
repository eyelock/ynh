# Terraform state backend

Where ynh's Terraform keeps its state: an S3 bucket in `us-east-1`, plus the IAM user that
day-to-day runs use. ynh has its own, as ynr, ynf and ynm each have theirs.

This is its own configuration with its own state, so a plan or apply elsewhere uses the bucket
but never touches it. It is applied once to bootstrap, and after that only to change the bucket
or the user.

Locking is S3's own (`use_lockfile`): a run writes `<key>.tflock` beside the state and removes it
when done, so Terraform 1.10 or later is needed.

| Resource | Name |
|---|---|
| State bucket (versioned, encrypted, no public access; old versions expire after 90 days) | `ynh-terraform-state.eyelock.net` |
| IAM group, policy and user: read and write state and its lock objects, nothing else | `ynh-terraform` |

Each configuration has its own key in the bucket:

| Configuration | Key |
|---|---|
| `infra/terraform-state` (this one) | `terraform-state/terraform.tfstate` |
| [`infra/github`](../github/README.md) | `github/terraform.tfstate` |

A run that dies can leave a lock behind; `terraform force-unlock <id>` (the id is in the error)
removes it.

A new configuration copies the `backend "s3"` block from `infra/github/versions.tf` with a key of
its own.

## Credentials

Day-to-day runs use the `ynh-terraform` profile. Its access key is made with the AWS CLI rather
than Terraform, so the secret never enters state:

```bash
aws iam create-access-key --user-name ynh-terraform    # with an admin profile
aws configure --profile ynh-terraform                  # paste the key; region us-east-1
```

Rotate it the same way: create a second key, reconfigure the profile, then
`aws iam delete-access-key --user-name ynh-terraform --access-key-id <old>`.

Changing this configuration (the bucket or the IAM resources) needs an admin profile: the
`ynh-terraform` user can use the state but not change the infrastructure around it.

```bash
cd infra/terraform-state
AWS_PROFILE=<admin> terraform init
AWS_PROFILE=<admin> terraform plan
```

## Bootstrap

This configuration stores its state in the bucket it creates, so the first apply runs on local
state and then moves it in. Once, with an admin profile:

```bash
cd infra/terraform-state
export AWS_PROFILE=<admin>
printf 'terraform {\n  backend "local" {}\n}\n' > backend_override.tf
terraform init
terraform apply
rm backend_override.tf
terraform init -migrate-state      # answer yes: copies the local state into the bucket
rm terraform.tfstate*
```

The bucket has `prevent_destroy`; removing it loses every configuration's state.
