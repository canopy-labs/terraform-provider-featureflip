# A kill switch a Grafana alert can pull: POSTing to the URL turns the flag off
# in production. Calling it when the flag is already off changes nothing.
resource "featureflip_flag_trigger" "checkout_errors" {
  project     = featureflip_project.web.key
  flag        = featureflip_feature_flag.new_checkout.key
  environment = "production"
  name        = "Grafana: checkout error rate"
  action      = "Disable"

  # Change this value to rotate the URL in place, e.g. after it leaks.
  # The old URL stops working at once.
  rotation_keepers = {
    rotated = "2026-11-06"
  }
}

# The URL contains the secret and lives in Terraform state. Store it in the
# monitoring tool as a secret.
output "checkout_kill_switch_url" {
  value     = featureflip_flag_trigger.checkout_errors.url
  sensitive = true
}
