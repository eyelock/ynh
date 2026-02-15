variable "github_owner" {
  description = "GitHub organization or username that will own the tap repository"
  type        = string
}

variable "tap_name" {
  description = "Tap name (repository will be created as homebrew-<tap_name>)"
  type        = string
  default     = "tap"

  validation {
    condition     = length(var.tap_name) > 0
    error_message = "Tap name must not be empty."
  }

  validation {
    condition     = !startswith(var.tap_name, "homebrew-")
    error_message = "Tap name should not include the 'homebrew-' prefix; it is added automatically."
  }
}

variable "description" {
  description = "Repository description"
  type        = string
  default     = "Homebrew tap"
}

variable "homepage_url" {
  description = "Homepage URL for the repository"
  type        = string
  default     = ""
}

variable "topics" {
  description = "GitHub repository topics"
  type        = list(string)
  default     = ["homebrew", "homebrew_tap"]
}

variable "formula_names" {
  description = "Formula names to list in the generated README (informational only)"
  type        = list(string)
  default     = []
}

variable "default_branch" {
  description = "Default branch name"
  type        = string
  default     = "main"
}

variable "visibility" {
  description = "Repository visibility (must be 'public' for Homebrew taps)"
  type        = string
  default     = "public"

  validation {
    condition     = var.visibility == "public"
    error_message = "Homebrew taps must be public repositories."
  }
}
