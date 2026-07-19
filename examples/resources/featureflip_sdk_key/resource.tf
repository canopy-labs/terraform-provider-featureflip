resource "featureflip_sdk_key" "backend" {
  project     = featureflip_project.web.key
  environment = "production"
  name        = "backend"
  type        = "ServerSide"
}

# The plaintext is only available at creation and lives in Terraform state.
output "sdk_key" {
  value     = featureflip_sdk_key.backend.key
  sensitive = true
}
