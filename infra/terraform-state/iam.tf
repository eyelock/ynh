# The identity day-to-day runs use (the ynh-terraform AWS profile): it can read and write state
# and its lock objects, and nothing else. Changing this configuration itself needs an admin profile.
# Its access key is made with the AWS CLI, not here, so the secret never enters state.

resource "aws_iam_group" "terraform" {
  name = "ynh-terraform"
  path = "/ynh/"
}

resource "aws_iam_policy" "state_access" {
  name        = "ynh-terraform-state-access"
  path        = "/ynh/"
  description = "Read and write ynh Terraform state and its lock objects"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ListStateBucket"
        Effect   = "Allow"
        Action   = ["s3:ListBucket"]
        Resource = [aws_s3_bucket.state.arn]
      },
      # State objects and their .tflock lock objects.
      {
        Sid      = "ReadWriteState"
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
        Resource = ["${aws_s3_bucket.state.arn}/*"]
      },
    ]
  })
}

resource "aws_iam_group_policy_attachment" "state_access" {
  group      = aws_iam_group.terraform.name
  policy_arn = aws_iam_policy.state_access.arn
}

resource "aws_iam_user" "terraform" {
  name = "ynh-terraform"
  path = "/ynh/"
}

resource "aws_iam_user_group_membership" "terraform" {
  user   = aws_iam_user.terraform.name
  groups = [aws_iam_group.terraform.name]
}
