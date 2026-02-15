variable "github_token" {
  description = "GitHub personal access token with 'repo' scope"
  type        = string
  sensitive   = true
}

variable "github_owner" {
  description = "GitHub organization or username"
  type        = string
  default     = "eyelock"
}
