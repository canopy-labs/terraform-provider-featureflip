# NOTE: Featureflip auto-creates development/staging/production environments
# on every new project — import those (see import.sh) rather than declaring
# them, and use this resource for additional environments.
resource "featureflip_environment" "qa" {
  project    = featureflip_project.web.key
  key        = "qa"
  name       = "QA"
  color      = "#e53935"
  sort_order = 10
}
