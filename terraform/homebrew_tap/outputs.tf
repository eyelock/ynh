output "repository_name" {
  description = "Repository name (e.g., homebrew_tap)"
  value       = github_repository.tap.name
}

output "repository_full_name" {
  description = "Full repository name (e.g., eyelock/homebrew_tap)"
  value       = github_repository.tap.full_name
}

output "clone_url" {
  description = "HTTPS clone URL"
  value       = github_repository.tap.http_clone_url
}

output "html_url" {
  description = "Repository URL in browser"
  value       = github_repository.tap.html_url
}

output "tap_command" {
  description = "The brew tap command users should run"
  value       = "brew tap ${var.github_owner}/${var.tap_name}"
}
