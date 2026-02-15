output "tap_repository" {
  description = "Full name of the tap repository"
  value       = module.homebrew_tap.repository_full_name
}

output "tap_url" {
  description = "URL of the tap repository"
  value       = module.homebrew_tap.html_url
}

output "tap_command" {
  description = "Command for users to add the tap"
  value       = module.homebrew_tap.tap_command
}

output "clone_url" {
  description = "Clone URL for the tap repository"
  value       = module.homebrew_tap.clone_url
}
