output "bucket" {
  description = "State bucket"
  value       = aws_s3_bucket.state.bucket
}

output "user" {
  description = "IAM user for day-to-day runs; make its access key with the AWS CLI"
  value       = aws_iam_user.terraform.name
}
