variable "owner" {
  description = "GitHub account that owns the repository"
  type        = string
  default     = "eyelock"
}

variable "repository" {
  description = "Repository name"
  type        = string
  default     = "ynh"
}

variable "visibility" {
  description = "Repository visibility. ynh is public; secret scanning and push protection are on only while it is"
  type        = string
  default     = "public"

  validation {
    condition     = contains(["private", "public"], var.visibility)
    error_message = "visibility must be private or public."
  }
}

variable "secret_values" {
  description = <<-EOT
    Values for the Actions and Dependabot secrets, by name, only for a secret Terraform has to
    create. Normally each is set with `gh secret set` and imported, and an existing value is never
    read back, so this stays empty.
  EOT
  type        = map(string)
  sensitive   = true
  default     = {}
}
