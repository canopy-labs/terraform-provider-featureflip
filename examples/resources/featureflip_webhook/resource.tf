# Send flag changes in the web project's production environment to a receiver.
# Omit event_types, project_ids or environment_ids to receive everything.
resource "featureflip_webhook" "flag_changes" {
  name            = "flag-changes"
  url             = "https://hooks.example.com/featureflip"
  event_types     = ["flag.toggled", "flag.updated", "flag.environment_config.updated"]
  project_ids     = [featureflip_project.web.id]
  environment_ids = [featureflip_environment.production.id]
}

# The signing secret is only available at creation and lives in Terraform state.
output "webhook_signing_secret" {
  value     = featureflip_webhook.flag_changes.secret
  sensitive = true
}
